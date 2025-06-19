package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	jarvis "github.com/jarvis-mcp/jarvis-mcp-sdk"
)

func main() {
	// Create a new Jarvis MCP server
	server := jarvis.NewServer("calculator", "1.0.0")

	// Register tools with fluent API (similar to FastMCP decorators)
	server.Tool("add", "Add two numbers", addTool).
		Tool("multiply", "Multiply two numbers", multiplyTool).
		Tool("divide", "Divide two numbers", divideTool)

	// Register a resource
	server.Resource("memory://calculations", "calculations", "Recent calculations", "text/plain", calculationsResource)

	// Register a prompt
	server.Prompt("math_problem", "Generate a math problem", []jarvis.PromptArgument{
		{Name: "difficulty", Description: "Problem difficulty (easy, medium, hard)", Required: false},
	}, mathProblemPrompt)

	// Start the server
	if err := server.Run(); err != nil {
		panic(err)
	}
}

// Tool handlers
func addTool(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var args struct {
		A float64 `json:"a"`
		B float64 `json:"b"`
	}
	
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	
	result := args.A + args.B
	return fmt.Sprintf("%.2f + %.2f = %.2f", args.A, args.B, result), nil
}

func multiplyTool(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var args struct {
		A float64 `json:"a"`
		B float64 `json:"b"`
	}
	
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	
	result := args.A * args.B
	return fmt.Sprintf("%.2f × %.2f = %.2f", args.A, args.B, result), nil
}

func divideTool(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var args struct {
		A float64 `json:"a"`
		B float64 `json:"b"`
	}
	
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	
	if args.B == 0 {
		return nil, fmt.Errorf("division by zero")
	}
	
	result := args.A / args.B
	return fmt.Sprintf("%.2f ÷ %.2f = %.2f", args.A, args.B, result), nil
}

// Resource handler
func calculationsResource(ctx context.Context, uri string) (interface{}, error) {
	return "Recent calculations:\n" +
		"10 + 5 = 15\n" +
		"20 × 3 = 60\n" +
		"100 ÷ 4 = 25", nil
}

// Prompt handler
func mathProblemPrompt(ctx context.Context, name string, arguments map[string]interface{}) (interface{}, error) {
	difficulty := "medium"
	if d, ok := arguments["difficulty"].(string); ok {
		difficulty = d
	}
	
	problems := map[string]string{
		"easy":   "What is 5 + 3?",
		"medium": "Calculate the area of a circle with radius 7cm (π ≈ 3.14159)",
		"hard":   "Solve for x: 2x² - 5x + 2 = 0",
	}
	
	problem, exists := problems[difficulty]
	if !exists {
		problem = problems["medium"]
	}
	
	return fmt.Sprintf("Math Problem (%s): %s", difficulty, problem), nil
}