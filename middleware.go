package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// MiddlewareFunc represents a middleware function
type MiddlewareFunc func(next RequestHandler) RequestHandler

// RequestHandler represents a function that handles MCP requests
type RequestHandler func(ctx context.Context, req *Request) *Response

// MiddlewareContext holds request-specific data that middleware can use
type MiddlewareContext struct {
	StartTime    time.Time
	RequestID    string
	UserID       string
	Metadata     map[string]interface{}
	mu           sync.RWMutex
}

// NewMiddlewareContext creates a new middleware context
func NewMiddlewareContext() *MiddlewareContext {
	return &MiddlewareContext{
		StartTime: time.Now(),
		RequestID: generateRequestID(),
		Metadata:  make(map[string]interface{}),
	}
}

// Set adds a key-value pair to the middleware context
func (mc *MiddlewareContext) Set(key string, value interface{}) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.Metadata[key] = value
}

// Get retrieves a value from the middleware context
func (mc *MiddlewareContext) Get(key string) (interface{}, bool) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	value, exists := mc.Metadata[key]
	return value, exists
}

// GetString retrieves a string value from the middleware context
func (mc *MiddlewareContext) GetString(key string) string {
	if value, exists := mc.Get(key); exists {
		if str, ok := value.(string); ok {
			return str
		}
	}
	return ""
}

// MiddlewareConfig represents middleware configuration
type MiddlewareConfig struct {
	Enabled     bool                   `json:"enabled"`
	Order       []string               `json:"order"`       // Middleware execution order
	Config      map[string]interface{} `json:"config"`      // Middleware-specific config
	SkipMethods []string               `json:"skipMethods"` // Methods to skip middleware
}

// MiddlewareManager manages middleware execution
type MiddlewareManager struct {
	middlewares map[string]MiddlewareFunc
	order       []string
	config      MiddlewareConfig
	server      *Server
	mu          sync.RWMutex
}

// NewMiddlewareManager creates a new middleware manager
func NewMiddlewareManager(server *Server, config MiddlewareConfig) *MiddlewareManager {
	return &MiddlewareManager{
		middlewares: make(map[string]MiddlewareFunc),
		order:       config.Order,
		config:      config,
		server:      server,
	}
}

// Register registers a middleware with a name
func (mm *MiddlewareManager) Register(name string, middleware MiddlewareFunc) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.middlewares[name] = middleware
}

// Unregister removes a middleware
func (mm *MiddlewareManager) Unregister(name string) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	delete(mm.middlewares, name)
}

// BuildChain builds the middleware chain
func (mm *MiddlewareManager) BuildChain(finalHandler RequestHandler) RequestHandler {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	if !mm.config.Enabled {
		return finalHandler
	}

	handler := finalHandler

	// Build chain in reverse order (last middleware first)
	for i := len(mm.order) - 1; i >= 0; i-- {
		middlewareName := mm.order[i]
		if middleware, exists := mm.middlewares[middlewareName]; exists {
			handler = middleware(handler)
		}
	}

	return handler
}

// ShouldSkipMethod checks if middleware should be skipped for a method
func (mm *MiddlewareManager) ShouldSkipMethod(method string) bool {
	for _, skipMethod := range mm.config.SkipMethods {
		if skipMethod == method {
			return true
		}
	}
	return false
}

// GetConfig returns configuration for a specific middleware
func (mm *MiddlewareManager) GetConfig(middlewareName string) map[string]interface{} {
	if config, exists := mm.config.Config[middlewareName]; exists {
		if configMap, ok := config.(map[string]interface{}); ok {
			return configMap
		}
	}
	return make(map[string]interface{})
}

// Server extensions for middleware

// EnableMiddleware enables middleware system
func (s *Server) EnableMiddleware(config MiddlewareConfig) *Server {
	s.middlewareManager = NewMiddlewareManager(s, config)
	return s
}

// Use registers a middleware
func (s *Server) Use(name string, middleware MiddlewareFunc) *Server {
	if s.middlewareManager != nil {
		s.middlewareManager.Register(name, middleware)
	}
	return s
}

// HandleRequestWithMiddleware processes a request through the middleware chain
func (s *Server) HandleRequestWithMiddleware(ctx context.Context, req *Request) *Response {
	if s.middlewareManager == nil {
		return s.HandleRequest(ctx, req)
	}

	// Check if method should skip middleware
	if s.middlewareManager.ShouldSkipMethod(req.Method) {
		return s.HandleRequest(ctx, req)
	}

	// Create middleware context
	mctx := NewMiddlewareContext()
	ctx = context.WithValue(ctx, "middleware", mctx)

	// Build and execute middleware chain
	finalHandler := func(ctx context.Context, req *Request) *Response {
		return s.HandleRequest(ctx, req)
	}

	handler := s.middlewareManager.BuildChain(finalHandler)
	return handler(ctx, req)
}

