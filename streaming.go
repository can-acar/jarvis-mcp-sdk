package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"
)

// StreamingToolHandler represents a handler that returns a channel of results
type StreamingToolHandler func(ctx context.Context, params json.RawMessage) (<-chan StreamingResult, error)

// StreamingResult represents a single result from a streaming tool
type StreamingResult struct {
	Type      string      `json:"type"`      // "data", "error", "progress", "status", "heartbeat"
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
	Finished  bool        `json:"finished"`
	Progress  *Progress   `json:"progress,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp int64       `json:"timestamp"`
	Sequence  int64       `json:"sequence"`  // Sequential number for ordering
	ChunkID   string      `json:"chunkId,omitempty"`   // For chunked data
	Status    string      `json:"status,omitempty"`    // "running", "finished", "cancelled", "error"
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
	ID               string                 `json:"id"`
	ToolName         string                 `json:"toolName"`
	Arguments        map[string]interface{} `json:"arguments"`
	Tool             *StreamingTool         `json:"-"`
	Context          context.Context        `json:"-"`
	Cancel           context.CancelFunc     `json:"-"`
	Results          <-chan StreamingResult `json:"-"`
	StartTime        time.Time              `json:"startTime"`
	LastActivity     time.Time              `json:"lastActivity"`
	LastChunk        time.Time              `json:"lastChunk"`
	Status           string                 `json:"status"`  // "active", "paused", "finished", "cancelled", "error"
	Progress         float64                `json:"progress"`
	TotalItems       int64                  `json:"totalItems"`
	ProcessedItems   int64                  `json:"processedItems"`
	mu               sync.RWMutex           `json:"-"`
	finished         bool                   `json:"-"`
	
	// Advanced features
	subscribers      map[string]*StreamingSubscription `json:"-"`
	sequenceNum      int64                            `json:"-"`
	buffer           []StreamingResult                `json:"-"`
	bufferMu         sync.RWMutex                     `json:"-"`
	maxBufferSize    int                              `json:"-"`
	metadata         map[string]interface{}           `json:"metadata"`
	compressionEnabled bool                           `json:"-"`
	heartbeatTicker  *time.Ticker                     `json:"-"`
}

// StreamingManager manages active streaming sessions
type StreamingManager struct {
	config      StreamingConfig
	sessions    map[string]*StreamingSession
	tools       map[string]AdvancedStreamingToolHandler
	logger      *log.Logger
	mu          sync.RWMutex
	cleanup     chan string
	
	// Statistics
	totalSessions      int64
	activeSessions     int64
	totalSubscriptions int64
	
	// WebSocket integration
	webSocketManager *WebSocketManager
	
	// Compression and batching
	batchProcessor *BatchProcessor
}

// NewStreamingManager creates a new streaming manager
func NewStreamingManager(config StreamingConfig, logger *log.Logger) *StreamingManager {
	if config.BufferSize == 0 {
		config = DefaultStreamingConfig()
	}
	
	sm := &StreamingManager{
		config:         config,
		sessions:       make(map[string]*StreamingSession),
		tools:          make(map[string]AdvancedStreamingToolHandler),
		logger:         logger,
		cleanup:        make(chan string, 100),
		batchProcessor: NewBatchProcessor(config.BatchSize),
	}

	// Start cleanup goroutine
	go sm.cleanupSessions()
	
	// Start heartbeat sender
	go sm.sendHeartbeats()

	return sm
}

// StreamingTool registers a streaming tool
func (s *Server) StreamingTool(name, description string, handler StreamingToolHandler) *Server {
	return s.StreamingToolWithConfig(name, description, handler, StreamingConfig{})
}

// StreamingConfig configures streaming behavior
type StreamingConfig struct {
	Enabled             bool          `json:"enabled"`
	MaxSessions         int           `json:"maxSessions"`
	SessionTimeout      time.Duration `json:"sessionTimeout"`
	BufferSize          int           `json:"bufferSize"`
	PollingInterval     time.Duration `json:"pollingInterval"`
	CompressionEnabled  bool          `json:"compressionEnabled"`
	BatchSize           int           `json:"batchSize"`
	MaxConcurrentStreams int          `json:"maxConcurrentStreams"`
	ChunkSize           int           `json:"chunkSize"`
	HeartbeatInterval   time.Duration `json:"heartbeatInterval"`
	
	// Legacy fields for compatibility
	Timeout      time.Duration // Total operation timeout
	ChunkTimeout time.Duration // Timeout between chunks
}

// DefaultStreamingConfig returns default streaming configuration
func DefaultStreamingConfig() StreamingConfig {
	return StreamingConfig{
		Enabled:             true,
		MaxSessions:         100,
		SessionTimeout:      30 * time.Minute,
		BufferSize:          1000,
		PollingInterval:     100 * time.Millisecond,
		CompressionEnabled:  true,
		BatchSize:           10,
		MaxConcurrentStreams: 10,
		ChunkSize:           4096,
		HeartbeatInterval:   30 * time.Second,
		
		// Legacy fields
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
			s.streamingManager = NewStreamingManager(DefaultStreamingConfig(), s.logger)
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
		Type:      "error",
		Error:     err.Error(),
		Finished:  true,
		Timestamp: time.Now().Unix(),
	}
}

// Advanced streaming types and features

// StreamingSubscription represents a client subscription to a streaming session
type StreamingSubscription struct {
	ID         string              `json:"id"`
	ClientID   string              `json:"clientId"`
	SessionID  string              `json:"sessionId"`
	LastSeq    int64               `json:"lastSeq"`
	CreatedAt  time.Time           `json:"createdAt"`
	Active     bool                `json:"active"`
	Filter     StreamingFilter     `json:"filter"`
	ctx        context.Context
	cancel     context.CancelFunc
}

// StreamingFilter defines filtering criteria for streaming results
type StreamingFilter struct {
	Types       []string `json:"types,omitempty"`       // Filter by result types
	MinProgress float64  `json:"minProgress,omitempty"` // Minimum progress to include
	MaxProgress float64  `json:"maxProgress,omitempty"` // Maximum progress to include
	Keywords    []string `json:"keywords,omitempty"`    // Filter by keywords in data
}

// AdvancedStreamingToolHandler defines an advanced streaming tool handler
type AdvancedStreamingToolHandler func(ctx context.Context, args json.RawMessage, session *StreamingSession) error

// BatchProcessor handles batching of streaming results
type BatchProcessor struct {
	batchSize int
	batches   map[string][]StreamingResult
	mu        sync.RWMutex
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(batchSize int) *BatchProcessor {
	return &BatchProcessor{
		batchSize: batchSize,
		batches:   make(map[string][]StreamingResult),
	}
}

// AddToBatch adds a result to a batch
func (bp *BatchProcessor) AddToBatch(sessionID string, result StreamingResult) []StreamingResult {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	
	if bp.batches[sessionID] == nil {
		bp.batches[sessionID] = make([]StreamingResult, 0, bp.batchSize)
	}
	
	bp.batches[sessionID] = append(bp.batches[sessionID], result)
	
	if len(bp.batches[sessionID]) >= bp.batchSize {
		// Return full batch and reset
		batch := bp.batches[sessionID]
		bp.batches[sessionID] = make([]StreamingResult, 0, bp.batchSize)
		return batch
	}
	
	return nil // Not ready yet
}

// FlushBatch flushes any remaining results in a batch
func (bp *BatchProcessor) FlushBatch(sessionID string) []StreamingResult {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	
	if batch := bp.batches[sessionID]; len(batch) > 0 {
		delete(bp.batches, sessionID)
		return batch
	}
	
	return nil
}

// Advanced streaming manager methods

// RegisterAdvancedStreamingTool registers an advanced streaming tool
func (sm *StreamingManager) RegisterAdvancedStreamingTool(name string, handler AdvancedStreamingToolHandler) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.tools[name] = handler
}

// CreateAdvancedSession creates a new advanced streaming session
func (sm *StreamingManager) CreateAdvancedSession(toolName string, args map[string]interface{}) (*StreamingSession, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	
	if sm.activeSessions >= int64(sm.config.MaxSessions) {
		return nil, fmt.Errorf("maximum number of streaming sessions reached")
	}
	
	// Check if tool supports streaming
	if _, exists := sm.tools[toolName]; !exists {
		return nil, fmt.Errorf("tool %s does not support streaming", toolName)
	}
	
	// Create session
	sessionID := fmt.Sprintf("stream_%s_%d", toolName, time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), sm.config.SessionTimeout)
	
	session := &StreamingSession{
		ID:               sessionID,
		ToolName:         toolName,
		Arguments:        args,
		Context:          ctx,
		Cancel:           cancel,
		StartTime:        time.Now(),
		LastActivity:     time.Now(),
		Status:           "active",
		Progress:         0.0,
		subscribers:      make(map[string]*StreamingSubscription),
		sequenceNum:      0,
		buffer:           make([]StreamingResult, 0),
		maxBufferSize:    sm.config.BufferSize,
		metadata:         make(map[string]interface{}),
		compressionEnabled: sm.config.CompressionEnabled,
	}
	
	// Start heartbeat if enabled
	if sm.config.HeartbeatInterval > 0 {
		session.heartbeatTicker = time.NewTicker(sm.config.HeartbeatInterval)
		go session.sendHeartbeats()
	}
	
	sm.sessions[sessionID] = session
	sm.totalSessions++
	sm.activeSessions++
	
	// Start the streaming tool
	go sm.runAdvancedStreamingTool(session)
	
	sm.logger.Printf("Created advanced streaming session: %s for tool: %s", sessionID, toolName)
	return session, nil
}

// runAdvancedStreamingTool executes the advanced streaming tool
func (sm *StreamingManager) runAdvancedStreamingTool(session *StreamingSession) {
	defer func() {
		sm.mu.Lock()
		sm.activeSessions--
		sm.mu.Unlock()
		
		if r := recover(); r != nil {
			sm.logger.Printf("Streaming tool panic in session %s: %v", session.ID, r)
			session.EmitError(fmt.Sprintf("Tool execution panic: %v", r))
		}
		
		session.finish()
	}()
	
	// Get tool handler
	sm.mu.RLock()
	handler, exists := sm.tools[session.ToolName]
	sm.mu.RUnlock()
	
	if !exists {
		session.EmitError(fmt.Sprintf("Tool %s not found", session.ToolName))
		return
	}
	
	// Execute tool
	argsJSON, _ := json.Marshal(session.Arguments)
	if err := handler(session.Context, argsJSON, session); err != nil {
		session.EmitError(fmt.Sprintf("Tool execution error: %v", err))
		return
	}
	
	session.SetStatus("finished")
}

// sendHeartbeats sends periodic heartbeat messages
func (sm *StreamingManager) sendHeartbeats() {
	ticker := time.NewTicker(sm.config.HeartbeatInterval)
	defer ticker.Stop()
	
	for range ticker.C {
		sm.mu.RLock()
		sessions := make([]*StreamingSession, 0, len(sm.sessions))
		for _, session := range sm.sessions {
			if session.Status == "active" {
				sessions = append(sessions, session)
			}
		}
		sm.mu.RUnlock()
		
		for _, session := range sessions {
			session.EmitHeartbeat()
		}
	}
}

// Subscribe creates a subscription to a streaming session
func (sm *StreamingManager) Subscribe(sessionID, clientID string, filter StreamingFilter) (*StreamingSubscription, error) {
	session, err := sm.GetSession(sessionID)
	if err != nil {
		return nil, err
	}
	
	subscriptionID := fmt.Sprintf("sub_%s_%s_%d", sessionID, clientID, time.Now().UnixNano())
	ctx, cancel := context.WithCancel(context.Background())
	
	subscription := &StreamingSubscription{
		ID:        subscriptionID,
		ClientID:  clientID,
		SessionID: sessionID,
		LastSeq:   0,
		CreatedAt: time.Now(),
		Active:    true,
		Filter:    filter,
		ctx:       ctx,
		cancel:    cancel,
	}
	
	session.mu.Lock()
	session.subscribers[subscriptionID] = subscription
	session.mu.Unlock()
	
	sm.mu.Lock()
	sm.totalSubscriptions++
	sm.mu.Unlock()
	
	return subscription, nil
}

// GetSession returns a streaming session by ID
func (sm *StreamingManager) GetSession(sessionID string) (*StreamingSession, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	session, exists := sm.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	
	return session, nil
}

// GetStatistics returns streaming manager statistics
func (sm *StreamingManager) GetStatistics() map[string]interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	activeSubscriptions := int64(0)
	for _, session := range sm.sessions {
		session.mu.RLock()
		activeSubscriptions += int64(len(session.subscribers))
		session.mu.RUnlock()
	}
	
	return map[string]interface{}{
		"totalSessions":        sm.totalSessions,
		"activeSessions":       sm.activeSessions,
		"totalSubscriptions":   sm.totalSubscriptions,
		"activeSubscriptions":  activeSubscriptions,
		"registeredTools":      len(sm.tools),
		"maxSessions":         sm.config.MaxSessions,
	}
}

// Advanced StreamingSession methods

// EmitData emits a data result to the streaming session
func (ss *StreamingSession) EmitData(data interface{}) {
	ss.emit(StreamingResult{
		Type:      "data",
		Data:      data,
		Timestamp: time.Now().Unix(),
	})
}

// EmitProgress emits a progress update
func (ss *StreamingSession) EmitProgress(progress float64, message string) {
	ss.mu.Lock()
	ss.Progress = progress
	ss.ProcessedItems++
	ss.mu.Unlock()
	
	ss.emit(StreamingResult{
		Type:      "progress",
		Progress:  NewProgress(ss.ProcessedItems, ss.TotalItems, message),
		Timestamp: time.Now().Unix(),
	})
}

// EmitStatus emits a status update
func (ss *StreamingSession) EmitStatus(status, message string) {
	ss.SetStatus(status)
	ss.emit(StreamingResult{
		Type:      "status",
		Status:    status,
		Data:      message,
		Timestamp: time.Now().Unix(),
	})
}

// EmitError emits an error result
func (ss *StreamingSession) EmitError(errorMsg string) {
	ss.SetStatus("error")
	ss.emit(StreamingResult{
		Type:      "error",
		Error:     errorMsg,
		Timestamp: time.Now().Unix(),
	})
}

// EmitChunk emits a data chunk with ID for reassembly
func (ss *StreamingSession) EmitChunk(chunkID string, data interface{}) {
	ss.emit(StreamingResult{
		Type:      "data",
		Data:      data,
		ChunkID:   chunkID,
		Timestamp: time.Now().Unix(),
	})
}

// EmitHeartbeat emits a heartbeat to keep the session alive
func (ss *StreamingSession) EmitHeartbeat() {
	ss.emit(StreamingResult{
		Type:      "heartbeat",
		Timestamp: time.Now().Unix(),
		Status:    ss.Status,
	})
}

// SetStatus sets the session status
func (ss *StreamingSession) SetStatus(status string) {
	ss.mu.Lock()
	ss.Status = status
	ss.LastActivity = time.Now()
	ss.mu.Unlock()
}

// SetMetadata sets session metadata
func (ss *StreamingSession) SetMetadata(key string, value interface{}) {
	ss.mu.Lock()
	ss.metadata[key] = value
	ss.mu.Unlock()
}

// GetMetadata gets session metadata
func (ss *StreamingSession) GetMetadata(key string) interface{} {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.metadata[key]
}

// SetTotalItems sets the total number of items to process
func (ss *StreamingSession) SetTotalItems(total int64) {
	ss.mu.Lock()
	ss.TotalItems = total
	ss.mu.Unlock()
}

// emit emits a result to all subscribers
func (ss *StreamingSession) emit(result StreamingResult) {
	ss.mu.Lock()
	ss.sequenceNum++
	result.Sequence = ss.sequenceNum
	ss.mu.Unlock()
	
	// Add to buffer
	ss.bufferMu.Lock()
	ss.buffer = append(ss.buffer, result)
	if len(ss.buffer) > ss.maxBufferSize {
		// Remove oldest results if buffer is full
		ss.buffer = ss.buffer[len(ss.buffer)-ss.maxBufferSize:]
	}
	ss.bufferMu.Unlock()
	
	// Update last activity
	ss.mu.Lock()
	ss.LastActivity = time.Now()
	ss.mu.Unlock()
	
	// Notify WebSocket subscribers if available
	ss.notifyWebSocketSubscribers(result)
}

// notifyWebSocketSubscribers notifies WebSocket subscribers
func (ss *StreamingSession) notifyWebSocketSubscribers(result StreamingResult) {
	ss.mu.RLock()
	subscribers := make([]*StreamingSubscription, 0, len(ss.subscribers))
	for _, sub := range ss.subscribers {
		if sub.Active {
			subscribers = append(subscribers, sub)
		}
	}
	ss.mu.RUnlock()
	
	for _, sub := range subscribers {
		// Filter result based on subscription filter
		if ss.shouldIncludeResult(result, sub.Filter) {
			// Send to WebSocket if available
			// This would integrate with the WebSocket manager
		}
	}
}

// shouldIncludeResult checks if a result should be included based on filter
func (ss *StreamingSession) shouldIncludeResult(result StreamingResult, filter StreamingFilter) bool {
	// Type filter
	if len(filter.Types) > 0 {
		found := false
		for _, t := range filter.Types {
			if t == result.Type {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	
	// Progress filter
	if result.Progress != nil {
		if filter.MinProgress > 0 && result.Progress.Percentage < filter.MinProgress {
			return false
		}
		if filter.MaxProgress > 0 && result.Progress.Percentage > filter.MaxProgress {
			return false
		}
	}
	
	// Keyword filter
	if len(filter.Keywords) > 0 {
		dataStr := fmt.Sprintf("%v", result.Data)
		found := false
		for _, keyword := range filter.Keywords {
			if strings.Contains(strings.ToLower(dataStr), strings.ToLower(keyword)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	
	return true
}

// GetBufferedResults gets buffered results from a specific sequence number
func (ss *StreamingSession) GetBufferedResults(fromSeq int64, maxResults int) []StreamingResult {
	ss.bufferMu.RLock()
	defer ss.bufferMu.RUnlock()
	
	results := make([]StreamingResult, 0, maxResults)
	count := 0
	
	for _, result := range ss.buffer {
		if result.Sequence > fromSeq && count < maxResults {
			results = append(results, result)
			count++
		}
	}
	
	return results
}

// sendHeartbeats sends periodic heartbeats for this session
func (ss *StreamingSession) sendHeartbeats() {
	if ss.heartbeatTicker == nil {
		return
	}
	
	for {
		select {
		case <-ss.Context.Done():
			return
		case <-ss.heartbeatTicker.C:
			ss.EmitHeartbeat()
		}
	}
}

// finish finishes the streaming session
func (ss *StreamingSession) finish() {
	ss.SetStatus("finished")
	
	// Stop heartbeat
	if ss.heartbeatTicker != nil {
		ss.heartbeatTicker.Stop()
	}
	
	// Cancel all subscriptions
	ss.mu.Lock()
	for _, sub := range ss.subscribers {
		sub.cancel()
	}
	ss.mu.Unlock()
	
	ss.Cancel()
}

// Common streaming tool implementations

// BatchProcessingTool creates a streaming tool for batch processing
func BatchProcessingTool(processFn func(ctx context.Context, item interface{}, session *StreamingSession) error) AdvancedStreamingToolHandler {
	return func(ctx context.Context, args json.RawMessage, session *StreamingSession) error {
		var params struct {
			Items    []interface{} `json:"items"`
			BatchSize int          `json:"batchSize,omitempty"`
		}
		
		if err := json.Unmarshal(args, &params); err != nil {
			return fmt.Errorf("invalid parameters: %w", err)
		}
		
		if params.BatchSize == 0 {
			params.BatchSize = 10
		}
		
		total := int64(len(params.Items))
		session.SetTotalItems(total)
		
		// Process items in batches
		for i := 0; i < len(params.Items); i += params.BatchSize {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			
			end := i + params.BatchSize
			if end > len(params.Items) {
				end = len(params.Items)
			}
			
			batch := params.Items[i:end]
			
			// Process batch
			for j, item := range batch {
				if err := processFn(ctx, item, session); err != nil {
					session.EmitError(fmt.Sprintf("Error processing item %d: %v", i+j, err))
					continue
				}
				
				// Update progress
				processed := float64(i+j+1) / float64(total) * 100
				session.EmitProgress(processed, fmt.Sprintf("Processed %d/%d items", i+j+1, total))
			}
		}
		
		return nil
	}
}

// FileStreamingTool creates a streaming tool for file processing
func FileStreamingTool(reader io.Reader, chunkSize int) AdvancedStreamingToolHandler {
	return func(ctx context.Context, args json.RawMessage, session *StreamingSession) error {
		buffer := make([]byte, chunkSize)
		chunkIndex := 0
		
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			
			n, err := reader.Read(buffer)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buffer[:n])
				
				session.EmitChunk(fmt.Sprintf("chunk_%d", chunkIndex), chunk)
				chunkIndex++
			}
			
			if err != nil {
				if err == io.EOF {
					break
				}
				return err
			}
		}
		
		return nil
	}
}

// Server extensions for enhanced streaming

// EnableAdvancedStreaming enables advanced streaming support
func (s *Server) EnableAdvancedStreaming(config StreamingConfig) *Server {
	if s.streamingManager == nil {
		s.streamingManager = NewStreamingManager(config, s.logger)
	}
	
	// Register advanced streaming tools with server
	s.Tool("streaming/create_session", "Create a new streaming session", s.createAdvancedStreamingSessionTool)
	s.Tool("streaming/poll_session", "Poll streaming session for results", s.pollAdvancedStreamingSessionTool)
	s.Tool("streaming/cancel_session", "Cancel a streaming session", s.cancelAdvancedStreamingSessionTool)
	s.Tool("streaming/get_statistics", "Get streaming statistics", s.getAdvancedStreamingStatisticsTool)
	s.Tool("streaming/subscribe", "Subscribe to streaming session", s.subscribeStreamingSessionTool)
	s.Tool("streaming/get_buffered", "Get buffered results from session", s.getBufferedResultsTool)
	
	return s
}

// RegisterAdvancedStreamingTool registers an advanced streaming tool
func (s *Server) RegisterAdvancedStreamingTool(name string, handler AdvancedStreamingToolHandler) *Server {
	if s.streamingManager != nil {
		s.streamingManager.RegisterAdvancedStreamingTool(name, handler)
	}
	return s
}

// Advanced streaming tool implementations

func (s *Server) createAdvancedStreamingSessionTool(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		ToolName  string                 `json:"toolName"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	
	if s.streamingManager == nil {
		return nil, fmt.Errorf("streaming not enabled")
	}
	
	session, err := s.streamingManager.CreateAdvancedSession(params.ToolName, params.Arguments)
	if err != nil {
		return nil, err
	}
	
	return map[string]interface{}{
		"sessionId":  session.ID,
		"toolName":   session.ToolName,
		"status":     session.Status,
		"startTime":  session.StartTime,
	}, nil
}

