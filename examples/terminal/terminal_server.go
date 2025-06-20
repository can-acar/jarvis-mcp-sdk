package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	mcp "github.com/can-acar/jarvis-mcp-sdk"
)

// TerminalSession represents an active terminal session
type TerminalSession struct {
	ID          string    `json:"id"`
	WorkingDir  string    `json:"workingDir"`
	Environment []string  `json:"environment"`
	CreatedAt   time.Time `json:"createdAt"`
	LastUsed    time.Time `json:"lastUsed"`
	Active      bool      `json:"active"`
}

// ProcessInfo represents information about a running process
type ProcessInfo struct {
	PID       int       `json:"pid"`
	Command   string    `json:"command"`
	StartTime time.Time `json:"startTime"`
	Status    string    `json:"status"`
	SessionID string    `json:"sessionId"`
}

// TerminalManager manages terminal sessions and processes
type TerminalManager struct {
	sessions   map[string]*TerminalSession
	processes  map[int]*ProcessInfo
	mu         sync.RWMutex
	server     *mcp.Server
	
	// Security settings
	allowedCommands []string
	blockedCommands []string
	safeMode        bool
	allowedDirs     []string
}

var terminalManager *TerminalManager

func main() {
	// Create MCP server for terminal control
	server := mcp.NewServer("terminal-control-server", "1.0.0")

	// Initialize terminal manager
	terminalManager = &TerminalManager{
		sessions:  make(map[string]*TerminalSession),
		processes: make(map[int]*ProcessInfo),
		server:    server,
		safeMode:  true, // Enable safe mode by default
		allowedCommands: []string{
			"ls", "dir", "pwd", "cd", "echo", "cat", "grep", "find", "ps", "top", "df", "du",
			"whoami", "date", "uptime", "uname", "which", "whereis", "history",
			"git", "npm", "yarn", "go", "python", "node", "java", "javac",
			"mkdir", "rmdir", "touch", "cp", "mv", "head", "tail", "wc", "sort", "uniq",
		},
		blockedCommands: []string{
			"rm", "del", "format", "fdisk", "mkfs", "dd", "shutdown", "reboot", "halt",
			"su", "sudo", "passwd", "useradd", "userdel", "chmod", "chown",
			"iptables", "ufw", "firewall-cmd", "systemctl", "service",
		},
		allowedDirs: []string{
			os.Getenv("HOME"),
			"/tmp",
			"/var/tmp",
			getCurrentDir(),
		},
	}

	// Register terminal control tools
	registerTerminalTools(server)
	registerProcessTools(server)
	registerFileSystemTools(server)
	registerSessionTools(server)

	// Register resources
	registerTerminalResources(server)

	// Setup web transport with SSE for real-time updates
	webConfig := mcp.WebConfig{
		Port:            8080,
		Host:            "localhost",
		AuthToken:       "terminal-server-2024",
		EnableCORS:      true,
		EnableDashboard: true,
	}
	server.EnableWebTransport(webConfig)

	// Enable SSE for real-time terminal output
	sseConfig := mcp.DefaultSSEConfig()
	sseConfig.HeartbeatInterval = 30 * time.Second
	server.EnableSSE(sseConfig)

	// Enable advanced streaming for terminal operations
	streamingConfig := mcp.DefaultStreamingConfig()
	streamingConfig.MaxSessions = 20
	streamingConfig.HeartbeatInterval = 15 * time.Second
	server.EnableAdvancedStreaming(streamingConfig)

	// Setup graceful shutdown
	setupGracefulShutdown(server)

	// Display startup information
	printStartupInfo(webConfig)

	// Start web transport
	if err := server.StartWebTransport(); err != nil {
		log.Fatalf("Failed to start web transport: %v", err)
	}

	// Keep the main goroutine alive
	select {}
}

