package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// SSEConfig configures Server-Sent Events
type SSEConfig struct {
	HeartbeatInterval time.Duration `json:"heartbeatInterval"`
	MaxConnections    int           `json:"maxConnections"`
	BufferSize        int           `json:"bufferSize"`
	EnableCORS        bool          `json:"enableCORS"`
}

// DefaultSSEConfig returns default SSE configuration
func DefaultSSEConfig() SSEConfig {
	return SSEConfig{
		HeartbeatInterval: 30 * time.Second,
		MaxConnections:    100,
		BufferSize:        1000,
		EnableCORS:        true,
	}
}

// SSEEvent represents a Server-Sent Event
type SSEEvent struct {
	ID    string      `json:"id,omitempty"`
	Event string      `json:"event,omitempty"`
	Data  interface{} `json:"data"`
	Retry int         `json:"retry,omitempty"` // milliseconds
}

// SSEConnection represents a single SSE connection
type SSEConnection struct {
	id           string
	writer       http.ResponseWriter
	flusher      http.Flusher
	ctx          context.Context
	cancel       context.CancelFunc
	eventChan    chan SSEEvent
	config       SSEConfig
	logger       *log.Logger
	lastActivity time.Time
	mu           sync.RWMutex
	isActive     bool
}

// SSEManager manages Server-Sent Event connections
type SSEManager struct {
	config      SSEConfig
	server      *Server
	logger      *log.Logger
	connections map[string]*SSEConnection
	mu          sync.RWMutex
}

// NewSSEManager creates a new SSE manager
func NewSSEManager(server *Server, config SSEConfig) *SSEManager {
	if config.HeartbeatInterval == 0 {
		config = DefaultSSEConfig()
	}

	return &SSEManager{
		config:      config,
		server:      server,
		logger:      server.logger,
		connections: make(map[string]*SSEConnection),
	}
}

// HandleSSE handles SSE requests
func (sm *SSEManager) HandleSSE(w http.ResponseWriter, r *http.Request) {
	// Check if we've reached max connections
	sm.mu.RLock()
	connCount := len(sm.connections)
	sm.mu.RUnlock()

	if connCount >= sm.config.MaxConnections {
		http.Error(w, "Too many connections", http.StatusTooManyRequests)
		return
	}

	// Check authentication if required
	if sm.server.webTransport != nil && sm.server.webTransport.config.AuthToken != "" {
		token := r.URL.Query().Get("token")
		if token == "" {
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" && len(authHeader) > 7 && authHeader[:7] == "Bearer " {
				token = authHeader[7:]
			}
		}

		if token != sm.server.webTransport.config.AuthToken {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Check if ResponseWriter supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Server-Sent Events not supported", http.StatusInternalServerError)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Cache-Control")

	if sm.config.EnableCORS {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	}

	// Create connection
	conn := sm.createConnection(w, flusher, r)

	// Register connection
	sm.mu.Lock()
	sm.connections[conn.id] = conn
	sm.mu.Unlock()

	sm.logger.Printf("SSE connection established: %s", conn.id)

	// Send initial connection event
	conn.sendEvent(SSEEvent{
		ID:    "connect",
		Event: "connection",
		Data: map[string]interface{}{
			"connected":   true,
			"connectionId": conn.id,
			"timestamp":   time.Now().Unix(),
		},
	})

	// Handle connection
	conn.handle()

	// Cleanup
	sm.mu.Lock()
	delete(sm.connections, conn.id)
	sm.mu.Unlock()

	sm.logger.Printf("SSE connection closed: %s", conn.id)
}

// createConnection creates a new SSE connection
func (sm *SSEManager) createConnection(w http.ResponseWriter, flusher http.Flusher, r *http.Request) *SSEConnection {
	ctx, cancel := context.WithCancel(r.Context())
	connID := fmt.Sprintf("sse_%d", time.Now().UnixNano())

	return &SSEConnection{
		id:           connID,
		writer:       w,
		flusher:      flusher,
		ctx:          ctx,
		cancel:       cancel,
		eventChan:    make(chan SSEEvent, sm.config.BufferSize),
		config:       sm.config,
		logger:       sm.logger,
		lastActivity: time.Now(),
		isActive:     true,
	}
}

// handle manages the SSE connection lifecycle
func (sc *SSEConnection) handle() {
	defer sc.cleanup()

	// Start heartbeat goroutine
	go sc.heartbeat()

	// Event sending loop
	for {
		select {
		case <-sc.ctx.Done():
			return
		case event := <-sc.eventChan:
			if !sc.writeEvent(event) {
				return
			}
		}
	}
}

