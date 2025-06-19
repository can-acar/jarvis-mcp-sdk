package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// SchemaGenerator generates JSON schemas from Go types using reflection
type SchemaGenerator struct {
	visited map[reflect.Type]bool
}

// NewSchemaGenerator creates a new schema generator
func NewSchemaGenerator() *SchemaGenerator {
	return &SchemaGenerator{
		visited: make(map[reflect.Type]bool),
	}
}

// GenerateFromType generates a JSON schema from a Go type
func (sg *SchemaGenerator) GenerateFromType(t reflect.Type) JSONSchema {
	sg.visited = make(map[reflect.Type]bool) // Reset for new generation
	return sg.generateSchema(t)
}

// GenerateFromHandler generates a JSON schema from a typed handler function
func (sg *SchemaGenerator) GenerateFromHandler(handlerType reflect.Type) JSONSchema {
	if handlerType.Kind() != reflect.Func {
		return JSONSchema{Type: "object"}
	}
	
	if handlerType.NumIn() < 2 {
		return JSONSchema{Type: "object"}
	}
	
	// Second parameter should be the input type (first is context.Context)
	paramType := handlerType.In(1)
	
	// If it's json.RawMessage, we can't generate a schema
	if paramType.String() == "json.RawMessage" {
		return JSONSchema{Type: "object"}
	}
	
	return sg.GenerateFromType(paramType)
}

func (sg *SchemaGenerator) generateSchema(t reflect.Type) JSONSchema {
	// Handle pointers
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	
	// Avoid infinite recursion
	if sg.visited[t] {
		return JSONSchema{Type: "object"}
	}
	sg.visited[t] = true
	defer func() { sg.visited[t] = false }()
	
	switch t.Kind() {
	case reflect.String:
		return sg.generateStringSchema(t)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		 reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return sg.generateIntegerSchema(t)
	case reflect.Float32, reflect.Float64:
		return sg.generateNumberSchema(t)
	case reflect.Bool:
		return JSONSchema{Type: "boolean"}
	case reflect.Slice, reflect.Array:
		return sg.generateArraySchema(t)
	case reflect.Map:
		return sg.generateMapSchema(t)
	case reflect.Struct:
		return sg.generateStructSchema(t)
	case reflect.Interface:
		return JSONSchema{} // Any type
	default:
		return JSONSchema{Type: "object"}
	}
}

func (sg *SchemaGenerator) generateStringSchema(t reflect.Type) JSONSchema {
	schema := JSONSchema{Type: "string"}
	
	// Check if it's an enum type (could be extended with custom tags)
	if t.Name() != "" {
		// Custom enum support could be added here
	}
	
	return schema
}

func (sg *SchemaGenerator) generateIntegerSchema(t reflect.Type) JSONSchema {
	schema := JSONSchema{Type: "integer"}
	
	// Add format based on type
	switch t.Kind() {
	case reflect.Int64, reflect.Uint64:
		// Could add format: "int64" if needed
	case reflect.Int32, reflect.Uint32:
		// Could add format: "int32" if needed
	}
	
	return schema
}

func (sg *SchemaGenerator) generateNumberSchema(t reflect.Type) JSONSchema {
	schema := JSONSchema{Type: "number"}
	
	// Add format based on type
	switch t.Kind() {
	case reflect.Float32:
		// Could add format: "float" if needed
	case reflect.Float64:
		// Could add format: "double" if needed
	}
	
	return schema
}

func (sg *SchemaGenerator) generateArraySchema(t reflect.Type) JSONSchema {
	itemType := t.Elem()
	itemSchema := sg.generateSchema(itemType)
	
	return JSONSchema{
		Type:  "array",
		Items: &itemSchema,
	}
}

func (sg *SchemaGenerator) generateMapSchema(t reflect.Type) JSONSchema {
	// JSON Schema doesn't have a direct map type, so we use object with additionalProperties
	schema := JSONSchema{Type: "object"}
	// Note: additionalProperties would need to be added to JSONSchema type
	
	return schema
}