func registerTerminalTools(server *mcp.Server) {
	// Tool: Execute terminal command
	server.Tool("execute_command", "Execute a terminal command in a session", func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var params struct {
			Command   string `json:"command"`
			SessionID string `json:"sessionId,omitempty"`
			Directory string `json:"directory,omitempty"`
			Timeout   int    `json:"timeout,omitempty"` // seconds
			Async     bool   `json:"async,omitempty"`
		}

		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		if params.Timeout == 0 {
			params.Timeout = 30 // default 30 seconds
		}

		// Security check
		if !isCommandAllowed(params.Command) {
			return nil, fmt.Errorf("command not allowed: %s", params.Command)
		}

		// Get or create session
		session := getOrCreateSession(params.SessionID)
		if params.Directory != "" && isDirectoryAllowed(params.Directory) {
			session.WorkingDir = params.Directory
		}

		// Execute command
		if params.Async {
			return executeCommandAsync(session, params.Command, params.Timeout)
		} else {
			return executeCommandSync(session, params.Command, params.Timeout)
		}
	})

	// Tool: Interactive command execution
	server.Tool("execute_interactive", "Execute command with real-time output via SSE", func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var params struct {
			Command   string `json:"command"`
			SessionID string `json:"sessionId,omitempty"`
		}

		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		// Security check
		if !isCommandAllowed(params.Command) {
			return nil, fmt.Errorf("command not allowed: %s", params.Command)
		}

		session := getOrCreateSession(params.SessionID)
		executionID := fmt.Sprintf("exec_%d", time.Now().UnixNano())

		// Start interactive execution
		go executeInteractiveCommand(session, params.Command, executionID)

		return map[string]interface{}{
			"executionId": executionID,
			"sessionId":   session.ID,
			"status":      "started",
			"message":     "Command execution started. Watch SSE events for real-time output.",
		}, nil
	})

	// Tool: Run shell script
	server.Tool("run_script", "Execute a shell script", func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var params struct {
			Script     string `json:"script"`
			SessionID  string `json:"sessionId,omitempty"`
			ScriptType string `json:"scriptType,omitempty"` // bash, sh, powershell, cmd
		}

		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		if params.ScriptType == "" {
			if runtime.GOOS == "windows" {
				params.ScriptType = "cmd"
			} else {
				params.ScriptType = "bash"
			}
		}

		session := getOrCreateSession(params.SessionID)
		return executeScript(session, params.Script, params.ScriptType)
	})
}

func registerProcessTools(server *mcp.Server) {
	// Tool: List running processes
	server.Tool("list_processes", "List running processes", func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var params struct {
			Filter string `json:"filter,omitempty"`
		}

		json.Unmarshal(args, &params)
		return listProcesses(params.Filter)
	})

	// Tool: Kill process
	server.Tool("kill_process", "Kill a process by PID", func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var params struct {
			PID    int  `json:"pid"`
			Force  bool `json:"force,omitempty"`
		}

		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		return killProcess(params.PID, params.Force)
	})

	// Tool: Get process info
	server.Tool("process_info", "Get detailed information about a process", func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var params struct {
			PID int `json:"pid"`
		}

		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		return getProcessInfo(params.PID)
	})
}

func registerFileSystemTools(server *mcp.Server) {
	// Tool: List directory contents
	server.Tool("list_directory", "List contents of a directory", func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var params struct {
			Path      string `json:"path,omitempty"`
			Hidden    bool   `json:"hidden,omitempty"`
			Recursive bool   `json:"recursive,omitempty"`
		}

		json.Unmarshal(args, &params)

		if params.Path == "" {
			params.Path = "."
		}

		if !isDirectoryAllowed(params.Path) {
			return nil, fmt.Errorf("directory access not allowed: %s", params.Path)
		}

		return listDirectory(params.Path, params.Hidden, params.Recursive)
	})

	// Tool: Read file content
	server.Tool("read_file", "Read content of a file", func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var params struct {
			Path   string `json:"path"`
			Lines  int    `json:"lines,omitempty"`
			Offset int    `json:"offset,omitempty"`
		}

		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		if !isDirectoryAllowed(filepath.Dir(params.Path)) {
			return nil, fmt.Errorf("file access not allowed: %s", params.Path)
		}

		return readFile(params.Path, params.Lines, params.Offset)
	})

	// Tool: Get file/directory info
	server.Tool("file_info", "Get information about a file or directory", func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var params struct {
			Path string `json:"path"`
		}

		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		if !isDirectoryAllowed(filepath.Dir(params.Path)) {
			return nil, fmt.Errorf("file access not allowed: %s", params.Path)
		}

		return getFileInfo(params.Path)
	})
}