func (s *Server) pollAdvancedStreamingSessionTool(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		SessionID  string `json:"sessionId"`
		MaxResults int    `json:"maxResults,omitempty"`
		FromSeq    int64  `json:"fromSeq,omitempty"`
	}
	
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	
	if params.MaxResults == 0 {
		params.MaxResults = 10
	}
	
	if s.streamingManager == nil {
		return nil, fmt.Errorf("streaming not enabled")
	}
	
	session, err := s.streamingManager.GetSession(params.SessionID)
	if err != nil {
		return nil, err
	}
	
	results := session.GetBufferedResults(params.FromSeq, params.MaxResults)
	
	return map[string]interface{}{
		"sessionId": params.SessionID,
		"results":   results,
		"count":     len(results),
		"status":    session.Status,
	}, nil
}

func (s *Server) cancelAdvancedStreamingSessionTool(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		SessionID string `json:"sessionId"`
	}
	
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	
	if s.streamingManager == nil {
		return nil, fmt.Errorf("streaming not enabled")
	}
	
	session, err := s.streamingManager.GetSession(params.SessionID)
	if err != nil {
		return nil, err
	}
	
	session.SetStatus("cancelled")
	session.Cancel()
	
	return map[string]interface{}{
		"sessionId": params.SessionID,
		"cancelled": true,
	}, nil
}

