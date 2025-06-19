# Jarvis  MCP GO SDK

🤖 **Jarvis MCP SDK** - Go dili için gelişmiş MCP (Model Context Protocol) server framework'ü. FastMCP'nin ötesinde özellikler sunan, yüksek performanslı ve type-safe bir SDK.

## Özellikler

- 🚀 **Hızlı Geliştirme**: Minimal kod ile MCP server oluşturma
- 🎯 **Fluent API**: Method chaining ile kolay kullanım
- 🔧 **Tool Support**: Fonksiyonları kolayca MCP tool'larına dönüştürme
- 📁 **Resource Management**: Resource handler'ları ile veri erişimi
- 💬 **Prompt Support**: Dinamik prompt oluşturma
- 🔍 **Type Safety**: Go'nun tip güvenliği avantajları

### 🌟 Benzersiz Özellikler

- ⚡ **Concurrent Processing**: Goroutine pool ile yüksek performans
- 🎭 **Typed Tools**: Reflection ile otomatik schema oluşturma
- 🌊 **Streaming Tools**: Uzun süren işlemler için real-time progress
- 📊 **Built-in Metrics**: Performans izleme ve observability
- 🛡️ **Middleware System**: Authentication, rate limiting, validation
- 🎯 **Zero Dependencies**: Sadece Go standard library

### 🌐 Web & Real-time Features

- 🔗 **WebSocket Transport**: Bidirectional real-time communication
- 📺 **Server-Sent Events (SSE)**: One-way streaming from server
- 🌐 **HTTP REST API**: RESTful interface for all MCP operations
- 🔐 **Web Authentication**: Bearer token security
- 🎮 **Web Dashboard**: Browser-based management interface

## Kurulum

```bash
go mod init your-mcp-server
go get github.com/can-acar/jarvis-mcp-sdk
```

## Hızlı Başlangıç

### Basit Hesap Makinesi MCP Server

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    jarvis "github.com/can-acar/jarvis-mcp-sdk"
)

func main() {
    // Jarvis MCP server oluştur
    server := jarvis.NewServer("calculator", "1.0.0")

    // Tool'ları kaydet (FastMCP decorator benzeri)
    server.Tool("add", "Add two numbers", addTool).
           Tool("multiply", "Multiply two numbers", multiplyTool)

    // Server'ı başlat
    server.Run()
}

func addTool(ctx context.Context, params json.RawMessage) (interface{}, error) {
    var args struct {
        A float64 `json:"a"`
        B float64 `json:"b"`
    }
    
    if err := json.Unmarshal(params, &args); err != nil {
        return nil, err
    }
    
    return args.A + args.B, nil
}

func multiplyTool(ctx context.Context, params json.RawMessage) (interface{}, error) {
    var args struct {
        A float64 `json:"a"`
        B float64 `json:"b"`
    }
    
    if err := json.Unmarshal(params, &args); err != nil {
        return nil, err
    }
    
    return args.A * args.B, nil
}
```

## Ana Bileşenler

### Server

MCP server'ının ana bileşeni:

```go
server := mcp.NewServer("my-server", "1.0.0")
```

### Tools

Fonksiyonları MCP tool'larına dönüştürme:

```go
server.Tool("tool_name", "Tool description", handlerFunction)
```

### Resources

Veri kaynaklarına erişim:

```go
server.Resource("resource://uri", "name", "description", "mime/type", handlerFunction)
```

### Prompts

Dinamik prompt oluşturma:

```go
server.Prompt("prompt_name", "description", arguments, handlerFunction)
```

## Örnekler

### Dosya Yöneticisi

```go
server.Tool("read_file", "Read file contents", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
    var args struct {
        Path string `json:"path"`
    }
    json.Unmarshal(params, &args)
    
    content, err := os.ReadFile(args.Path)
    if err != nil {
        return nil, err
    }
    
    return string(content), nil
})
```

### Web API Entegrasyonu

```go
server.Tool("weather", "Get weather information", func(ctx context.Context, params json.RawMessage) (interface{}, error) {
    var args struct {
        City string `json:"city"`
    }
    json.Unmarshal(params, &args)
    
    // API çağrısı yapın
    resp, err := http.Get(fmt.Sprintf("https://api.weather.com/v1/current?q=%s", args.City))
    // ... API response işleme
    
    return weatherData, nil
})
```

## Gelişmiş Özellikler

### 🎭 Typed Tools (Otomatik Schema)

```go
type ProcessParams struct {
    FilePath string `json:"filePath" description:"File to process" required:"true"`
    Mode     string `json:"mode" enum:"fast,slow,batch" description:"Processing mode"`
}