func registerSessionTools(server *mcp.Server) {
	// Tool: Create terminal session
	server.Tool("create_session", "Create a new terminal session", func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var params struct {
			WorkingDir  string            `json:"workingDir,omitempty"`
			Environment map[string]string `json:"environment,omitempty"`
		}

		json.Unmarshal(args, &params)

		session := createSession(params.WorkingDir, params.Environment)
		return map[string]interface{}{
			"sessionId":   session.ID,
			"workingDir":  session.WorkingDir,
			"environment": session.Environment,
			"createdAt":   session.CreatedAt,
		}, nil
	})

	// Tool: List terminal sessions
	server.Tool("list_sessions", "List all terminal sessions", func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		terminalManager.mu.RLock()
		defer terminalManager.mu.RUnlock()

		sessions := make([]TerminalSession, 0, len(terminalManager.sessions))
		for _, session := range terminalManager.sessions {
			sessions = append(sessions, *session)
		}

		return map[string]interface{}{
			"sessions": sessions,
			"count":    len(sessions),
		}, nil
	})

	// Tool: Delete terminal session
	server.Tool("delete_session", "Delete a terminal session", func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var params struct {
			SessionID string `json:"sessionId"`
		}

		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		return deleteSession(params.SessionID)
	})

	// Tool: Change session directory
	server.Tool("change_directory", "Change working directory of a session", func(ctx context.Context, args json.RawMessage) (interface{}, error) {
		var params struct {
			SessionID string `json:"sessionId,omitempty"`
			Directory string `json:"directory"`
		}

		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}

		if !isDirectoryAllowed(params.Directory) {
			return nil, fmt.Errorf("directory access not allowed: %s", params.Directory)
		}

		session := getOrCreateSession(params.SessionID)
		session.WorkingDir = params.Directory
		session.LastUsed = time.Now()

		return map[string]interface{}{
			"sessionId":    session.ID,
			"workingDir":   session.WorkingDir,
			"changedAt":    time.Now(),
		}, nil
	})
}

func registerTerminalResources(server *mcp.Server) {
	// Resource: System information
	server.Resource("terminal://system", "system_info", "System information", "application/json",
		func(ctx context.Context, uri string) (interface{}, error) {
			return getSystemInfo()
		})

	// Resource: Terminal sessions
	server.Resource("terminal://sessions", "sessions", "Active terminal sessions", "application/json",
		func(ctx context.Context, uri string) (interface{}, error) {
			terminalManager.mu.RLock()
			defer terminalManager.mu.RUnlock()

			return map[string]interface{}{
				"sessions":    terminalManager.sessions,
				"totalCount":  len(terminalManager.sessions),
				"activeCount": getActiveSessionCount(),
			}, nil
		})

	// Resource: Running processes
	server.Resource("terminal://processes", "processes", "Running processes", "application/json",
		func(ctx context.Context, uri string) (interface{}, error) {
			terminalManager.mu.RLock()
			defer terminalManager.mu.RUnlock()

			return map[string]interface{}{
				"processes": terminalManager.processes,
				"count":     len(terminalManager.processes),
			}, nil
		})

	// Resource: Security configuration
	server.Resource("terminal://security", "security", "Security configuration", "application/json",
		func(ctx context.Context, uri string) (interface{}, error) {
			return map[string]interface{}{
				"safeMode":        terminalManager.safeMode,
				"allowedCommands": terminalManager.allowedCommands,
				"blockedCommands": terminalManager.blockedCommands,
				"allowedDirs":     terminalManager.allowedDirs,
			}, nil
		})
}

