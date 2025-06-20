package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	mcp "github.com/can-acar/jarvis-mcp-sdk"
)

func main() {
	// Create MCP SSE Server like the GitHub example
	server := mcp.NewServer("MCP SSE Server", "1.0.0")

	// Register tools
	registerTools(server)

	// Configure web transport
	webConfig := mcp.WebConfig{
		Port:            8001,
		Host:            "localhost",
		AuthToken:       "",
		EnableCORS:      true,
		EnableDashboard: false,
	}
	server.EnableWebTransport(webConfig)

	// Enable SSE
	sseConfig := mcp.SSEConfig{
		HeartbeatInterval: 30 * time.Second,
		MaxConnections:    50,
		BufferSize:        1000,
		EnableCORS:        true,
	}
	server.EnableSSE(sseConfig)

	// Add custom root endpoint after enabling web transport
	server.GetWebTransport().GetMux().HandleFunc("/", handleRoot(server))

	log.Printf("🚀 MCP SSE Server starting on http://localhost:%d", webConfig.Port)

	if err := server.StartWebTransport(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}

	select {}
}

func handleRoot(server *mcp.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get tools dynamically from server
		tools := []map[string]string{}
		for _, tool := range server.GetTools() {
			tools = append(tools, map[string]string{
				"name":        tool.Name,
				"description": tool.Description,
			})
		}

		response := map[string]interface{}{
			"name":    "MCP SSE Server",
			"version": "1.0.0",
			"status":  "running",
			"endpoints": map[string]string{
				"/":         "Server information (this response)",
				"/sse":      "Server-Sent Events endpoint for MCP connection",
				"/messages": "POST endpoint for MCP messages",
			},
			"tools": tools,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}
}

func registerTools(server *mcp.Server) {
	// Add tool
	server.Tool("add", "Add two numbers together", func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var params struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}

		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		result := params.A + params.B
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": fmt.Sprintf("%.2f + %.2f = %.2f", params.A, params.B, result),
				},
			},
			"result": result,
		}, nil
	})

	// Search tool
	server.Tool("search", "Search the web using Brave Search API", func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var params struct {
			Query string `json:"query"`
		}

		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		// Mock search results
		results := []map[string]interface{}{
			{
				"title":   fmt.Sprintf("Search result for: %s", params.Query),
				"url":     "https://example.com/result1",
				"snippet": fmt.Sprintf("This is a mock search result for '%s'", params.Query),
			},
			{
				"title":   fmt.Sprintf("Another result for: %s", params.Query),
				"url":     "https://example.com/result2",
				"snippet": "This is another mock search result",
			},
		}

		return map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": fmt.Sprintf("Found %d results for: %s", len(results), params.Query),
				},
			},
			"query":   params.Query,
			"results": results,
		}, nil
	})

	// New test tool - automatically added to tools list
	server.Tool("multiply", "Multiply two numbers", func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var params struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}

		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		result := params.A * params.B
		return map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": fmt.Sprintf("%.2f × %.2f = %.2f", params.A, params.B, result),
				},
			},
			"result": result,
		}, nil
	})
}