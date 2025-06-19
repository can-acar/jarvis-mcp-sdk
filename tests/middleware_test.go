package tests

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"testing"
	"time"

	mcp "github.com/jarvis-mcp/jarvis-mcp-sdk"
)

func TestMiddlewareChain(t *testing.T) {
	// Create server with middleware
	server := mcp.NewServer("test", "1.0.0")
	
	config := mcp.MiddlewareConfig{
		Enabled: true,
		Order:   []string{"logging", "metrics", "auth"},
	}
	
	server.EnableMiddleware(config)
	
	// Add middleware
	logger := mcp.NewBasicLogger(log.New(os.Stdout, "[TEST] ", log.LstdFlags))
	server.Use("logging", mcp.LoggingMiddleware(logger))
	
	collector := mcp.NewMemoryMetricsCollector()
	server.Use("metrics", mcp.MetricsMiddleware(collector))
	
	bearerConfig := mcp.BearerTokenConfig{
		Tokens: []string{"test-token"},
	}
	server.Use("auth", mcp.BearerTokenMiddleware(bearerConfig))
	
	// Register test tool
	server.Tool("test", "Test tool", func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		return "test result", nil
	})
	
	// Test request with valid token
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"test","arguments":{},"_auth":"Bearer test-token"}`),
	}
	
	ctx := context.Background()
	response := server.HandleRequestWithMiddleware(ctx, req)
	
	if response.Error != nil {
		t.Errorf("Expected successful response, got error: %v", response.Error)
	}
	
	// Check metrics were collected
	metrics := collector.GetMetrics()
	if counters, ok := metrics["counters"].(map[string]int64); ok {
		if counters["mcp_requests_total{method:tools/call}"] == 0 {
			t.Error("Expected request counter to be incremented")
		}
	}
}

func TestValidationMiddleware(t *testing.T) {
	config := mcp.ValidationConfig{
		Enabled:      true,
		ValidateJSON: true,
		MaxDepth:     3,
		CustomRules: map[string]mcp.ValidationRule{
			"arguments.value": {
				Field:   "arguments.value",
				Type:    "range",
				Value:   map[string]interface{}{"min": 1.0, "max": 10.0},
				Message: "Value must be between 1 and 10",
			},
		},
	}
	
	validator := mcp.NewRequestValidator(config)
	
	// Test valid request
	validReq := &mcp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"test","arguments":{"value":5}}`),
	}
	
	result := validator.ValidateRequest(validReq)
	if !result.Valid {
		t.Errorf("Expected valid request, got errors: %v", result.Errors)
	}
	
	// Test invalid request (value out of range)
	invalidReq := &mcp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"test","arguments":{"value":15}}`),
	}
	
	result = validator.ValidateRequest(invalidReq)
	if result.Valid {
		t.Error("Expected invalid request due to range validation")
	}
	
	if len(result.Errors) == 0 {
		t.Error("Expected validation errors")
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	config := mcp.RateLimitConfig{
		RequestsPerWindow: 2,
		WindowSize:        time.Minute,  // Longer window to avoid timing issues
		BurstSize:         10,           // High burst to avoid burst limiting
		KeyFunc:          func(ctx context.Context, req *mcp.Request) string { return "test-user" },
	}
	
	limiter := mcp.NewRateLimiter(config)
	
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
	}
	
	ctx := context.Background()
	
	// First request should be allowed
	allowed, _, err := limiter.IsAllowed(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !allowed {
		t.Error("First request should be allowed")
	}
	
	// Second request should be allowed (within window limit)
	allowed, _, err = limiter.IsAllowed(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !allowed {
		t.Error("Second request should be allowed")
	}
	
	// Third request should be rejected (exceeds window limit)
	allowed, _, err = limiter.IsAllowed(ctx, req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if allowed {
		t.Error("Third request should be rejected")
	}
}

func TestCircuitBreakerMiddleware(t *testing.T) {
	config := mcp.CircuitBreakerConfig{
		FailureThreshold: 1,  // Simplified to 1 failure
		SuccessThreshold: 1,
		Timeout:          100 * time.Millisecond,
		MaxRequests:      3,
		SlidingWindow:    time.Second,
	}
	
	breaker := mcp.NewCircuitBreaker("test", config)
	
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
	}
	
	ctx := context.Background()
	
	// Create a handler that fails
	failingHandler := func(ctx context.Context, req *mcp.Request) *mcp.Response {
		return &mcp.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &mcp.Error{
				Code:    500,
				Message: "Internal server error",
			},
		}
	}
	
	// First failure should open the circuit immediately
	response := breaker.Execute(ctx, req, failingHandler)
	if response.Error == nil {
		t.Error("Expected error response")
	}
	
	stats := breaker.GetStats()
	if stats.State != mcp.StateOpen {
		t.Errorf("Expected circuit to be open after first failure, got %s", stats.State)
	}
	
	// Subsequent requests should be rejected immediately
	response = breaker.Execute(ctx, req, failingHandler)
	if response.Error == nil {
		t.Error("Expected circuit breaker error")
	}
	if response.Error.Code != 503 {
		t.Errorf("Expected 503 error code, got %d", response.Error.Code)
	}
}

func TestJWTMiddleware(t *testing.T) {
	config := mcp.JWTConfig{
		Secret:        "test-secret",
		Algorithm:     "HS256",
		SkipClaimsExp: true, // Skip expiry for testing
	}
	
	// Use a pre-generated valid JWT token for testing
	// This token contains: {"sub":"test-user","name":"Test User","roles":["user"],"exp":9999999999}
	token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ0ZXN0LXVzZXIiLCJuYW1lIjoiVGVzdCBVc2VyIiwicm9sZXMiOlsidXNlciJdLCJleHAiOjk5OTk5OTk5OTl9.Kf_jiJKEF-TaVhEamzxBMX1b0dLEHkLpd1fZEec6Usk"
	
	// Test valid token
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"_auth":"Bearer ` + token + `"}`),
	}
	
	middleware := mcp.JWTMiddleware(config)
	
	// Create a simple next handler
	nextHandler := func(ctx context.Context, req *mcp.Request) *mcp.Response {
		// Check if user was set in context
		if user := mcp.GetCurrentUser(ctx); user != nil {
			return &mcp.Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  map[string]interface{}{"user": user.ID},
			}
		}
		
		// Also check middleware context directly
		mctx := mcp.GetMiddlewareContext(ctx)
		if userInterface, exists := mctx.Get("user"); exists {
			if user, ok := userInterface.(*mcp.User); ok {
				return &mcp.Response{
					JSONRPC: "2.0",
					ID:      req.ID,
					Result:  map[string]interface{}{"user": user.ID},
				}
			}
		}
		
		return &mcp.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &mcp.Error{Code: 401, Message: "No user found"},
		}
	}
	
	handler := middleware(nextHandler)
	ctx := context.Background()
	
	// Create middleware context like the server would
	mctx := mcp.NewMiddlewareContext()
	ctx = context.WithValue(ctx, "middleware", mctx)
	
	response := handler(ctx, req)
	
	if response.Error != nil {
		t.Errorf("Expected successful response, got error: %v", response.Error)
	}
	
	if result, ok := response.Result.(map[string]interface{}); ok {
		if user, exists := result["user"]; !exists || user != "test-user" {
			t.Errorf("Expected user 'test-user', got %v", user)
		}
	} else {
		t.Error("Expected result with user information")
	}
}