// Session management functions
func createSession(workingDir string, environment map[string]string) *TerminalSession {
	terminalManager.mu.Lock()
	defer terminalManager.mu.Unlock()

	sessionID := fmt.Sprintf("session_%d", time.Now().UnixNano())

	if workingDir == "" {
		workingDir = getCurrentDir()
	}

	env := os.Environ()
	if environment != nil {
		for key, value := range environment {
			env = append(env, fmt.Sprintf("%s=%s", key, value))
		}
	}

	session := &TerminalSession{
		ID:          sessionID,
		WorkingDir:  workingDir,
		Environment: env,
		CreatedAt:   time.Now(),
		LastUsed:    time.Now(),
		Active:      true,
	}

	terminalManager.sessions[sessionID] = session
	return session
}

func getOrCreateSession(sessionID string) *TerminalSession {
	terminalManager.mu.Lock()
	defer terminalManager.mu.Unlock()

	if sessionID == "" || terminalManager.sessions[sessionID] == nil {
		// Create new session
		sessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
		session := &TerminalSession{
			ID:          sessionID,
			WorkingDir:  getCurrentDir(),
			Environment: os.Environ(),
			CreatedAt:   time.Now(),
			LastUsed:    time.Now(),
			Active:      true,
		}
		terminalManager.sessions[sessionID] = session
		return session
	}

	session := terminalManager.sessions[sessionID]
	session.LastUsed = time.Now()
	return session
}

func deleteSession(sessionID string) (interface{}, error) {
	terminalManager.mu.Lock()
	defer terminalManager.mu.Unlock()

	if terminalManager.sessions[sessionID] == nil {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	delete(terminalManager.sessions, sessionID)

	return map[string]interface{}{
		"sessionId": sessionID,
		"deleted":   true,
		"deletedAt": time.Now(),
	}, nil
}

// Command execution functions
func executeCommandSync(session *TerminalSession, command string, timeoutSec int) (interface{}, error) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = session.WorkingDir
	cmd.Env = session.Environment

	startTime := time.Now()
	output, err := cmd.CombinedOutput()
	duration := time.Since(startTime)

	result := map[string]interface{}{
		"command":     command,
		"sessionId":   session.ID,
		"workingDir":  session.WorkingDir,
		"output":      string(output),
		"duration":    duration.Milliseconds(),
		"startTime":   startTime,
		"success":     err == nil,
	}

	if err != nil {
		result["error"] = err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok {
			result["exitCode"] = exitErr.ExitCode()
		}
	} else {
		result["exitCode"] = 0
	}

	// Send SSE event
	sendSSEEvent("command_completed", result)

	return result, nil
}

func executeCommandAsync(session *TerminalSession, command string, timeoutSec int) (interface{}, error) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	executionID := fmt.Sprintf("async_%d", time.Now().UnixNano())

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
		cmd.Dir = session.WorkingDir
		cmd.Env = session.Environment

		// Store process info
		if cmd.Process != nil {
			terminalManager.mu.Lock()
			terminalManager.processes[cmd.Process.Pid] = &ProcessInfo{
				PID:       cmd.Process.Pid,
				Command:   command,
				StartTime: time.Now(),
				Status:    "running",
				SessionID: session.ID,
			}
			terminalManager.mu.Unlock()
		}

		startTime := time.Now()
		output, err := cmd.CombinedOutput()
		duration := time.Since(startTime)

		result := map[string]interface{}{
			"executionId": executionID,
			"command":     command,
			"sessionId":   session.ID,
			"output":      string(output),
			"duration":    duration.Milliseconds(),
			"success":     err == nil,
		}

		if err != nil {
			result["error"] = err.Error()
		}

		// Clean up process info
		if cmd.Process != nil {
			terminalManager.mu.Lock()
			delete(terminalManager.processes, cmd.Process.Pid)
			terminalManager.mu.Unlock()
		}

		// Send completion event
		sendSSEEvent("async_command_completed", result)
	}()

	return map[string]interface{}{
		"executionId": executionID,
		"command":     command,
		"sessionId":   session.ID,
		"status":      "started",
		"async":       true,
	}, nil
}

