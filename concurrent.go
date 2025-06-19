package mcp

import (
	"context"
	"sync"
	"time"
)

// ConcurrencyConfig configures concurrent request handling
type ConcurrencyConfig struct {
	MaxWorkers     int           // Maximum number of worker goroutines
	QueueSize      int           // Size of the request queue
	RequestTimeout time.Duration // Timeout for individual requests
	EnableMetrics  bool          // Enable concurrency metrics
}

// DefaultConcurrencyConfig returns a default concurrency configuration
func DefaultConcurrencyConfig() ConcurrencyConfig {
	return ConcurrencyConfig{
		MaxWorkers:     10,
		QueueSize:      100,
		RequestTimeout: 30 * time.Second,
		EnableMetrics:  false,
	}
}

// WorkerPool manages concurrent request processing
type WorkerPool struct {
	config      ConcurrencyConfig
	requestChan chan *WorkerRequest
	workers     []*Worker
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
	metrics     *ConcurrencyMetrics
}

// WorkerRequest represents a request to be processed by a worker
type WorkerRequest struct {
	ID       interface{}
	Request  *Request
	Response chan *Response
	Context  context.Context
}

// Worker represents a single worker goroutine
type Worker struct {
	id          int
	pool        *WorkerPool
	requestChan chan *WorkerRequest
	quit        chan bool
}

// ConcurrencyMetrics tracks concurrency-related metrics
type ConcurrencyMetrics struct {
	mu                    sync.RWMutex
	TotalRequests         int64
	ActiveRequests        int64
	QueuedRequests        int64
	CompletedRequests     int64
	FailedRequests        int64
	AverageResponseTime   time.Duration
	MaxResponseTime       time.Duration
	totalResponseTime     time.Duration
	RequestsPerSecond     float64
	lastSecondRequests    int64
	lastSecondTime        time.Time
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(config ConcurrencyConfig) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())
	
	pool := &WorkerPool{
		config:      config,
		requestChan: make(chan *WorkerRequest, config.QueueSize),
		workers:     make([]*Worker, config.MaxWorkers),
		ctx:         ctx,
		cancel:      cancel,
		metrics:     &ConcurrencyMetrics{},
	}
	
	// Start workers
	for i := 0; i < config.MaxWorkers; i++ {
		worker := &Worker{
			id:          i,
			pool:        pool,
			requestChan: pool.requestChan,
			quit:        make(chan bool),
		}
		pool.workers[i] = worker
		pool.wg.Add(1)
		go worker.start()
	}
	
	// Start metrics updater if enabled
	if config.EnableMetrics {
		go pool.updateMetrics()
	}
	
	return pool
}

// SubmitRequest submits a request for concurrent processing
func (wp *WorkerPool) SubmitRequest(req *Request) *Response {
	// Create request context with timeout
	ctx, cancel := context.WithTimeout(wp.ctx, wp.config.RequestTimeout)
	defer cancel()
	
	// Create worker request
	workerReq := &WorkerRequest{
		ID:       req.ID,
		Request:  req,
		Response: make(chan *Response, 1),
		Context:  ctx,
	}
	
	// Update metrics
	if wp.config.EnableMetrics {
		wp.metrics.mu.Lock()
		wp.metrics.TotalRequests++
		wp.metrics.QueuedRequests++
		wp.metrics.mu.Unlock()
	}
	
	// Submit to queue
	select {
	case wp.requestChan <- workerReq:
		// Request queued successfully
	case <-ctx.Done():
		// Request timed out while queuing
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &Error{
				Code:    -32603,
				Message: "Request timeout while queuing",
			},
		}
	}
	
	// Update metrics
	if wp.config.EnableMetrics {
		wp.metrics.mu.Lock()
		wp.metrics.QueuedRequests--
		wp.metrics.ActiveRequests++
		wp.metrics.mu.Unlock()
	}
	
	// Wait for response
	select {
	case response := <-workerReq.Response:
		return response
	case <-ctx.Done():
		// Request timed out
		if wp.config.EnableMetrics {
			wp.metrics.mu.Lock()
			wp.metrics.ActiveRequests--
			wp.metrics.FailedRequests++
			wp.metrics.mu.Unlock()
		}
		
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &Error{
				Code:    -32603,
				Message: "Request timeout",
			},
		}
	}
}

