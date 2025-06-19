package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// RateLimitConfig represents rate limiting configuration
type RateLimitConfig struct {
	RequestsPerWindow int           `json:"requestsPerWindow"` // Number of requests allowed per window
	WindowSize        time.Duration `json:"windowSize"`        // Time window duration
	BurstSize         int           `json:"burstSize"`         // Burst allowance
	KeyFunc           RateLimitKeyFunc `json:"-"`             // Function to generate rate limit key
	SkipSuccessful    bool          `json:"skipSuccessful"`    // Skip counting successful requests
	SkipMethods       []string      `json:"skipMethods"`       // Methods to skip rate limiting
}

// RateLimitKeyFunc generates a key for rate limiting based on the request
type RateLimitKeyFunc func(ctx context.Context, req *Request) string

// RateLimitEntry represents a rate limit entry for a specific key
type RateLimitEntry struct {
	Count         int       `json:"count"`
	WindowStart   time.Time `json:"windowStart"`
	LastRequestAt time.Time `json:"lastRequestAt"`
	BurstCount    int       `json:"burstCount"`
}

// RateLimiter manages rate limiting state
type RateLimiter struct {
	config  RateLimitConfig
	entries map[string]*RateLimitEntry
	mu      sync.RWMutex
	cleanup chan string
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	if config.KeyFunc == nil {
		config.KeyFunc = DefaultRateLimitKeyFunc
	}
	
	rl := &RateLimiter{
		config:  config,
		entries: make(map[string]*RateLimitEntry),
		cleanup: make(chan string, 1000),
	}
	
	// Start cleanup goroutine
	go rl.cleanupExpiredEntries()
	
	return rl
}

// IsAllowed checks if a request is allowed under the rate limit
func (rl *RateLimiter) IsAllowed(ctx context.Context, req *Request) (bool, *RateLimitInfo, error) {
	// Check if method should be skipped
	for _, skipMethod := range rl.config.SkipMethods {
		if req.Method == skipMethod {
			return true, &RateLimitInfo{
				Allowed:   true,
				Remaining: rl.config.RequestsPerWindow,
				ResetTime: time.Now().Add(rl.config.WindowSize),
			}, nil
		}
	}
	
	key := rl.config.KeyFunc(ctx, req)
	now := time.Now()
	
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	entry, exists := rl.entries[key]
	if !exists {
		entry = &RateLimitEntry{
			Count:         0,
			WindowStart:   now,
			LastRequestAt: now,
			BurstCount:    0,
		}
		rl.entries[key] = entry
	}
	
	// Check if we need to reset the window
	if now.Sub(entry.WindowStart) >= rl.config.WindowSize {
		entry.Count = 0
		entry.WindowStart = now
		entry.BurstCount = 0
	}
	
	// Check burst allowance
	if entry.BurstCount >= rl.config.BurstSize {
		timeSinceLastRequest := now.Sub(entry.LastRequestAt)
		if timeSinceLastRequest < time.Second {
			info := &RateLimitInfo{
				Allowed:   false,
				Remaining: 0,
				ResetTime: entry.WindowStart.Add(rl.config.WindowSize),
				RetryAfter: time.Second - timeSinceLastRequest,
			}
			return false, info, nil
		}
		entry.BurstCount = 0
	}
	
	// Check window limit
	if entry.Count >= rl.config.RequestsPerWindow {
		info := &RateLimitInfo{
			Allowed:   false,
			Remaining: 0,
			ResetTime: entry.WindowStart.Add(rl.config.WindowSize),
			RetryAfter: entry.WindowStart.Add(rl.config.WindowSize).Sub(now),
		}
		return false, info, nil
	}
	
	// Request is allowed
	entry.Count++
	entry.BurstCount++
	entry.LastRequestAt = now
	
	info := &RateLimitInfo{
		Allowed:   true,
		Remaining: rl.config.RequestsPerWindow - entry.Count,
		ResetTime: entry.WindowStart.Add(rl.config.WindowSize),
	}
	
	return true, info, nil
}

// RecordResponse records the response for potential rate limit adjustments
func (rl *RateLimiter) RecordResponse(ctx context.Context, req *Request, resp *Response) {
	// If configured to skip successful requests, remove them from count
	if rl.config.SkipSuccessful && resp.Error == nil {
		key := rl.config.KeyFunc(ctx, req)
		
		rl.mu.Lock()
		defer rl.mu.Unlock()
		
		if entry, exists := rl.entries[key]; exists && entry.Count > 0 {
			entry.Count--
		}
	}
}

// GetStats returns rate limiting statistics
func (rl *RateLimiter) GetStats() map[string]*RateLimitEntry {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	
	stats := make(map[string]*RateLimitEntry)
	for key, entry := range rl.entries {
		// Create a copy to avoid race conditions
		stats[key] = &RateLimitEntry{
			Count:         entry.Count,
			WindowStart:   entry.WindowStart,
			LastRequestAt: entry.LastRequestAt,
			BurstCount:    entry.BurstCount,
		}
	}
	return stats
}

