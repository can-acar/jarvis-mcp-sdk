# 🌐 Jarvis MCP Web Server Example

Bu örnek, Jarvis MCP SDK'nın web transport özelliklerini demonstre eder.

## 🚀 Özellikler

- **HTTP REST API**: MCP tools'larına HTTP üzerinden erişim
- **Authentication**: Bearer token ile güvenlik
- **CORS Support**: Cross-origin istekleri destekler
- **Web Dashboard**: Tarayıcı tabanlı arayüz
- **Concurrency**: Çoklu istek paralel işleme
- **Graceful Shutdown**: Temiz kapanış

## 📦 Kurulum

```bash
cd example/web-server
go run main.go
```

## 🌟 Kullanım

### Server Başlatma
```bash
go run main.go
```

Server başladığında şu bilgileri göreceksiniz:
```
🤖 Starting Jarvis MCP Web Server...
📡 Web Dashboard: http://localhost:8080/dashboard
🔐 Auth Token: demo-secret-token-2024
📚 API Endpoints:
   GET  http://localhost:8080/health
   GET  http://localhost:8080/api/v1/server/info
   GET  http://localhost:8080/api/v1/tools/list
   POST http://localhost:8080/api/v1/tools/call
```

### 🎮 Web Dashboard

Tarayıcınızda açın: http://localhost:8080/dashboard

Dashboard şunları içerir:
- Server istatistikleri
- Kayıtlı tools/resources/prompts sayısı
- API endpoint listesi
- Authentication bilgileri

### 🔧 API Kullanımı

#### Health Check
```bash
curl http://localhost:8080/health
```

#### Server Info
```bash
curl -H "Authorization: Bearer demo-secret-token-2024" \
     http://localhost:8080/api/v1/server/info
```

#### Tools Listesi
```bash
curl -H "Authorization: Bearer demo-secret-token-2024" \
     http://localhost:8080/api/v1/tools/list
```

#### Calculator Tool
```bash
curl -H "Authorization: Bearer demo-secret-token-2024" \
     -H "Content-Type: application/json" \
     -X POST http://localhost:8080/api/v1/tools/call \
     -d '{"name": "calculator", "arguments": {"operation": "add", "a": 10, "b": 5}}'
```

#### Text Processor Tool
```bash
curl -H "Authorization: Bearer demo-secret-token-2024" \
     -H "Content-Type: application/json" \
     -X POST http://localhost:8080/api/v1/tools/call \
     -d '{"name": "text_processor", "arguments": {"text": "Hello World", "operation": "uppercase"}}'
```

#### System Info Tool
```bash
curl -H "Authorization: Bearer demo-secret-token-2024" \
     -H "Content-Type: application/json" \
     -X POST http://localhost:8080/api/v1/tools/call \
     -d '{"name": "system_info", "arguments": {}}'
```

#### Resource Okuma
```bash
curl -H "Authorization: Bearer demo-secret-token-2024" \
     -H "Content-Type: application/json" \
     -X POST http://localhost:8080/api/v1/resources/read \
     -d '{"uri": "stats://server"}'
```

### 🐍 Python Client Örneği

```python
import requests

BASE_URL = "http://localhost:8080"
TOKEN = "demo-secret-token-2024"
HEADERS = {"Authorization": f"Bearer {TOKEN}"}

# Calculator kullanımı
response = requests.post(
    f"{BASE_URL}/api/v1/tools/call",
    headers={**HEADERS, "Content-Type": "application/json"},
    json={
        "name": "calculator",
        "arguments": {"operation": "multiply", "a": 7, "b": 6}
    }
)
print(response.json())
```

### 🌐 JavaScript Client Örneği

```javascript
const baseURL = "http://localhost:8080";
const token = "demo-secret-token-2024";

async function callTool(name, arguments) {
    const response = await fetch(`${baseURL}/api/v1/tools/call`, {
        method: 'POST',
        headers: {
            'Authorization': `Bearer ${token}`,
            'Content-Type': 'application/json'
        },
        body: JSON.stringify({ name, arguments })
    });
    return response.json();
}

// Text processor kullanımı
callTool('text_processor', { 
    text: 'Jarvis MCP SDK', 
    operation: 'reverse' 
}).then(console.log);
```

## 📊 Monitöring

### Performance Metrics
```bash
curl -H "Authorization: Bearer demo-secret-token-2024" \
     -H "Content-Type: application/json" \
     -X POST http://localhost:8080/api/v1/resources/read \
     -d '{"uri": "stats://server"}'
```

### API Documentation
```bash
curl -H "Authorization: Bearer demo-secret-token-2024" \
     -H "Content-Type: application/json" \
     -X POST http://localhost:8080/api/v1/resources/read \
     -d '{"uri": "docs://api"}'
```

## 🛡️ Security

- **Authentication**: Bearer token gerekli (health endpoint hariç)
- **CORS**: Cross-origin istekleri desteklenir
- **Request Size Limit**: 10MB maksimum request boyutu
- **Timeouts**: 30 saniye read/write timeout

## 🚨 Error Handling

API hataları standart format döner:
```json
{
  "success": false,
  "error": {
    "code": 400,
    "message": "Error description"
  }
}
```

Test için error endpoint:
```bash
curl -H "Authorization: Bearer demo-secret-token-2024" \
     -H "Content-Type: application/json" \
     -X POST http://localhost:8080/api/v1/tools/call \
     -d '{"name": "demo_error", "arguments": {}}'
```

## 🔧 Configuration

`WebConfig` ile özelleştirilebilir:
```go
webConfig := jarvis.WebConfig{
    Port:            8080,
    Host:            "localhost",
    AuthToken:       "your-secret-token",
    EnableCORS:      true,
    EnableDashboard: true,
    ReadTimeout:     30 * time.Second,
    WriteTimeout:    30 * time.Second,
    MaxRequestSize:  10 * 1024 * 1024,
}
```

## 🎯 Use Cases

1. **Development**: Tools'ları web arayüzünden test etme
2. **Integration**: Frontend uygulamalarla entegrasyon
3. **Automation**: CI/CD pipeline'larda HTTP API kullanımı
4. **Monitoring**: Performance metrikleri takibi
5. **Documentation**: API dokümantasyonu ve örnekler