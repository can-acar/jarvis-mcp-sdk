package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	mcp "github.com/jarvis-mcp/jarvis-mcp-sdk"
)

func main() {
	// Create Jarvis MCP server with streaming capabilities
	server := mcp.NewServer("streaming-demo-server", "2.0.0")

	// Register streaming tools
	registerStreamingTools(server)

	// Register regular tools
	registerRegularTools(server)

	// Register resources
	registerResources(server)

	// Enable concurrency for better performance
	server.EnableConcurrency(mcp.ConcurrencyConfig{
		MaxWorkers:     8,
		QueueSize:      200,
		RequestTimeout: 60 * time.Second,
		EnableMetrics:  true,
	})

	// Configure web transport
	webConfig := mcp.WebConfig{
		Port:            8080,
		Host:            "localhost",
		AuthToken:       "streaming-demo-token-2024",
		EnableCORS:      true,
		EnableDashboard: true,
		ReadTimeout:     60 * time.Second,
		WriteTimeout:    60 * time.Second,
		MaxRequestSize:  20 * 1024 * 1024, // 20MB for large streaming data
	}
	server.EnableWebTransport(webConfig)

	// Enable WebSocket for real-time streaming
	wsConfig := mcp.DefaultWebSocketConfig()
	wsConfig.MaxMessageSize = 2 * 1024 * 1024 // 2MB messages
	wsConfig.PingInterval = 20 * time.Second
	server.EnableWebSocket(wsConfig)

	// Enable Server-Sent Events for one-way streaming
	sseConfig := mcp.DefaultSSEConfig()
	sseConfig.HeartbeatInterval = 15 * time.Second
	sseConfig.MaxConnections = 200
	server.EnableSSE(sseConfig)

	// Setup graceful shutdown
	setupGracefulShutdown(server)

	// Print startup information
	printStartupInfo(webConfig)

	// Start the server with multi-transport support
	if err := server.RunMultiTransport(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func registerStreamingTools(server *mcp.Server) {
	// File processing with progress
	server.StreamingTool("process_files", "Process multiple files with real-time progress", func(ctx context.Context, params json.RawMessage) (<-chan mcp.StreamingResult, error) {
		var args struct {
			FilePattern string `json:"filePattern"`
			Operation   string `json:"operation"`
			BatchSize   int    `json:"batchSize"`
		}

		if err := json.Unmarshal(params, &args); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		if args.BatchSize == 0 {
			args.BatchSize = 10
		}

		resultChan := make(chan mcp.StreamingResult, 100)

		go func() {
			defer close(resultChan)

			// Simulate file discovery
			resultChan <- mcp.StreamingResult{
				Data:     "Discovering files...",
				Progress: mcp.NewProgress(0, 100, "Scanning directories"),
			}

			time.Sleep(500 * time.Millisecond)

			// Simulate processing files
			totalFiles := 50 + rand.Intn(50) // 50-100 files
			for i := 0; i < totalFiles; i++ {
				select {
				case <-ctx.Done():
					resultChan <- mcp.NewErrorResult(ctx.Err())
					return
				default:
				}

				filename := fmt.Sprintf("file_%03d.txt", i+1)

				// Simulate processing time
				processingTime := time.Duration(50+rand.Intn(200)) * time.Millisecond
				time.Sleep(processingTime)

				// Send progress update
				progress := mcp.NewProgress(int64(i+1), int64(totalFiles), fmt.Sprintf("Processing %s", filename))
				progress.EstimatedETA = time.Duration(totalFiles-i-1) * processingTime

				resultChan <- mcp.StreamingResult{
					Data:     fmt.Sprintf("Processed %s (%s operation)", filename, args.Operation),
					Progress: progress,
					Metadata: map[string]interface{}{
						"filename":        filename,
						"operation":       args.Operation,
						"processing_time": processingTime.String(),
						"file_size":       rand.Intn(10000) + 1000,
					},
				}

				// Batch updates for better performance
				if (i+1)%args.BatchSize == 0 {
					resultChan <- mcp.StreamingResult{
						Data:     fmt.Sprintf("Completed batch %d/%d", (i+1)/args.BatchSize, (totalFiles+args.BatchSize-1)/args.BatchSize),
						Progress: progress,
						Metadata: map[string]interface{}{
							"batch_completed": (i + 1) / args.BatchSize,
							"files_in_batch":  args.BatchSize,
						},
					}
				}
			}

			// Final result
			resultChan <- mcp.NewFinalResult(map[string]interface{}{
				"total_files":    totalFiles,
				"operation":      args.Operation,
				"completed_at":   time.Now().Format(time.RFC3339),
				"total_duration": "simulated",
			})
		}()

		return resultChan, nil
	})

	// Data analysis with streaming results
	server.StreamingTool("analyze_dataset", "Analyze large dataset with streaming progress", func(ctx context.Context, params json.RawMessage) (<-chan mcp.StreamingResult, error) {
		var args struct {
			DatasetName string   `json:"datasetName"`
			ChunkSize   int      `json:"chunkSize"`
			Metrics     []string `json:"metrics"`
		}

		if err := json.Unmarshal(params, &args); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		if args.ChunkSize == 0 {
			args.ChunkSize = 1000
		}

		if len(args.Metrics) == 0 {
			args.Metrics = []string{"mean", "std", "min", "max"}
		}

		resultChan := make(chan mcp.StreamingResult, 100)

		go func() {
			defer close(resultChan)

			// Simulate data loading
			resultChan <- mcp.StreamingResult{
				Data:     fmt.Sprintf("Loading dataset: %s", args.DatasetName),
				Progress: mcp.NewProgress(0, 100, "Initializing"),
			}

			time.Sleep(1 * time.Second)

			totalRows := 50000 + rand.Intn(50000) // 50k-100k rows
			chunks := (totalRows + args.ChunkSize - 1) / args.ChunkSize

			analysisResults := make(map[string]interface{})

			for chunk := 0; chunk < chunks; chunk++ {
				select {
				case <-ctx.Done():
					resultChan <- mcp.NewErrorResult(ctx.Err())
					return
				default:
				}

				startRow := chunk * args.ChunkSize
				endRow := startRow + args.ChunkSize
				if endRow > totalRows {
					endRow = totalRows
				}

				// Simulate analysis
				time.Sleep(time.Duration(100+rand.Intn(300)) * time.Millisecond)

				// Generate mock metrics for this chunk
				chunkMetrics := make(map[string]float64)
				for _, metric := range args.Metrics {
					chunkMetrics[metric] = rand.Float64() * 100
				}

				progress := mcp.NewProgress(int64(chunk+1), int64(chunks), fmt.Sprintf("Analyzing chunk %d/%d", chunk+1, chunks))

				resultChan <- mcp.StreamingResult{
					Data:     fmt.Sprintf("Analyzed rows %d-%d", startRow, endRow),
					Progress: progress,
					Metadata: map[string]interface{}{
						"chunk":         chunk + 1,
						"rows_start":    startRow,
						"rows_end":      endRow,
						"chunk_metrics": chunkMetrics,
						"memory_usage":  fmt.Sprintf("%.1f MB", rand.Float64()*500+100),
					},
				}

				// Update overall results
				for metric, value := range chunkMetrics {
					if _, exists := analysisResults[metric]; !exists {
						analysisResults[metric] = []float64{}
					}
					analysisResults[metric] = append(analysisResults[metric].([]float64), value)
				}

				// Send intermediate summary every 10 chunks
				if (chunk+1)%10 == 0 {
					resultChan <- mcp.StreamingResult{
						Data:     fmt.Sprintf("Intermediate results after %d chunks", chunk+1),
						Progress: progress,
						Metadata: map[string]interface{}{
							"partial_analysis": analysisResults,
							"chunks_completed": chunk + 1,
						},
					}
				}
			}

			// Calculate final metrics
			finalMetrics := make(map[string]float64)
			for metric, values := range analysisResults {
				valuesSlice := values.([]float64)
				sum := 0.0
				for _, v := range valuesSlice {
					sum += v
				}
				finalMetrics[metric+"_avg"] = sum / float64(len(valuesSlice))
			}

			resultChan <- mcp.NewFinalResult(map[string]interface{}{
				"dataset_name":          args.DatasetName,
				"total_rows":            totalRows,
				"chunks_processed":      chunks,
				"final_metrics":         finalMetrics,
				"analysis_completed_at": time.Now().Format(time.RFC3339),
			})
		}()

		return resultChan, nil
	})

	// Log streaming tool
	server.StreamingTool("stream_logs", "Stream live log data", func(ctx context.Context, params json.RawMessage) (<-chan mcp.StreamingResult, error) {
		var args struct {
			LogLevel string `json:"logLevel"`
			MaxLines int    `json:"maxLines"`
			Interval int    `json:"interval"` // milliseconds
		}

		if err := json.Unmarshal(params, &args); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		if args.MaxLines == 0 {
			args.MaxLines = 100
		}
		if args.Interval == 0 {
			args.Interval = 1000 // 1 second
		}

		resultChan := make(chan mcp.StreamingResult, 50)

		go func() {
			defer close(resultChan)

			logLevels := []string{"DEBUG", "INFO", "WARN", "ERROR"}
			services := []string{"api-server", "database", "cache", "worker", "scheduler"}

			for i := 0; i < args.MaxLines; i++ {
				select {
				case <-ctx.Done():
					resultChan <- mcp.NewErrorResult(ctx.Err())
					return
				default:
				}

				// Generate mock log entry
				level := logLevels[rand.Intn(len(logLevels))]
				service := services[rand.Intn(len(services))]
				message := generateLogMessage(level)

				logEntry := map[string]interface{}{
					"timestamp": time.Now().Format(time.RFC3339),
					"level":     level,
					"service":   service,
					"message":   message,
					"line":      i + 1,
				}

				// Filter by log level if specified
				if args.LogLevel != "" && !strings.EqualFold(level, args.LogLevel) {
					continue
				}

				resultChan <- mcp.StreamingResult{
					Data:     logEntry,
					Progress: mcp.NewProgress(int64(i+1), int64(args.MaxLines), fmt.Sprintf("Streaming log %d/%d", i+1, args.MaxLines)),
					Metadata: map[string]interface{}{
						"log_level": level,
						"service":   service,
						"filtered":  args.LogLevel != "",
					},
				}

				time.Sleep(time.Duration(args.Interval) * time.Millisecond)
			}

			resultChan <- mcp.NewFinalResult(map[string]interface{}{
				"total_logs_streamed": args.MaxLines,
				"log_level_filter":    args.LogLevel,
				"stream_completed_at": time.Now().Format(time.RFC3339),
			})
		}()

		return resultChan, nil
	})
}

func registerRegularTools(server *mcp.Server) {
	// System status tool
	server.Tool("system_status", "Get current system status", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		return map[string]interface{}{
			"timestamp": time.Now().Format(time.RFC3339),
			"status":    "operational",
			"uptime":    "simulated",
			"features": map[string]bool{
				"streaming":     true,
				"websockets":    true,
				"sse":           true,
				"concurrency":   true,
				"web_dashboard": true,
			},
			"active_connections": map[string]interface{}{
				"websocket": "simulated",
				"sse":       "simulated",
				"http":      "simulated",
			},
		}, nil
	})

	// Performance metrics tool
	server.Tool("performance_metrics", "Get performance metrics", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		metrics := server.GetConcurrencyMetrics()

		return map[string]interface{}{
			"concurrency": map[string]interface{}{
				"total_requests":     metrics.TotalRequests,
				"active_requests":    metrics.ActiveRequests,
				"completed_requests": metrics.CompletedRequests,
				"failed_requests":    metrics.FailedRequests,
				"requests_per_sec":   metrics.RequestsPerSecond,
				"avg_response_time":  metrics.AverageResponseTime.String(),
			},
			"memory": map[string]interface{}{
				"usage_mb":   rand.Float64()*200 + 50,
				"gc_cycles":  rand.Intn(1000),
				"goroutines": rand.Intn(100) + 10,
			},
			"timestamp": time.Now().Format(time.RFC3339),
		}, nil
	})
}

