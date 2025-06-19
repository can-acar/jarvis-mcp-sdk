package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CircuitBreakerState represents the state of a circuit breaker
type CircuitBreakerState int

const (
	StateClosed CircuitBreakerState = iota
	StateHalfOpen
	StateOpen
)

func (s CircuitBreakerState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateHalfOpen:
		return "half-open"
	case StateOpen:
		return "open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig represents circuit breaker configuration
type CircuitBreakerConfig struct {
	FailureThreshold   int           `json:"failureThreshold"`   // Number of failures to open circuit
	SuccessThreshold   int           `json:"successThreshold"`   // Number of successes to close circuit
	Timeout            time.Duration `json:"timeout"`            // Time to wait before half-open
	MaxRequests        int           `json:"maxRequests"`        // Max requests in half-open state
	SlidingWindow      time.Duration `json:"slidingWindow"`      // Time window for failure counting
	FailureDetector    func(*Response) bool `json:"-"`         // Custom failure detection
	OnStateChange      func(string, CircuitBreakerState, CircuitBreakerState) `json:"-"` // State change callback
	PerMethodBreakers  bool          `json:"perMethodBreakers"`  // Create separate breakers per method
	PerUserBreakers    bool          `json:"perUserBreakers"`    // Create separate breakers per user
}

// CircuitBreakerStats represents circuit breaker statistics
type CircuitBreakerStats struct {
	State               CircuitBreakerState `json:"state"`
	FailureCount        int                 `json:"failureCount"`
	SuccessCount        int                 `json:"successCount"`
	LastFailureTime     time.Time           `json:"lastFailureTime"`
	LastSuccessTime     time.Time           `json:"lastSuccessTime"`
	NextRetryTime       time.Time           `json:"nextRetryTime"`
	TotalRequests       int64               `json:"totalRequests"`
	FailedRequests      int64               `json:"failedRequests"`
	SuccessfulRequests  int64               `json:"successfulRequests"`
	RejectedRequests    int64               `json:"rejectedRequests"`
	StateChangeCount    int                 `json:"stateChangeCount"`
	LastStateChangeTime time.Time           `json:"lastStateChangeTime"`
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	config        CircuitBreakerConfig
	stats         CircuitBreakerStats
	mu            sync.RWMutex
	failureWindow []time.Time // Sliding window of failure times
	name          string
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(name string, config CircuitBreakerConfig) *CircuitBreaker {
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = 5
	}
	if config.SuccessThreshold <= 0 {
		config.SuccessThreshold = 2
	}
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}
	if config.MaxRequests <= 0 {
		config.MaxRequests = 3
	}
	if config.SlidingWindow <= 0 {
		config.SlidingWindow = 60 * time.Second
	}
	if config.FailureDetector == nil {
		config.FailureDetector = defaultFailureDetector
	}

	return &CircuitBreaker{
		config:        config,
		stats:         CircuitBreakerStats{State: StateClosed},
		failureWindow: make([]time.Time, 0),
		name:          name,
	}
}

// Execute executes a request through the circuit breaker
func (cb *CircuitBreaker) Execute(ctx context.Context, req *Request, next RequestHandler) *Response {
	cb.mu.Lock()
	cb.stats.TotalRequests++

	// Check if circuit is open
	if cb.stats.State == StateOpen {
		if time.Now().Before(cb.stats.NextRetryTime) {
			cb.stats.RejectedRequests++
			stats := cb.getPublicStatsUnsafe() // Use unsafe version while locked
			cb.mu.Unlock()
			return createCircuitBreakerErrorResponse(req.ID, "Circuit breaker is open", stats)
		}
		// Time to try half-open
		cb.changeState(StateHalfOpen)
	}

	// Check if we're in half-open and have reached max requests
	if cb.stats.State == StateHalfOpen && cb.stats.SuccessCount >= cb.config.MaxRequests {
		cb.stats.RejectedRequests++
		stats := cb.getPublicStatsUnsafe() // Use unsafe version while locked
		cb.mu.Unlock()
		return createCircuitBreakerErrorResponse(req.ID, "Circuit breaker is half-open and at capacity", stats)
	}

	// Execute the request
	cb.mu.Unlock() // Unlock during execution
	response := next(ctx, req)
	cb.mu.Lock() // Re-lock for state updates

	// Check if response indicates failure
	isFailure := cb.config.FailureDetector(response)

	if isFailure {
		cb.recordFailure()
	} else {
		cb.recordSuccess()
	}

	cb.mu.Unlock()
	return response
}

