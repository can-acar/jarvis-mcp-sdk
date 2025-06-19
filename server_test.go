package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer(t *testing.T) {
	server := NewServer("test-server", "1.0.0")
	
	assert.Equal(t, "test-server", server.name)
	assert.Equal(t, "1.0.0", server.version)
	assert.NotNil(t, server.tools)
	assert.NotNil(t, server.toolHandlers)
	assert.NotNil(t, server.resources)
	assert.NotNil(t, server.resourceHandlers)
	assert.NotNil(t, server.prompts)
	assert.NotNil(t, server.promptHandlers)
}

func TestServerTool(t *testing.T) {
	server := NewServer("test", "1.0.0")
	
	handler := func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		return "test result", nil
	}
	
	server.Tool("test_tool", "Test tool description", handler)
	
	assert.Contains(t, server.tools, "test_tool")
	assert.Equal(t, "test_tool", server.tools["test_tool"].Name)
	assert.Equal(t, "Test tool description", server.tools["test_tool"].Description)
	assert.Contains(t, server.toolHandlers, "test_tool")
}

func TestServerResource(t *testing.T) {
	server := NewServer("test", "1.0.0")
	
	handler := func(ctx context.Context, uri string) (interface{}, error) {
		return "resource content", nil
	}
	
	server.Resource("test://resource", "test_resource", "Test resource", "text/plain", handler)
	
	assert.Contains(t, server.resources, "test://resource")
	assert.Equal(t, "test://resource", server.resources["test://resource"].URI)
	assert.Equal(t, "test_resource", server.resources["test://resource"].Name)
	assert.Equal(t, "Test resource", server.resources["test://resource"].Description)
	assert.Equal(t, "text/plain", server.resources["test://resource"].MimeType)
}

func TestServerPrompt(t *testing.T) {
	server := NewServer("test", "1.0.0")
	
	handler := func(ctx context.Context, name string, arguments map[string]interface{}) (interface{}, error) {
		return "prompt result", nil
	}
	
	args := []PromptArgument{
		{Name: "arg1", Description: "First argument", Required: true},
	}
	
	server.Prompt("test_prompt", "Test prompt", args, handler)
	
	assert.Contains(t, server.prompts, "test_prompt")
	assert.Equal(t, "test_prompt", server.prompts["test_prompt"].Name)
	assert.Equal(t, "Test prompt", server.prompts["test_prompt"].Description)
	assert.Len(t, server.prompts["test_prompt"].Arguments, 1)
}

func TestHandleInitialize(t *testing.T) {
	server := NewServer("test-server", "1.0.0")
	ctx := context.Background()
	
	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  json.RawMessage(`{}`),
	}
	
	response := server.HandleRequest(ctx, req)
	
	assert.Equal(t, "2.0", response.JSONRPC)
	assert.Equal(t, 1, response.ID)
	assert.Nil(t, response.Error)
	
	result, ok := response.Result.(map[string]interface{})
	require.True(t, ok)
	
	assert.Equal(t, "2024-11-05", result["protocolVersion"])
	
	serverInfo, ok := result["serverInfo"].(map[string]string)
	require.True(t, ok)
	assert.Equal(t, "test-server", serverInfo["name"])
	assert.Equal(t, "1.0.0", serverInfo["version"])
}

func TestHandleToolsList(t *testing.T) {
	server := NewServer("test", "1.0.0")
	
	handler := func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		return "result", nil
	}
	
	server.Tool("tool1", "First tool", handler)
	server.Tool("tool2", "Second tool", handler)
	
	ctx := context.Background()
	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}
	
	response := server.HandleRequest(ctx, req)
	
	assert.Nil(t, response.Error)
	
	result, ok := response.Result.(map[string]interface{})
	require.True(t, ok)
	
	tools, ok := result["tools"].([]*Tool)
	require.True(t, ok)
	assert.Len(t, tools, 2)
}

func TestHandleToolsCall(t *testing.T) {
	server := NewServer("test", "1.0.0")
	
	handler := func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		var args struct {
			Value string `json:"value"`
		}
		json.Unmarshal(params, &args)
		return "Hello " + args.Value, nil
	}
	
	server.Tool("greet", "Greeting tool", handler)
	
	ctx := context.Background()
	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name": "greet", "arguments": {"value": "World"}}`),
	}
	
	response := server.HandleRequest(ctx, req)
	
	assert.Nil(t, response.Error)
	
	result, ok := response.Result.(map[string]interface{})
	require.True(t, ok)
	
	content, ok := result["content"].([]map[string]interface{})
	require.True(t, ok)
	require.Len(t, content, 1)
	
	assert.Equal(t, "text", content[0]["type"])
	assert.Equal(t, "Hello World", content[0]["text"])
}

func TestMethodNotFound(t *testing.T) {
	server := NewServer("test", "1.0.0")
	ctx := context.Background()
	
	req := &Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "unknown/method",
	}
	
	response := server.HandleRequest(ctx, req)
	
	assert.NotNil(t, response.Error)
	assert.Equal(t, -32601, response.Error.Code)
	assert.Contains(t, response.Error.Message, "Method not found")
}

func TestFluentAPI(t *testing.T) {
	server := NewServer("test", "1.0.0")
	
	handler := func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		return "result", nil
	}
	
	resourceHandler := func(ctx context.Context, uri string) (interface{}, error) {
		return "resource", nil
	}
	
	promptHandler := func(ctx context.Context, name string, arguments map[string]interface{}) (interface{}, error) {
		return "prompt", nil
	}
	
	// Test method chaining
	result := server.
		Tool("tool1", "Tool 1", handler).
		Tool("tool2", "Tool 2", handler).
		Resource("res://1", "resource1", "Resource 1", "text/plain", resourceHandler).
		Prompt("prompt1", "Prompt 1", nil, promptHandler)
	
	assert.Same(t, server, result)
	assert.Len(t, server.tools, 2)
	assert.Len(t, server.resources, 1)
	assert.Len(t, server.prompts, 1)
}

func TestRunWithTransport(t *testing.T) {
	server := NewServer("test", "1.0.0")
	
	handler := func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		return "test result", nil
	}
	
	server.Tool("test", "Test tool", handler)
	
	// Prepare input
	initRequest := Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  json.RawMessage(`{}`),
	}
	
	// This would normally block, so we'll test the request handling directly
	ctx := context.Background()
	response := server.HandleRequest(ctx, &initRequest)
	
	assert.NotNil(t, response)
	assert.Nil(t, response.Error)
}