func (s *Server) getAdvancedStreamingStatisticsTool(ctx context.Context, args json.RawMessage) (interface{}, error) {
	if s.streamingManager == nil {
		return nil, fmt.Errorf("streaming not enabled")
	}
	
	return s.streamingManager.GetStatistics(), nil
}

func (s *Server) subscribeStreamingSessionTool(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		SessionID string          `json:"sessionId"`
		ClientID  string          `json:"clientId"`
		Filter    StreamingFilter `json:"filter,omitempty"`
	}
	
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	
	if s.streamingManager == nil {
		return nil, fmt.Errorf("streaming not enabled")
	}
	
	subscription, err := s.streamingManager.Subscribe(params.SessionID, params.ClientID, params.Filter)
	if err != nil {
		return nil, err
	}
	
	return map[string]interface{}{
		"subscriptionId": subscription.ID,
		"sessionId":      subscription.SessionID,
		"clientId":       subscription.ClientID,
		"active":         subscription.Active,
	}, nil
}

func (s *Server) getBufferedResultsTool(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		SessionID  string `json:"sessionId"`
		FromSeq    int64  `json:"fromSeq"`
		MaxResults int    `json:"maxResults,omitempty"`
	}
	
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	
	if params.MaxResults == 0 {
		params.MaxResults = 50
	}
	
	if s.streamingManager == nil {
		return nil, fmt.Errorf("streaming not enabled")
	}
	
	session, err := s.streamingManager.GetSession(params.SessionID)
	if err != nil {
		return nil, err
	}
	
	results := session.GetBufferedResults(params.FromSeq, params.MaxResults)
	
	return map[string]interface{}{
		"sessionId": params.SessionID,
		"results":   results,
		"count":     len(results),
		"fromSeq":   params.FromSeq,
	}, nil
}
