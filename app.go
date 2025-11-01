package main

import (
	"cam-feed/internal/hub"
	"context"
	"fmt"
	"log"
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

func (a *App) GetHubConfig() HubConfig {
	return HubConfig{
		Host:   "192.168.0.103:6699",
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
