package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	jarvis "github.com/can-acar/jarvis-mcp-sdk"
)

func main() {
	// Create Jarvis MCP server
	server := jarvis.NewServer("web-demo-server", "1.0.0")

	// Register tools
	registerTools(server)

	// Register resources
	registerResources(server)

	// Register prompts
	registerPrompts(server)

	// Enable concurrency for better performance
	server.EnableConcurrency(jarvis.ConcurrencyConfig{
		MaxWorkers:     5,
		QueueSize:      100,
		RequestTimeout: 30 * time.Second,
		EnableMetrics:  true,
	})

	// Configure web transport
	webConfig := jarvis.WebConfig{
		Port:            8080,
		Host:            "localhost",
		AuthToken:       "demo-secret-token-2024",
		EnableCORS:      true,
		EnableDashboard: true,
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    30 * time.Second,
		MaxRequestSize:  10 * 1024 * 1024, // 10MB
	}

	// Enable web transport
	server.EnableWebTransport(webConfig)

	// Setup graceful shutdown
	setupGracefulShutdown(server)

	// Start the server
	fmt.Println("🤖 Starting Jarvis MCP Web Server...")
	fmt.Printf("📡 Web Dashboard: http://%s:%d/dashboard\n", webConfig.Host, webConfig.Port)
	fmt.Printf("🔐 Auth Token: %s\n", webConfig.AuthToken)
	fmt.Println("📚 API Endpoints:")
	fmt.Printf("   GET  http://%s:%d/health\n", webConfig.Host, webConfig.Port)
	fmt.Printf("   GET  http://%s:%d/api/v1/server/info\n", webConfig.Host, webConfig.Port)
	fmt.Printf("   GET  http://%s:%d/api/v1/tools/list\n", webConfig.Host, webConfig.Port)
	fmt.Printf("   POST http://%s:%d/api/v1/tools/call\n", webConfig.Host, webConfig.Port)
	fmt.Println()
	fmt.Println("🔥 Example API calls:")
	fmt.Printf(`
curl -H "Authorization: Bearer %s" \
     -H "Content-Type: application/json" \
     -X POST http://%s:%d/api/v1/tools/call \
     -d '{"name": "calculator", "arguments": {"operation": "add", "a": 10, "b": 5}}'

curl -H "Authorization: Bearer %s" \
     http://%s:%d/api/v1/tools/list
`, webConfig.AuthToken, webConfig.Host, webConfig.Port,
		webConfig.AuthToken, webConfig.Host, webConfig.Port)

	// Run with both stdio and web transports
	if err := server.RunMultiTransport(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func registerTools(server *jarvis.Server) {
	// Calculator tool
	server.Tool("calculator", "Perform basic arithmetic operations", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		var args struct {
			Operation string  `json:"operation"`
			A         float64 `json:"a"`
			B         float64 `json:"b"`
		}

		if err := json.Unmarshal(params, &args); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		switch args.Operation {
		case "add":
			return map[string]interface{}{
				"result":    args.A + args.B,
				"operation": fmt.Sprintf("%.2f + %.2f = %.2f", args.A, args.B, args.A+args.B),
			}, nil
		case "subtract":
			return map[string]interface{}{
				"result":    args.A - args.B,
				"operation": fmt.Sprintf("%.2f - %.2f = %.2f", args.A, args.B, args.A-args.B),
			}, nil
		case "multiply":
			return map[string]interface{}{
				"result":    args.A * args.B,
				"operation": fmt.Sprintf("%.2f × %.2f = %.2f", args.A, args.B, args.A*args.B),
			}, nil
		case "divide":
			if args.B == 0 {
				return nil, fmt.Errorf("division by zero")
			}
			return map[string]interface{}{
				"result":    args.A / args.B,
				"operation": fmt.Sprintf("%.2f ÷ %.2f = %.2f", args.A, args.B, args.A/args.B),
			}, nil
		default:
			return nil, fmt.Errorf("unsupported operation: %s", args.Operation)
		}
	})

	// Text processor tool
	server.Tool("text_processor", "Process text with various operations", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		var args struct {
			Text      string `json:"text"`
			Operation string `json:"operation"`
		}

		if err := json.Unmarshal(params, &args); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		switch args.Operation {
		case "uppercase":
			return map[string]interface{}{
				"original":  args.Text,
				"processed": fmt.Sprintf("%s", fmt.Sprintf("%s", args.Text)),
				"operation": "uppercase",
			}, nil
		case "lowercase":
			return map[string]interface{}{
				"original":  args.Text,
				"processed": fmt.Sprintf("%s", fmt.Sprintf("%s", args.Text)),
				"operation": "lowercase",
			}, nil
		case "length":
			return map[string]interface{}{
				"text":   args.Text,
				"length": len(args.Text),
			}, nil
		case "reverse":
			runes := []rune(args.Text)
			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
				runes[i], runes[j] = runes[j], runes[i]
			}
			return map[string]interface{}{
				"original": args.Text,
				"reversed": string(runes),
			}, nil
		default:
			return nil, fmt.Errorf("unsupported operation: %s", args.Operation)
		}
	})

	// System info tool
	server.Tool("system_info", "Get system information", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		return map[string]interface{}{
			"timestamp":   time.Now().Format(time.RFC3339),
			"server_name": "web-demo-server",
			"version":     "1.0.0",
			"uptime":      "unknown", // Could track actual uptime
			"features": []string{
				"web_transport",
				"concurrency",
				"authentication",
				"cors",
				"dashboard",
			},
		}, nil
	})

	// Demo error tool
	server.Tool("demo_error", "Demonstrates error handling", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		return nil, fmt.Errorf("this is a demo error for testing error handling")
	})
}

