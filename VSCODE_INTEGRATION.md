# VSCode MCP Integration Guide

Bu rehber, MCP server'larınızı VSCode'da `http` ve `sse` transport türleri ile nasıl kullanacağınızı açıklar.

## 🚀 Kurulum

### 1. Server'ları Build Edin

```bash
cd /mnt/d/mcp-server/mcp-sdk

# HTTP server build
go build -o vscode-http-server examples/vscode_http_server.go

# SSE server build  
go build -o vscode-sse-server examples/vscode_sse_server.go
```

### 2. Server'ları Başlatın

**Terminal 1 - HTTP Server:**
```bash
./vscode-http-server
```

**Terminal 2 - SSE Server:**
```bash
./vscode-sse-server
```

## 🔧 VSCode Konfigürasyonu

### HTTP Transport Konfigürasyonu

VSCode settings.json'a ekleyin:

```json
{
  "mcp.servers": {
    "vscode-http-server": {
      "type": "http",
      "url": "http://localhost:8080/api/v1",
      "headers": {
        "Authorization": "Bearer vscode-http-token-2024"
      }
    }
  }
}
```

### SSE Transport Konfigürasyonu

```json
{
  "mcp.servers": {
    "vscode-sse-server": {
      "type": "sse", 
      "url": "http://localhost:8081/events",
      "headers": {
        "Authorization": "Bearer vscode-sse-token-2024"
      }
    }
  }
}
```

## 🛠️ HTTP Server Tools

### 1. Code Formatter
```json
{
  "name": "format_code",
  "arguments": {
    "code": "func main(){fmt.Println(\"hello\")}",
    "language": "go",
    "style": "gofmt"
  }
}
```

### 2. Project Analyzer
```json
{
  "name": "analyze_project", 
  "arguments": {
    "projectPath": "/workspace/my-project",
    "fileTypes": ["go", "js", "ts"]
  }
}
```

### 3. Git Status
```json
{
  "name": "git_status",
  "arguments": {
    "repoPath": "/workspace/my-repo"
  }
}
```

### 4. Code Generator
```json
{
  "name": "generate_code",
  "arguments": {
    "template": "function",
    "language": "go", 
    "parameters": {
      "name": "calculateSum"
    }
  }
}
```

### 5. Workspace Scanner
```json
{
  "name": "scan_workspace",
  "arguments": {
    "pattern": "*.go",
    "fileTypes": ["go"],
    "maxResults": 20
  }
}
```

## 📡 SSE Server Tools (Real-time)

### 1. Log Monitor
```json
{
  "name": "start_log_monitor",
  "arguments": {
    "logPath": "/var/log/app.log",
    "pattern": "ERROR",
    "duration": 60
  }
}
```

### 2. Build Progress
```json
{
  "name": "start_build",
  "arguments": {
    "projectPath": "/workspace/my-project",
    "buildType": "release"
  }
}
```

### 3. File Watcher
```json
{
  "name": "watch_files",
  "arguments": {
    "paths": ["/workspace/src"],
    "extensions": ["go", "js", "ts"],
    "duration": 300
  }
}
```

### 4. Test Runner
```json
{
  "name": "run_tests",
  "arguments": {
    "testPath": "/workspace/tests",
    "pattern": "Test*",
    "parallel": true
  }
}
```

### 5. Performance Monitor
```json
{
  "name": "monitor_performance",
  "arguments": {
    "metrics": ["cpu", "memory", "disk"],
    "interval": 5,
    "duration": 120
  }
}
```

## 🌐 Web Dashboard

### HTTP Server Dashboard
- URL: http://localhost:8080/dashboard
- Token: vscode-http-token-2024

### SSE Server Dashboard  
- URL: http://localhost:8081/dashboard
- SSE Events: http://localhost:8081/events
- Token: vscode-sse-token-2024

## 📋 Test Komutları

### HTTP Server Test
```bash
# Tools list
curl -H "Authorization: Bearer vscode-http-token-2024" \
     http://localhost:8080/api/v1/tools/list

# Code format
curl -X POST \
     -H "Authorization: Bearer vscode-http-token-2024" \
     -H "Content-Type: application/json" \
     -d '{"method":"tools/call","params":{"name":"format_code","arguments":{"code":"func main(){fmt.Println(\"hello\")}","language":"go"}}}' \
     http://localhost:8080/api/v1/tools/call
```

### SSE Server Test
```bash
# SSE connection
curl -H "Authorization: Bearer vscode-sse-token-2024" \
     http://localhost:8081/events

# Start build monitoring
curl -X POST \
     -H "Authorization: Bearer vscode-sse-token-2024" \
     -H "Content-Type: application/json" \
     -d '{"method":"tools/call","params":{"name":"start_build","arguments":{"projectPath":"/workspace","buildType":"debug"}}}' \
     http://localhost:8081/api/v1/tools/call
```

## 🔒 Güvenlik

- Her server farklı port kullanır (8080, 8081)
- Bearer token authentication
- CORS enabled for web access
- Input validation ve sanitization

## 🐛 Sorun Giderme

### Server başlamıyor
```bash
# Port kullanımını kontrol edin
netstat -tlnp | grep :8080
netstat -tlnp | grep :8081

# Executable permission
chmod +x vscode-http-server vscode-sse-server
```

### VSCode'da görünmüyor
1. Server'ların çalıştığını kontrol edin
2. Konfigürasyon dosyasını kontrol edin
3. VSCode'u yeniden başlatın
4. Developer Console'da hata loglarını kontrol edin

### SSE bağlantısı kopuyor
1. Network connectivity kontrol edin
2. Firewall ayarlarını kontrol edin
3. Token'ın doğru olduğunu kontrol edin

## 📁 Dosya Yapısı

```
/mnt/d/mcp-server/mcp-sdk/
├── vscode-http-server              # HTTP server executable
├── vscode-sse-server               # SSE server executable
├── vscode_http_config.json         # HTTP konfigürasyonu
├── vscode_sse_config.json          # SSE konfigürasyonu
├── examples/
│   ├── vscode_http_server.go       # HTTP server kaynak
│   └── vscode_sse_server.go        # SSE server kaynak
└── VSCODE_INTEGRATION.md           # Bu dosya
```

## 🚀 Geliştirme

Yeni özellik eklemek için:

1. İlgili server dosyasını düzenleyin
2. Server'ı rebuild edin
3. Server'ı yeniden başlatın
4. VSCode'da test edin

Her iki transport türü de MCP protokolünü tam olarak destekler ve farklı kullanım senaryoları için optimize edilmiştir.