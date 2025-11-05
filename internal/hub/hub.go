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

// one write mutex per websocket.Conn to prevent concurrent writes
var wsWriteMu sync.Map // Map[*websocket.Conn] -> *sync.Mutex{}

func connMu(c *websocket.Conn) *sync.Mutex {
	m, _ := wsWriteMu.LoadOrStore(c, &sync.Mutex{})
	return m.(*sync.Mutex)
}

func safeWriteJSON(c *websocket.Conn, v any) error {
	m := connMu(c)
	m.Lock()
	defer m.Unlock()

	c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.WriteJSON(v)
}

func safeWriteMessage(c *websocket.Conn, msgType int, data []byte) error {
	m := connMu(c)
	m.Lock()
	defer m.Unlock()

	c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.WriteMessage(msgType, data)
}

func safeWriteControl(c *websocket.Conn, msgType int, data []byte, deadline time.Time) error {
	m := connMu(c)
	m.Lock()
	defer m.Unlock()
	return c.WriteControl(msgType, data, deadline)
}

func forgetConn(c *websocket.Conn) {
	wsWriteMu.Delete(c)
	_ = c.Close()
}

func New() *Hub {
	return &Hub{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  MaxReadBufferSize,
			WriteBufferSize: MaxWriteBufferSize,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
			EnableCompression: false,
			HandshakeTimeout:  10 * time.Second,
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
	defer forgetConn(c)

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
		meta.Room = DefaultRoom
	}

	// Updating the hub state
	h.mu.Lock()

	// close old connection if device reconnects
	old := h.ingest[meta.DeviceID]
	if old != nil && old != c {
		delete(h.ingest, meta.DeviceID)
	}

	h.ingest[meta.DeviceID] = c
	meta.LastSeen = time.Now().UnixMilli()
	h.metas[meta.DeviceID] = meta

	// Collecting all viewers of the room
	var roomViewers []*websocket.Conn
	if vset := h.viewers[meta.Room]; vset != nil {
		for v := range vset {
			roomViewers = append(roomViewers, v)
		}
	}
	h.mu.Unlock()

	// Closing old connection after releasing the lock
	if old != nil && old != c {
		forgetConn(old)
	}

	// Notifying all the viewers about the new screen
	if len(roomViewers) > 0 {
		event := map[string]any{"type": "join", "stream": meta}
		for _, viewerConn := range roomViewers {
			_ = safeWriteJSON(viewerConn, event)
		}
	}

	// Reading frames and forwarding to viewers
	deviceID := meta.DeviceID
	for {

		c.SetReadLimit(int64(MaxReadBufferSizeForFrames))
		_ = c.SetReadDeadline(time.Now().Add(MaxTimeLimitForPong * time.Second))
		c.SetPongHandler(func(string) error {
			return c.SetReadDeadline(time.Now().Add(MaxTimeLimitForPong * time.Second))
		})
		go func() {
			t := time.NewTicker(MaxTimeLimitForPing * time.Second)
			defer t.Stop()
			for range t.C {
				// Some mobile stacks reply to ping; others don't — harmless either way.
				_ = safeWriteControl(c, websocket.PingMessage, nil, time.Now().Add(5*time.Second))
			}
		}()

		mt, frame, err := c.ReadMessage()
		if err != nil {
			break
		}

		if mt != websocket.BinaryMessage {
			continue
		}

		// Sending new Frames to viewers
		header := make([]byte, 2+len(deviceID))
		binary.BigEndian.PutUint16(header[:2], uint16(len(deviceID)))
		copy(header[2:], []byte(deviceID))

		payload := append(header, frame...)

		// Collecting websocket conn of viewers
		h.mu.RLock()
		var viewers []*websocket.Conn
		for v := range h.viewers[meta.Room] {
			viewers = append(viewers, v)
		}
		h.mu.RUnlock()

		// Pumping frames to the viewers
		var failConns []*websocket.Conn
		for _, v := range viewers {
			if err := safeWriteMessage(v, websocket.BinaryMessage, payload); err != nil {
				failConns = append(failConns, v)
			}
		}

		// Removing dead fail connections
		if len(failConns) > 0 {
			h.mu.Lock()
			for _, v := range failConns {
				delete(h.viewers[meta.Room], v)
				forgetConn(v)
			}
			h.mu.Unlock()
		}
	}

	// Cleaning up disconnects
	h.mu.Lock()
	delete(h.ingest, meta.DeviceID)
	delete(h.metas, meta.DeviceID)

	// Collecting viewers
	var viewers []*websocket.Conn
	if vset := h.viewers[meta.Room]; vset != nil {
		for v := range vset {
			viewers = append(viewers, v)
		}
	}

	h.mu.Unlock()

	// Notifiying existing viewers about deviceID is disconnected
	if len(viewers) > 0 {
		event := map[string]any{"type": "leave", "device_id": meta.DeviceID}
		for _, v := range viewers {
			_ = safeWriteJSON(v, event)
		}
	}
}

func (h *Hub) HandleView(w http.ResponseWriter, r *http.Request) {
	room := r.URL.Query().Get("room")
	if strings.TrimSpace(room) == "" {
		room = DefaultRoom
	}

	c, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	defer func() {
		h.mu.Lock()
		if h.viewers[room] != nil {
			delete(h.viewers[room], c)
		}
		h.mu.Unlock()
		forgetConn(c)
	}()

	// register viewer
	h.mu.Lock()
	if h.viewers[room] == nil {
		h.viewers[room] = map[*websocket.Conn]bool{}
	}
	h.viewers[room][c] = true

	// Sending manifest: list of all devices connected
	var deviceList []StreamMeta
	for _, m := range h.metas {
		if m.Room == room {
			deviceList = append(deviceList, m)
		}
	}
	h.mu.Unlock()

	// send manifest to viewer
	_ = safeWriteJSON(c, map[string]any{"type": "manifest", "streams": deviceList})

	// Creating a heartbeat service
	c.SetReadLimit(int64(MaxReadBufferSize))
	c.SetReadDeadline(time.Now().Add(MaxTimeLimitForPong * time.Second))

	c.SetPongHandler(func(string) error {
		// On receiving a ping from the client, increasing the read deadline to next limit
		// 60, 120, 180, ...
		return c.SetReadDeadline(time.Now().Add(MaxTimeLimitForPong * time.Second))
	})

	// Setting a ticket to capture ping
	go func() {
		t := time.NewTicker(MaxTimeLimitForPing * time.Second)
		defer t.Stop()

		for range t.C {
			_ = safeWriteControl(c, websocket.PingMessage, nil, time.Now().Add(5*time.Second))
		}
	}()

	// Prevent reading messages from viewers if they disconnects
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			break
		}
	}
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
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "manifest", "stream": list})
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
