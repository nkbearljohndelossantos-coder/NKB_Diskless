package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512

	// Maximum simultaneous WebSocket client connections.
	maxWSConnections = 50
)

// validateWebSocketOrigin enforces strict same-origin WebSocket access to prevent Cross-Origin WebSocket Hijacking (CSWSH).
func validateWebSocketOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Non-browser clients (native desktop manager, curl, Go tests) do not send Origin header
		return true
	}

	u, err := url.Parse(origin)
	if err != nil {
		return false
	}

	// Host must match the request Host (standard same-origin policy)
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}

	// Allow local loopback development origins (localhost, 127.0.0.1, [::1]) matching server port
	uHost := u.Hostname()
	rHost := r.URL.Hostname()
	if rHost == "" {
		rHost = strings.Split(r.Host, ":")[0]
	}
	isULocal := uHost == "localhost" || uHost == "127.0.0.1" || uHost == "::1"
	isRLocal := rHost == "localhost" || rHost == "127.0.0.1" || rHost == "::1"

	if isULocal && isRLocal {
		uPort := u.Port()
		rPort := r.URL.Port()
		if rPort == "" && strings.Contains(r.Host, ":") {
			parts := strings.Split(r.Host, ":")
			rPort = parts[len(parts)-1]
		}
		if uPort == rPort || uPort == "5173" || uPort == "3000" { // Allow standard local dev frontend ports
			return true
		}
	}

	slog.Warn("WebSocket origin rejected by safety policy", "origin", origin, "host", r.Host)
	return false
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     validateWebSocketOrigin,
}

// WSEvent represents a typed real-time notification sent to connected dashboards.
type WSEvent struct {
	Type      string    `json:"type"` // workstation.registered, workstation.updated, workstation.offline, telemetry.updated, command.updated
	Payload   any       `json:"payload"`
	Timestamp time.Time `json:"timestamp"`
}

// WSClient represents a single connected admin dashboard WebSocket.
type WSClient struct {
	hub  *WSHub
	conn *websocket.Conn
	send chan []byte
}

// WSHub maintains the set of active clients and broadcasts events.
type WSHub struct {
	clients    map[*WSClient]bool
	broadcast  chan []byte
	register   chan *WSClient
	unregister chan *WSClient
	mu         sync.RWMutex
	stopChan   chan struct{}
}

func NewWSHub() *WSHub {
	return &WSHub{
		clients:    make(map[*WSClient]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *WSClient),
		unregister: make(chan *WSClient),
		stopChan:   make(chan struct{}),
	}
}

func (h *WSHub) Run() {
	for {
		select {
		case <-h.stopChan:
			h.mu.Lock()
			for client := range h.clients {
				close(client.send)
				delete(h.clients, client)
			}
			h.mu.Unlock()
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			slog.Debug("WebSocket client connected", "active_clients", h.ClientCount())

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				slog.Debug("WebSocket client disconnected", "active_clients", len(h.clients))
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					// Slow client buffer full; disconnect safely
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Stop cleanly terminates the hub.
func (h *WSHub) Stop() {
	select {
	case <-h.stopChan:
	default:
		close(h.stopChan)
	}
}

// Broadcast serializes and dispatches an event to all connected dashboard sessions.
func (h *WSHub) Broadcast(eventType string, payload any) {
	evt := WSEvent{
		Type:      eventType,
		Payload:   payload,
		Timestamp: time.Now(),
	}
	bytes, err := json.Marshal(evt)
	if err != nil {
		slog.Error("Failed to serialize WebSocket event", "type", eventType, "err", err)
		return
	}

	select {
	case h.broadcast <- bytes:
	default:
		slog.Warn("WebSocket broadcast channel saturated; dropping event", "type", eventType)
	}
}

// ClientCount returns the number of active connected clients.
func (h *WSHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (c *WSClient) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (c *WSClient) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			_, _ = w.Write(message)

			// Add queued messages to the current websocket message
			n := len(c.send)
			for i := 0; i < n; i++ {
				_, _ = w.Write([]byte{'\n'})
				_, _ = w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// HandleWebSocket upgrades HTTP to WS with strict authentication and origin verification.
func (s *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 1. HARD REJECTION: Query-string token authentication is strictly prohibited
	if r.URL.Query().Get("token") != "" {
		http.Error(w, `{"error":"query string token authentication is prohibited for security; use authenticated session cookies or Authorization header"}`, http.StatusBadRequest)
		return
	}

	// 2. Strict Origin verification
	if !validateWebSocketOrigin(r) {
		http.Error(w, `{"error":"forbidden: cross-origin websocket connection rejected"}`, http.StatusForbidden)
		return
	}

	// 3. Connection limit check
	if s.wsHub.ClientCount() >= maxWSConnections {
		http.Error(w, `{"error":"service unavailable: maximum concurrent websocket connections reached"}`, http.StatusServiceUnavailable)
		return
	}

	// 4. Extract authentication token from secure cookie or Authorization header
	var tokenStr string
	if cookie, err := r.Cookie("nkb_session"); err == nil && cookie.Value != "" {
		tokenStr = cookie.Value
	} else {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}

	if tokenStr == "" {
		http.Error(w, `{"error":"authentication required for live websocket feed; session cookie or Authorization header missing"}`, http.StatusUnauthorized)
		return
	}

	// 5. Validate JWT token
	claims, err := s.auth.ValidateToken(tokenStr)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"unauthorized: %s"}`, err.Error()), http.StatusUnauthorized)
		return
	}

	// 6. Verify user exists and is active in database
	user, err := s.db.GetUserByID(claims.UserID)
	if err != nil || user == nil || !user.IsActive {
		http.Error(w, `{"error":"user account inactive, revoked, or not found"}`, http.StatusUnauthorized)
		return
	}

	// 7. Upgrade connection
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("WebSocket upgrade failed", "err", err, "remote", r.RemoteAddr)
		return
	}

	client := &WSClient{
		hub:  s.wsHub,
		conn: conn,
		send: make(chan []byte, 256),
	}
	s.wsHub.register <- client

	go client.writePump()
	go client.readPump()
}
