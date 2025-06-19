package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketConfig configures WebSocket transport
type WebSocketConfig struct {
	ReadBufferSize    int           `json:"readBufferSize"`
	WriteBufferSize   int           `json:"writeBufferSize"`
	HandshakeTimeout  time.Duration `json:"handshakeTimeout"`
	ReadDeadline      time.Duration `json:"readDeadline"`
	WriteDeadline     time.Duration `json:"writeDeadline"`
	PingInterval      time.Duration `json:"pingInterval"`
	MaxMessageSize    int64         `json:"maxMessageSize"`
	EnableCompression bool          `json:"enableCompression"`
}

// DefaultWebSocketConfig returns default WebSocket configuration
func DefaultWebSocketConfig() WebSocketConfig {
	return WebSocketConfig{
		ReadBufferSize:    4096,
		WriteBufferSize:   4096,
		HandshakeTimeout:  10 * time.Second,
		ReadDeadline:      60 * time.Second,
		WriteDeadline:     10 * time.Second,
		PingInterval:      30 * time.Second,
		MaxMessageSize:    1024 * 1024, // 1MB
		EnableCompression: true,
	}
}

// WebSocketMessage represents a WebSocket message
type WebSocketMessage struct {
	Type      string          `json:"type"`      // "request", "response", "ping", "pong", "error"
	ID        string          `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    interface{}     `json:"result,omitempty"`
	Error     *APIError       `json:"error,omitempty"`
	Timestamp int64           `json:"timestamp"`
}

// WebSocketConnection represents a single WebSocket connection
type WebSocketConnection struct {
	id          string
	conn        *websocket.Conn
	server      *Server
	config      WebSocketConfig
	logger      *log.Logger
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.RWMutex
	lastPing    time.Time
	lastPong    time.Time
	isActive    bool
	sendChan    chan WebSocketMessage
	sessions    map[string]*StreamingSession // Active streaming sessions
}

// WebSocketManager manages WebSocket connections
type WebSocketManager struct {
	upgrader    websocket.Upgrader
	config      WebSocketConfig
	server      *Server
	logger      *log.Logger
	connections map[string]*WebSocketConnection
	mu          sync.RWMutex
}

// NewWebSocketManager creates a new WebSocket manager
func NewWebSocketManager(server *Server, config WebSocketConfig) *WebSocketManager {
	if config.ReadBufferSize == 0 {
		config = DefaultWebSocketConfig()
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:    config.ReadBufferSize,
		WriteBufferSize:   config.WriteBufferSize,
		HandshakeTimeout:  config.HandshakeTimeout,
		EnableCompression: config.EnableCompression,
		CheckOrigin: func(r *http.Request) bool {
			// Allow all origins for now (should be configurable in production)
			return true
		},
	}

	return &WebSocketManager{
		upgrader:    upgrader,
		config:      config,
		server:      server,
		logger:      server.logger,
		connections: make(map[string]*WebSocketConnection),
	}
}

// HandleWebSocket handles WebSocket upgrade and connection
func (wsm *WebSocketManager) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Check authentication if required
	if wsm.server.webTransport != nil && wsm.server.webTransport.config.AuthToken != "" {
		// Check token in query parameter or header
		token := r.URL.Query().Get("token")
		if token == "" {
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" && len(authHeader) > 7 && authHeader[:7] == "Bearer " {
				token = authHeader[7:]
			}
		}

		if token != wsm.server.webTransport.config.AuthToken {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Upgrade to WebSocket
	conn, err := wsm.upgrader.Upgrade(w, r, nil)
	if err != nil {
		wsm.logger.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	// Create connection
	wsConn := wsm.createConnection(conn)
	
	// Register connection
	wsm.mu.Lock()
	wsm.connections[wsConn.id] = wsConn
	wsm.mu.Unlock()

	wsm.logger.Printf("WebSocket connection established: %s", wsConn.id)

	// Handle connection
	wsConn.handle()

	// Cleanup
	wsm.mu.Lock()
	delete(wsm.connections, wsConn.id)
	wsm.mu.Unlock()

	wsm.logger.Printf("WebSocket connection closed: %s", wsConn.id)
}

// createConnection creates a new WebSocket connection
func (wsm *WebSocketManager) createConnection(conn *websocket.Conn) *WebSocketConnection {
	ctx, cancel := context.WithCancel(context.Background())
	
	connID := fmt.Sprintf("ws_%d", time.Now().UnixNano())
	
	wsConn := &WebSocketConnection{
		id:       connID,
		conn:     conn,
		server:   wsm.server,
		config:   wsm.config,
		logger:   wsm.logger,
		ctx:      ctx,
		cancel:   cancel,
		isActive: true,
		sendChan: make(chan WebSocketMessage, 100),
		sessions: make(map[string]*StreamingSession),
	}

	// Set connection limits
	conn.SetReadLimit(wsm.config.MaxMessageSize)
	conn.SetReadDeadline(time.Now().Add(wsm.config.ReadDeadline))
	conn.SetWriteDeadline(time.Now().Add(wsm.config.WriteDeadline))

	// Set ping/pong handlers
	conn.SetPingHandler(func(appData string) error {
		wsConn.mu.Lock()
		wsConn.lastPing = time.Now()
		wsConn.mu.Unlock()
		
		conn.SetReadDeadline(time.Now().Add(wsm.config.ReadDeadline))
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(wsm.config.WriteDeadline))
	})

	conn.SetPongHandler(func(appData string) error {
		wsConn.mu.Lock()
		wsConn.lastPong = time.Now()
		wsConn.mu.Unlock()
		
		conn.SetReadDeadline(time.Now().Add(wsm.config.ReadDeadline))
		return nil
	})

	return wsConn
}

// handle manages the WebSocket connection lifecycle
func (wsc *WebSocketConnection) handle() {
	defer func() {
		wsc.cleanup()
	}()

	// Start sender goroutine
	go wsc.sender()

	// Start ping goroutine
	go wsc.pinger()

	// Message handling loop
	for {
		select {
		case <-wsc.ctx.Done():
			return
		default:
		}

		var msg WebSocketMessage
		err := wsc.conn.ReadJSON(&msg)
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				wsc.logger.Printf("WebSocket read error: %v", err)
			}
			return
		}

		wsc.conn.SetReadDeadline(time.Now().Add(wsc.config.ReadDeadline))
		
		// Process message
		go wsc.processMessage(msg)
	}
}

// processMessage processes incoming WebSocket messages
func (wsc *WebSocketConnection) processMessage(msg WebSocketMessage) {
	switch msg.Type {
	case "request":
		wsc.handleRequest(msg)
	case "stream_subscribe":
		wsc.handleStreamSubscribe(msg)
	case "stream_unsubscribe":
		wsc.handleStreamUnsubscribe(msg)
	case "ping":
		wsc.sendMessage(WebSocketMessage{
			Type:      "pong",
			ID:        msg.ID,
			Timestamp: time.Now().Unix(),
		})
	default:
		wsc.sendErrorMessage(msg.ID, "unknown_message_type", fmt.Sprintf("Unknown message type: %s", msg.Type))
	}
}

// handleRequest handles MCP requests via WebSocket
func (wsc *WebSocketConnection) handleRequest(msg WebSocketMessage) {
	if msg.Method == "" {
		wsc.sendErrorMessage(msg.ID, "missing_method", "Method is required")
		return
	}

	// Create MCP request
	mcpRequest := &Request{
		JSONRPC: "2.0",
		ID:      msg.ID,
		Method:  msg.Method,
		Params:  msg.Params,
	}

	// Process via server
	ctx := wsc.ctx
	response := wsc.server.HandleRequest(ctx, mcpRequest)

	// Send response
	responseMsg := WebSocketMessage{
		Type:      "response",
		ID:        msg.ID,
		Timestamp: time.Now().Unix(),
	}

	if response.Error != nil {
		responseMsg.Error = &APIError{
			Code:    response.Error.Code,
			Message: response.Error.Message,
		}
	} else {
		responseMsg.Result = response.Result
	}

	wsc.sendMessage(responseMsg)
}

// handleStreamSubscribe handles streaming tool subscription
func (wsc *WebSocketConnection) handleStreamSubscribe(msg WebSocketMessage) {
	var params struct {
		ToolName  string                 `json:"toolName"`
		Arguments map[string]interface{} `json:"arguments"`
		SessionID string                 `json:"sessionId,omitempty"`
	}

	if err := json.Unmarshal(msg.Params, &params); err != nil {
		wsc.sendErrorMessage(msg.ID, "invalid_params", "Invalid subscription parameters")
		return
	}

	// Generate session ID if not provided
	if params.SessionID == "" {
		params.SessionID = fmt.Sprintf("stream_%s_%d", params.ToolName, time.Now().UnixNano())
	}

	// Check if tool is streaming-capable
	if wsc.server.streamingManager == nil {
		wsc.sendErrorMessage(msg.ID, "streaming_not_supported", "Streaming not supported")
		return
	}

	// Find streaming tool handler
	toolName := params.ToolName
	if !wsc.isStreamingTool(toolName) {
		wsc.sendErrorMessage(msg.ID, "not_streaming_tool", fmt.Sprintf("Tool %s does not support streaming", toolName))
		return
	}

	// Start streaming session
	argsJSON, _ := json.Marshal(params.Arguments)
	
	// Call the streaming tool
	mcpRequest := &Request{
		JSONRPC: "2.0",
		ID:      params.SessionID,
		Method:  "tools/call",
		Params:  json.RawMessage(fmt.Sprintf(`{"name": "%s", "arguments": %s}`, toolName, string(argsJSON))),
	}

	response := wsc.server.HandleRequest(wsc.ctx, mcpRequest)
	
	// Send subscription confirmation
	wsc.sendMessage(WebSocketMessage{
		Type:      "response",
		ID:        msg.ID,
		Result:    map[string]interface{}{
			"subscribed": true,
			"sessionId":  params.SessionID,
			"status":     "started",
		},
		Timestamp: time.Now().Unix(),
	})

	// If this started a streaming session, we'll get updates via polling
	// This is a simplified implementation - in a real system, you'd integrate
	// more deeply with the streaming system
	if response.Result != nil {
		if resultMap, ok := response.Result.(map[string]interface{}); ok {
			if sessionID, ok := resultMap["sessionId"].(string); ok {
				// Start polling for streaming results
				go wsc.pollStreamingSession(sessionID, params.SessionID)
			}
		}
	}
}

// handleStreamUnsubscribe handles streaming unsubscription
func (wsc *WebSocketConnection) handleStreamUnsubscribe(msg WebSocketMessage) {
	var params struct {
		SessionID string `json:"sessionId"`
	}

	if err := json.Unmarshal(msg.Params, &params); err != nil {
		wsc.sendErrorMessage(msg.ID, "invalid_params", "Invalid unsubscription parameters")
		return
	}

	// Cancel streaming session
	if wsc.server.streamingManager != nil {
		wsc.server.streamingManager.CancelSession(params.SessionID)
	}

	// Remove from local sessions
	wsc.mu.Lock()
	delete(wsc.sessions, params.SessionID)
	wsc.mu.Unlock()

	// Send confirmation
	wsc.sendMessage(WebSocketMessage{
		Type:      "response",
		ID:        msg.ID,
		Result:    map[string]interface{}{
			"unsubscribed": true,
			"sessionId":    params.SessionID,
		},
		Timestamp: time.Now().Unix(),
	})
}

// pollStreamingSession polls for streaming results and sends via WebSocket
func (wsc *WebSocketConnection) pollStreamingSession(streamingSessionID, wsSessionID string) {
	ticker := time.NewTicker(500 * time.Millisecond) // Poll every 500ms
	defer ticker.Stop()

	for {
		select {
		case <-wsc.ctx.Done():
			return
		case <-ticker.C:
			if wsc.server.streamingManager == nil {
				return
			}

			// Poll for results
			result, err := wsc.server.streamingManager.PollSession(streamingSessionID, 10)
			if err != nil {
				wsc.sendMessage(WebSocketMessage{
					Type: "stream_error",
					ID:   wsSessionID,
					Error: &APIError{
						Code:    500,
						Message: err.Error(),
					},
					Timestamp: time.Now().Unix(),
				})
				return
			}

			// Send streaming update
			wsc.sendMessage(WebSocketMessage{
				Type:      "stream_data",
				ID:        wsSessionID,
				Result:    result,
				Timestamp: time.Now().Unix(),
			})

			// Check if finished
			if resultMap, ok := result.(map[string]interface{}); ok {
				if status, ok := resultMap["status"].(string); ok {
					if status == "finished" || status == "cancelled" {
						return
					}
				}
			}
		}
	}
}

// isStreamingTool checks if a tool supports streaming
func (wsc *WebSocketConnection) isStreamingTool(toolName string) bool {
	// Check if the tool is registered as a streaming tool
	// This is a simplified check - in a real implementation, you'd track which tools support streaming
	if wsc.server.streamingManager != nil {
		// For now, assume tools with "batch" or "stream" in the name support streaming
		return len(toolName) > 0 && (contains(toolName, "batch") || contains(toolName, "stream") || contains(toolName, "process"))
	}
	return false
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsInMiddle(s, substr))))
}

func containsInMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// sendMessage sends a message via WebSocket
func (wsc *WebSocketConnection) sendMessage(msg WebSocketMessage) {
	select {
	case wsc.sendChan <- msg:
	case <-wsc.ctx.Done():
	default:
		wsc.logger.Printf("WebSocket send channel full, dropping message")
	}
}

// sendErrorMessage sends an error message
func (wsc *WebSocketConnection) sendErrorMessage(id, code, message string) {
	wsc.sendMessage(WebSocketMessage{
		Type: "error",
		ID:   id,
		Error: &APIError{
			Code:    400,
			Message: message,
		},
		Timestamp: time.Now().Unix(),
	})
}

// sender handles outgoing messages
func (wsc *WebSocketConnection) sender() {
	for {
		select {
		case <-wsc.ctx.Done():
			return
		case msg := <-wsc.sendChan:
			wsc.conn.SetWriteDeadline(time.Now().Add(wsc.config.WriteDeadline))
			
			if err := wsc.conn.WriteJSON(msg); err != nil {
				wsc.logger.Printf("WebSocket write error: %v", err)
				wsc.cancel()
				return
			}
		}
	}
}

// pinger sends periodic ping messages
func (wsc *WebSocketConnection) pinger() {
	ticker := time.NewTicker(wsc.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-wsc.ctx.Done():
			return
		case <-ticker.C:
			wsc.conn.SetWriteDeadline(time.Now().Add(wsc.config.WriteDeadline))
			
			if err := wsc.conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				wsc.logger.Printf("WebSocket ping error: %v", err)
				wsc.cancel()
				return
			}
		}
	}
}

// cleanup cleans up the connection
func (wsc *WebSocketConnection) cleanup() {
	wsc.mu.Lock()
	wsc.isActive = false
	wsc.mu.Unlock()

	// Cancel all streaming sessions
	for sessionID := range wsc.sessions {
		if wsc.server.streamingManager != nil {
			wsc.server.streamingManager.CancelSession(sessionID)
		}
	}

	// Close connection
	wsc.conn.Close()
	wsc.cancel()
}

// GetConnections returns active WebSocket connections
func (wsm *WebSocketManager) GetConnections() map[string]*WebSocketConnection {
	wsm.mu.RLock()
	defer wsm.mu.RUnlock()
	
	connections := make(map[string]*WebSocketConnection)
	for id, conn := range wsm.connections {
		connections[id] = conn
	}
	return connections
}

// BroadcastMessage broadcasts a message to all connections
func (wsm *WebSocketManager) BroadcastMessage(msg WebSocketMessage) {
	wsm.mu.RLock()
	defer wsm.mu.RUnlock()
	
	for _, conn := range wsm.connections {
		conn.sendMessage(msg)
	}
}

// Server extensions for WebSocket

// EnableWebSocket enables WebSocket support
func (s *Server) EnableWebSocket(config WebSocketConfig) *Server {
	s.wsManager = NewWebSocketManager(s, config)
	
	// Add WebSocket endpoint to web transport if available
	if s.webTransport != nil {
		s.webTransport.mux.HandleFunc("/ws", s.wsManager.HandleWebSocket)
	}
	
	return s
}