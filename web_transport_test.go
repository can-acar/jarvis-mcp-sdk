package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebTransportBasic(t *testing.T) {
	// Create server with tools
	server := NewServer("test-web-server", "1.0.0")
	
	// Add a test tool
	server.Tool("add", "Add two numbers", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		var args struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		if err := json.Unmarshal(params, &args); err != nil {
			return nil, err
		}
		return args.A + args.B, nil
	})
	
	// Add a test resource
	server.Resource("test://resource", "test_resource", "Test resource", "text/plain", func(ctx context.Context, uri string) (interface{}, error) {
		return "test resource content", nil
	})
	
	// Enable web transport
	config := WebConfig{
		Port:       8081,
		Host:       "localhost",
		AuthToken:  "test-token",
		EnableCORS: true,
	}
	server.EnableWebTransport(config)
	
	// Start web transport
	err := server.StartWebTransport()
	require.NoError(t, err)
	
	// Give server time to start
	time.Sleep(100 * time.Millisecond)
	
	// Cleanup
	defer func() {
		err := server.StopWebTransport()
		assert.NoError(t, err)
	}()
	
	baseURL := "http://localhost:8081"
	
	// Test health endpoint
	t.Run("Health Check", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/health")
		require.NoError(t, err)
		defer resp.Body.Close()
		
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		
		var apiResp APIResponse
		err = json.NewDecoder(resp.Body).Decode(&apiResp)
		require.NoError(t, err)
		
		assert.True(t, apiResp.Success)
		assert.NotNil(t, apiResp.Data)
	})
	
	// Test unauthorized access
	t.Run("Unauthorized Access", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/v1/tools/list")
		require.NoError(t, err)
		defer resp.Body.Close()
		
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
	
	// Test authorized access
	t.Run("Authorized Tools List", func(t *testing.T) {
		req, err := http.NewRequest("GET", baseURL+"/api/v1/tools/list", nil)
		require.NoError(t, err)
		
		req.Header.Set("Authorization", "Bearer test-token")
		
		client := &http.Client{}
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		
		var apiResp APIResponse
		err = json.NewDecoder(resp.Body).Decode(&apiResp)
		require.NoError(t, err)
		
		assert.True(t, apiResp.Success)
		assert.NotNil(t, apiResp.Data)
	})
	
	// Test tool call
	t.Run("Tool Call", func(t *testing.T) {
		callReq := ToolCallRequest{
			Name: "add",
			Arguments: map[string]interface{}{
				"a": 5.0,
				"b": 3.0,
			},
		}
		
		jsonData, err := json.Marshal(callReq)
		require.NoError(t, err)
		
		req, err := http.NewRequest("POST", baseURL+"/api/v1/tools/call", bytes.NewBuffer(jsonData))
		require.NoError(t, err)
		
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("Content-Type", "application/json")
		
		client := &http.Client{}
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		
		var apiResp APIResponse
		err = json.NewDecoder(resp.Body).Decode(&apiResp)
		require.NoError(t, err)
		
		assert.True(t, apiResp.Success)
		assert.NotNil(t, apiResp.Data)
	})
	
	// Test resource read
	t.Run("Resource Read", func(t *testing.T) {
		readReq := ResourceReadRequest{
			URI: "test://resource",
		}
		
		jsonData, err := json.Marshal(readReq)
		require.NoError(t, err)
		
		req, err := http.NewRequest("POST", baseURL+"/api/v1/resources/read", bytes.NewBuffer(jsonData))
		require.NoError(t, err)
		
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("Content-Type", "application/json")
		
		client := &http.Client{}
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		
		var apiResp APIResponse
		err = json.NewDecoder(resp.Body).Decode(&apiResp)
		require.NoError(t, err)
		
		assert.True(t, apiResp.Success)
		assert.NotNil(t, apiResp.Data)
	})
}

func TestWebTransportCORS(t *testing.T) {
	server := NewServer("test-cors-server", "1.0.0")
	
	config := WebConfig{
		Port:       8082,
		Host:       "localhost",
		EnableCORS: true,
	}
	server.EnableWebTransport(config)
	
	err := server.StartWebTransport()
	require.NoError(t, err)
	
	time.Sleep(100 * time.Millisecond)
	
	defer func() {
		err := server.StopWebTransport()
		assert.NoError(t, err)
	}()
	
	// Test OPTIONS request (CORS preflight)
	req, err := http.NewRequest("OPTIONS", "http://localhost:8082/api/v1/tools/list", nil)
	require.NoError(t, err)
	
	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "*", resp.Header.Get("Access-Control-Allow-Origin"))
	assert.Contains(t, resp.Header.Get("Access-Control-Allow-Methods"), "GET")
	assert.Contains(t, resp.Header.Get("Access-Control-Allow-Methods"), "POST")
}

