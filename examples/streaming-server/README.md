# 🌊 Jarvis MCP Streaming Server Example

Bu örnek, Jarvis MCP GO SDK'nın gelişmiş streaming özelliklerini demonstre eder - WebSocket, Server-Sent Events (SSE) ve real-time streaming tools.

## 🚀 Özellikler

- **📡 WebSocket Transport**: Bidirectional real-time communication
- **📺 Server-Sent Events (SSE)**: One-way streaming from server
- **🌊 Streaming Tools**: Long-running operations with real-time progress
- **⚡ Concurrent Processing**: Multiple parallel requests
- **🔐 Authentication**: Token-based security
- **📊 Real-time Metrics**: Performance monitoring
- **🎮 Web Dashboard**: Browser-based interface

## 📦 Kurulum

```bash
cd example/streaming-server
go mod tidy
go run main.go
```

## 🌟 Kullanım

### Server Başlatma
```bash
go run main.go
```

Server başladığında:
```
🌊 Starting Jarvis MCP Streaming Server...
📡 Web Dashboard: http://localhost:8080/dashboard
🔗 WebSocket Endpoint: ws://localhost:8080/ws
📺 SSE Endpoint: http://localhost:8080/events | sse
🔐 Auth Token: streaming-demo-token-2024
```

### 🎮 Web Dashboard

Tarayıcınızda açın: http://localhost:8080/dashboard

Dashboard şunları içerir:
- Active streaming sessions
- WebSocket connections
- Performance metrics
- API documentation

## 🔌 WebSocket Kullanımı

### JavaScript WebSocket Client

```javascript
const ws = new WebSocket('ws://localhost:8080/ws?token=streaming-demo-token-2024');

// Connection events
ws.onopen = () => console.log('Connected to WebSocket');
ws.onclose = () => console.log('Disconnected from WebSocket');
ws.onerror = (error) => console.error('WebSocket error:', error);

// Handle messages
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  
  switch(msg.type) {
    case 'pong':
      console.log('Ping response received');
      break;
    case 'response':
      console.log('Request response:', msg.result);
      break;
    case 'stream_data':
      console.log('Streaming update:', msg.result);
      updateProgressBar(msg.result);
      break;
    case 'error':
      console.error('Error:', msg.error);
      break;
  }
};

// Send ping
ws.send(JSON.stringify({
  type: 'ping',
  id: 'ping-1'
}));

// Call a regular tool
ws.send(JSON.stringify({
  type: 'request',
  id: 'req-1',
  method: 'tools/call',
  params: {
    name: 'system_status',
    arguments: {}
  }
}));

// Subscribe to streaming tool
ws.send(JSON.stringify({
  type: 'stream_subscribe',
  id: 'stream-1',
  params: {
    toolName: 'process_files',
    arguments: {
      filePattern: '*.txt',
      operation: 'analyze',
      batchSize: 10
    }
  }
}));

// Unsubscribe from streaming
ws.send(JSON.stringify({
  type: 'stream_unsubscribe',
  id: 'unsub-1',
  params: {
    sessionId: 'stream-session-id'
  }
}));
```

### Progress Bar Example

```javascript
function updateProgressBar(result) {
  if (result.results && result.results[0] && result.results[0].progress) {
    const progress = result.results[0].progress;
    const percent = progress.percentage;
    
    document.getElementById('progress-bar').style.width = percent + '%';
    document.getElementById('progress-text').textContent = 
      `${progress.current}/${progress.total} - ${progress.message}`;
    
    if (progress.estimatedETA) {
      document.getElementById('eta').textContent = 
        `ETA: ${progress.estimatedETA}`;
    }
  }
}
```

## 📺 Server-Sent Events (SSE)

### JavaScript SSE Client

```javascript
const eventSource = new EventSource('http://localhost:8080/events?token=streaming-demo-token-2024');

eventSource.onopen = () => {
  console.log('SSE connection opened');
};

eventSource.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log('SSE event:', data);
};

eventSource.addEventListener('connection', (event) => {
  const data = JSON.parse(event.data);
  console.log('Connected with ID:', data.connectionId);
});

eventSource.addEventListener('heartbeat', (event) => {
  console.log('Heartbeat received');
});

eventSource.onerror = (error) => {
  console.error('SSE error:', error);
};

// Close connection when done
// eventSource.close();
```

### curl ile SSE

```bash
curl -N -H "Accept: text/event-stream" \
     "http://localhost:8080/events?token=streaming-demo-token-2024"
```

## 🌊 Streaming Tools

### 1. File Processing (`process_files`)

Batch file processing with real-time progress:

```javascript
ws.send(JSON.stringify({
  type: 'stream_subscribe',
  id: 'file-processing',
  params: {
    toolName: 'process_files',
    arguments: {
      filePattern: '*.log',
      operation: 'compress',
      batchSize: 5
    }
  }
}));
```

**Response Stream:**
```json
{
  "type": "stream_data",
  "result": {
    "results": [{
      "data": "Processed file_001.txt (compress operation)",
      "progress": {
        "current": 1,
        "total": 100,
        "percentage": 1.0,
        "message": "Processing file_001.txt"
      },
      "metadata": {
        "filename": "file_001.txt",
        "operation": "compress",
        "processing_time": "150ms",
        "file_size": 5432
      }
    }]
  }
}
```