// sendEvent sends an event to the connection
func (sc *SSEConnection) sendEvent(event SSEEvent) {
	select {
	case sc.eventChan <- event:
		sc.mu.Lock()
		sc.lastActivity = time.Now()
		sc.mu.Unlock()
	case <-sc.ctx.Done():
	default:
		sc.logger.Printf("SSE event channel full for connection %s, dropping event", sc.id)
	}
}

// writeEvent writes an event to the HTTP response
func (sc *SSEConnection) writeEvent(event SSEEvent) bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if !sc.isActive {
		return false
	}

	// Write event ID
	if event.ID != "" {
		if _, err := fmt.Fprintf(sc.writer, "id: %s\n", event.ID); err != nil {
			sc.logger.Printf("SSE write error (id): %v", err)
			return false
		}
	}

	// Write event type
	if event.Event != "" {
		if _, err := fmt.Fprintf(sc.writer, "event: %s\n", event.Event); err != nil {
			sc.logger.Printf("SSE write error (event): %v", err)
			return false
		}
	}

	// Write retry interval
	if event.Retry > 0 {
		if _, err := fmt.Fprintf(sc.writer, "retry: %d\n", event.Retry); err != nil {
			sc.logger.Printf("SSE write error (retry): %v", err)
			return false
		}
	}

	// Write data
	dataBytes, err := json.Marshal(event.Data)
	if err != nil {
		sc.logger.Printf("SSE JSON marshal error: %v", err)
		return false
	}

	if _, err := fmt.Fprintf(sc.writer, "data: %s\n\n", string(dataBytes)); err != nil {
		sc.logger.Printf("SSE write error (data): %v", err)
		return false
	}

	// Flush the response
	sc.flusher.Flush()
	return true
}

// heartbeat sends periodic heartbeat events
func (sc *SSEConnection) heartbeat() {
	ticker := time.NewTicker(sc.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sc.ctx.Done():
			return
		case <-ticker.C:
			sc.sendEvent(SSEEvent{
				Event: "heartbeat",
				Data: map[string]interface{}{
					"timestamp": time.Now().Unix(),
				},
			})
		}
	}
}

// cleanup cleans up the connection
func (sc *SSEConnection) cleanup() {
	sc.mu.Lock()
	sc.isActive = false
	sc.mu.Unlock()

	sc.cancel()
}

// BroadcastEvent broadcasts an event to all SSE connections
func (sm *SSEManager) BroadcastEvent(event SSEEvent) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for _, conn := range sm.connections {
		conn.sendEvent(event)
	}
}

// SendEventToConnection sends an event to a specific connection
func (sm *SSEManager) SendEventToConnection(connectionID string, event SSEEvent) bool {
	sm.mu.RLock()
	conn, exists := sm.connections[connectionID]
	sm.mu.RUnlock()

	if !exists {
		return false
	}

	conn.sendEvent(event)
	return true
}

// GetConnections returns active SSE connections
func (sm *SSEManager) GetConnections() map[string]*SSEConnection {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	connections := make(map[string]*SSEConnection)
	for id, conn := range sm.connections {
		connections[id] = conn
	}
	return connections
}

// GetConnectionCount returns the number of active connections
func (sm *SSEManager) GetConnectionCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.connections)
}

// Server extensions for SSE

// EnableSSE enables Server-Sent Events support
func (s *Server) EnableSSE(config SSEConfig) *Server {
	s.sseManager = NewSSEManager(s, config)

	// Add SSE endpoint to web transport if available
	if s.webTransport != nil {
		s.webTransport.mux.HandleFunc("/events", s.sseManager.HandleSSE)
		s.webTransport.mux.HandleFunc("/sse", s.sseManager.HandleSSE)
	}

	return s
}

// BroadcastSSEEvent broadcasts an SSE event to all connections
func (s *Server) BroadcastSSEEvent(event SSEEvent) {
	if s.sseManager != nil {
		s.sseManager.BroadcastEvent(event)
	}
}

// SendSSEEventToConnection sends an SSE event to a specific connection
func (s *Server) SendSSEEventToConnection(connectionID string, event SSEEvent) bool {
	if s.sseManager != nil {
		return s.sseManager.SendEventToConnection(connectionID, event)
	}
	return false
}