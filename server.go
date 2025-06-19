package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
)

// Server represents an MCP server
type Server struct {
	name         string
	version      string
	tools        map[string]*Tool
	toolHandlers map[string]ToolHandler
	resources    map[string]*Resource
	resourceHandlers map[string]ResourceHandler
	prompts      map[string]*Prompt
	promptHandlers map[string]PromptHandler
	capabilities ServerCapabilities
	logger       *log.Logger
	workerPool   *WorkerPool
	concurrencyEnabled bool
	streamingManager *StreamingManager
	webTransport *WebTransport
	wsManager *WebSocketManager
	sseManager *SSEManager
	middlewareManager *MiddlewareManager
}

// NewServer creates a new MCP server
func NewServer(name, version string) *Server {
	return &Server{
		name:             name,
		version:          version,
		tools:            make(map[string]*Tool),
		toolHandlers:     make(map[string]ToolHandler),
		resources:        make(map[string]*Resource),
		resourceHandlers: make(map[string]ResourceHandler),
		prompts:          make(map[string]*Prompt),
		promptHandlers:   make(map[string]PromptHandler),
		capabilities: ServerCapabilities{
			Tools:     &ToolsCapability{ListChanged: false},
			Resources: &ResourcesCapability{Subscribe: false, ListChanged: false},
			Prompts:   &PromptsCapability{ListChanged: false},
		},
		logger: log.New(os.Stderr, "[MCP] ", log.LstdFlags),
	}
}

// Tool decorator-like function to register a tool
func (s *Server) Tool(name, description string, handler ToolHandler) *Server {
	// Generate JSON schema from handler function
	schema := s.generateSchemaFromHandler(handler)
	
	tool := &Tool{
		Name:        name,
		Description: description,
		InputSchema: schema,
	}
	
	s.tools[name] = tool
	s.toolHandlers[name] = handler
	return s
}

// Resource registers a resource handler
func (s *Server) Resource(uri, name, description, mimeType string, handler ResourceHandler) *Server {
	resource := &Resource{
		URI:         uri,
		Name:        name,
		Description: description,
		MimeType:    mimeType,
	}
	
	s.resources[uri] = resource
	s.resourceHandlers[uri] = handler
	return s
}

// Prompt registers a prompt handler
func (s *Server) Prompt(name, description string, arguments []PromptArgument, handler PromptHandler) *Server {
	prompt := &Prompt{
		Name:        name,
		Description: description,
		Arguments:   arguments,
	}
	
	s.prompts[name] = prompt
	s.promptHandlers[name] = handler
	return s
}

// generateSchemaFromHandler generates JSON schema from function signature
func (s *Server) generateSchemaFromHandler(handler ToolHandler) JSONSchema {
	// This is a simplified schema generator
	// In a real implementation, you'd use reflection to analyze the handler's expected parameters
	return JSONSchema{
		Type: "object",
		Properties: map[string]JSONSchema{
			"input": {
				Type:        "string",
				Description: "Input parameters",
			},
		},
	}
}

// HandleRequest processes an MCP request
func (s *Server) HandleRequest(ctx context.Context, req *Request) *Response {
	// Use concurrent processing if enabled
	if s.concurrencyEnabled && s.workerPool != nil {
		return s.workerPool.SubmitRequest(req)
	}
	
	// Process synchronously
	return s.handleRequestSync(ctx, req)
}

// handleRequestSync processes an MCP request synchronously
func (s *Server) handleRequestSync(ctx context.Context, req *Request) *Response {
	response := &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	switch req.Method {
	case "initialize":
		response.Result = s.handleInitialize(ctx, req.Params)
	case "tools/list":
		response.Result = s.handleToolsList(ctx)
	case "tools/call":
		result, err := s.handleToolsCall(ctx, req.Params)
		if err != nil {
			response.Error = &Error{
				Code:    -32603,
				Message: err.Error(),
			}
		} else {
			response.Result = result
		}
	case "resources/list":
		response.Result = s.handleResourcesList(ctx)
	case "resources/read":
		result, err := s.handleResourcesRead(ctx, req.Params)
		if err != nil {
			response.Error = &Error{
				Code:    -32603,
				Message: err.Error(),
			}
		} else {
			response.Result = result
		}
	case "prompts/list":
		response.Result = s.handlePromptsList(ctx)
	case "prompts/get":
		result, err := s.handlePromptsGet(ctx, req.Params)
		if err != nil {
			response.Error = &Error{
				Code:    -32603,
				Message: err.Error(),
			}
		} else {
			response.Result = result
		}
	default:
		response.Error = &Error{
			Code:    -32601,
			Message: fmt.Sprintf("Method not found: %s", req.Method),
		}
	}

	return response
}

