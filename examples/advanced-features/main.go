package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	jarvis "github.com/can-acar/jarvis-mcp-sdk"
)

// Typed tool parameter structs
type FileProcessParams struct {
	FilePath    string                 `json:"filePath" description:"Path to the file to process" required:"true"`
	ProcessType string                 `json:"processType" description:"Type of processing" enum:"count,analyze,transform" required:"true"`
	Options     map[string]interface{} `json:"options,omitempty" description:"Additional processing options"`
}

type BatchParams struct {
	Directory string `json:"directory" description:"Directory to process" required:"true"`
	Pattern   string `json:"pattern" description:"File pattern to match" required:"true"`
	Operation string `json:"operation" description:"Operation to perform" required:"true"`
	Parallel  int    `json:"parallel" description:"Number of parallel workers" min:"1" max:"10"`
}

func main() {
	// Create server with advanced features
	server := jarvis.NewServer("advanced-file-processor", "2.0.0")

	// Enable concurrency with custom config
	server.EnableConcurrency(jarvis.ConcurrencyConfig{
		MaxWorkers:     5,
		QueueSize:      50,
		RequestTimeout: 60 * time.Second,
		EnableMetrics:  true,
	})

	// Register typed tools with automatic schema generation
	server.RegisterTypedTool("process_file", "Process a single file with typed parameters", processFileTyped)

	// Register streaming tools for long-running operations
	server.StreamingTool("batch_process", "Process multiple files with progress", batchProcessStreaming)

	// Register traditional tools
	server.Tool("get_metrics", "Get server performance metrics", getMetrics).
		Tool("file_stats", "Get file statistics", getFileStats)

	// Register resources
	server.Resource("metrics://server", "server_metrics", "Server performance metrics", "application/json", serverMetricsResource)

	// Start server
	if err := server.Run(); err != nil {
		panic(err)
	}
}

// Typed tool handler with automatic schema generation
func processFileTyped(ctx context.Context, params FileProcessParams) (interface{}, error) {
	switch params.ProcessType {
	case "count":
		return countLines(params.FilePath)
	case "analyze":
		return analyzeFile(params.FilePath)
	case "transform":
		return transformFile(params.FilePath, params.Options)
	default:
		return nil, fmt.Errorf("unknown process type: %s", params.ProcessType)
	}
}

// Streaming tool for batch processing
func batchProcessStreaming(ctx context.Context, params json.RawMessage) (<-chan jarvis.StreamingResult, error) {
	var batchParams BatchParams
	if err := json.Unmarshal(params, &batchParams); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	// Create result channel
	resultChan := make(chan jarvis.StreamingResult, 100)

	// Start processing in goroutine
	go func() {
		defer close(resultChan)

		// Find matching files
		files, err := filepath.Glob(filepath.Join(batchParams.Directory, batchParams.Pattern))
		if err != nil {
			resultChan <- jarvis.NewErrorResult(fmt.Errorf("failed to find files: %w", err))
			return
		}

		total := len(files)
		if total == 0 {
			resultChan <- jarvis.NewFinalResult("No files found matching pattern")
			return
		}

		// Send initial progress
		resultChan <- jarvis.StreamingResult{
			Data:     fmt.Sprintf("Found %d files to process", total),
			Progress: jarvis.NewProgress(0, int64(total), "Starting batch processing"),
		}

		// Process files
		for i, file := range files {
			select {
			case <-ctx.Done():
				resultChan <- jarvis.NewErrorResult(ctx.Err())
				return
			default:
			}

			// Process single file
			result, err := processFile(file, batchParams.Operation)
			if err != nil {
				resultChan <- jarvis.StreamingResult{
					Data:     fmt.Sprintf("Error processing %s: %v", file, err),
					Progress: jarvis.NewProgress(int64(i+1), int64(total), fmt.Sprintf("Processed %d/%d files", i+1, total)),
				}
			} else {
				resultChan <- jarvis.StreamingResult{
					Data:     fmt.Sprintf("Processed %s: %s", file, result),
					Progress: jarvis.NewProgress(int64(i+1), int64(total), fmt.Sprintf("Processed %d/%d files", i+1, total)),
				}
			}

			// Small delay to show streaming effect
			time.Sleep(100 * time.Millisecond)
		}

		// Send final result
		resultChan <- jarvis.NewFinalResult(fmt.Sprintf("Batch processing completed. Processed %d files.", total))
	}()

	return resultChan, nil
}

// Regular tool for metrics
func getMetrics(ctx context.Context, params json.RawMessage) (interface{}, error) {
	// This would typically get metrics from the server
	// For demo, we'll return mock metrics
	return map[string]interface{}{
		"requests_total":     150,
		"requests_per_sec":   2.5,
		"average_latency_ms": 45.2,
		"active_connections": 3,
		"uptime_seconds":     3600,
	}, nil
}

// File statistics tool
func getFileStats(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var args struct {
		Path string `json:"path"`
	}

	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}

	info, err := os.Stat(args.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to get file stats: %w", err)
	}

	return map[string]interface{}{
		"name":    info.Name(),
		"size":    info.Size(),
		"mode":    info.Mode().String(),
		"modTime": info.ModTime().Format(time.RFC3339),
		"isDir":   info.IsDir(),
	}, nil
}

// Resource handler for server metrics
func serverMetricsResource(ctx context.Context, uri string) (interface{}, error) {
	return map[string]interface{}{
		"server_info": map[string]interface{}{
			"name":    "advanced-file-processor",
			"version": "2.0.0",
			"uptime":  time.Since(time.Now().Add(-1 * time.Hour)).String(),
		},
		"features": []string{
			"typed-tools",
			"streaming",
			"concurrency",
			"metrics",
		},
		"performance": map[string]interface{}{
			"goroutines":  10,
			"memory_mb":   25.6,
			"cpu_percent": 12.3,
		},
	}, nil
}

// Helper functions
func countLines(filePath string) (interface{}, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		count++
	}

	return map[string]interface{}{
		"file":  filePath,
		"lines": count,
	}, scanner.Err()
}

func analyzeFile(filePath string) (interface{}, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	words := len(strings.Fields(string(content)))
	chars := len(content)

	return map[string]interface{}{
		"file":          filePath,
		"size_bytes":    info.Size(),
		"characters":    chars,
		"words":         words,
		"last_modified": info.ModTime().Format(time.RFC3339),
	}, nil
}

func transformFile(filePath string, options map[string]interface{}) (interface{}, error) {
	operation := "uppercase"
	if op, ok := options["operation"].(string); ok {
		operation = op
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var transformed string
	switch operation {
	case "uppercase":
		transformed = strings.ToUpper(string(content))
	case "lowercase":
		transformed = strings.ToLower(string(content))
	case "reverse":
		runes := []rune(string(content))
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		transformed = string(runes)
	default:
		return nil, fmt.Errorf("unknown transformation: %s", operation)
	}

	// Write to new file
	newPath := filePath + ".transformed"
	err = os.WriteFile(newPath, []byte(transformed), 0644)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"original_file":    filePath,
		"transformed_file": newPath,
		"operation":        operation,
		"size_change":      len(transformed) - len(content),
	}, nil
}

func processFile(filePath, operation string) (string, error) {
	switch operation {
	case "count":
		result, err := countLines(filePath)
		if err != nil {
			return "", err
		}
		data := result.(map[string]interface{})
		return fmt.Sprintf("%d lines", data["lines"]), nil
	case "size":
		info, err := os.Stat(filePath)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d bytes", info.Size()), nil
	default:
		return "processed", nil
	}
}