// Utility functions

func generateRequestID() string {
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}

// GetMiddlewareContext retrieves middleware context from request context
func GetMiddlewareContext(ctx context.Context) *MiddlewareContext {
	if mctx := ctx.Value("middleware"); mctx != nil {
		if mc, ok := mctx.(*MiddlewareContext); ok {
			return mc
		}
	}
	return NewMiddlewareContext()
}

// Built-in Middleware Functions

// LoggingConfig represents logging middleware configuration
type LoggingConfig struct {
	Level          string                    `json:"level"`          // "debug", "info", "warn", "error"
	Format         string                    `json:"format"`         // "json", "text"
	IncludeHeaders bool                      `json:"includeHeaders"` // Include request headers
	IncludeBody    bool                      `json:"includeBody"`    // Include request/response body
	MaxBodySize    int                       `json:"maxBodySize"`    // Max body size to log
	SanitizeFields []string                  `json:"sanitizeFields"` // Fields to sanitize (passwords, tokens)
	CustomFields   map[string]func(*Request, *Response) interface{} `json:"-"` // Custom log fields
}

// LoggingMiddleware logs request/response information with configurable options
func LoggingMiddleware(logger Logger) MiddlewareFunc {
	return LoggingMiddlewareWithConfig(logger, LoggingConfig{
		Level:       "info",
		Format:      "text",
		MaxBodySize: 1024,
	})
}

// LoggingMiddlewareWithConfig creates a logging middleware with custom configuration
func LoggingMiddlewareWithConfig(logger Logger, config LoggingConfig) MiddlewareFunc {
	return func(next RequestHandler) RequestHandler {
		return func(ctx context.Context, req *Request) *Response {
			mctx := GetMiddlewareContext(ctx)
			start := time.Now()

			// Log request start
			logRequest(logger, config, req, mctx, "started")

			response := next(ctx, req)

			duration := time.Since(start)
			status := "success"
			errorCode := 0
			if response.Error != nil {
				status = "error"
				errorCode = response.Error.Code
			}

			// Add timing to middleware context
			mctx.Set("request_duration", duration)
			mctx.Set("response_status", status)
			mctx.Set("response_error_code", errorCode)

			// Log request completion
			logRequestCompletion(logger, config, req, response, mctx, duration, status)

			return response
		}
	}
}

func logRequest(logger Logger, config LoggingConfig, req *Request, mctx *MiddlewareContext, stage string) {
	if config.Format == "json" {
		logData := map[string]interface{}{
			"timestamp":  time.Now().Format(time.RFC3339),
			"level":      config.Level,
			"stage":      stage,
			"method":     req.Method,
			"request_id": req.ID,
			"middleware_request_id": mctx.RequestID,
		}

		if config.IncludeBody && req.Params != nil {
			body := string(req.Params)
			if len(body) > config.MaxBodySize {
				body = body[:config.MaxBodySize] + "..."
			}
			logData["request_body"] = sanitizeFields(body, config.SanitizeFields)
		}

		if jsonBytes, err := json.Marshal(logData); err == nil {
			logger.Printf("%s", string(jsonBytes))
		}
	} else {
		logger.Printf("Request %s: %s %s [%s]", stage, req.Method, req.ID, mctx.RequestID)
	}
}

func logRequestCompletion(logger Logger, config LoggingConfig, req *Request, resp *Response, mctx *MiddlewareContext, duration time.Duration, status string) {
	if config.Format == "json" {
		logData := map[string]interface{}{
			"timestamp":  time.Now().Format(time.RFC3339),
			"level":      config.Level,
			"stage":      "completed",
			"method":     req.Method,
			"request_id": req.ID,
			"middleware_request_id": mctx.RequestID,
			"duration":   duration.String(),
			"status":     status,
		}

		if resp.Error != nil {
			logData["error_code"] = resp.Error.Code
			logData["error_message"] = resp.Error.Message
		}

		if config.IncludeBody && resp.Result != nil {
			if resultBytes, err := json.Marshal(resp.Result); err == nil {
				body := string(resultBytes)
				if len(body) > config.MaxBodySize {
					body = body[:config.MaxBodySize] + "..."
				}
				logData["response_body"] = body
			}
		}

		if jsonBytes, err := json.Marshal(logData); err == nil {
			logger.Printf("%s", string(jsonBytes))
		}
	} else {
		logger.Printf("Request completed: %s %s [%s] - %s (%v)", 
			req.Method, req.ID, mctx.RequestID, status, duration)
	}
}