func executeInteractiveCommand(session *TerminalSession, command string, executionID string) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = session.WorkingDir
	cmd.Env = session.Environment

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		sendSSEEvent("command_error", map[string]interface{}{
			"executionId": executionID,
			"error":       err.Error(),
		})
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		sendSSEEvent("command_error", map[string]interface{}{
			"executionId": executionID,
			"error":       err.Error(),
		})
		return
	}

	// Start command
	if err := cmd.Start(); err != nil {
		sendSSEEvent("command_error", map[string]interface{}{
			"executionId": executionID,
			"error":       err.Error(),
		})
		return
	}

	// Send start event
	sendSSEEvent("command_started", map[string]interface{}{
		"executionId": executionID,
		"command":     command,
		"sessionId":   session.ID,
		"pid":         cmd.Process.Pid,
	})

	// Read stdout
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			sendSSEEvent("command_output", map[string]interface{}{
				"executionId": executionID,
				"stream":      "stdout",
				"data":        scanner.Text(),
				"timestamp":   time.Now(),
			})
		}
	}()

	// Read stderr
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			sendSSEEvent("command_output", map[string]interface{}{
				"executionId": executionID,
				"stream":      "stderr",
				"data":        scanner.Text(),
				"timestamp":   time.Now(),
			})
		}
	}()

	// Wait for completion
	err = cmd.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	// Send completion event
	sendSSEEvent("command_finished", map[string]interface{}{
		"executionId": executionID,
		"exitCode":    exitCode,
		"success":     err == nil,
		"timestamp":   time.Now(),
	})
}

func executeScript(session *TerminalSession, script, scriptType string) (interface{}, error) {
	// Create temporary script file
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("script_*.%s", getScriptExtension(scriptType)))
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write script content
	if _, err := tmpFile.WriteString(script); err != nil {
		return nil, fmt.Errorf("failed to write script: %w", err)
	}
	tmpFile.Close()

	// Execute script
	var cmd *exec.Cmd
	switch scriptType {
	case "bash":
		cmd = exec.Command("bash", tmpFile.Name())
	case "sh":
		cmd = exec.Command("sh", tmpFile.Name())
	case "powershell":
		cmd = exec.Command("powershell", "-File", tmpFile.Name())
	case "cmd":
		cmd = exec.Command("cmd", "/C", tmpFile.Name())
	default:
		return nil, fmt.Errorf("unsupported script type: %s", scriptType)
	}

	cmd.Dir = session.WorkingDir
	cmd.Env = session.Environment

	startTime := time.Now()
	output, err := cmd.CombinedOutput()
	duration := time.Since(startTime)

	result := map[string]interface{}{
		"script":      script,
		"scriptType":  scriptType,
		"sessionId":   session.ID,
		"output":      string(output),
		"duration":    duration.Milliseconds(),
		"success":     err == nil,
	}

	if err != nil {
		result["error"] = err.Error()
	}

	return result, nil
}

// Security functions
func isCommandAllowed(command string) bool {
	if !terminalManager.safeMode {
		return true
	}

	parts := strings.Fields(command)
	if len(parts) == 0 {
		return false
	}

	baseCommand := strings.ToLower(filepath.Base(parts[0]))

	// Check blocked commands first
	for _, blocked := range terminalManager.blockedCommands {
		if strings.Contains(baseCommand, blocked) {
			return false
		}
	}

	// Check allowed commands
	for _, allowed := range terminalManager.allowedCommands {
		if baseCommand == allowed || strings.HasPrefix(baseCommand, allowed) {
			return true
		}
	}

	return false
}

func isDirectoryAllowed(dir string) bool {
	if !terminalManager.safeMode {
		return true
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}

	for _, allowedDir := range terminalManager.allowedDirs {
		allowedAbs, err := filepath.Abs(allowedDir)
		if err != nil {
			continue
		}

		if strings.HasPrefix(absDir, allowedAbs) {
			return true
		}
	}

	return false
}

// Utility functions
func getCurrentDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return "/"
	}
	return dir
}

func getScriptExtension(scriptType string) string {
	switch scriptType {
	case "bash", "sh":
		return "sh"
	case "powershell":
		return "ps1"
	case "cmd":
		return "bat"
	default:
		return "txt"
	}
}