// cleanupExpiredEntries removes expired rate limit entries
func (rl *RateLimiter) cleanupExpiredEntries() {
	ticker := time.NewTicker(rl.config.WindowSize)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			rl.performCleanup()
		case key := <-rl.cleanup:
			rl.mu.Lock()
			delete(rl.entries, key)
			rl.mu.Unlock()
		}
	}
}

func (rl *RateLimiter) performCleanup() {
	now := time.Now()
	expiredKeys := make([]string, 0)
	
	rl.mu.RLock()
	for key, entry := range rl.entries {
		// Remove entries that haven't been accessed for 2 window periods
		if now.Sub(entry.LastRequestAt) > 2*rl.config.WindowSize {
			expiredKeys = append(expiredKeys, key)
		}
	}
	rl.mu.RUnlock()
	
	if len(expiredKeys) > 0 {
		rl.mu.Lock()
		for _, key := range expiredKeys {
			delete(rl.entries, key)
		}
		rl.mu.Unlock()
	}
}

// RateLimitInfo contains information about rate limiting status
type RateLimitInfo struct {
	Allowed    bool          `json:"allowed"`
	Remaining  int           `json:"remaining"`
	ResetTime  time.Time     `json:"resetTime"`
	RetryAfter time.Duration `json:"retryAfter"`
}

// RateLimitMiddleware creates rate limiting middleware
func RateLimitMiddleware(config RateLimitConfig) MiddlewareFunc {
	limiter := NewRateLimiter(config)
	
	return func(next RequestHandler) RequestHandler {
		return func(ctx context.Context, req *Request) *Response {
			// Check if request is allowed
			allowed, info, err := limiter.IsAllowed(ctx, req)
			if err != nil {
				return &Response{
					JSONRPC: "2.0",
					ID:      req.ID,
					Error: &Error{
						Code:    500,
						Message: fmt.Sprintf("Rate limiting error: %v", err),
					},
				}
			}
			
			if !allowed {
				// Add rate limit info to middleware context
				mctx := GetMiddlewareContext(ctx)
				mctx.Set("rate_limit_exceeded", true)
				mctx.Set("rate_limit_info", info)
				
				return &Response{
					JSONRPC: "2.0",
					ID:      req.ID,
					Error: &Error{
						Code:    429,
						Message: "Rate limit exceeded",
						Data: map[string]interface{}{
							"remaining":  info.Remaining,
							"resetTime":  info.ResetTime.Unix(),
							"retryAfter": info.RetryAfter.Seconds(),
						},
					},
				}
			}
			
			// Add rate limit info to middleware context
			mctx := GetMiddlewareContext(ctx)
			mctx.Set("rate_limit_info", info)
			
			// Process request
			response := next(ctx, req)
			
			// Record response for potential adjustments
			limiter.RecordResponse(ctx, req, response)
			
			return response
		}
	}
}

// Built-in key functions

// DefaultRateLimitKeyFunc generates a key based on user ID or IP
func DefaultRateLimitKeyFunc(ctx context.Context, req *Request) string {
	mctx := GetMiddlewareContext(ctx)
	
	// Try to get user ID first
	if user := GetCurrentUser(ctx); user != nil {
		return fmt.Sprintf("user:%s", user.ID)
	}
	
	// Try to get user ID from middleware context
	if userID := mctx.GetString("user_id"); userID != "" {
		return fmt.Sprintf("user:%s", userID)
	}
	
	// Try to get IP address
	if ip := mctx.GetString("client_ip"); ip != "" {
		return fmt.Sprintf("ip:%s", ip)
	}
	
	// Fallback to request ID or method
	if req.ID != nil {
		return fmt.Sprintf("request:%v", req.ID)
	}
	
	return fmt.Sprintf("method:%s", req.Method)
}

// IPBasedRateLimitKeyFunc generates a key based on IP address
func IPBasedRateLimitKeyFunc(ctx context.Context, req *Request) string {
	mctx := GetMiddlewareContext(ctx)
	ip := mctx.GetString("client_ip")
	if ip == "" {
		ip = "unknown"
	}
	return fmt.Sprintf("ip:%s", ip)
}

// UserBasedRateLimitKeyFunc generates a key based on user ID
func UserBasedRateLimitKeyFunc(ctx context.Context, req *Request) string {
	if user := GetCurrentUser(ctx); user != nil {
		return fmt.Sprintf("user:%s", user.ID)
	}
	
	mctx := GetMiddlewareContext(ctx)
	userID := mctx.GetString("user_id")
	if userID == "" {
		userID = "anonymous"
	}
	return fmt.Sprintf("user:%s", userID)
}

