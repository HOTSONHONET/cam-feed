package detector

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

type ortRuntime struct {
	mu         sync.Mutex
	inited     bool
	device     Device
	refCount   int
	cpuLibPath string
	gpuLibPath string
	gpuLibDir  string // folder container CUDA provider libs (*.so)
}

var rt = &ortRuntime{}

func (r *ortRuntime) Configure(cpuLibPath, gpuLibPath, gpuLibDir string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cpuLibPath = cpuLibPath
	r.gpuLibPath = gpuLibPath
	r.gpuLibDir = gpuLibDir
}

func (r *ortRuntime) acquire(device Device) (activeDevice Device, warn error, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	initWith := func(dev Device, libPath string) error {
		ort.SetSharedLibraryPath(libPath)

		if dev == DeviceGPU && r.gpuLibDir != "" {
			abs, _ := filepath.Abs(r.gpuLibDir)
			old := os.Getenv("LD_LIBRARY_PATH")

			if old == "" {
				_ = os.Setenv("LD_LIBRARY_PATH", abs)
			} else {
				_ = os.Setenv("LD_LIBRARY_PATH", abs+":"+old)
			}
		}

		if err := ort.InitializeEnvironment(); err != nil {
			return fmt.Errorf("ort: failed to initialize environment: %w", err)
		}

		r.device = dev
		r.inited = true
		return nil
	}

	// If already inited on same device, bumping refcount
	if r.inited && r.device == device {
		r.refCount++
		return r.device, nil, nil
	}

	// If already inited on a different device, only switch if no active detectors.
	if r.inited && r.device != device {
		if r.refCount > 0 {
			return "", nil, errors.New("detector: cannot switch ORT device while detectors are active (close them first)")
		}
		ort.DestroyEnvironment()
		r.inited = false
	}

	// Fresh initialization
	if device == DeviceGPU {
		if r.gpuLibPath == "" {
			// No GPU library configured -> fallback to CPU
			if err := initWith(DeviceCPU, r.cpuLibPath); err != nil {
				return "", nil, err
			}
		}

		if err := initWith(DeviceGPU, r.gpuLibPath); err == nil {
			r.refCount++
			return DeviceGPU, nil, nil
		}

		// GPU init failed -> fallback to CPU
		gpuErr := errors.New("GPU init failed; falling back to CPU: " + err.Error())
		if err := initWith(DeviceCPU, r.cpuLibPath); err != nil {
			return "", nil, err
		}

		r.refCount++
		return DeviceGPU, gpuErr, nil
	}

	// CPU requested
	if err := initWith(DeviceCPU, r.cpuLibPath); err != nil {
		return "", nil, err
	}

	r.refCount++
	return DeviceCPU, nil, nil
}

func (r *ortRuntime) release() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.refCount > 0 {
		r.refCount--
	}

	if r.refCount == 0 && r.inited {
		ort.DestroyEnvironment()
		r.inited = false
	}
}
