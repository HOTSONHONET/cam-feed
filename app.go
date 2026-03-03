package main

import (
	"cam-feed/internal/detector"
	"cam-feed/internal/hub"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"runtime"
)

// App struct
type App struct {
	ctx context.Context
	det detector.Detector
	h   *hub.Hub

	// keep last device selection
	device detector.Device

	// last warning given
	lastWarn string
}

type HubConfig struct {
	Host   string
	UseTLS bool
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{h: hub.New(), device: detector.DeviceCPU}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// 1) Start your servers
	go func() {
		if err := a.h.StartServers(ctx); err != nil {
			log.Printf("server closed: %v\n", err)
		}
	}()

	// 2) Configure ONNX Runtime shared libraries (CPU/GPU)
	// You must set correct paths for your packaged app.
	cpuLib, gpuLib, gpuLibDir, err := a.resolveOrtPaths()
	if err != nil {
		log.Printf("[detector] ORT path resolve failed: %v", err)
		return
	}
	detector.ConfigureOrtRuntime(cpuLib, gpuLib, gpuLibDir) // you’ll add this helper (see below)

	// 3) Create the detector (default CPU). If you want auto-GPU when available,
	// set a.device = detector.DeviceGPU here, then let fallback happen.
	if err := a.rebuildDetector(a.device); err != nil {
		log.Printf("[detector] init failed: %v", err)
	}
}

func getLocalIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip == nil || ip.IsLoopback() {
				continue
			}

			ip = ip.To4()
			if ip == nil {
				continue
			}

			return ip.String()
		}
	}

	return "127.0.0.1"
}

func (a *App) GetHubConfig() HubConfig {
	ip := getLocalIP()
	return HubConfig{
		Host:   fmt.Sprintf("%s:6699", ip),
		UseTLS: true,
	}
}

// Method for frontend to show all the QR/addresses
func (a *App) GetLanIngestURLs(token string, room string, wsScheme string) []string {
	if room == "" {
		room = "home"
	}

	var out []string
	for _, ip := range hub.LocalIPs() {
		ws_url := fmt.Sprintf(
			"%v://%v:6699/ingest?token=%v&room=%v",
			wsScheme, ip, token, room,
		)
		log.Printf("----------> new ws_url: %v\n", ws_url)
		out = append(out, ws_url)
	}

	return out
}

// --- Detector control (called by frontend) ---

func (a *App) resolveOrtPaths() (cpuLib, gpuLib, gpuLibDir string, err error) {
	// For now assume you're running from repo root in dev mode.
	// For packaged mode, you'll adjust to use the app's resources path.
	ver := "1.24.2"
	base := filepath.Join("third_party", "onnxruntime", ver)

	switch runtime.GOOS {
	case "linux":
		cpuLib = filepath.Join(base, "onnxruntime_cpu.so")
		gpuLib = filepath.Join(base, "onnxruntime_gpu.so")
		gpuLibDir = filepath.Join(base, "gpu_libs")
		return cpuLib, gpuLib, gpuLibDir, nil
	default:
		return "", "", "", fmt.Errorf("resolveOrtPaths: GOOS=%s not wired yet", runtime.GOOS)
	}
}

// SetInferenceDevice lets user pick "cpu" or "gpu". It rebuilds the detector.
// Returns the active device (could be "cpu" even if requested "gpu" due to fallback).
func (a *App) SetInferenceDevice(device string) (string, error) {
	var d detector.Device
	switch device {
	case string(detector.DeviceCPU):
		d = detector.DeviceCPU
	case string(detector.DeviceGPU):
		d = detector.DeviceGPU
	default:
		return "", errors.New("device must be 'cpu' or 'gpu'")
	}

	if err := a.rebuildDetector(d); err != nil {
		return "", err
	}
	return string(a.device), nil
}

// DetectImage runs inference on image bytes from the frontend.
func (a *App) DetectImage(imageBytes []byte) ([]detector.Detection, error) {
	if a.det == nil {
		return nil, errors.New("detector not initialized")
	}
	return a.det.Detect(imageBytes)
}

// GetDetectorInfo is optional: useful for UI to show status.
func (a *App) GetDetectorInfo() map[string]string {
	return map[string]string{
		"device":  string(a.device),
		"warning": a.lastWarn,
	}
}

// rebuildDetector closes old detector and creates a new one with requested device.
// If GPU fails, your runtime manager will fallback to CPU; we reflect that via info.
func (a *App) rebuildDetector(req detector.Device) error {
	// close existing
	if a.det != nil {
		_ = a.det.Close()
		a.det = nil
	}

	cfg := detector.DefaultConfig()
	cfg.Device = req

	d, activeDev, warn, err := detector.NewWithInfo(cfg) // you’ll add this helper (see below)
	if err != nil {
		return err
	}

	a.det = d
	a.device = activeDev
	if warn != nil {
		a.lastWarn = warn.Error()
	} else {
		a.lastWarn = ""
	}
	return nil
}
