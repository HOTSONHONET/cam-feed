package hub

import (
	"context"
	"embed"
	"encoding/binary"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

//go:embed static/camera.html
var staticFS embed.FS

type StreamMeta struct {
	DeviceID string `json:"device_id"`
	Room     string `json:"room"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	FPS      int    `json:"fps"`
	LastSeen int64  `json:"last_seen,omitempty"`
}

type Hub struct {
	upgrader websocket.Upgrader

	mu      sync.RWMutex
	viewers map[string]map[*websocket.Conn]bool // room -> viewers
	ingest  map[string]*websocket.Conn          // deviceID -> conn
	metas   map[string]StreamMeta               // deviceID -> meta
}

func New() *Hub {
	return &Hub{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1 << 10,
			WriteBufferSize: 1 << 10,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		viewers: map[string]map[*websocket.Conn]bool{},
		ingest:  map[string]*websocket.Conn{},
		metas:   map[string]StreamMeta{},
	}
}

func (h *Hub) HealthCheck(w http.ResponseWriter, _ *http.Request) {
	w.Write([]byte("I am alive"))
}

// Endpont to recieved frames from cameras
// and send to viewers and also notify them
func (h *Hub) HandleIngest(w http.ResponseWriter, r *http.Request) {
	c, err := h.upgrader.Upgrade(w, r, nil)

	if err != nil {
		log.Printf("failed to establish connection in handleIngest")
		return
	}
	defer c.Close()

	// Recving the first message which is a JSON
	_, msg, err := c.ReadMessage()
	if err != nil {
		return
	}

	var meta StreamMeta
	if err := json.Unmarshal(msg, &meta); err != nil || meta.DeviceID == "" {
		log.Println("bad meta: ", err)
		return
	}

	if meta.Room == "" {
		meta.Room = "home"
	}

	// Updating the hub state
	h.mu.Lock()
	h.ingest[meta.DeviceID] = c
	meta.LastSeen = time.Now().UnixMilli()
	h.metas[meta.DeviceID] = meta
	if h.viewers[meta.Room] != nil {
		// Notifying viewers that a stream has joined
		event, _ := json.Marshal(
			map[string]any{
				"type":   "join",
				"stream": meta,
			},
		)
		for v := range h.viewers[meta.Room] {
			_ = v.WriteMessage(websocket.TextMessage, event)
		}
	}
	h.mu.Unlock()

	// watching for frame recieves
	for {
		mt, frame, err := c.ReadMessage()
		if err != nil {
			break
		}

		if mt != websocket.BinaryMessage {
			continue
		}

		// Sending new Frame to viewers
		id := meta.DeviceID
		h.mu.RLock()

		for v := range h.viewers[meta.Room] {
			_ = v.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))

			// sending device-id prefixed frame for multiplexing frames to viewers
			header := make([]byte, 2+len(id)) // first 2 bytes for len(device id), rest n or len(id) space for id string
			binary.BigEndian.PutUint16(header[:2], uint16(len(id)))
			copy(header[2:], id)

			_ = v.WriteMessage(websocket.BinaryMessage, append(header, frame...))
		}

		h.mu.RUnlock()
	}

	// Cleaning up disconnects
	h.mu.Lock()
	delete(h.ingest, meta.DeviceID)
	delete(h.metas, meta.DeviceID)
	if h.viewers[meta.Room] != nil {
		event, _ := json.Marshal(map[string]any{"type": "leave", "device_id": meta.DeviceID})
		for v := range h.viewers[meta.Room] {
			_ = v.WriteMessage(websocket.TextMessage, event)
		}
	}
	h.mu.Unlock()
}

func (h *Hub) HandleView(w http.ResponseWriter, r *http.Request) {
	room := r.URL.Query().Get("room")
	if strings.TrimSpace(room) == "" {
		room = "home"
	}

	c, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	defer c.Close()

	// registering viewer for the room
	h.mu.Lock()
	if h.viewers[room] == nil {
		h.viewers[room] = map[*websocket.Conn]bool{}
	}

	h.viewers[room][c] = true

	// Sending manifest
	var list []StreamMeta

	for _, m := range h.metas {
		if m.Room == room {
			list = append(list, m)
		}
	}
	manifest, _ := json.Marshal(map[string]any{"type": "manifest", "streams": list})
	_ = c.WriteMessage(websocket.TextMessage, manifest)

	h.mu.Unlock()

	// Creating a heartbeat service
	c.SetReadLimit(1 << 10)
	c.SetReadDeadline(time.Now().Add(60 * time.Second))

	c.SetPongHandler(func(string) error {
		// On receiving a pong from the client, reset the ReadDeadline for the connection
		c.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()

		for range t.C {
			_ = c.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
		}
	}()

	for {
		if _, _, err := c.ReadMessage(); err != nil {
			break
		}
	}

	// Unregister the viewer
	h.mu.Lock()
	delete(h.viewers[room], c)
	h.mu.Unlock()

}

// Endpoint to list down all the metadata from all the camera
func (h *Hub) HandleManifest(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()

	defer h.mu.RUnlock()

	var list []StreamMeta
	for _, m := range h.metas {
		list = append(list, m)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(
		map[string]any{
			"type":   "manifest",
			"stream": list,
		},
	)
}

func (h *Hub) StartServers(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthcheck", h.HealthCheck)
	mux.HandleFunc("/", h.HealthCheck)
	mux.HandleFunc("/ingest", h.HandleIngest) // used by phones (WSS on :6699)
	mux.HandleFunc("/view", h.HandleView)     // used by Wails viewer (WS on :6698)
	mux.HandleFunc("/manifest", h.HandleManifest)
	mux.HandleFunc("/camera", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, staticFS, "static/camera.html")
	})

	https := &http.Server{Addr: "0.0.0.0:6699", Handler: mux}

	go func() {
		<-ctx.Done()
		_ = https.Shutdown(context.Background())
	}()

	return https.ListenAndServeTLS("cert.pem", "key.pem")
}