func (sg *SchemaGenerator) generateStructSchema(t reflect.Type) JSONSchema {
	schema := JSONSchema{
		Type:       "object",
		Properties: make(map[string]JSONSchema),
		Required:   []string{},
	}
	
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		
		// Skip unexported fields
		if !field.IsExported() {
			continue
		}
		
		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}
		
		fieldName := sg.getFieldName(field, jsonTag)
		if fieldName == "" {
			continue
		}
		
		fieldSchema := sg.generateSchema(field.Type)
		
		// Add description from tag
		if desc := field.Tag.Get("description"); desc != "" {
			fieldSchema.Description = desc
		}
		
		// Add validation constraints
		sg.addValidationConstraints(&fieldSchema, field)
		
		schema.Properties[fieldName] = fieldSchema
		
		// Check if field is required
		if sg.isRequired(field, jsonTag) {
			schema.Required = append(schema.Required, fieldName)
		}
	}
	
	return schema
}

func (sg *SchemaGenerator) getFieldName(field reflect.StructField, jsonTag string) string {
	if jsonTag == "" {
		return strings.ToLower(field.Name)
	}
	
	parts := strings.Split(jsonTag, ",")
	if parts[0] == "" {
		return strings.ToLower(field.Name)
	}
	
	return parts[0]
}

func (sg *SchemaGenerator) isRequired(field reflect.StructField, jsonTag string) bool {
	// Check for required tag
	if required := field.Tag.Get("required"); required == "true" {
		return true
	}
	
	// Check if field is optional (pointer or has omitempty)
	if field.Type.Kind() == reflect.Ptr {
		return false
	}
	
	if strings.Contains(jsonTag, "omitempty") {
		return false
	}
	
	// Default to required for non-pointer types
	return true
}

func (sg *SchemaGenerator) addValidationConstraints(schema *JSONSchema, field reflect.StructField) {
	// Add minimum/maximum constraints
	if min := field.Tag.Get("min"); min != "" {
		if val, err := strconv.ParseFloat(min, 64); err == nil {
			// Note: These would need to be added to JSONSchema type
			_ = val // minimum := val
		}
	}
	
	if max := field.Tag.Get("max"); max != "" {
		if val, err := strconv.ParseFloat(max, 64); err == nil {
			// Note: These would need to be added to JSONSchema type
			_ = val // maximum := val
		}
	}
	
	// Add enum constraints
	if enum := field.Tag.Get("enum"); enum != "" {
		values := strings.Split(enum, ",")
		schema.Enum = make([]interface{}, len(values))
		for i, v := range values {
			schema.Enum[i] = strings.TrimSpace(v)
		}
	}
	
	// Add pattern for strings
	if pattern := field.Tag.Get("pattern"); pattern != "" && schema.Type == "string" {
		// Note: Pattern would need to be added to JSONSchema type
		_ = pattern
	}
}

// TypedToolHandler represents a strongly-typed tool handler
type TypedToolHandler[T any, R any] func(ctx interface{}, params T) (R, error)

// RegisterTypedTool registers a tool with automatic schema generation from types
func (s *Server) RegisterTypedTool(name, description string, handler interface{}) *Server {
	handlerValue := reflect.ValueOf(handler)
	handlerType := handlerValue.Type()
	
	if handlerType.Kind() != reflect.Func {
		s.logger.Printf("Warning: handler for tool %s is not a function", name)
		return s
	}
	
	// Generate schema from handler
	generator := NewSchemaGenerator()
	schema := generator.GenerateFromHandler(handlerType)
	
	// Create wrapper that handles type conversion
	wrapper := func(ctx context.Context, params json.RawMessage) (interface{}, error) {
		// Get parameter type from handler
		if handlerType.NumIn() < 2 {
			return nil, fmt.Errorf("handler must have at least 2 parameters (context, input)")
		}
		
		paramType := handlerType.In(1)
		
		// Create new instance of parameter type
		paramValue := reflect.New(paramType)
		
		// Unmarshal JSON into parameter
		if err := json.Unmarshal(params, paramValue.Interface()); err != nil {
			return nil, fmt.Errorf("failed to unmarshal parameters: %w", err)
		}
		
		// Call the handler
		args := []reflect.Value{
			reflect.ValueOf(ctx),
			paramValue.Elem(),
		}
		
		results := handlerValue.Call(args)
		
		// Handle return values
		if len(results) != 2 {
			return nil, fmt.Errorf("handler must return exactly 2 values (result, error)")
		}
		
		// Check for error
		if !results[1].IsNil() {
			return nil, results[1].Interface().(error)
		}
		
		return results[0].Interface(), nil
	}
	
	// Register the tool
	tool := &Tool{
		Name:        name,
		Description: description,
		InputSchema: schema,
	}
	
	s.tools[name] = tool
	s.toolHandlers[name] = wrapper
	
	return s
}