// Shutdown gracefully shuts down the worker pool
func (wp *WorkerPool) Shutdown() {
	// Cancel context to signal shutdown
	wp.cancel()
	
	// Close request channel
	close(wp.requestChan)
	
	// Wait for all workers to finish
	wp.wg.Wait()
}

// GetMetrics returns current concurrency metrics
func (wp *WorkerPool) GetMetrics() ConcurrencyMetrics {
	if !wp.config.EnableMetrics {
		return ConcurrencyMetrics{}
	}
	
	wp.metrics.mu.RLock()
	defer wp.metrics.mu.RUnlock()
	
	return *wp.metrics
}

// Worker methods

func (w *Worker) start() {
	defer w.pool.wg.Done()
	
	for {
		select {
		case req := <-w.requestChan:
			if req == nil {
				// Channel closed, worker should exit
				return
			}
			w.processRequest(req)
		case <-w.pool.ctx.Done():
			// Pool is shutting down
			return
		case <-w.quit:
			// Worker is stopping
			return
		}
	}
}

func (w *Worker) processRequest(req *WorkerRequest) {
	startTime := time.Now()
	
	// Process the request using the server's HandleRequest method
	// This would need to be passed to the worker or accessible via the pool
	var response *Response
	
	// Simulate request processing (this would be replaced with actual server logic)
	select {
	case <-req.Context.Done():
		// Request cancelled
		response = &Response{
			JSONRPC: "2.0",
			ID:      req.Request.ID,
			Error: &Error{
				Code:    -32603,
				Message: "Request cancelled",
			},
		}
	default:
		// Process request normally
		// response = w.pool.server.HandleRequest(req.Context, req.Request)
		response = &Response{
			JSONRPC: "2.0",
			ID:      req.Request.ID,
			Result:  "Processed by worker " + string(rune(w.id)),
		}
	}
	
	// Update metrics
	if w.pool.config.EnableMetrics {
		duration := time.Since(startTime)
		w.pool.metrics.mu.Lock()
		w.pool.metrics.ActiveRequests--
		w.pool.metrics.CompletedRequests++
		w.pool.metrics.totalResponseTime += duration
		if duration > w.pool.metrics.MaxResponseTime {
			w.pool.metrics.MaxResponseTime = duration
		}
		if w.pool.metrics.CompletedRequests > 0 {
			w.pool.metrics.AverageResponseTime = w.pool.metrics.totalResponseTime / time.Duration(w.pool.metrics.CompletedRequests)
		}
		w.pool.metrics.mu.Unlock()
	}
	
	// Send response
	select {
	case req.Response <- response:
		// Response sent successfully
	case <-req.Context.Done():
		// Request context cancelled, don't send response
	}
}

func (w *Worker) stop() {
	w.quit <- true
}

// updateMetrics updates requests per second metrics
func (wp *WorkerPool) updateMetrics() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			wp.metrics.mu.Lock()
			now := time.Now()
			if !wp.metrics.lastSecondTime.IsZero() {
				elapsed := now.Sub(wp.metrics.lastSecondTime).Seconds()
				if elapsed > 0 {
					wp.metrics.RequestsPerSecond = float64(wp.metrics.lastSecondRequests) / elapsed
				}
			}
			wp.metrics.lastSecondTime = now
			wp.metrics.lastSecondRequests = wp.metrics.CompletedRequests
			wp.metrics.mu.Unlock()
		case <-wp.ctx.Done():
			return
		}
	}
}

// Server extension for concurrency

// EnableConcurrency enables concurrent request processing
func (s *Server) EnableConcurrency(config ConcurrencyConfig) *Server {
	s.workerPool = NewWorkerPool(config)
	s.concurrencyEnabled = true
	return s
}

// DisableConcurrency disables concurrent request processing
func (s *Server) DisableConcurrency() *Server {
	if s.workerPool != nil {
		s.workerPool.Shutdown()
		s.workerPool = nil
	}
	s.concurrencyEnabled = false
	return s
}

// GetConcurrencyMetrics returns current concurrency metrics
func (s *Server) GetConcurrencyMetrics() ConcurrencyMetrics {
	if s.workerPool == nil {
		return ConcurrencyMetrics{}
	}
	return s.workerPool.GetMetrics()
}