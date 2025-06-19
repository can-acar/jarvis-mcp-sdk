package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"context"
)

// API Response structures
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *APIError   `json:"error,omitempty"`
}

type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

type ToolCallRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type ResourceReadRequest struct {
	URI string `json:"uri"`
}

type PromptGetRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// handleHealth handles health check requests
func (wt *WebTransport) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		wt.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": fmt.Sprintf("%d", wt.server.getCurrentTimestamp()),
		"version":   wt.server.version,
		"uptime":    "unknown", // Could be calculated if we track start time
	}

	// Add concurrency metrics if available
	if wt.server.concurrencyEnabled && wt.server.workerPool != nil {
		metrics := wt.server.GetConcurrencyMetrics()
		health["metrics"] = map[string]interface{}{
			"total_requests":   metrics.TotalRequests,
			"active_requests":  metrics.ActiveRequests,
			"failed_requests":  metrics.FailedRequests,
			"requests_per_sec": metrics.RequestsPerSecond,
		}
	}

	wt.writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    health,
	})
}

// handleServerInfo handles server info requests
func (wt *WebTransport) handleServerInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		wt.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	info := map[string]interface{}{
		"name":         wt.server.name,
		"version":      wt.server.version,
		"capabilities": wt.server.capabilities,
		"features": map[string]bool{
			"concurrency":     wt.server.concurrencyEnabled,
			"streaming":       wt.server.streamingManager != nil,
			"web_transport":   true,
			"authentication":  wt.config.AuthToken != "",
		},
		"statistics": map[string]interface{}{
			"tools_count":     len(wt.server.tools),
			"resources_count": len(wt.server.resources),
			"prompts_count":   len(wt.server.prompts),
		},
	}

	wt.writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    info,
	})
}

// handleToolsList handles GET /api/v1/tools/list
func (wt *WebTransport) handleToolsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		wt.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Create MCP request
	mcpRequest := &Request{
		JSONRPC: "2.0",
		ID:      wt.generateRequestID(),
		Method:  "tools/list",
	}

	// Process via MCP server
	ctx := context.Background()
	response := wt.server.HandleRequest(ctx, mcpRequest)

	if response.Error != nil {
		wt.writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Error: &APIError{
				Code:    response.Error.Code,
				Message: response.Error.Message,
			},
		})
		return
	}

	wt.writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    response.Result,
	})
}

// handleToolsCall handles POST /api/v1/tools/call
func (wt *WebTransport) handleToolsCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		wt.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Parse request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		wt.writeError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}

	var toolCall ToolCallRequest
	if err := json.Unmarshal(body, &toolCall); err != nil {
		wt.writeError(w, http.StatusBadRequest, "Invalid JSON in request body")
		return
	}

	// Validate required fields
	if toolCall.Name == "" {
		wt.writeError(w, http.StatusBadRequest, "Tool name is required")
		return
	}

	// Prepare MCP request params
	params := map[string]interface{}{
		"name":      toolCall.Name,
		"arguments": toolCall.Arguments,
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		wt.writeError(w, http.StatusInternalServerError, "Failed to serialize parameters")
		return
	}

	// Create MCP request
	mcpRequest := &Request{
		JSONRPC: "2.0",
		ID:      wt.generateRequestID(),
		Method:  "tools/call",
		Params:  paramsJSON,
	}

	// Process via MCP server
	ctx := context.Background()
	response := wt.server.HandleRequest(ctx, mcpRequest)

	if response.Error != nil {
		wt.writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Error: &APIError{
				Code:    response.Error.Code,
				Message: response.Error.Message,
			},
		})
		return
	}

	wt.writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    response.Result,
	})
}

// handleResourcesList handles GET /api/v1/resources/list
func (wt *WebTransport) handleResourcesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		wt.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Create MCP request
	mcpRequest := &Request{
		JSONRPC: "2.0",
		ID:      wt.generateRequestID(),
		Method:  "resources/list",
	}

	// Process via MCP server
	ctx := context.Background()
	response := wt.server.HandleRequest(ctx, mcpRequest)

	if response.Error != nil {
		wt.writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Error: &APIError{
				Code:    response.Error.Code,
				Message: response.Error.Message,
			},
		})
		return
	}

	wt.writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    response.Result,
	})
}

// handleResourcesRead handles POST /api/v1/resources/read
func (wt *WebTransport) handleResourcesRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		wt.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Parse request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		wt.writeError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}

	var resourceRead ResourceReadRequest
	if err := json.Unmarshal(body, &resourceRead); err != nil {
		wt.writeError(w, http.StatusBadRequest, "Invalid JSON in request body")
		return
	}

	// Validate required fields
	if resourceRead.URI == "" {
		wt.writeError(w, http.StatusBadRequest, "Resource URI is required")
		return
	}

	// Prepare MCP request params
	params := map[string]interface{}{
		"uri": resourceRead.URI,
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		wt.writeError(w, http.StatusInternalServerError, "Failed to serialize parameters")
		return
	}

	// Create MCP request
	mcpRequest := &Request{
		JSONRPC: "2.0",
		ID:      wt.generateRequestID(),
		Method:  "resources/read",
		Params:  paramsJSON,
	}

	// Process via MCP server
	ctx := context.Background()
	response := wt.server.HandleRequest(ctx, mcpRequest)

	if response.Error != nil {
		wt.writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Error: &APIError{
				Code:    response.Error.Code,
				Message: response.Error.Message,
			},
		})
		return
	}

	wt.writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    response.Result,
	})
}

