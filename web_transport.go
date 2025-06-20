package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// WebConfig configures the web transport
type WebConfig struct {
	Port           int           `json:"port"`
	Host           string        `json:"host"`
	AuthToken      string        `json:"authToken,omitempty"`
	EnableCORS     bool          `json:"enableCORS"`
	EnableDashboard bool         `json:"enableDashboard"`
	ReadTimeout    time.Duration `json:"readTimeout"`
	WriteTimeout   time.Duration `json:"writeTimeout"`
	MaxRequestSize int64         `json:"maxRequestSize"`
}

// DefaultWebConfig returns default web configuration
func DefaultWebConfig() WebConfig {
	return WebConfig{
		Port:           8080,
		Host:           "localhost",
		EnableCORS:     true,
		EnableDashboard: true,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		MaxRequestSize: 10 * 1024 * 1024, // 10MB
	}
}

// WebTransport handles HTTP/WebSocket transport
type WebTransport struct {
	config     WebConfig
	server     *Server
	httpServer *http.Server
	mux        *http.ServeMux
	logger     *log.Logger
	mu         sync.RWMutex
	running    bool
}

// NewWebTransport creates a new web transport
func NewWebTransport(server *Server, config WebConfig) *WebTransport {
	if config.Port == 0 {
		config = DefaultWebConfig()
	}
	
	mux := http.NewServeMux()
	
	httpServer := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", config.Host, config.Port),
		Handler:      mux,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
	}
	
	wt := &WebTransport{
		config:     config,
		server:     server,
		httpServer: httpServer,
		mux:        mux,
		logger:     server.logger,
		running:    false,
	}
	
	// Register routes
	wt.registerRoutes()
	
	return wt
}

// registerRoutes registers HTTP routes
func (wt *WebTransport) registerRoutes() {
	// Health check endpoint
	wt.mux.HandleFunc("/health", wt.handleHealth)
	
	// API endpoints
	wt.mux.HandleFunc("/api/v1/tools/list", wt.withMiddleware(wt.handleToolsList))
	wt.mux.HandleFunc("/api/v1/tools/call", wt.withMiddleware(wt.handleToolsCall))
	wt.mux.HandleFunc("/api/v1/resources/list", wt.withMiddleware(wt.handleResourcesList))
	wt.mux.HandleFunc("/api/v1/resources/read", wt.withMiddleware(wt.handleResourcesRead))
	wt.mux.HandleFunc("/api/v1/prompts/list", wt.withMiddleware(wt.handlePromptsList))
	wt.mux.HandleFunc("/api/v1/prompts/get", wt.withMiddleware(wt.handlePromptsGet))
	
	// Server info endpoint
	wt.mux.HandleFunc("/api/v1/server/info", wt.withMiddleware(wt.handleServerInfo))
	
	// MCP-over-HTTP endpoints for SSE transport (FastMCP compatible)
	wt.mux.HandleFunc("/message", wt.withMiddleware(wt.handleMCPMessage))   // Legacy
	wt.mux.HandleFunc("/messages", wt.withMiddleware(wt.handleMCPMessage))  // FastMCP standard
	wt.mux.HandleFunc("/messages/", wt.withMiddleware(wt.handleMCPMessage)) // FastMCP with trailing slash
	
	// Dashboard (if enabled)
	if wt.config.EnableDashboard {
		wt.mux.HandleFunc("/", wt.handleDashboard)
		wt.mux.HandleFunc("/dashboard", wt.handleDashboard)
	}
}

// withMiddleware applies middleware to handlers
func (wt *WebTransport) withMiddleware(handler http.HandlerFunc) http.HandlerFunc {
	return wt.withCORS(wt.withAuth(wt.withLogging(handler)))
}

// withCORS adds CORS headers
func (wt *WebTransport) withCORS(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if wt.config.EnableCORS {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			
			// Handle preflight requests
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		
		handler(w, r)
	}
}

