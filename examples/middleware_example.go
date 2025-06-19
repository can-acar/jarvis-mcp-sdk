package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	mcp "github.com/jarvis-mcp/jarvis-mcp-sdk"
)

func main() {
	// Create server
	server := mcp.NewServer("middleware-demo", "1.0.0")

	// Configure middleware system
	middlewareConfig := mcp.MiddlewareConfig{
		Enabled: true,
		Order: []string{
			"logging",
			"metrics", 
			"request_id",
			"auth",
			"rate_limit",
			"validation",
			"circuit_breaker",
		},
		SkipMethods: []string{"initialize"},
	}

	// Enable middleware
	server.EnableMiddleware(middlewareConfig)

	// Setup logging middleware
	logger := mcp.NewBasicLogger(log.New(os.Stdout, "[MIDDLEWARE] ", log.LstdFlags))
	loggingConfig := mcp.LoggingConfig{
		Level:          "info",
		Format:         "json",
		IncludeBody:    true,
		MaxBodySize:    512,
		SanitizeFields: []string{"password", "token", "secret"},
	}
	server.Use("logging", mcp.LoggingMiddlewareWithConfig(logger, loggingConfig))

	// Setup metrics middleware
	metricsCollector := mcp.NewMemoryMetricsCollector()
	metricsConfig := mcp.MetricsConfig{
		IncludeUserMetrics:   true,
		IncludeDetailedTiming: true,
		SampleRate:          1.0,
	}
	server.Use("metrics", mcp.MetricsMiddlewareWithConfig(metricsCollector, metricsConfig))

	// Setup request ID middleware
	server.Use("request_id", mcp.RequestIDMiddleware())

	// Setup authentication middleware
	jwtConfig := mcp.JWTConfig{
		Secret:    "your-secret-key",
		Algorithm: "HS256",
		Issuer:    "middleware-demo",
		Audience:  "api-users",
	}
	server.Use("auth", mcp.JWTMiddleware(jwtConfig))

	// Setup rate limiting middleware
	rateLimitConfig := mcp.RateLimitConfig{
		RequestsPerWindow: 10,
		WindowSize:        time.Minute,
		BurstSize:         3,
		KeyFunc:          mcp.UserBasedRateLimitKeyFunc,
	}
	server.Use("rate_limit", mcp.RateLimitMiddleware(rateLimitConfig))

	// Setup validation middleware
	validationConfig := mcp.ValidationConfig{
		Enabled:      true,
		StrictMode:   false,
		ValidateJSON: true,
		MaxDepth:     5,
		CustomRules: map[string]mcp.ValidationRule{
			"arguments.count": {
				Field:   "arguments.count",
				Type:    "range",
				Value:   map[string]interface{}{"min": 1.0, "max": 100.0},
				Message: "Count must be between 1 and 100",
			},
		},
	}
	server.Use("validation", mcp.ValidationMiddleware(validationConfig))

	// Setup circuit breaker middleware
	circuitBreakerConfig := mcp.CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
		MaxRequests:      5,
		SlidingWindow:    60 * time.Second,
		OnStateChange: func(name string, from, to mcp.CircuitBreakerState) {
			log.Printf("Circuit breaker %s changed from %s to %s", name, from, to)
		},
	}
	server.Use("circuit_breaker", mcp.CircuitBreakerMiddleware(circuitBreakerConfig))

	// Register a simple tool for testing
	server.Tool("echo", "Echo back the input", func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var params struct {
			Message string `json:"message"`
			Count   int    `json:"count"`
		}

		if err := json.Unmarshal(args, &params); err != nil {
			return nil, err
		}

		// Simulate some processing time
		time.Sleep(100 * time.Millisecond)

		result := make([]string, params.Count)
		for i := 0; i < params.Count; i++ {
			result[i] = params.Message
		}

		return map[string]interface{}{
			"echoed": result,
			"count":  params.Count,
		}, nil
	})

	// Register a tool that demonstrates failure for circuit breaker
	server.Tool("unreliable", "A tool that fails sometimes", func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var params struct {
			ShouldFail bool `json:"shouldFail"`
		}

		if err := json.Unmarshal(args, &params); err != nil {
			return nil, err
		}

		if params.ShouldFail {
			return nil, &mcp.Error{
				Code:    500,
				Message: "Simulated failure",
			}
		}

		return map[string]interface{}{
			"status": "success",
		}, nil
	})

	// Register a resource for demonstration
	server.Resource("file://demo.txt", "demo", "Demo resource", "text/plain", 
		func(ctx context.Context, uri string) (interface{}, error) {
			return "This is demo content", nil
		})

	// Register a prompt for demonstration
	server.Prompt("greeting", "Generate a greeting", []mcp.PromptArgument{
		{Name: "name", Description: "Name to greet", Required: true},
	}, func(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
		userName := "World"
		if nameValue, exists := args["name"]; exists {
			if nameStr, ok := nameValue.(string); ok {
				userName = nameStr
			}
		}
		return "Hello, " + userName + "!", nil
	})

	// Start server
	log.Println("Starting MCP server with full middleware stack...")
	log.Println("Middleware order:", middlewareConfig.Order)
	
	// Print example requests for testing
	printExampleRequests()

	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}

func printExampleRequests() {
	log.Println("\n=== Example Test Requests ===")
	
	// Example 1: Tool call with authentication
	toolCallReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "echo",
			"arguments": map[string]interface{}{
				"message": "Hello Middleware!",
				"count":   3,
			},
			"_auth": "Bearer your-jwt-token-here",
		},
	}
	
	if reqBytes, err := json.MarshalIndent(toolCallReq, "", "  "); err == nil {
		log.Printf("\n1. Authenticated Tool Call:\n%s\n", string(reqBytes))
	}

	// Example 2: Resource read
	resourceReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "resources/read",
		"params": map[string]interface{}{
			"uri":   "file://demo.txt",
			"_auth": "Bearer your-jwt-token-here",
		},
	}
	
	if reqBytes, err := json.MarshalIndent(resourceReq, "", "  "); err == nil {
		log.Printf("2. Resource Read:\n%s\n", string(reqBytes))
	}

	// Example 3: Prompt execution
	promptReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "prompts/get",
		"params": map[string]interface{}{
			"name": "greeting",
			"arguments": map[string]interface{}{
				"name": "Middleware User",
			},
			"_auth": "Bearer your-jwt-token-here",
		},
	}
	
	if reqBytes, err := json.MarshalIndent(promptReq, "", "  "); err == nil {
		log.Printf("3. Prompt Execution:\n%s\n", string(reqBytes))
	}

	// Example 4: Unreliable tool to test circuit breaker
	unreliableReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "unreliable",
			"arguments": map[string]interface{}{
				"shouldFail": true,
			},
			"_auth": "Bearer your-jwt-token-here",
		},
	}
	
	if reqBytes, err := json.MarshalIndent(unreliableReq, "", "  "); err == nil {
		log.Printf("4. Unreliable Tool (Circuit Breaker Test):\n%s\n", string(reqBytes))
	}

	log.Println("\n=== Middleware Features Demonstrated ===")
	log.Println("- Request logging with JSON format and body sanitization")
	log.Println("- Metrics collection with user tracking and detailed timing")
	log.Println("- Unique request ID generation and tracking")
	log.Println("- JWT authentication with token validation")
	log.Println("- Rate limiting per user with sliding window")
	log.Println("- Request validation with custom rules and JSON schema")
	log.Println("- Circuit breaker with failure detection and state management")
	log.Println("\nSend requests via stdin to see middleware in action!")
}