func registerResources(server *mcp.Server) {
	// Streaming sessions resource
	server.Resource("streaming://sessions", "active_streaming_sessions", "Information about active streaming sessions", "application/json", func(ctx context.Context, uri string) (interface{}, error) {
		// Mock streaming session data
		return map[string]interface{}{
			"total_sessions": rand.Intn(10),
			"sessions": []map[string]interface{}{
				{
					"id":         "stream_001",
					"tool":       "process_files",
					"started_at": time.Now().Add(-5 * time.Minute).Format(time.RFC3339),
					"progress":   75.5,
					"status":     "active",
				},
				{
					"id":         "stream_002",
					"tool":       "analyze_dataset",
					"started_at": time.Now().Add(-2 * time.Minute).Format(time.RFC3339),
					"progress":   25.0,
					"status":     "active",
				},
			},
			"updated_at": time.Now().Format(time.RFC3339),
		}, nil
	})

	// WebSocket connections resource
	server.Resource("connections://websocket", "websocket_connections", "Active WebSocket connections", "application/json", func(ctx context.Context, uri string) (interface{}, error) {
		connections := make([]map[string]interface{}, 0)

		if server.GetWebSocketManager() != nil {
			wsConnections := server.GetWebSocketManager().GetConnections()
			for id, _ := range wsConnections {
				connections = append(connections, map[string]interface{}{
					"id":           id,
					"connected_at": "simulated",
					"last_ping":    "simulated",
					"is_active":    true,
				})
			}
		}

		return map[string]interface{}{
			"total_connections": len(connections),
			"connections":       connections,
			"updated_at":        time.Now().Format(time.RFC3339),
		}, nil
	})
}