func sanitizeFields(text string, sensitiveFields []string) string {
	result := text
	for _, _ = range sensitiveFields {
		// Simple regex-based sanitization (in production, use more sophisticated methods)
		result = fmt.Sprintf("%s", result) // Placeholder for actual sanitization
	}
	return result
}

// MetricsConfig represents metrics middleware configuration
type MetricsConfig struct {
	IncludeUserMetrics   bool     `json:"includeUserMetrics"`   // Include user-based metrics
	IncludeDetailedTiming bool    `json:"includeDetailedTiming"` // Include detailed timing breakdowns
	CustomLabels         map[string]func(*Request, *Response) string `json:"-"` // Custom metric labels
	HistogramBuckets     []float64 `json:"histogramBuckets"`     // Custom histogram buckets
	SampleRate          float64   `json:"sampleRate"`           // Sampling rate (0.0-1.0)
}

// MetricsMiddleware collects request metrics with basic configuration
func MetricsMiddleware(collector MetricsCollector) MiddlewareFunc {
	return MetricsMiddlewareWithConfig(collector, MetricsConfig{
		SampleRate: 1.0,
		HistogramBuckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0},
	})
}

// MetricsMiddlewareWithConfig creates a metrics middleware with custom configuration
func MetricsMiddlewareWithConfig(collector MetricsCollector, config MetricsConfig) MiddlewareFunc {
	return func(next RequestHandler) RequestHandler {
		return func(ctx context.Context, req *Request) *Response {
			// Sample requests based on sample rate
			if config.SampleRate < 1.0 && time.Now().UnixNano()%100 >= int64(config.SampleRate*100) {
				return next(ctx, req)
			}

			start := time.Now()
			mctx := GetMiddlewareContext(ctx)

			// Base labels
			labels := map[string]string{
				"method": req.Method,
			}

			// Add user metrics if enabled
			if config.IncludeUserMetrics {
				if user := GetCurrentUser(ctx); user != nil {
					labels["user_id"] = user.ID
					if len(user.Roles) > 0 {
						labels["user_role"] = user.Roles[0]
					}
				} else {
					labels["user_id"] = "anonymous"
				}
			}

			// Increment request counter
			collector.IncrementCounter("mcp_requests_total", labels)

			// Record concurrent requests
			collector.IncrementCounter("mcp_requests_concurrent", labels)
			defer collector.RecordGauge("mcp_requests_concurrent", -1, labels)

			response := next(ctx, req)

			duration := time.Since(start)
			
			// Determine status
			status := "success"
			if response.Error != nil {
				status = "error"
				errorLabels := make(map[string]string)
				for k, v := range labels {
					errorLabels[k] = v
				}
				errorLabels["error_code"] = fmt.Sprintf("%d", response.Error.Code)
				errorLabels["error_type"] = getErrorType(response.Error.Code)
				
				collector.IncrementCounter("mcp_requests_errors_total", errorLabels)
			}

			// Record timing metrics
			durationLabels := make(map[string]string)
			for k, v := range labels {
				durationLabels[k] = v
			}
			durationLabels["status"] = status

			collector.RecordDuration("mcp_request_duration_seconds", duration, durationLabels)

			// Record detailed timing if enabled
			if config.IncludeDetailedTiming {
				if authDuration := getDurationFromContext(mctx, "auth_duration"); authDuration > 0 {
					collector.RecordDuration("mcp_auth_duration_seconds", authDuration, labels)
				}
				if validationDuration := getDurationFromContext(mctx, "validation_duration"); validationDuration > 0 {
					collector.RecordDuration("mcp_validation_duration_seconds", validationDuration, labels)
				}
			}

			// Add custom labels if configured
			if config.CustomLabels != nil {
				customLabels := make(map[string]string)
				for k, v := range labels {
					customLabels[k] = v
				}
				for labelName, labelFunc := range config.CustomLabels {
					customLabels[labelName] = labelFunc(req, response)
				}
				collector.RecordDuration("mcp_request_duration_custom", duration, customLabels)
			}

			// Record request size metrics
			if req.Params != nil {
				requestSize := float64(len(req.Params))
				collector.RecordGauge("mcp_request_size_bytes", requestSize, labels)
			}

			// Record response size metrics
			if response.Result != nil {
				if resultBytes, err := json.Marshal(response.Result); err == nil {
					responseSize := float64(len(resultBytes))
					collector.RecordGauge("mcp_response_size_bytes", responseSize, durationLabels)
				}
			}

			// Store metrics in middleware context for other middleware
			mctx.Set("metrics_duration", duration)
			mctx.Set("metrics_status", status)
			mctx.Set("metrics_labels", labels)

			return response
		}
	}
}

func getErrorType(errorCode int) string {
	switch {
	case errorCode >= 400 && errorCode < 500:
		return "client_error"
	case errorCode >= 500:
		return "server_error"
	default:
		return "unknown"
	}
}