func getActiveSessionCount() int {
	count := 0
	for _, session := range terminalManager.sessions {
		if session.Active {
			count++
		}
	}
	return count
}

func sendSSEEvent(eventType string, data interface{}) {
	if terminalManager.server != nil {
		event := mcp.SSEEvent{
			ID:    fmt.Sprintf("%s_%d", eventType, time.Now().UnixNano()),
			Event: eventType,
			Data:  data,
		}
		terminalManager.server.BroadcastSSEEvent(event)
	}
}

// System information functions
func getSystemInfo() (interface{}, error) {
	return map[string]interface{}{
		"os":           runtime.GOOS,
		"architecture": runtime.GOARCH,
		"hostname":     getHostname(),
		"user":         getCurrentUser(),
		"workingDir":   getCurrentDir(),
		"goVersion":    runtime.Version(),
		"timestamp":    time.Now(),
	}, nil
}

func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

func getCurrentUser() string {
	if user := os.Getenv("USER"); user != "" {
		return user
	}
	if user := os.Getenv("USERNAME"); user != "" {
		return user
	}
	return "unknown"
}

// Process management functions
func listProcesses(filter string) (interface{}, error) {
	var cmd *exec.Cmd
	
	if runtime.GOOS == "windows" {
		cmd = exec.Command("tasklist", "/fo", "csv")
	} else {
		cmd = exec.Command("ps", "aux")
	}

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list processes: %w", err)
	}

	// Parse output (simplified)
	lines := strings.Split(string(output), "\n")
	processes := make([]map[string]string, 0)

	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue // Skip header or empty lines
		}

		if filter != "" && !strings.Contains(strings.ToLower(line), strings.ToLower(filter)) {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) >= 3 {
			process := map[string]string{
				"pid":     fields[1],
				"command": strings.Join(fields[10:], " "),
				"cpu":     fields[2],
				"memory":  fields[3],
			}
			
			if runtime.GOOS != "windows" && len(fields) > 10 {
				process["user"] = fields[0]
			}
			
			processes = append(processes, process)
		}
	}

	return map[string]interface{}{
		"processes": processes,
		"count":     len(processes),
		"filter":    filter,
	}, nil
}

func killProcess(pid int, force bool) (interface{}, error) {
	var cmd *exec.Cmd
	
	if runtime.GOOS == "windows" {
		if force {
			cmd = exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/F")
		} else {
			cmd = exec.Command("taskkill", "/PID", strconv.Itoa(pid))
		}
	} else {
		if force {
			cmd = exec.Command("kill", "-9", strconv.Itoa(pid))
		} else {
			cmd = exec.Command("kill", strconv.Itoa(pid))
		}
	}

	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to kill process %d: %w", pid, err)
	}

	return map[string]interface{}{
		"pid":       pid,
		"killed":    true,
		"force":     force,
		"timestamp": time.Now(),
	}, nil
}

func getProcessInfo(pid int) (interface{}, error) {
	terminalManager.mu.RLock()
	processInfo := terminalManager.processes[pid]
	terminalManager.mu.RUnlock()

	if processInfo != nil {
		return processInfo, nil
	}

	// Try to get system process info
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("tasklist", "/PID", strconv.Itoa(pid), "/fo", "csv")
	} else {
		cmd = exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "pid,command,etime,pcpu,pmem")
	}

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("process not found: %d", pid)
	}

	return map[string]interface{}{
		"pid":    pid,
		"info":   string(output),
		"source": "system",
	}, nil
}

// File system functions
func listDirectory(path string, hidden, recursive bool) (interface{}, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var files []map[string]interface{}
	
	for _, entry := range entries {
		if !hidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		file := map[string]interface{}{
			"name":     entry.Name(),
			"isDir":    entry.IsDir(),
			"size":     info.Size(),
			"modified": info.ModTime(),
			"mode":     info.Mode().String(),
		}

		files = append(files, file)

		// Recursive listing for directories
		if recursive && entry.IsDir() {
			subPath := filepath.Join(path, entry.Name())
			if isDirectoryAllowed(subPath) {
				subResult, err := listDirectory(subPath, hidden, recursive)
				if err == nil {
					if subData, ok := subResult.(map[string]interface{}); ok {
						if subFiles, ok := subData["files"].([]map[string]interface{}); ok {
							for _, subFile := range subFiles {
								subFile["path"] = filepath.Join(entry.Name(), subFile["name"].(string))
								files = append(files, subFile)
							}
						}
					}
				}
			}
		}
	}

	return map[string]interface{}{
		"path":      path,
		"files":     files,
		"count":     len(files),
		"hidden":    hidden,
		"recursive": recursive,
	}, nil
}