### 2. Dataset Analysis (`analyze_dataset`)

Large dataset analysis with chunked processing:

```javascript
ws.send(JSON.stringify({
  type: 'stream_subscribe',
  id: 'data-analysis',
  params: {
    toolName: 'analyze_dataset',
    arguments: {
      datasetName: 'sales_data_2024',
      chunkSize: 1000,
      metrics: ['mean', 'std', 'min', 'max']
    }
  }
}));
```

### 3. Log Streaming (`stream_logs`)

Real-time log streaming:

```javascript
ws.send(JSON.stringify({
  type: 'stream_subscribe',
  id: 'log-stream',
  params: {
    toolName: 'stream_logs',
    arguments: {
      logLevel: 'ERROR',
      maxLines: 50,
      interval: 500
    }
  }
}));
```

## 🔧 HTTP API (Non-Streaming)

### System Status

```bash
curl -H "Authorization: Bearer streaming-demo-token-2024" \
     -H "Content-Type: application/json" \
     -X POST http://localhost:8080/api/v1/tools/call \
     -d '{"name": "system_status", "arguments": {}}'
```

### Performance Metrics

```bash
curl -H "Authorization: Bearer streaming-demo-token-2024" \
     -H "Content-Type: application/json" \
     -X POST http://localhost:8080/api/v1/tools/call \
     -d '{"name": "performance_metrics", "arguments": {}}'
```

### Resources

```bash
# Active streaming sessions
curl -H "Authorization: Bearer streaming-demo-token-2024" \
     -H "Content-Type: application/json" \
     -X POST http://localhost:8080/api/v1/resources/read \
     -d '{"uri": "streaming://sessions"}'

# WebSocket connections
curl -H "Authorization: Bearer streaming-demo-token-2024" \
     -H "Content-Type: application/json" \
     -X POST http://localhost:8080/api/v1/resources/read \
     -d '{"uri": "connections://websocket"}'
```

## 🎯 Use Cases

### 1. **File Processing Dashboard**
Real-time file processing with progress tracking
```javascript
// Start file processing
ws.send(JSON.stringify({
  type: 'stream_subscribe',
  params: {
    toolName: 'process_files',
    arguments: { filePattern: '*.csv', operation: 'validate' }
  }
}));
```

### 2. **Data Analytics Pipeline**
Stream large dataset analysis results
```javascript
// Start dataset analysis
ws.send(JSON.stringify({
  type: 'stream_subscribe', 
  params: {
    toolName: 'analyze_dataset',
    arguments: { 
      datasetName: 'customer_data',
      chunkSize: 5000,
      metrics: ['mean', 'median', 'std']
    }
  }
}));
```

### 3. **Real-time Monitoring**
Live system log monitoring
```javascript
// Stream ERROR logs only
ws.send(JSON.stringify({
  type: 'stream_subscribe',
  params: {
    toolName: 'stream_logs',
    arguments: { 
      logLevel: 'ERROR',
      interval: 1000
    }
  }
}));
```

### 4. **Progress Tracking UI**
```html
<div class="progress-container">
  <div class="progress-bar">
    <div id="progress-fill" style="width: 0%"></div>
  </div>
  <div id="progress-text">Ready to start...</div>
  <div id="eta-text"></div>
</div>

<script>
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  if (msg.type === 'stream_data' && msg.result.results[0].progress) {
    const progress = msg.result.results[0].progress;
    document.getElementById('progress-fill').style.width = progress.percentage + '%';
    document.getElementById('progress-text').textContent = 
      `${progress.current}/${progress.total} - ${progress.message}`;
    if (progress.estimatedETA) {
      document.getElementById('eta-text').textContent = 
        `ETA: ${progress.estimatedETA}`;
    }
  }
};
</script>
```

## 🛡️ Security

- **Authentication**: Bearer token required for all endpoints
- **WebSocket Auth**: Token via query parameter or Authorization header
- **SSE Auth**: Token via query parameter or Authorization header
- **CORS**: Enabled for cross-origin requests

## 📊 Monitoring

- **Concurrency Metrics**: Request rates, response times
- **Connection Counts**: Active WebSocket/SSE connections
- **Streaming Sessions**: Active streaming operations
- **Memory Usage**: Server resource utilization

## 🔧 Configuration

```go
// WebSocket Configuration
wsConfig := jarvis.DefaultWebSocketConfig()
wsConfig.MaxMessageSize = 2 * 1024 * 1024 // 2MB
wsConfig.PingInterval = 20 * time.Second

// SSE Configuration
sseConfig := jarvis.DefaultSSEConfig()
sseConfig.HeartbeatInterval = 15 * time.Second
sseConfig.MaxConnections = 200

// Web Transport Configuration
webConfig := jarvis.WebConfig{
    Port: 8080,
    AuthToken: "your-secret-token",
    MaxRequestSize: 20 * 1024 * 1024, // 20MB
}
```

Bu örnek, Jarvis MCP SDK'nın real-time streaming capabilities'ini göstererek modern web applications için güçlü bir foundation sağlar! 🌊🚀