func getDurationFromContext(mctx *MiddlewareContext, key string) time.Duration {
	if value, exists := mctx.Get(key); exists {
		if duration, ok := value.(time.Duration); ok {
			return duration
		}
	}
	return 0
}

// RequestIDMiddleware adds a unique request ID
func RequestIDMiddleware() MiddlewareFunc {
	return func(next RequestHandler) RequestHandler {
		return func(ctx context.Context, req *Request) *Response {
			mctx := GetMiddlewareContext(ctx)
			
			// Add request ID to response headers or metadata
			response := next(ctx, req)
			
			// Could add request ID to response metadata
			if response.Result != nil {
				if resultMap, ok := response.Result.(map[string]interface{}); ok {
					resultMap["_request_id"] = mctx.RequestID
				}
			}

			return response
		}
	}
}

// Interfaces for extensibility

// Logger interface for middleware logging
type Logger interface {
	Printf(format string, v ...interface{})
}

// MetricsCollector interface for collecting metrics
type MetricsCollector interface {
	IncrementCounter(name string, labels map[string]string)
	RecordDuration(name string, duration time.Duration, labels map[string]string)
	RecordGauge(name string, value float64, labels map[string]string)
}

// BasicLogger implements Logger interface using standard log
type BasicLogger struct {
	logger *log.Logger
}

// NewBasicLogger creates a new basic logger
func NewBasicLogger(logger *log.Logger) *BasicLogger {
	return &BasicLogger{logger: logger}
}

// Printf implements Logger interface
func (bl *BasicLogger) Printf(format string, v ...interface{}) {
	bl.logger.Printf(format, v...)
}

// MemoryMetricsCollector implements MetricsCollector interface with in-memory storage
type MemoryMetricsCollector struct {
	counters  map[string]int64
	durations map[string][]time.Duration
	gauges    map[string]float64
	mu        sync.RWMutex
}

// NewMemoryMetricsCollector creates a new memory-based metrics collector
func NewMemoryMetricsCollector() *MemoryMetricsCollector {
	return &MemoryMetricsCollector{
		counters:  make(map[string]int64),
		durations: make(map[string][]time.Duration),
		gauges:    make(map[string]float64),
	}
}

// IncrementCounter implements MetricsCollector interface
func (mmc *MemoryMetricsCollector) IncrementCounter(name string, labels map[string]string) {
	mmc.mu.Lock()
	defer mmc.mu.Unlock()
	
	key := name + labelsToString(labels)
	mmc.counters[key]++
}

// RecordDuration implements MetricsCollector interface
func (mmc *MemoryMetricsCollector) RecordDuration(name string, duration time.Duration, labels map[string]string) {
	mmc.mu.Lock()
	defer mmc.mu.Unlock()
	
	key := name + labelsToString(labels)
	mmc.durations[key] = append(mmc.durations[key], duration)
}

// RecordGauge implements MetricsCollector interface
func (mmc *MemoryMetricsCollector) RecordGauge(name string, value float64, labels map[string]string) {
	mmc.mu.Lock()
	defer mmc.mu.Unlock()
	
	key := name + labelsToString(labels)
	mmc.gauges[key] = value
}

// GetMetrics returns collected metrics
func (mmc *MemoryMetricsCollector) GetMetrics() map[string]interface{} {
	mmc.mu.RLock()
	defer mmc.mu.RUnlock()
	
	metrics := make(map[string]interface{})
	
	// Add counters
	counters := make(map[string]int64)
	for key, value := range mmc.counters {
		counters[key] = value
	}
	metrics["counters"] = counters
	
	// Add duration summaries
	durationSummaries := make(map[string]map[string]interface{})
	for key, durations := range mmc.durations {
		if len(durations) > 0 {
			var total time.Duration
			min := durations[0]
			max := durations[0]
			
			for _, d := range durations {
				total += d
				if d < min {
					min = d
				}
				if d > max {
					max = d
				}
			}
			
			avg := total / time.Duration(len(durations))
			
			durationSummaries[key] = map[string]interface{}{
				"count": len(durations),
				"min":   min.String(),
				"max":   max.String(),
				"avg":   avg.String(),
				"total": total.String(),
			}
		}
	}
	metrics["durations"] = durationSummaries
	
	// Add gauges
	gauges := make(map[string]float64)
	for key, value := range mmc.gauges {
		gauges[key] = value
	}
	metrics["gauges"] = gauges
	
	return metrics
}

func labelsToString(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	
	result := "{"
	first := true
	for key, value := range labels {
		if !first {
			result += ","
		}
		result += key + ":" + value
		first = false
	}
	result += "}"
	return result
}