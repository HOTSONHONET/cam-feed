package hub

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

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

func (h *Hub) StartServer(ctx context.Context, addr string) error {
	mux := http.NewServeMux()

	// Adding endpoints
	mux.HandleFunc("/healthcheck", h.HealthCheck)
	mux.HandleFunc("/", h.HealthCheck)
	mux.HandleFunc("/ingest", h.HandleIngest)
	mux.HandleFunc("/view", h.HandleView)
	mux.HandleFunc("/manifest", h.HandleManifest)

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()

	log.Println("Hub listening on", addr)
	return server.ListenAndServe()
}
