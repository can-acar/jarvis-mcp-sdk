package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// ValidationConfig represents validation configuration
type ValidationConfig struct {
	Enabled       bool                   `json:"enabled"`
	StrictMode    bool                   `json:"strictMode"`    // Fail on unknown fields
	ValidateJSON  bool                   `json:"validateJSON"`  // Validate JSON syntax
	MaxDepth      int                    `json:"maxDepth"`      // Maximum JSON nesting depth
	CustomRules   map[string]ValidationRule `json:"-"`         // Custom validation rules
	SkipMethods   []string               `json:"skipMethods"`   // Methods to skip validation
}

// ValidationRule represents a custom validation rule
type ValidationRule struct {
	Field     string      `json:"field"`
	Type      string      `json:"type"`      // "required", "pattern", "range", "custom"
	Value     interface{} `json:"value"`     // Rule-specific value
	Message   string      `json:"message"`   // Custom error message
	Validator func(interface{}) error `json:"-"` // Custom validator function
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Value   interface{} `json:"value,omitempty"`
}

// ValidationResult represents the result of validation
type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors"`
}

// RequestValidator validates MCP requests
type RequestValidator struct {
	config ValidationConfig
}

// NewRequestValidator creates a new request validator
func NewRequestValidator(config ValidationConfig) *RequestValidator {
	if config.MaxDepth == 0 {
		config.MaxDepth = 10
	}
	
	return &RequestValidator{
		config: config,
	}
}

// ValidateRequest validates an MCP request
func (rv *RequestValidator) ValidateRequest(req *Request) *ValidationResult {
	result := &ValidationResult{
		Valid:  true,
		Errors: []ValidationError{},
	}
	
	// Check if method should be skipped
	for _, skipMethod := range rv.config.SkipMethods {
		if req.Method == skipMethod {
			return result
		}
	}
	
	// Validate basic structure
	rv.validateBasicStructure(req, result)
	
	// Validate JSON syntax if enabled
	if rv.config.ValidateJSON && req.Params != nil {
		rv.validateJSONSyntax(req.Params, result)
	}
	
	// Validate method-specific rules
	rv.validateMethodSpecific(req, result)
	
	// Apply custom validation rules
	rv.applyCustomRules(req, result)
	
	return result
}

func (rv *RequestValidator) validateBasicStructure(req *Request, result *ValidationResult) {
	// Validate JSONRPC version
	if req.JSONRPC != "2.0" {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "jsonrpc",
			Message: "Invalid JSONRPC version, must be '2.0'",
			Value:   req.JSONRPC,
		})
	}
	
	// Validate method
	if req.Method == "" {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "method",
			Message: "Method is required",
		})
	} else if !rv.isValidMethod(req.Method) {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "method",
			Message: "Invalid method name",
			Value:   req.Method,
		})
	}
	
	// Validate ID (should be present for requests)
	if req.ID == nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "id",
			Message: "Request ID is required",
		})
	}
}

func (rv *RequestValidator) validateJSONSyntax(params json.RawMessage, result *ValidationResult) {
	var obj interface{}
	if err := json.Unmarshal(params, &obj); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "params",
			Message: fmt.Sprintf("Invalid JSON syntax: %v", err),
		})
		return
	}
	
	// Check nesting depth
	if depth := rv.getJSONDepth(obj); depth > rv.config.MaxDepth {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "params",
			Message: fmt.Sprintf("JSON nesting too deep: %d (max: %d)", depth, rv.config.MaxDepth),
		})
	}
}

func (rv *RequestValidator) validateMethodSpecific(req *Request, result *ValidationResult) {
	switch req.Method {
	case "tools/call":
		rv.validateToolsCall(req, result)
	case "resources/read":
		rv.validateResourcesRead(req, result)
	case "prompts/get":
		rv.validatePromptsGet(req, result)
	}
}

func (rv *RequestValidator) validateToolsCall(req *Request, result *ValidationResult) {
	if req.Params == nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "params",
			Message: "Parameters required for tools/call",
		})
		return
	}
	
	var params map[string]interface{}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "params",
			Message: "Invalid parameters format",
		})
		return
	}
	
	// Validate tool name
	name, exists := params["name"]
	if !exists {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "params.name",
			Message: "Tool name is required",
		})
	} else if nameStr, ok := name.(string); !ok || nameStr == "" {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "params.name",
			Message: "Tool name must be a non-empty string",
			Value:   name,
		})
	}
	
	// Validate arguments (optional, but if present should be an object)
	if args, exists := params["arguments"]; exists {
		if reflect.TypeOf(args).Kind() != reflect.Map {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Field:   "params.arguments",
				Message: "Arguments must be an object",
				Value:   args,
			})
		}
	}
}