// MethodBasedRateLimitKeyFunc generates a key based on method and user
func MethodBasedRateLimitKeyFunc(ctx context.Context, req *Request) string {
	userKey := "anonymous"
	if user := GetCurrentUser(ctx); user != nil {
		userKey = user.ID
	} else {
		mctx := GetMiddlewareContext(ctx)
		if userID := mctx.GetString("user_id"); userID != "" {
			userKey = userID
		}
	}
	
	return fmt.Sprintf("method:%s:user:%s", req.Method, userKey)
}

// PerToolRateLimitMiddleware creates rate limiting middleware for specific tools
func PerToolRateLimitMiddleware(toolLimits map[string]RateLimitConfig) MiddlewareFunc {
	limiters := make(map[string]*RateLimiter)
	for tool, config := range toolLimits {
		limiters[tool] = NewRateLimiter(config)
	}
	
	return func(next RequestHandler) RequestHandler {
		return func(ctx context.Context, req *Request) *Response {
			// Only apply to tool calls
			if req.Method != "tools/call" {
				return next(ctx, req)
			}
			
			// Extract tool name from params
			toolName := extractToolNameFromParams(req.Params)
			if toolName == "" {
				return next(ctx, req)
			}
			
			// Check if we have a limiter for this tool
			limiter, exists := limiters[toolName]
			if !exists {
				return next(ctx, req)
			}
			
			// Apply rate limiting
			allowed, info, err := limiter.IsAllowed(ctx, req)
			if err != nil {
				return &Response{
					JSONRPC: "2.0",
					ID:      req.ID,
					Error: &Error{
						Code:    500,
						Message: fmt.Sprintf("Rate limiting error: %v", err),
					},
				}
			}
			
			if !allowed {
				return &Response{
					JSONRPC: "2.0",
					ID:      req.ID,
					Error: &Error{
						Code:    429,
						Message: fmt.Sprintf("Rate limit exceeded for tool '%s'", toolName),
						Data: map[string]interface{}{
							"tool":       toolName,
							"remaining":  info.Remaining,
							"resetTime":  info.ResetTime.Unix(),
							"retryAfter": info.RetryAfter.Seconds(),
						},
					},
				}
			}
			
			// Add rate limit info to middleware context
			mctx := GetMiddlewareContext(ctx)
			mctx.Set("tool_rate_limit_info", map[string]interface{}{
				"tool": toolName,
				"info": info,
			})
			
			// Process request
			response := next(ctx, req)
			
			// Record response
			limiter.RecordResponse(ctx, req, response)
			
			return response
		}
	}
}

// Helper function to extract tool name from params
func extractToolNameFromParams(params []byte) string {
	if params == nil {
		return ""
	}
	
	var paramsMap map[string]interface{}
	if err := json.Unmarshal(params, &paramsMap); err != nil {
		return ""
	}
	
	if name, exists := paramsMap["name"]; exists {
		if nameStr, ok := name.(string); ok {
			return nameStr
		}
	}
	
	return ""
}

// Adaptive rate limiting middleware that adjusts limits based on system load
func AdaptiveRateLimitMiddleware(baseConfig RateLimitConfig, loadFunc func() float64) MiddlewareFunc {
	return func(next RequestHandler) RequestHandler {
		return func(ctx context.Context, req *Request) *Response {
			// Get current system load
			load := loadFunc()
			
			// Adjust rate limit based on load
			adjustedConfig := baseConfig
			if load > 0.8 { // High load
				adjustedConfig.RequestsPerWindow = int(float64(baseConfig.RequestsPerWindow) * 0.5)
			} else if load > 0.6 { // Medium load
				adjustedConfig.RequestsPerWindow = int(float64(baseConfig.RequestsPerWindow) * 0.75)
			}
			
			// Create temporary limiter with adjusted config
			tempLimiter := NewRateLimiter(adjustedConfig)
			
			// Check if request is allowed
			allowed, info, err := tempLimiter.IsAllowed(ctx, req)
			if err != nil {
				return &Response{
					JSONRPC: "2.0",
					ID:      req.ID,
					Error: &Error{
						Code:    500,
						Message: fmt.Sprintf("Adaptive rate limiting error: %v", err),
					},
				}
			}
			
			if !allowed {
				return &Response{
					JSONRPC: "2.0",
					ID:      req.ID,
					Error: &Error{
						Code:    429,
						Message: "Adaptive rate limit exceeded due to high system load",
						Data: map[string]interface{}{
							"systemLoad": load,
							"remaining":  info.Remaining,
							"resetTime":  info.ResetTime.Unix(),
							"retryAfter": info.RetryAfter.Seconds(),
						},
					},
				}
			}
			
			return next(ctx, req)
		}
	}
}