// recordFailure records a failure and potentially opens the circuit
func (cb *CircuitBreaker) recordFailure() {
	now := time.Now()
	cb.stats.FailedRequests++
	cb.stats.LastFailureTime = now
	cb.failureWindow = append(cb.failureWindow, now)

	// Clean old failures from sliding window
	cb.cleanFailureWindow(now)

	switch cb.stats.State {
	case StateClosed:
		if len(cb.failureWindow) >= cb.config.FailureThreshold {
			cb.changeState(StateOpen)
		}
	case StateHalfOpen:
		cb.changeState(StateOpen)
	}
}

// recordSuccess records a successful request
func (cb *CircuitBreaker) recordSuccess() {
	cb.stats.SuccessfulRequests++
	cb.stats.LastSuccessTime = time.Now()

	switch cb.stats.State {
	case StateHalfOpen:
		cb.stats.SuccessCount++
		if cb.stats.SuccessCount >= cb.config.SuccessThreshold {
			cb.changeState(StateClosed)
		}
	case StateClosed:
		// Reset failure count on success in closed state
		cb.stats.FailureCount = 0
		cb.failureWindow = cb.failureWindow[:0]
	}
}

// changeState changes the circuit breaker state
func (cb *CircuitBreaker) changeState(newState CircuitBreakerState) {
	oldState := cb.stats.State
	cb.stats.State = newState
	cb.stats.StateChangeCount++
	cb.stats.LastStateChangeTime = time.Now()

	switch newState {
	case StateOpen:
		cb.stats.NextRetryTime = time.Now().Add(cb.config.Timeout)
		cb.stats.SuccessCount = 0
	case StateHalfOpen:
		cb.stats.SuccessCount = 0
	case StateClosed:
		cb.stats.FailureCount = 0
		cb.stats.SuccessCount = 0
		cb.failureWindow = cb.failureWindow[:0]
	}

	// Call state change callback if provided
	if cb.config.OnStateChange != nil {
		cb.config.OnStateChange(cb.name, oldState, newState)
	}
}

// cleanFailureWindow removes failures outside the sliding window
func (cb *CircuitBreaker) cleanFailureWindow(now time.Time) {
	cutoff := now.Add(-cb.config.SlidingWindow)
	validFailures := cb.failureWindow[:0]

	for _, failureTime := range cb.failureWindow {
		if failureTime.After(cutoff) {
			validFailures = append(validFailures, failureTime)
		}
	}

	cb.failureWindow = validFailures
	cb.stats.FailureCount = len(cb.failureWindow)
}

// GetStats returns current circuit breaker statistics
func (cb *CircuitBreaker) GetStats() CircuitBreakerStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	stats := cb.stats
	stats.FailureCount = len(cb.failureWindow)
	return stats
}

// getPublicStats returns stats safe for external consumption
func (cb *CircuitBreaker) getPublicStats() map[string]interface{} {
	stats := cb.GetStats()
	return map[string]interface{}{
		"state":            stats.State.String(),
		"failureCount":     stats.FailureCount,
		"totalRequests":    stats.TotalRequests,
		"failedRequests":   stats.FailedRequests,
		"rejectedRequests": stats.RejectedRequests,
		"nextRetryTime":    stats.NextRetryTime.Unix(),
	}
}

// getPublicStatsUnsafe returns stats without acquiring locks (must be called while holding lock)
func (cb *CircuitBreaker) getPublicStatsUnsafe() map[string]interface{} {
	return map[string]interface{}{
		"state":            cb.stats.State.String(),
		"failureCount":     len(cb.failureWindow),
		"totalRequests":    cb.stats.TotalRequests,
		"failedRequests":   cb.stats.FailedRequests,
		"rejectedRequests": cb.stats.RejectedRequests,
		"nextRetryTime":    cb.stats.NextRetryTime.Unix(),
	}
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.changeState(StateClosed)
}

// CircuitBreakerManager manages multiple circuit breakers
type CircuitBreakerManager struct {
	breakers map[string]*CircuitBreaker
	config   CircuitBreakerConfig
	mu       sync.RWMutex
}

// NewCircuitBreakerManager creates a new circuit breaker manager
func NewCircuitBreakerManager(config CircuitBreakerConfig) *CircuitBreakerManager {
	return &CircuitBreakerManager{
		breakers: make(map[string]*CircuitBreaker),
		config:   config,
	}
}

// GetBreaker gets or creates a circuit breaker for the given key
func (cbm *CircuitBreakerManager) GetBreaker(key string) *CircuitBreaker {
	cbm.mu.RLock()
	breaker, exists := cbm.breakers[key]
	cbm.mu.RUnlock()

	if exists {
		return breaker
	}

	cbm.mu.Lock()
	defer cbm.mu.Unlock()

	// Double-check pattern
	if breaker, exists := cbm.breakers[key]; exists {
		return breaker
	}

	breaker = NewCircuitBreaker(key, cbm.config)
	cbm.breakers[key] = breaker
	return breaker
}