func (rv *RequestValidator) validateResourcesRead(req *Request, result *ValidationResult) {
	if req.Params == nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "params",
			Message: "Parameters required for resources/read",
		})
		return
	}
	
	var params map[string]interface{}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "params",
			Message: "Invalid parameters format",
		})
		return
	}
	
	// Validate URI
	uri, exists := params["uri"]
	if !exists {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "params.uri",
			Message: "Resource URI is required",
		})
	} else if uriStr, ok := uri.(string); !ok || uriStr == "" {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "params.uri",
			Message: "Resource URI must be a non-empty string",
			Value:   uri,
		})
	} else if !rv.isValidURI(uriStr) {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "params.uri",
			Message: "Invalid URI format",
			Value:   uri,
		})
	}
}

func (rv *RequestValidator) validatePromptsGet(req *Request, result *ValidationResult) {
	if req.Params == nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "params",
			Message: "Parameters required for prompts/get",
		})
		return
	}
	
	var params map[string]interface{}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "params",
			Message: "Invalid parameters format",
		})
		return
	}
	
	// Validate prompt name
	name, exists := params["name"]
	if !exists {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "params.name",
			Message: "Prompt name is required",
		})
	} else if nameStr, ok := name.(string); !ok || nameStr == "" {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "params.name",
			Message: "Prompt name must be a non-empty string",
			Value:   name,
		})
	}
}

func (rv *RequestValidator) applyCustomRules(req *Request, result *ValidationResult) {
	if req.Params == nil {
		return
	}
	
	var params map[string]interface{}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return // Already handled in JSON validation
	}
	
	for _, rule := range rv.config.CustomRules {
		rv.applyRule(rule, params, result)
	}
}

func (rv *RequestValidator) applyRule(rule ValidationRule, params map[string]interface{}, result *ValidationResult) {
	value, exists := rv.getNestedValue(params, rule.Field)
	
	switch rule.Type {
	case "required":
		if !exists {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Field:   rule.Field,
				Message: rv.getErrorMessage(rule, "Field is required"),
			})
		}
	case "pattern":
		if exists && value != nil {
			if strValue, ok := value.(string); ok {
				if pattern, ok := rule.Value.(string); ok {
					if matched, _ := regexp.MatchString(pattern, strValue); !matched {
						result.Valid = false
						result.Errors = append(result.Errors, ValidationError{
							Field:   rule.Field,
							Message: rv.getErrorMessage(rule, "Value does not match required pattern"),
							Value:   value,
						})
					}
				}
			}
		}
	case "range":
		if exists && value != nil {
			rv.validateRange(rule, value, result)
		}
	case "custom":
		if rule.Validator != nil && exists {
			if err := rule.Validator(value); err != nil {
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					Field:   rule.Field,
					Message: rv.getErrorMessage(rule, err.Error()),
					Value:   value,
				})
			}
		}
	}
}

func (rv *RequestValidator) validateRange(rule ValidationRule, value interface{}, result *ValidationResult) {
	rangeMap, ok := rule.Value.(map[string]interface{})
	if !ok {
		return
	}
	
	var numValue float64
	var valid bool
	
	switch v := value.(type) {
	case int:
		numValue = float64(v)
		valid = true
	case int64:
		numValue = float64(v)
		valid = true
	case float64:
		numValue = v
		valid = true
	case string:
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			numValue = parsed
			valid = true
		}
	}
	
	if !valid {
		return
	}
	
	if min, exists := rangeMap["min"]; exists {
		if minFloat, ok := min.(float64); ok && numValue < minFloat {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Field:   rule.Field,
				Message: rv.getErrorMessage(rule, fmt.Sprintf("Value must be >= %v", min)),
				Value:   value,
			})
		}
	}
	
	if max, exists := rangeMap["max"]; exists {
		if maxFloat, ok := max.(float64); ok && numValue > maxFloat {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Field:   rule.Field,
				Message: rv.getErrorMessage(rule, fmt.Sprintf("Value must be <= %v", max)),
				Value:   value,
			})
		}
	}
}

func (rv *RequestValidator) getNestedValue(params map[string]interface{}, field string) (interface{}, bool) {
	keys := strings.Split(field, ".")
	current := params
	
	for i, key := range keys {
		if i == len(keys)-1 {
			// Last key
			value, exists := current[key]
			return value, exists
		}
		
		// Intermediate key
		if next, exists := current[key]; exists {
			if nextMap, ok := next.(map[string]interface{}); ok {
				current = nextMap
			} else {
				return nil, false
			}
		} else {
			return nil, false
		}
	}
	
	return nil, false
}

func (rv *RequestValidator) getErrorMessage(rule ValidationRule, defaultMessage string) string {
	if rule.Message != "" {
		return rule.Message
	}
	return defaultMessage
}