func registerResources(server *jarvis.Server) {
	// Server stats resource
	server.Resource("stats://server", "server_statistics", "Server performance statistics", "application/json", func(ctx context.Context, uri string) (interface{}, error) {
		// Get concurrency metrics if available
		var metrics interface{}
		if server.GetConcurrencyMetrics().TotalRequests > 0 {
			m := server.GetConcurrencyMetrics()
			metrics = map[string]interface{}{
				"total_requests":     m.TotalRequests,
				"active_requests":    m.ActiveRequests,
				"completed_requests": m.CompletedRequests,
				"failed_requests":    m.FailedRequests,
				"requests_per_sec":   m.RequestsPerSecond,
				"avg_response_time":  m.AverageResponseTime.String(),
				"max_response_time":  m.MaxResponseTime.String(),
			}
		}

		return map[string]interface{}{
			"server_name": "web-demo-server",
			"timestamp":   time.Now().Format(time.RFC3339),
			"metrics":     metrics,
			"endpoints": []map[string]string{
				{"method": "GET", "path": "/health", "description": "Health check"},
				{"method": "GET", "path": "/api/v1/server/info", "description": "Server information"},
				{"method": "GET", "path": "/api/v1/tools/list", "description": "List tools"},
				{"method": "POST", "path": "/api/v1/tools/call", "description": "Call tool"},
				{"method": "GET", "path": "/api/v1/resources/list", "description": "List resources"},
				{"method": "POST", "path": "/api/v1/resources/read", "description": "Read resource"},
			},
		}, nil
	})

	// Documentation resource
	server.Resource("docs://api", "api_documentation", "API documentation and examples", "text/markdown", func(ctx context.Context, uri string) (interface{}, error) {
		docs := `# Jarvis MCP Web Server API

## Authentication
All API endpoints (except /health) require authentication:
` + "```" + `
Authorization: Bearer demo-secret-token-2024
` + "```" + `

## Available Tools

### calculator
Perform arithmetic operations.
` + "```json" + `
{
  "name": "calculator",
  "arguments": {
    "operation": "add|subtract|multiply|divide",
    "a": 10.5,
    "b": 5.2
  }
}
` + "```" + `

### text_processor
Process text with various operations.
` + "```json" + `
{
  "name": "text_processor",
  "arguments": {
    "text": "Hello World",
    "operation": "uppercase|lowercase|length|reverse"
  }
}
` + "```" + `

### system_info
Get system information (no arguments required).
` + "```json" + `
{
  "name": "system_info",
  "arguments": {}
}
` + "```" + `

## Example Requests

### List Tools
` + "```bash" + `
curl -H "Authorization: Bearer demo-secret-token-2024" \
     http://localhost:8080/api/v1/tools/list
` + "```" + `

### Call Calculator
` + "```bash" + `
curl -H "Authorization: Bearer demo-secret-token-2024" \
     -H "Content-Type: application/json" \
     -X POST http://localhost:8080/api/v1/tools/call \
     -d '{"name": "calculator", "arguments": {"operation": "multiply", "a": 7, "b": 6}}'
` + "```" + `
`
		return docs, nil
	})
}

func registerPrompts(server *jarvis.Server) {
	// API example prompt
	server.Prompt("api_example", "Generate API usage examples", []jarvis.PromptArgument{
		{Name: "tool_name", Description: "Name of the tool to generate example for", Required: true},
		{Name: "format", Description: "Output format (curl, javascript, python)", Required: false},
	}, func(ctx context.Context, name string, arguments map[string]interface{}) (interface{}, error) {
		toolName, ok := arguments["tool_name"].(string)
		if !ok {
			return nil, fmt.Errorf("tool_name is required")
		}

		format := "curl"
		if f, ok := arguments["format"].(string); ok {
			format = f
		}

		examples := map[string]map[string]string{
			"calculator": {
				"curl": `curl -H "Authorization: Bearer demo-secret-token-2024" \
     -H "Content-Type: application/json" \
     -X POST http://localhost:8080/api/v1/tools/call \
     -d '{"name": "calculator", "arguments": {"operation": "add", "a": 10, "b": 5}}'`,
				"javascript": `fetch('http://localhost:8080/api/v1/tools/call', {
  method: 'POST',
  headers: {
    'Authorization': 'Bearer demo-secret-token-2024',
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({
    name: 'calculator',
    arguments: { operation: 'add', a: 10, b: 5 }
  })
})`,
				"python": `import requests

response = requests.post(
    'http://localhost:8080/api/v1/tools/call',
    headers={'Authorization': 'Bearer demo-secret-token-2024'},
    json={'name': 'calculator', 'arguments': {'operation': 'add', 'a': 10, 'b': 5}}
)`,
			},
		}

		if toolExamples, exists := examples[toolName]; exists {
			if example, exists := toolExamples[format]; exists {
				return fmt.Sprintf("Example for %s (%s):\n\n%s", toolName, format, example), nil
			}
		}

		return fmt.Sprintf("No example available for tool '%s' in format '%s'", toolName, format), nil
	})
}

func setupGracefulShutdown(server *jarvis.Server) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		fmt.Println("\n🛑 Shutting down Jarvis MCP Web Server...")

		// Stop web transport
		if err := server.StopWebTransport(); err != nil {
			log.Printf("Error stopping web transport: %v", err)
		}

		fmt.Println("✅ Server stopped gracefully")
		os.Exit(0)
	}()
}
