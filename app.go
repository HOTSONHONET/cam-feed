package main

import (
	"cam-feed/internal/hub"
	"context"
	"fmt"
	"log"
	"net"
)

// App struct
type App struct {
	ctx context.Context
	h   *hub.Hub
}

type HubConfig struct {
	Host   string
	UseTLS bool
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{h: hub.New()}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go func() {
		if err := a.h.StartServers(ctx); err != nil {
			log.Printf("server closed: %v\n", err)
		}
	}()
}

func getLocalIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}

	for _, iface := range ifaces {
		if iface.Flags*net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
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