func (rv *RequestValidator) isValidMethod(method string) bool {
	// Basic method name validation
	validMethods := []string{
		"initialize",
		"tools/list",
		"tools/call",
		"resources/list",
		"resources/read",
		"prompts/list",
		"prompts/get",
		"stream/poll",
		"stream/cancel",
	}
	
	for _, validMethod := range validMethods {
		if method == validMethod {
			return true
		}
	}
	
	// Allow custom methods that follow naming convention
	return regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*(/[a-zA-Z][a-zA-Z0-9_]*)*$`).MatchString(method)
}

func (rv *RequestValidator) isValidURI(uri string) bool {
	// Basic URI validation - should contain scheme
	return strings.Contains(uri, "://") || strings.HasPrefix(uri, "/")
}

func (rv *RequestValidator) getJSONDepth(obj interface{}) int {
	switch v := obj.(type) {
	case map[string]interface{}:
		maxDepth := 0
		for _, value := range v {
			depth := rv.getJSONDepth(value)
			if depth > maxDepth {
				maxDepth = depth
			}
		}
		return maxDepth + 1
	case []interface{}:
		maxDepth := 0
		for _, value := range v {
			depth := rv.getJSONDepth(value)
			if depth > maxDepth {
				maxDepth = depth
			}
		}
		return maxDepth + 1
	default:
		return 1
	}
}

// ValidationMiddleware creates request validation middleware
func ValidationMiddleware(config ValidationConfig) MiddlewareFunc {
	validator := NewRequestValidator(config)
	
	return func(next RequestHandler) RequestHandler {
		return func(ctx context.Context, req *Request) *Response {
			if !config.Enabled {
				return next(ctx, req)
			}
			
			// Validate request
			result := validator.ValidateRequest(req)
			
			if !result.Valid {
				// Add validation errors to middleware context
				mctx := GetMiddlewareContext(ctx)
				mctx.Set("validation_errors", result.Errors)
				
				return &Response{
					JSONRPC: "2.0",
					ID:      req.ID,
					Error: &Error{
						Code:    400,
						Message: "Request validation failed",
						Data: map[string]interface{}{
							"errors": result.Errors,
						},
					},
				}
			}
			
			// Add validation success to middleware context
			mctx := GetMiddlewareContext(ctx)
			mctx.Set("validation_passed", true)
			
			return next(ctx, req)
		}
	}
}

// SchemaValidationMiddleware validates tool parameters against JSON schema
func SchemaValidationMiddleware(toolSchemas map[string]JSONSchema) MiddlewareFunc {
	return func(next RequestHandler) RequestHandler {
		return func(ctx context.Context, req *Request) *Response {
			// Only validate tool calls
			if req.Method != "tools/call" || req.Params == nil {
				return next(ctx, req)
			}
			
			var params map[string]interface{}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return next(ctx, req) // Let other validation handle this
			}
			
			toolName, exists := params["name"]
			if !exists {
				return next(ctx, req) // Let other validation handle this
			}
			
			toolNameStr, ok := toolName.(string)
			if !ok {
				return next(ctx, req) // Let other validation handle this
			}
			
			// Check if we have a schema for this tool
			schema, exists := toolSchemas[toolNameStr]
			if !exists {
				return next(ctx, req) // No schema, skip validation
			}
			
			// Validate arguments against schema
			if args, exists := params["arguments"]; exists {
				if err := validateAgainstSchema(args, schema); err != nil {
					return &Response{
						JSONRPC: "2.0",
						ID:      req.ID,
						Error: &Error{
							Code:    400,
							Message: fmt.Sprintf("Tool arguments validation failed: %v", err),
							Data: map[string]interface{}{
								"tool":   toolNameStr,
								"schema": schema,
							},
						},
					}
				}
			}
			
			return next(ctx, req)
		}
	}
}

// validateAgainstSchema validates data against a JSON schema (simplified implementation)
func validateAgainstSchema(data interface{}, schema JSONSchema) error {
	// This is a simplified schema validation
	// In a production system, you might want to use a full JSON Schema validator
	
	if schema.Type != "" {
		if !validateType(data, schema.Type) {
			return fmt.Errorf("expected type %s", schema.Type)
		}
	}
	
	if schema.Type == "object" && schema.Properties != nil {
		dataMap, ok := data.(map[string]interface{})
		if !ok {
			return fmt.Errorf("expected object")
		}
		
		// Check required fields
		for _, required := range schema.Required {
			if _, exists := dataMap[required]; !exists {
				return fmt.Errorf("required field '%s' is missing", required)
			}
		}
		
		// Validate each property
		for propName, propSchema := range schema.Properties {
			if value, exists := dataMap[propName]; exists {
				if err := validateAgainstSchema(value, propSchema); err != nil {
					return fmt.Errorf("property '%s': %v", propName, err)
				}
			}
		}
	}
	
	return nil
}

func validateType(data interface{}, expectedType string) bool {
	switch expectedType {
	case "string":
		_, ok := data.(string)
		return ok
	case "number":
		switch data.(type) {
		case int, int64, float64:
			return true
		default:
			return false
		}
	case "integer":
		switch data.(type) {
		case int, int64:
			return true
		default:
			return false
		}
	case "boolean":
		_, ok := data.(bool)
		return ok
	case "array":
		_, ok := data.([]interface{})
		return ok
	case "object":
		_, ok := data.(map[string]interface{})
		return ok
	default:
		return true // Unknown type, assume valid
	}
}