func TestMemoryMetricsCollector(t *testing.T) {
	collector := mcp.NewMemoryMetricsCollector()
	
	// Test counter
	labels := map[string]string{"method": "test"}
	collector.IncrementCounter("test_counter", labels)
	collector.IncrementCounter("test_counter", labels)
	
	// Test duration
	collector.RecordDuration("test_duration", 100*time.Millisecond, labels)
	collector.RecordDuration("test_duration", 200*time.Millisecond, labels)
	
	// Test gauge
	collector.RecordGauge("test_gauge", 42.5, labels)
	
	metrics := collector.GetMetrics()
	
	// Check counters
	if counters, ok := metrics["counters"].(map[string]int64); ok {
		expected := "test_counter{method:test}"
		if counters[expected] != 2 {
			t.Errorf("Expected counter value 2, got %d", counters[expected])
		}
	} else {
		t.Error("Expected counters in metrics")
	}
	
	// Check durations
	if durations, ok := metrics["durations"].(map[string]map[string]interface{}); ok {
		expected := "test_duration{method:test}"
		if summary, exists := durations[expected]; exists {
			if count, ok := summary["count"].(int); !ok || count != 2 {
				t.Errorf("Expected duration count 2, got %v", summary["count"])
			}
		} else {
			t.Error("Expected duration summary in metrics")
		}
	} else {
		t.Error("Expected durations in metrics")
	}
	
	// Check gauges
	if gauges, ok := metrics["gauges"].(map[string]float64); ok {
		expected := "test_gauge{method:test}"
		if gauges[expected] != 42.5 {
			t.Errorf("Expected gauge value 42.5, got %f", gauges[expected])
		}
	} else {
		t.Error("Expected gauges in metrics")
	}
}

func TestMiddlewareContext(t *testing.T) {
	mctx := mcp.NewMiddlewareContext()
	
	// Test basic set/get
	mctx.Set("key1", "value1")
	mctx.Set("key2", 42)
	
	if value := mctx.GetString("key1"); value != "value1" {
		t.Errorf("Expected 'value1', got '%s'", value)
	}
	
	if value, exists := mctx.Get("key2"); !exists || value != 42 {
		t.Errorf("Expected 42, got %v (exists: %v)", value, exists)
	}
	
	// Test non-existent key
	if value := mctx.GetString("nonexistent"); value != "" {
		t.Errorf("Expected empty string for non-existent key, got '%s'", value)
	}
	
	// Test request ID generation
	if mctx.RequestID == "" {
		t.Error("Expected non-empty request ID")
	}
}

func BenchmarkMiddlewareChain(b *testing.B) {
	server := mcp.NewServer("bench", "1.0.0")
	
	config := mcp.MiddlewareConfig{
		Enabled: true,
		Order:   []string{"logging", "metrics", "validation"},
	}
	
	server.EnableMiddleware(config)
	
	// Add lightweight middleware
	logger := mcp.NewBasicLogger(log.New(os.Stdout, "", 0))
	server.Use("logging", mcp.LoggingMiddleware(logger))
	
	collector := mcp.NewMemoryMetricsCollector()
	server.Use("metrics", mcp.MetricsMiddleware(collector))
	
	validationConfig := mcp.ValidationConfig{Enabled: true}
	server.Use("validation", mcp.ValidationMiddleware(validationConfig))
	
	server.Tool("bench", "Benchmark tool", func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		return "result", nil
	})
	
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"bench","arguments":{}}`),
	}
	
	ctx := context.Background()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		server.HandleRequestWithMiddleware(ctx, req)
	}
}