func TestWebTransportWithoutAuth(t *testing.T) {
	server := NewServer("test-noauth-server", "1.0.0")
	
	server.Tool("test", "Test tool", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		return "test result", nil
	})
	
	config := WebConfig{
		Port:       8083,
		Host:       "localhost",
		AuthToken:  "", // No auth token
		EnableCORS: true,
	}
	server.EnableWebTransport(config)
	
	err := server.StartWebTransport()
	require.NoError(t, err)
	
	time.Sleep(100 * time.Millisecond)
	
	defer func() {
		err := server.StopWebTransport()
		assert.NoError(t, err)
	}()
	
	// Test access without authentication (should work)
	resp, err := http.Get("http://localhost:8083/api/v1/tools/list")
	require.NoError(t, err)
	defer resp.Body.Close()
	
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestWebTransportServerInfo(t *testing.T) {
	server := NewServer("info-test-server", "2.0.0")
	
	// Add some tools and resources for counting
	server.Tool("tool1", "Tool 1", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		return "result1", nil
	})
	server.Tool("tool2", "Tool 2", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		return "result2", nil
	})
	
	server.Resource("res://1", "resource1", "Resource 1", "text/plain", func(ctx context.Context, uri string) (interface{}, error) {
		return "content1", nil
	})
	
	config := WebConfig{
		Port:       8084,
		Host:       "localhost",
		AuthToken:  "info-token",
		EnableCORS: true,
	}
	server.EnableWebTransport(config)
	
	err := server.StartWebTransport()
	require.NoError(t, err)
	
	time.Sleep(100 * time.Millisecond)
	
	defer func() {
		err := server.StopWebTransport()
		assert.NoError(t, err)
	}()
	
	// Test server info endpoint
	req, err := http.NewRequest("GET", "http://localhost:8084/api/v1/server/info", nil)
	require.NoError(t, err)
	
	req.Header.Set("Authorization", "Bearer info-token")
	
	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	
	var apiResp APIResponse
	err = json.NewDecoder(resp.Body).Decode(&apiResp)
	require.NoError(t, err)
	
	assert.True(t, apiResp.Success)
	
	// Check server info structure
	data, ok := apiResp.Data.(map[string]interface{})
	require.True(t, ok)
	
	assert.Equal(t, "info-test-server", data["name"])
	assert.Equal(t, "2.0.0", data["version"])
	
	statistics, ok := data["statistics"].(map[string]interface{})
	require.True(t, ok)
	
	assert.Equal(t, float64(2), statistics["tools_count"])     // 2 tools
	assert.Equal(t, float64(1), statistics["resources_count"]) // 1 resource
	assert.Equal(t, float64(0), statistics["prompts_count"])   // 0 prompts
}

func TestWebTransportDashboard(t *testing.T) {
	server := NewServer("dashboard-test", "1.0.0")
	
	config := WebConfig{
		Port:            8085,
		Host:            "localhost",
		EnableDashboard: true,
	}
	server.EnableWebTransport(config)
	
	err := server.StartWebTransport()
	require.NoError(t, err)
	
	time.Sleep(100 * time.Millisecond)
	
	defer func() {
		err := server.StopWebTransport()
		assert.NoError(t, err)
	}()
	
	// Test dashboard endpoint
	resp, err := http.Get("http://localhost:8085/dashboard")
	require.NoError(t, err)
	defer resp.Body.Close()
	
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/html", resp.Header.Get("Content-Type"))
	
	// Read response body
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	
	// Check that it contains expected HTML elements
	bodyStr := string(body)
	assert.Contains(t, bodyStr, "Jarvis MCP Dashboard")
	assert.Contains(t, bodyStr, "dashboard-test")
	assert.Contains(t, bodyStr, "API Endpoints")
}

func TestWebTransportMethodNotAllowed(t *testing.T) {
	server := NewServer("method-test", "1.0.0")
	
	config := WebConfig{
		Port: 8086,
		Host: "localhost",
	}
	server.EnableWebTransport(config)
	
	err := server.StartWebTransport()
	require.NoError(t, err)
	
	time.Sleep(100 * time.Millisecond)
	
	defer func() {
		err := server.StopWebTransport()
		assert.NoError(t, err)
	}()
	
	// Test wrong method on GET endpoint
	resp, err := http.Post("http://localhost:8086/api/v1/tools/list", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
	
	// Test wrong method on POST endpoint
	req, err := http.NewRequest("GET", "http://localhost:8086/api/v1/tools/call", nil)
	require.NoError(t, err)
	
	client := &http.Client{}
	resp, err = client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestWebTransportErrorHandling(t *testing.T) {
	server := NewServer("error-test", "1.0.0")
	
	// Add a tool that returns an error
	server.Tool("error_tool", "Tool that errors", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		return nil, fmt.Errorf("test error")
	})
	
	config := WebConfig{
		Port: 8087,
		Host: "localhost",
	}
	server.EnableWebTransport(config)
	
	err := server.StartWebTransport()
	require.NoError(t, err)
	
	time.Sleep(100 * time.Millisecond)
	
	defer func() {
		err := server.StopWebTransport()
		assert.NoError(t, err)
	}()
	
	// Test calling tool that returns error
	callReq := ToolCallRequest{
		Name:      "error_tool",
		Arguments: map[string]interface{}{},
	}
	
	jsonData, err := json.Marshal(callReq)
	require.NoError(t, err)
	
	req, err := http.NewRequest("POST", "http://localhost:8087/api/v1/tools/call", bytes.NewBuffer(jsonData))
	require.NoError(t, err)
	
	req.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	
	var apiResp APIResponse
	err = json.NewDecoder(resp.Body).Decode(&apiResp)
	require.NoError(t, err)
	
	assert.False(t, apiResp.Success)
	assert.NotNil(t, apiResp.Error)
	assert.Contains(t, apiResp.Error.Message, "test error")
}