// handlePromptsList handles GET /api/v1/prompts/list
func (wt *WebTransport) handlePromptsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		wt.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Create MCP request
	mcpRequest := &Request{
		JSONRPC: "2.0",
		ID:      wt.generateRequestID(),
		Method:  "prompts/list",
	}

	// Process via MCP server
	ctx := context.Background()
	response := wt.server.HandleRequest(ctx, mcpRequest)

	if response.Error != nil {
		wt.writeJSON(w, http.StatusInternalServerError, APIResponse{
			Success: false,
			Error: &APIError{
				Code:    response.Error.Code,
				Message: response.Error.Message,
			},
		})
		return
	}

	wt.writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    response.Result,
	})
}

// handlePromptsGet handles POST /api/v1/prompts/get
func (wt *WebTransport) handlePromptsGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		wt.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Parse request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		wt.writeError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}

	var promptGet PromptGetRequest
	if err := json.Unmarshal(body, &promptGet); err != nil {
		wt.writeError(w, http.StatusBadRequest, "Invalid JSON in request body")
		return
	}

	// Validate required fields
	if promptGet.Name == "" {
		wt.writeError(w, http.StatusBadRequest, "Prompt name is required")
		return
	}

	// Prepare MCP request params
	params := map[string]interface{}{
		"name":      promptGet.Name,
		"arguments": promptGet.Arguments,
	}

	paramsJSON, err := json.Marshal(params)
	if err != nil {
		wt.writeError(w, http.StatusInternalServerError, "Failed to serialize parameters")
		return
	}

	// Create MCP request
	mcpRequest := &Request{
		JSONRPC: "2.0",
		ID:      wt.generateRequestID(),
		Method:  "prompts/get",
		Params:  paramsJSON,
	}

	// Process via MCP server
	ctx := context.Background()
	response := wt.server.HandleRequest(ctx, mcpRequest)

	if response.Error != nil {
		wt.writeJSON(w, http.StatusBadRequest, APIResponse{
			Success: false,
			Error: &APIError{
				Code:    response.Error.Code,
				Message: response.Error.Message,
			},
		})
		return
	}

	wt.writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    response.Result,
	})
}

// handleDashboard serves a basic web dashboard
func (wt *WebTransport) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		wt.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	html := wt.generateDashboardHTML()
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))
}

// generateDashboardHTML generates a basic HTML dashboard
func (wt *WebTransport) generateDashboardHTML() string {
	return fmt.Sprintf(`
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s - Jarvis MCP Dashboard</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; background-color: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; background: white; padding: 20px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .header { border-bottom: 1px solid #eee; padding-bottom: 10px; margin-bottom: 20px; }
        .stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 20px; margin-bottom: 20px; }
        .stat-card { background: #f8f9fa; padding: 15px; border-radius: 5px; border-left: 4px solid #007bff; }
        .endpoints { margin-top: 30px; }
        .endpoint { background: #f8f9fa; margin: 10px 0; padding: 15px; border-radius: 5px; border-left: 4px solid #28a745; }
        .method { display: inline-block; padding: 3px 8px; border-radius: 3px; color: white; font-size: 12px; margin-right: 10px; }
        .get { background-color: #007bff; }
        .post { background-color: #28a745; }
        pre { background: #f4f4f4; padding: 10px; border-radius: 3px; overflow-x: auto; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🤖 %s</h1>
            <p>Jarvis MCP SDK - Web Dashboard</p>
        </div>
        
        <div class="stats">
            <div class="stat-card">
                <h3>Tools</h3>
                <p>%d registered</p>
            </div>
            <div class="stat-card">
                <h3>Resources</h3>
                <p>%d registered</p>
            </div>
            <div class="stat-card">
                <h3>Prompts</h3>
                <p>%d registered</p>
            </div>
            <div class="stat-card">
                <h3>Version</h3>
                <p>%s</p>
            </div>
        </div>

        <div class="endpoints">
            <h2>API Endpoints</h2>
            
            <div class="endpoint">
                <span class="method get">GET</span>
                <strong>/health</strong>
                <p>Health check endpoint</p>
            </div>
            
            <div class="endpoint">
                <span class="method get">GET</span>
                <strong>/api/v1/server/info</strong>
                <p>Server information and capabilities</p>
            </div>
            
            <div class="endpoint">
                <span class="method get">GET</span>
                <strong>/api/v1/tools/list</strong>
                <p>List all available tools</p>
            </div>
            
            <div class="endpoint">
                <span class="method post">POST</span>
                <strong>/api/v1/tools/call</strong>
                <p>Call a tool</p>
                <pre>{"name": "tool_name", "arguments": {...}}</pre>
            </div>
            
            <div class="endpoint">
                <span class="method get">GET</span>
                <strong>/api/v1/resources/list</strong>
                <p>List all available resources</p>
            </div>
            
            <div class="endpoint">
                <span class="method post">POST</span>
                <strong>/api/v1/resources/read</strong>
                <p>Read a resource</p>
                <pre>{"uri": "resource_uri"}</pre>
            </div>
        </div>
        
        <div style="margin-top: 30px; padding: 20px; background: #e3f2fd; border-radius: 5px;">
            <h3>🔐 Authentication</h3>
            <p>Include the following header in your requests:</p>
            <pre>Authorization: Bearer %s</pre>
        </div>
    </div>
</body>
</html>`,
		wt.server.name,
		wt.server.name,
		len(wt.server.tools),
		len(wt.server.resources),
		len(wt.server.prompts),
		wt.server.version,
		wt.config.AuthToken,
	)
}

// generateRequestID generates a unique request ID
func (wt *WebTransport) generateRequestID() string {
	return fmt.Sprintf("web_%d", wt.server.getCurrentTimestamp())
}

// Helper method for server to get current timestamp  
func (s *Server) getCurrentTimestamp() int64 {
	// Simple incrementing counter for request IDs
	// In a real implementation, you might use time.Now().UnixNano()
	return int64(len(s.tools) + len(s.resources) + len(s.prompts))
}