// Otomatik JSON schema oluşturur
server.RegisterTypedTool("process", "Process file", func(ctx context.Context, params ProcessParams) (string, error) {
    return fmt.Sprintf("Processing %s in %s mode", params.FilePath, params.Mode), nil
})
```

### ⚡ Concurrent Processing

```go
// Goroutine pool ile concurrent processing
server.EnableConcurrency(jarvis.ConcurrencyConfig{
    MaxWorkers:     10,
    QueueSize:      100,
    RequestTimeout: 30 * time.Second,
    EnableMetrics:  true,
})

// Metrics'leri al
metrics := server.GetConcurrencyMetrics()
fmt.Printf("RPS: %.2f, Active: %d", metrics.RequestsPerSecond, metrics.ActiveRequests)
```

### 🌊 Streaming Tools

```go
server.StreamingTool("batch_process", "Process large dataset", func(ctx context.Context, params json.RawMessage) (<-chan jarvis.StreamingResult, error) {
    resultChan := make(chan jarvis.StreamingResult, 100)
    
    go func() {
        defer close(resultChan)
        
        for i := 0; i < 1000; i++ {
            // Uzun süren işlem
            result := processItem(i)
            
            resultChan <- jarvis.StreamingResult{
                Data:     result,
                Progress: jarvis.NewProgress(int64(i), 1000, "Processing..."),
                Finished: i == 999,
            }
        }
    }()
    
    return resultChan, nil
})

// Client tarafında polling
// 1. Tool'u çağır -> sessionId al
// 2. stream/poll ile sonuçları al
// 3. stream/cancel ile iptal et
```

### 🔗 WebSocket Real-time Communication

```go
// Enable WebSocket support
server.EnableWebSocket(jarvis.DefaultWebSocketConfig())

// Start with multi-transport (HTTP + WebSocket + stdio)
server.RunMultiTransport()
```

**JavaScript WebSocket Client:**
```javascript
const ws = new WebSocket('ws://localhost:8080/ws?token=your-token');

// Subscribe to streaming tool
ws.send(JSON.stringify({
  type: 'stream_subscribe',
  id: 'stream-1',
  params: {
    toolName: 'batch_process',
    arguments: { filePattern: '*.txt' }
  }
}));

// Receive real-time updates
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  if (msg.type === 'stream_data') {
    updateProgressBar(msg.result.progress);
  }
};
```

### 📺 Server-Sent Events (SSE)

```go
// Enable SSE support
server.EnableSSE(jarvis.DefaultSSEConfig())

// Broadcast events to all SSE connections
server.BroadcastSSEEvent(jarvis.SSEEvent{
  Event: "notification",
  Data: map[string]interface{}{
    "message": "System maintenance starting",
    "level": "warning",
  },
})
```

**JavaScript SSE Client:**
```javascript
const eventSource = new EventSource('http://localhost:8080/events?token=your-token');

eventSource.onmessage = (event) => {
  const data = JSON.parse(event.data);
  showNotification(data);
};
```

### Custom Logger

```go
logger := log.New(os.Stdout, "[MyServer] ", log.LstdFlags)
server.SetLogger(logger)
```

### Custom Transport

```go
// Stdin/Stdout yerine custom reader/writer kullanma
server.RunWithTransport(reader, writer)
```

## Karşılaştırma: FastMCP vs Jarvis MCP SDK

| Özellik | FastMCP (Python) | Jarvis MCP SDK |
|---------|------------------|------------|
| Decorator API | `@app.tool()` | `server.Tool()` |
| Method Chaining | ❌ | ✅ |
| Type Safety | Limited | Strong |
| Performance | Good | Excellent |
| Memory Usage | Higher | Lower |
| Deployment | Python required | Single binary |
| Concurrency | Limited (GIL) | Native Goroutines |
| Streaming | ❌ | Real-time Progress |
| Schema Gen | Manual | Automatic (Reflection) |
| Performance | Good | Excellent |
| WebSocket | ❌ | Full Support |
| SSE | ❌ | Built-in |
| Web Dashboard | ❌ | Included |
| Multi-Transport | ❌ | HTTP+WS+stdio |

## Lisans

MIT License