func readFile(path string, lines, offset int) (interface{}, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var content strings.Builder
	scanner := bufio.NewScanner(file)
	
	lineNum := 0
	readLines := 0
	
	for scanner.Scan() {
		lineNum++
		
		if lineNum <= offset {
			continue
		}
		
		if lines > 0 && readLines >= lines {
			break
		}
		
		content.WriteString(scanner.Text())
		content.WriteString("\n")
		readLines++
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	info, _ := file.Stat()

	return map[string]interface{}{
		"path":      path,
		"content":   content.String(),
		"lines":     readLines,
		"offset":    offset,
		"fileSize":  info.Size(),
		"modified":  info.ModTime(),
	}, nil
}

func getFileInfo(path string) (interface{}, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	return map[string]interface{}{
		"name":     info.Name(),
		"size":     info.Size(),
		"mode":     info.Mode().String(),
		"modified": info.ModTime(),
		"isDir":    info.IsDir(),
		"path":     path,
	}, nil
}

func printStartupInfo(webConfig mcp.WebConfig) {
	fmt.Println("🖥️  Starting Terminal Control MCP Server...")
	fmt.Printf("📡 Dashboard: http://%s:%d/dashboard\n", webConfig.Host, webConfig.Port)
	fmt.Printf("🔐 Auth Token: %s\n", webConfig.AuthToken)
	fmt.Printf("📊 SSE Events: http://%s:%d/events?token=%s\n", webConfig.Host, webConfig.Port, webConfig.AuthToken)
	fmt.Println()
	fmt.Println("🛠️  Available Tools:")
	fmt.Println("   - execute_command: Execute terminal commands")
	fmt.Println("   - execute_interactive: Real-time command execution")
	fmt.Println("   - run_script: Execute shell scripts")
	fmt.Println("   - list_processes: List running processes")
	fmt.Println("   - kill_process: Kill processes")
	fmt.Println("   - list_directory: Browse directories")
	fmt.Println("   - read_file: Read file contents")
	fmt.Println("   - create_session: Create terminal sessions")
	fmt.Println()
	fmt.Println("🔒 Security Features:")
	fmt.Printf("   - Safe Mode: %v\n", terminalManager.safeMode)
	fmt.Printf("   - Allowed Commands: %d\n", len(terminalManager.allowedCommands))
	fmt.Printf("   - Blocked Commands: %d\n", len(terminalManager.blockedCommands))
	fmt.Printf("   - Allowed Directories: %d\n", len(terminalManager.allowedDirs))
	fmt.Println()
	fmt.Println("📋 Example API Calls:")
	fmt.Printf(`
curl -X POST http://%s:%d/api/v1/tools/call \
  -H "Authorization: Bearer %s" \
  -H "Content-Type: application/json" \
  -d '{"name": "execute_command", "arguments": {"command": "ls -la"}}'
`, webConfig.Host, webConfig.Port, webConfig.AuthToken)
	fmt.Println()
}

func setupGracefulShutdown(server *mcp.Server) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		log.Println("\n🛑 Shutting down terminal server...")

		// Send shutdown notification
		sendSSEEvent("server_shutdown", map[string]interface{}{
			"message":   "Terminal server is shutting down",
			"timestamp": time.Now(),
		})

		// Give clients time to receive the shutdown event
		time.Sleep(2 * time.Second)

		if err := server.StopWebTransport(); err != nil {
			log.Printf("Error stopping web transport: %v", err)
		}

		log.Println("✅ Terminal server stopped gracefully")
		os.Exit(0)
	}()
}