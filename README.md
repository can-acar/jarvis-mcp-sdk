# Go MCP SDK

FastMCP benzeri Go dili için MCP (Model Context Protocol) server SDK'sı.

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

## Kurulum

```bash
go mod init your-mcp-server
go get github.com/mcp-sdk/go-mcp
```

## Hızlı Başlangıç

### Basit Hesap Makinesi MCP Server

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    mcp "github.com/mcp-sdk/go-mcp"
)

func main() {
    // MCP server oluştur
    server := mcp.NewServer("calculator", "1.0.0")

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
server.EnableConcurrency(mcp.ConcurrencyConfig{
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
server.StreamingTool("batch_process", "Process large dataset", func(ctx context.Context, params json.RawMessage) (<-chan mcp.StreamingResult, error) {
    resultChan := make(chan mcp.StreamingResult, 100)
    
    go func() {
        defer close(resultChan)
        
        for i := 0; i < 1000; i++ {
            // Uzun süren işlem
            result := processItem(i)
            
            resultChan <- mcp.StreamingResult{
                Data:     result,
                Progress: mcp.NewProgress(int64(i), 1000, "Processing..."),
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

## Karşılaştırma: FastMCP vs Go MCP SDK

| Özellik | FastMCP (Python) | Go MCP SDK |
|---------|------------------|------------|
| Decorator API | `@app.tool()` | `server.Tool()` |
| Method Chaining | ❌ | ✅ |
| Type Safety | Limited | Strong |
| Performance | Good | Excellent |
| Memory Usage | Higher | Lower |
| Deployment | Python required | Single binary |

## Lisans

MIT License