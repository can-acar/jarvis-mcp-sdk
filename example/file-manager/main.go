package main

import (
	"context" 
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	mcp "github.com/mcp-sdk/go-mcp"
)

func main() {
	server := mcp.NewServer("file-manager", "1.0.0")

	// File management tools
	server.Tool("list_files", "List files in a directory", listFilesTool).
		Tool("read_file", "Read contents of a file", readFileTool).
		Tool("write_file", "Write content to a file", writeFileTool).
		Tool("delete_file", "Delete a file", deleteFileTool)

	// File system resources
	server.Resource("file://", "filesystem", "Access to file system", "application/json", fileSystemResource)

	// File templates prompt
	server.Prompt("create_template", "Create a file template", []mcp.PromptArgument{
		{Name: "type", Description: "Template type (go, python, html, etc.)", Required: true},
		{Name: "name", Description: "Template name", Required: true},
	}, createTemplatePrompt)

	if err := server.Run(); err != nil {
		panic(err)
	}
}

func listFilesTool(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var args struct {
		Path string `json:"path"`
	}
	
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	
	if args.Path == "" {
		args.Path = "."
	}
	
	var files []string
	err := filepath.WalkDir(args.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		
		if d.IsDir() {
			files = append(files, fmt.Sprintf("[DIR] %s", path))
		} else {
			info, _ := d.Info()
			files = append(files, fmt.Sprintf("%s (%d bytes)", path, info.Size()))
		}
		return nil
	})
	
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}
	
	result := "Files in " + args.Path + ":\n"
	for _, file := range files {
		result += file + "\n"
	}
	
	return result, nil
}

func readFileTool(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var args struct {
		Path string `json:"path"`
	}
	
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	
	content, err := os.ReadFile(args.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	
	return string(content), nil
}

func writeFileTool(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	
	err := os.WriteFile(args.Path, []byte(args.Content), 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}
	
	return fmt.Sprintf("Successfully wrote %d bytes to %s", len(args.Content), args.Path), nil
}

func deleteFileTool(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var args struct {
		Path string `json:"path"`
	}
	
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	
	err := os.Remove(args.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to delete file: %w", err)
	}
	
	return fmt.Sprintf("Successfully deleted %s", args.Path), nil
}

func fileSystemResource(ctx context.Context, uri string) (interface{}, error) {
	wd, _ := os.Getwd()
	return map[string]interface{}{
		"current_directory": wd,
		"available_operations": []string{
			"list_files", "read_file", "write_file", "delete_file",
		},
	}, nil
}

func createTemplatePrompt(ctx context.Context, name string, arguments map[string]interface{}) (interface{}, error) {
	templateType, ok := arguments["type"].(string)
	if !ok {
		return nil, fmt.Errorf("template type is required")
	}
	
	templateName, ok := arguments["name"].(string)
	if !ok {
		return nil, fmt.Errorf("template name is required")
	}
	
	templates := map[string]string{
		"go": `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}`,
		"python": `#!/usr/bin/env python3

def main():
    print("Hello, World!")

if __name__ == "__main__":
    main()`,
		"html": `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Name}}</title>
</head>
<body>
    <h1>Hello, World!</h1>
</body>
</html>`,
	}
	
	template, exists := templates[templateType]
	if !exists {
		return nil, fmt.Errorf("template type '%s' not supported", templateType)
	}
	
	return fmt.Sprintf("Create a new %s file named '%s' with this template:\n\n%s", 
		templateType, templateName, template), nil
}