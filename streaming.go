package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// StreamingToolHandler represents a handler that returns a channel of results
type StreamingToolHandler func(ctx context.Context, params json.RawMessage) (<-chan StreamingResult, error)

// StreamingResult represents a single result from a streaming tool
type StreamingResult struct {
	Data     interface{}            `json:"data"`
	Error    error                  `json:"error,omitempty"`
	Finished bool                   `json:"finished"`
	Progress *Progress              `json:"progress,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Progress represents progress information for long-running operations
type Progress struct {
	Current      int64         `json:"current"`
	Total        int64         `json:"total"`
	Percentage   float64       `json:"percentage"`
	Message      string        `json:"message,omitempty"`
	EstimatedETA time.Duration `json:"estimatedETA,omitempty"`
}

// StreamingTool represents a streaming MCP tool
type StreamingTool struct {
	*Tool
	BufferSize   int           // Size of the result buffer
	Timeout      time.Duration // Timeout for the entire operation
	ChunkTimeout time.Duration // Timeout between chunks
}

// StreamingSession manages a single streaming session
type StreamingSession struct {
	ID        string
	Tool      *StreamingTool
	Context   context.Context
	Cancel    context.CancelFunc
	Results   <-chan StreamingResult
	StartTime time.Time
	LastChunk time.Time
	mu        sync.RWMutex
	finished  bool
}

// StreamingManager manages active streaming sessions
type StreamingManager struct {
	sessions map[string]*StreamingSession
	mu       sync.RWMutex
	cleanup  chan string
}

// NewStreamingManager creates a new streaming manager
func NewStreamingManager() *StreamingManager {
	sm := &StreamingManager{
		sessions: make(map[string]*StreamingSession),
		cleanup:  make(chan string, 100),
	}

	// Start cleanup goroutine
	go sm.cleanupSessions()

	return sm
}

// StreamingTool registers a streaming tool
func (s *Server) StreamingTool(name, description string, handler StreamingToolHandler) *Server {
	return s.StreamingToolWithConfig(name, description, handler, StreamingConfig{})
}

// StreamingConfig configures streaming behavior
type StreamingConfig struct {
	BufferSize   int           // Buffer size for result channel
	Timeout      time.Duration // Total operation timeout
	ChunkTimeout time.Duration // Timeout between chunks
}

// DefaultStreamingConfig returns default streaming configuration
func DefaultStreamingConfig() StreamingConfig {
	return StreamingConfig{
		BufferSize:   100,
		Timeout:      5 * time.Minute,
		ChunkTimeout: 30 * time.Second,
	}
}

// StreamingToolWithConfig registers a streaming tool with custom configuration
func (s *Server) StreamingToolWithConfig(name, description string, handler StreamingToolHandler, config StreamingConfig) *Server {
	if config.BufferSize == 0 {
		config.BufferSize = DefaultStreamingConfig().BufferSize
	}
	if config.Timeout == 0 {
		config.Timeout = DefaultStreamingConfig().Timeout
	}
	if config.ChunkTimeout == 0 {
		config.ChunkTimeout = DefaultStreamingConfig().ChunkTimeout
	}

	// Create streaming tool
	streamingTool := &StreamingTool{
		Tool: &Tool{
			Name:        name,
			Description: description + " (streaming)",
			InputSchema: JSONSchema{Type: "object"}, // Simplified for streaming tools
		},
		BufferSize:   config.BufferSize,
		Timeout:      config.Timeout,
		ChunkTimeout: config.ChunkTimeout,
	}

	// Create wrapper that handles streaming
	wrapper := func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		sessionID := fmt.Sprintf("%s_%d", name, time.Now().UnixNano())

		// Create session context with timeout
		sessionCtx, cancel := context.WithTimeout(ctx, streamingTool.Timeout)

		// Start streaming handler
		resultChan, err := handler(sessionCtx, params)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to start streaming: %w", err)
		}

		// Create session
		session := &StreamingSession{
			ID:        sessionID,
			Tool:      streamingTool,
			Context:   sessionCtx,
			Cancel:    cancel,
			Results:   resultChan,
			StartTime: time.Now(),
			LastChunk: time.Now(),
		}

		// Register session
		if s.streamingManager == nil {
			s.streamingManager = NewStreamingManager()
		}
		s.streamingManager.RegisterSession(session)

		// Return session info for client to poll
		return map[string]interface{}{
			"type":      "streaming",
			"sessionId": sessionID,
			"status":    "started",
			"message":   "Streaming started. Use stream/poll to get results.",
		}, nil
	}

	// Register as regular tool
	s.tools[name] = streamingTool.Tool
	s.toolHandlers[name] = wrapper

	// Also register polling endpoints
	s.registerStreamingEndpoints()

	return s
}

// registerStreamingEndpoints registers endpoints for streaming management
func (s *Server) registerStreamingEndpoints() {
	// Only register once
	if _, exists := s.toolHandlers["stream/poll"]; exists {
		return
	}

	// Poll endpoint
	s.Tool("stream/poll", "Poll streaming results", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		var pollParams struct {
			SessionID string `json:"sessionId"`
			Count     int    `json:"count,omitempty"` // Number of results to fetch
		}

		if err := json.Unmarshal(params, &pollParams); err != nil {
			return nil, fmt.Errorf("invalid poll parameters: %w", err)
		}

		if pollParams.Count == 0 {
			pollParams.Count = 10 // Default
		}

		return s.streamingManager.PollSession(pollParams.SessionID, pollParams.Count)
	})

	// Cancel endpoint
	s.Tool("stream/cancel", "Cancel streaming session", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		var cancelParams struct {
			SessionID string `json:"sessionId"`
		}

		if err := json.Unmarshal(params, &cancelParams); err != nil {
			return nil, fmt.Errorf("invalid cancel parameters: %w", err)
		}

		return s.streamingManager.CancelSession(cancelParams.SessionID)
	})
}

// RegisterSession registers a new streaming session
func (sm *StreamingManager) RegisterSession(session *StreamingSession) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.sessions[session.ID] = session
}

// PollSession polls for results from a streaming session
func (sm *StreamingManager) PollSession(sessionID string, count int) (interface{}, error) {
	sm.mu.RLock()
	session, exists := sm.sessions[sessionID]
	sm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	// Check if session is finished
	session.mu.RLock()
	finished := session.finished
	session.mu.RUnlock()

	if finished {
		return map[string]interface{}{
			"sessionId": sessionID,
			"status":    "finished",
			"results":   []interface{}{},
		}, nil
	}

	// Collect results
	var results []StreamingResult
	timeout := time.NewTimer(session.Tool.ChunkTimeout)
	defer timeout.Stop()

	for i := 0; i < count; i++ {
		select {
		case result, ok := <-session.Results:
			if !ok {
				// Channel closed, session finished
				session.mu.Lock()
				session.finished = true
				session.mu.Unlock()

				sm.cleanup <- sessionID

				return map[string]interface{}{
					"sessionId": sessionID,
					"status":    "finished",
					"results":   results,
				}, nil
			}

			results = append(results, result)
			session.LastChunk = time.Now()

			// Check if this is the final result
			if result.Finished {
				session.mu.Lock()
				session.finished = true
				session.mu.Unlock()

				sm.cleanup <- sessionID

				return map[string]interface{}{
					"sessionId": sessionID,
					"status":    "finished",
					"results":   results,
				}, nil
			}

		case <-timeout.C:
			// Timeout waiting for next result
			if len(results) == 0 {
				return map[string]interface{}{
					"sessionId": sessionID,
					"status":    "timeout",
					"message":   "No results received within timeout",
				}, nil
			}
			// Return partial results
			return map[string]interface{}{
				"sessionId": sessionID,
				"status":    "partial",
				"results":   results,
				"hasMore":   true,
			}, nil

		case <-session.Context.Done():
			// Session cancelled or timed out
			session.mu.Lock()
			session.finished = true
			session.mu.Unlock()

			sm.cleanup <- sessionID

			return map[string]interface{}{
				"sessionId": sessionID,
				"status":    "cancelled",
				"results":   results,
				"error":     session.Context.Err().Error(),
			}, nil
		}
	}

	return map[string]interface{}{
		"sessionId": sessionID,
		"status":    "streaming",
		"results":   results,
		"hasMore":   true,
	}, nil
}

// CancelSession cancels a streaming session
func (sm *StreamingManager) CancelSession(sessionID string) (interface{}, error) {
	sm.mu.RLock()
	session, exists := sm.sessions[sessionID]
	sm.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	// Cancel the session
	session.Cancel()

	// Mark as finished
	session.mu.Lock()
	session.finished = true
	session.mu.Unlock()

	// Schedule cleanup
	sm.cleanup <- sessionID

	return map[string]interface{}{
		"sessionId": sessionID,
		"status":    "cancelled",
	}, nil
}

// cleanupSessions removes finished sessions
func (sm *StreamingManager) cleanupSessions() {
	for sessionID := range sm.cleanup {
		sm.mu.Lock()
		delete(sm.sessions, sessionID)
		sm.mu.Unlock()
	}
}

// Helper function to create progress
func NewProgress(current, total int64, message string) *Progress {
	percentage := float64(current) / float64(total) * 100
	if total == 0 {
		percentage = 0
	}

	return &Progress{
		Current:    current,
		Total:      total,
		Percentage: percentage,
		Message:    message,
	}
}

// Helper function to create streaming result
func NewStreamingResult(data interface{}) StreamingResult {
	return StreamingResult{
		Data:     data,
		Finished: false,
	}
}

// Helper function to create final streaming result
func NewFinalResult(data interface{}) StreamingResult {
	return StreamingResult{
		Data:     data,
		Finished: true,
	}
}

// Helper function to create error result
func NewErrorResult(err error) StreamingResult {
	return StreamingResult{
		Error:    err,
		Finished: true,
	}
}