func generateLogMessage(level string) string {
	messages := map[string][]string{
		"DEBUG": {
			"Processing request with ID: %d",
			"Cache hit for key: user_%d",
			"Database query executed in %dms",
			"Memory usage: %.1fMB",
		},
		"INFO": {
			"User authentication successful",
			"Request processed successfully",
			"Database connection established",
			"Service started on port 8080",
		},
		"WARN": {
			"High memory usage detected: %.1fMB",
			"Slow query detected: %dms",
			"Connection pool nearly exhausted",
			"Rate limit threshold reached",
		},
		"ERROR": {
			"Database connection failed",
			"Authentication failed for user",
			"Service unavailable",
			"Timeout waiting for response",
		},
	}

	levelMessages := messages[level]
	template := levelMessages[rand.Intn(len(levelMessages))]

	switch level {
	case "DEBUG":
		return fmt.Sprintf(template, rand.Intn(10000), rand.Intn(1000), rand.Intn(500), rand.Float64()*100)
	case "WARN":
		return fmt.Sprintf(template, rand.Float64()*1000, rand.Intn(5000))
	default:
		return template
	}
}

func printStartupInfo(webConfig mcp.WebConfig) {
	fmt.Println("🌊 Starting Jarvis MCP Streaming Server...")
	fmt.Printf("📡 Web Dashboard: http://%s:%d/dashboard\n", webConfig.Host, webConfig.Port)
	fmt.Printf("🔗 WebSocket Endpoint: ws://%s:%d/ws\n", webConfig.Host, webConfig.Port)
	fmt.Printf("📺 SSE Endpoint: http://%s:%d/events\n", webConfig.Host, webConfig.Port)
	fmt.Printf("🔐 Auth Token: %s\n", webConfig.AuthToken)
	fmt.Println()
	fmt.Println("🌊 Streaming Tools:")
	fmt.Println("   • process_files - File processing with progress")
	fmt.Println("   • analyze_dataset - Dataset analysis with real-time updates")
	fmt.Println("   • stream_logs - Live log streaming")
	fmt.Println()
	fmt.Println("🔥 Example WebSocket Usage:")
	fmt.Printf(`
// Connect to WebSocket
const ws = new WebSocket('ws://%s:%d/ws?token=%s');

// Subscribe to streaming tool
ws.send(JSON.stringify({
  type: 'stream_subscribe',
  id: 'sub-1',
  params: {
    toolName: 'process_files',
    arguments: {
      filePattern: '*.txt',
      operation: 'analyze',
      batchSize: 5
    }
  }
}));

// Listen for streaming updates
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  if (msg.type === 'stream_data') {
    console.log('Progress:', msg.result);
  }
};
`, webConfig.Host, webConfig.Port, webConfig.AuthToken)

	fmt.Println("\n🚀 Example HTTP Streaming:")
	fmt.Printf(`
curl -H "Authorization: Bearer %s" \
     -H "Content-Type: application/json" \
     -X POST http://%s:%d/api/v1/tools/call \
     -d '{
       "name": "process_files",
       "arguments": {
         "filePattern": "*.log",
         "operation": "compress"
       }
     }'
`, webConfig.AuthToken, webConfig.Host, webConfig.Port)
}

func setupGracefulShutdown(server *mcp.Server) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		fmt.Println("\n🛑 Shutting down Jarvis MCP Streaming Server...")

		// Stop web transport (includes WebSocket and SSE)
		if err := server.StopWebTransport(); err != nil {
			log.Printf("Error stopping web transport: %v", err)
		}

		fmt.Println("✅ Server stopped gracefully")
		os.Exit(0)
	}()
}