func (s *Server) handleInitialize(ctx context.Context, params json.RawMessage) interface{} {
	return map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    s.capabilities,
		"serverInfo": map[string]string{
			"name":    s.name,
			"version": s.version,
		},
	}
}

func (s *Server) handleToolsList(ctx context.Context) interface{} {
	tools := make([]*Tool, 0, len(s.tools))
	for _, tool := range s.tools {
		tools = append(tools, tool)
	}
	return map[string]interface{}{
		"tools": tools,
	}
}

func (s *Server) handleToolsCall(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var callParams struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	
	if err := json.Unmarshal(params, &callParams); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	
	handler, exists := s.toolHandlers[callParams.Name]
	if !exists {
		return nil, fmt.Errorf("tool not found: %s", callParams.Name)
	}
	
	result, err := handler(ctx, callParams.Arguments)
	if err != nil {
		return nil, err
	}
	
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("%v", result),
			},
		},
	}, nil
}

func (s *Server) handleResourcesList(ctx context.Context) interface{} {
	resources := make([]*Resource, 0, len(s.resources))
	for _, resource := range s.resources {
		resources = append(resources, resource)
	}
	return map[string]interface{}{
		"resources": resources,
	}
}

func (s *Server) handleResourcesRead(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var readParams struct {
		URI string `json:"uri"`
	}
	
	if err := json.Unmarshal(params, &readParams); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	
	handler, exists := s.resourceHandlers[readParams.URI]
	if !exists {
		return nil, fmt.Errorf("resource not found: %s", readParams.URI)
	}
	
	result, err := handler(ctx, readParams.URI)
	if err != nil {
		return nil, err
	}
	
	return map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"uri":      readParams.URI,
				"mimeType": s.resources[readParams.URI].MimeType,
				"text":     fmt.Sprintf("%v", result),
			},
		},
	}, nil
}

func (s *Server) handlePromptsList(ctx context.Context) interface{} {
	prompts := make([]*Prompt, 0, len(s.prompts))
	for _, prompt := range s.prompts {
		prompts = append(prompts, prompt)
	}
	return map[string]interface{}{
		"prompts": prompts,
	}
}

func (s *Server) handlePromptsGet(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var getParams struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	
	if err := json.Unmarshal(params, &getParams); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	
	handler, exists := s.promptHandlers[getParams.Name]
	if !exists {
		return nil, fmt.Errorf("prompt not found: %s", getParams.Name)
	}
	
	result, err := handler(ctx, getParams.Name, getParams.Arguments)
	if err != nil {
		return nil, err
	}
	
	return map[string]interface{}{
		"description": s.prompts[getParams.Name].Description,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": map[string]interface{}{
					"type": "text",
					"text": fmt.Sprintf("%v", result),
				},
			},
		},
	}, nil
}

// Run starts the server with stdio transport
func (s *Server) Run() error {
	return s.RunWithTransport(os.Stdin, os.Stdout)
}

// RunWithTransport starts the server with custom transport
func (s *Server) RunWithTransport(reader io.Reader, writer io.Writer) error {
	s.logger.Printf("Starting MCP server: %s v%s", s.name, s.version)
	
	decoder := json.NewDecoder(reader)
	encoder := json.NewEncoder(writer)
	
	for {
		var req Request
		if err := decoder.Decode(&req); err != nil {
			if err == io.EOF {
				break
			}
			s.logger.Printf("Error decoding request: %v", err)
			continue
		}
		
		ctx := context.Background()
		response := s.HandleRequest(ctx, &req)
		
		if err := encoder.Encode(response); err != nil {
			s.logger.Printf("Error encoding response: %v", err)
			continue
		}
	}
	
	return nil
}

// SetLogger sets a custom logger
func (s *Server) SetLogger(logger *log.Logger) *Server {
	s.logger = logger
	return s
}

// GetWebSocketManager returns the WebSocket manager
func (s *Server) GetWebSocketManager() *WebSocketManager {
	return s.wsManager
}

// GetSSEManager returns the SSE manager
func (s *Server) GetSSEManager() *SSEManager {
	return s.sseManager
}