// withAuth adds authentication middleware
func (wt *WebTransport) withAuth(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for health check
		if r.URL.Path == "/health" {
			handler(w, r)
			return
		}
		
		// Skip auth if no token configured
		if wt.config.AuthToken == "" {
			handler(w, r)
			return
		}
		
		// Check Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			wt.writeError(w, http.StatusUnauthorized, "Authorization header required")
			return
		}
		
		// Validate Bearer token
		expectedAuth := "Bearer " + wt.config.AuthToken
		if authHeader != expectedAuth {
			wt.writeError(w, http.StatusUnauthorized, "Invalid authorization token")
			return
		}
		
		handler(w, r)
	}
}

// withLogging adds request logging
func (wt *WebTransport) withLogging(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		
		// Create a response writer that captures the status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		
		handler(rw, r)
		
		duration := time.Since(start)
		wt.logger.Printf("%s %s %d %v", r.Method, r.URL.Path, rw.statusCode, duration)
	}
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Start starts the web transport
func (wt *WebTransport) Start() error {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	
	if wt.running {
		return fmt.Errorf("web transport already running")
	}
	
	wt.logger.Printf("Starting web transport on %s", wt.httpServer.Addr)
	
	go func() {
		if err := wt.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			wt.logger.Printf("Web transport error: %v", err)
		}
	}()
	
	wt.running = true
	return nil
}

// Stop stops the web transport
func (wt *WebTransport) Stop() error {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	
	if !wt.running {
		return nil
	}
	
	wt.logger.Printf("Stopping web transport")
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := wt.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown web transport: %w", err)
	}
	
	wt.running = false
	return nil
}

// IsRunning returns whether the web transport is running
func (wt *WebTransport) IsRunning() bool {
	wt.mu.RLock()
	defer wt.mu.RUnlock()
	return wt.running
}

// GetAddr returns the server address
func (wt *WebTransport) GetAddr() string {
	return wt.httpServer.Addr
}

// GetMux returns the HTTP mux for custom route registration
func (wt *WebTransport) GetMux() *http.ServeMux {
	return wt.mux
}

// writeJSON writes a JSON response
func (wt *WebTransport) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	
	if err := json.NewEncoder(w).Encode(data); err != nil {
		wt.logger.Printf("Failed to encode JSON response: %v", err)
	}
}

// writeError writes an error response
func (wt *WebTransport) writeError(w http.ResponseWriter, status int, message string) {
	wt.writeJSON(w, status, map[string]interface{}{
		"error": map[string]interface{}{
			"code":    status,
			"message": message,
		},
	})
}

// parseLimitOffset parses limit and offset query parameters
func (wt *WebTransport) parseLimitOffset(r *http.Request) (limit, offset int) {
	limit = 50 // default
	offset = 0 // default
	
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}
	
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}
	
	return limit, offset
}

// Server extensions for web transport

// EnableWebTransport enables web transport for the server
func (s *Server) EnableWebTransport(config WebConfig) *Server {
	s.webTransport = NewWebTransport(s, config)
	return s
}

// StartWebTransport starts the web transport
func (s *Server) StartWebTransport() error {
	if s.webTransport == nil {
		return fmt.Errorf("web transport not enabled")
	}
	return s.webTransport.Start()
}

// StopWebTransport stops the web transport
func (s *Server) StopWebTransport() error {
	if s.webTransport == nil {
		return nil
	}
	return s.webTransport.Stop()
}

// GetWebTransport returns the web transport instance
func (s *Server) GetWebTransport() *WebTransport {
	return s.webTransport
}

// RunWithWebTransport runs the server with web transport only
func (s *Server) RunWithWebTransport() error {
	if err := s.StartWebTransport(); err != nil {
		return err
	}
	
	// Block forever
	select {}
}

// RunMultiTransport runs both stdio and web transports
func (s *Server) RunMultiTransport() error {
	// Start web transport
	if s.webTransport != nil {
		if err := s.StartWebTransport(); err != nil {
			return fmt.Errorf("failed to start web transport: %w", err)
		}
	}
	
	// Run stdio transport (blocking)
	return s.Run()
}