// GetStats returns statistics for all circuit breakers
func (cbm *CircuitBreakerManager) GetStats() map[string]CircuitBreakerStats {
	cbm.mu.RLock()
	defer cbm.mu.RUnlock()

	stats := make(map[string]CircuitBreakerStats)
	for key, breaker := range cbm.breakers {
		stats[key] = breaker.GetStats()
	}
	return stats
}

// Reset resets all circuit breakers
func (cbm *CircuitBreakerManager) Reset() {
	cbm.mu.RLock()
	defer cbm.mu.RUnlock()

	for _, breaker := range cbm.breakers {
		breaker.Reset()
	}
}

// CircuitBreakerMiddleware creates circuit breaker middleware
func CircuitBreakerMiddleware(config CircuitBreakerConfig) MiddlewareFunc {
	manager := NewCircuitBreakerManager(config)

	return func(next RequestHandler) RequestHandler {
		return func(ctx context.Context, req *Request) *Response {
			// Determine circuit breaker key
			key := "default"
			
			if config.PerMethodBreakers {
				key = fmt.Sprintf("method:%s", req.Method)
			}
			
			if config.PerUserBreakers {
				mctx := GetMiddlewareContext(ctx)
				userID := "anonymous"
				if user := GetCurrentUser(ctx); user != nil {
					userID = user.ID
				} else if uid := mctx.GetString("user_id"); uid != "" {
					userID = uid
				}
				
				if config.PerMethodBreakers {
					key = fmt.Sprintf("method:%s:user:%s", req.Method, userID)
				} else {
					key = fmt.Sprintf("user:%s", userID)
				}
			}

			// Get circuit breaker for this key
			breaker := manager.GetBreaker(key)

			// Add circuit breaker info to middleware context
			mctx := GetMiddlewareContext(ctx)
			mctx.Set("circuit_breaker_key", key)
			mctx.Set("circuit_breaker_stats", breaker.getPublicStats())

			// Execute through circuit breaker
			return breaker.Execute(ctx, req, next)
		}
	}
}

// MethodSpecificCircuitBreakerMiddleware creates method-specific circuit breakers
func MethodSpecificCircuitBreakerMiddleware(methodConfigs map[string]CircuitBreakerConfig) MiddlewareFunc {
	managers := make(map[string]*CircuitBreakerManager)
	for method, config := range methodConfigs {
		managers[method] = NewCircuitBreakerManager(config)
	}

	return func(next RequestHandler) RequestHandler {
		return func(ctx context.Context, req *Request) *Response {
			// Check if we have a specific config for this method
			manager, exists := managers[req.Method]
			if !exists {
				return next(ctx, req)
			}

			// Use method-specific circuit breaker
			breaker := manager.GetBreaker(req.Method)
			
			// Add info to middleware context
			mctx := GetMiddlewareContext(ctx)
			mctx.Set("circuit_breaker_method", req.Method)
			mctx.Set("circuit_breaker_stats", breaker.getPublicStats())

			return breaker.Execute(ctx, req, next)
		}
	}
}

// Default failure detector
func defaultFailureDetector(response *Response) bool {
	if response == nil {
		return true
	}
	
	// Consider responses with errors as failures
	if response.Error != nil {
		// Don't consider client errors (4xx) as circuit breaker failures
		// Only server errors (5xx) and some specific errors
		if response.Error.Code >= 500 {
			return true
		}
		
		// Consider timeout and availability errors as failures
		if response.Error.Code == 408 || response.Error.Code == 503 || response.Error.Code == 504 {
			return true
		}
	}
	
	return false
}

// Helper function to create circuit breaker error response
func createCircuitBreakerErrorResponse(requestID interface{}, message string, stats map[string]interface{}) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      requestID,
		Error: &Error{
			Code:    503,
			Message: message,
			Data: map[string]interface{}{
				"circuitBreaker": stats,
				"retryAfter":     stats["nextRetryTime"],
			},
		},
	}
}

// CustomFailureDetector creates a custom failure detector
func CustomFailureDetector(detector func(*Response) bool) func(*Response) bool {
	return detector
}

// TimeoutFailureDetector creates a failure detector based on response time
func TimeoutFailureDetector(maxDuration time.Duration) func(*Response) bool {
	return func(response *Response) bool {
		// This would need to be implemented with response timing
		// For now, use default detection plus error code checking
		return defaultFailureDetector(response)
	}
}

// ErrorCodeFailureDetector creates a failure detector based on specific error codes
func ErrorCodeFailureDetector(failureCodes []int) func(*Response) bool {
	codeMap := make(map[int]bool)
	for _, code := range failureCodes {
		codeMap[code] = true
	}

	return func(response *Response) bool {
		if response == nil || response.Error == nil {
			return false
		}
		return codeMap[response.Error.Code]
	}
}