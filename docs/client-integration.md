# Client Integration Guide

Comprehensive guide for integrating clients with MCP Bridge gateway and router services.

## Table of Contents

- [Overview](#overview)
- [Authentication](#authentication)
- [Protocol Clients](#protocol-clients)
- [Language Examples](#language-examples)
- [Best Practices](#best-practices)
- [Error Handling](#error-handling)
- [Advanced Features](#advanced-features)

## Overview

MCP Bridge supports multiple client connection methods:

| Protocol | Port | Use Case | Features |
|----------|------|----------|----------|
| WebSocket | 8443 | Real-time bidirectional | Full-duplex, low latency |
| HTTP | 8080 | Request/response | Simple, stateless |
| SSE | 8081 | Server push events | Unidirectional streaming |
| TCP Binary | 8444 | High performance | Low overhead, binary protocol |
| stdio | - | Local CLI/process | Direct process integration |

## Authentication

### Bearer Token Authentication

**Obtain token from gateway:**

```bash
# Using curl with JWT endpoint
TOKEN=$(curl -X POST https://gateway.example.com:8443/auth/token \
  -H "Content-Type: application/json" \
  -d '{
    "grant_type": "client_credentials",
    "client_id": "my-client",
    "client_secret": "secret"
  }' | jq -r '.access_token')
```

**Use token in requests:**

```bash
# WebSocket (in header during handshake)
wscat -c wss://gateway.example.com:8443 \
  -H "Authorization: Bearer $TOKEN"

# HTTP (in each request)
curl -H "Authorization: Bearer $TOKEN" \
  https://gateway.example.com:8443/mcp
```

### JWT Authentication

**Token structure:**

```json
{
  "header": {
    "alg": "HS256",
    "typ": "JWT"
  },
  "payload": {
    "iss": "mcp-gateway",
    "sub": "client-123",
    "aud": "mcp-services",
    "exp": 1735689600,
    "iat": 1735686000,
    "scope": ["tools:read", "tools:write"]
  }
}
```

**Generate JWT (Node.js):**

```javascript
const jwt = require('jsonwebtoken');

const token = jwt.sign(
  {
    sub: 'client-123',
    aud: 'mcp-services',
    scope: ['tools:read', 'tools:write']
  },
  process.env.JWT_SECRET,
  {
    issuer: 'mcp-gateway',
    expiresIn: '1h',
    algorithm: 'HS256'
  }
);
```

### OAuth2 Authentication

**Client credentials flow:**

```python
import requests

# Get access token
token_response = requests.post(
    'https://auth.example.com/oauth/token',
    data={
        'grant_type': 'client_credentials',
        'client_id': 'my-client',
        'client_secret': 'secret',
        'scope': 'mcp:read mcp:write'
    }
)

access_token = token_response.json()['access_token']

# Use token with gateway
headers = {'Authorization': f'Bearer {access_token}'}
```

### Per-Message Authentication

For enhanced security, include auth with each message:

```json
{
  "jsonrpc": "2.0",
  "method": "tools/call",
  "params": {
    "name": "weather/forecast",
    "arguments": {"location": "NYC"}
  },
  "meta": {
    "auth": {
      "token": "eyJhbGc..."
    }
  },
  "id": 1
}
```

## Protocol Clients

### WebSocket Client

**JavaScript/TypeScript:**

```typescript
import WebSocket from 'ws';

class MCPWebSocketClient {
  private ws: WebSocket;
  private messageId: number = 0;
  private pendingRequests: Map<number, any> = new Map();

  constructor(url: string, token: string) {
    this.ws = new WebSocket(url, {
      headers: {
        'Authorization': `Bearer ${token}`
      }
    });

    this.ws.on('open', () => {
      console.log('Connected to MCP Gateway');
    });

    this.ws.on('message', (data: string) => {
      const response = JSON.parse(data);
      const pending = this.pendingRequests.get(response.id);

      if (pending) {
        if (response.error) {
          pending.reject(new Error(response.error.message));
        } else {
          pending.resolve(response.result);
        }
        this.pendingRequests.delete(response.id);
      }
    });

    this.ws.on('error', (error) => {
      console.error('WebSocket error:', error);
    });
  }

  async call(method: string, params?: any): Promise<any> {
    return new Promise((resolve, reject) => {
      const id = ++this.messageId;

      this.pendingRequests.set(id, { resolve, reject });

      this.ws.send(JSON.stringify({
        jsonrpc: '2.0',
        method,
        params,
        id
      }));

      // Timeout after 30 seconds
      setTimeout(() => {
        if (this.pendingRequests.has(id)) {
          this.pendingRequests.delete(id);
          reject(new Error('Request timeout'));
        }
      }, 30000);
    });
  }

  async listTools(): Promise<any> {
    return this.call('tools/list');
  }

  async callTool(name: string, args: any): Promise<any> {
    return this.call('tools/call', { name, arguments: args });
  }

  close() {
    this.ws.close();
  }
}

// Usage
const client = new MCPWebSocketClient(
  'wss://gateway.example.com:8443',
  process.env.MCP_TOKEN!
);

const tools = await client.listTools();
console.log('Available tools:', tools);

const result = await client.callTool('weather/forecast', {
  location: 'San Francisco'
});
console.log('Forecast:', result);

client.close();
```

**Python:**

```python
import asyncio
import json
import websockets

class MCPWebSocketClient:
    def __init__(self, url: str, token: str):
        self.url = url
        self.token = token
        self.message_id = 0
        self.pending_requests = {}

    async def connect(self):
        self.ws = await websockets.connect(
            self.url,
            extra_headers={'Authorization': f'Bearer {self.token}'}
        )
        asyncio.create_task(self._receive_messages())

    async def _receive_messages(self):
        async for message in self.ws:
            response = json.loads(message)
            request_id = response.get('id')

            if request_id in self.pending_requests:
                future = self.pending_requests.pop(request_id)
                if 'error' in response:
                    future.set_exception(Exception(response['error']['message']))
                else:
                    future.set_result(response.get('result'))

    async def call(self, method: str, params=None):
        self.message_id += 1
        request_id = self.message_id

        future = asyncio.Future()
        self.pending_requests[request_id] = future

        await self.ws.send(json.dumps({
            'jsonrpc': '2.0',
            'method': method,
            'params': params or {},
            'id': request_id
        }))

        return await asyncio.wait_for(future, timeout=30.0)

    async def list_tools(self):
        return await self.call('tools/list')

    async def call_tool(self, name: str, arguments: dict):
        return await self.call('tools/call', {
            'name': name,
            'arguments': arguments
        })

    async def close(self):
        await self.ws.close()

# Usage
async def main():
    client = MCPWebSocketClient(
        'wss://gateway.example.com:8443',
        os.getenv('MCP_TOKEN')
    )

    await client.connect()

    tools = await client.list_tools()
    print(f'Available tools: {tools}')

    result = await client.call_tool('weather/forecast', {
        'location': 'San Francisco'
    })
    print(f'Forecast: {result}')

    await client.close()

asyncio.run(main())
```

### HTTP Client

**JavaScript/TypeScript:**

```typescript
class MCPHttpClient {
  constructor(
    private baseUrl: string,
    private token: string
  ) {}

  async call(method: string, params?: any): Promise<any> {
    const response = await fetch(`${this.baseUrl}/mcp`, {
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${this.token}`,
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({
        jsonrpc: '2.0',
        method,
        params,
        id: Date.now()
      })
    });

    if (!response.ok) {
      throw new Error(`HTTP ${response.status}: ${response.statusText}`);
    }

    const data = await response.json();

    if (data.error) {
      throw new Error(data.error.message);
    }

    return data.result;
  }

  async listTools(): Promise<any> {
    return this.call('tools/list');
  }

  async callTool(name: string, args: any): Promise<any> {
    return this.call('tools/call', { name, arguments: args });
  }
}

// Usage
const client = new MCPHttpClient(
  'https://gateway.example.com:8443',
  process.env.MCP_TOKEN!
);

const tools = await client.listTools();
const result = await client.callTool('weather/forecast', {
  location: 'New York'
});
```

**Python:**

```python
import requests
from typing import Any, Dict, Optional

class MCPHttpClient:
    def __init__(self, base_url: str, token: str):
        self.base_url = base_url
        self.session = requests.Session()
        self.session.headers.update({
            'Authorization': f'Bearer {token}',
            'Content-Type': 'application/json'
        })
        self.request_id = 0

    def call(self, method: str, params: Optional[Dict[str, Any]] = None) -> Any:
        self.request_id += 1

        response = self.session.post(
            f'{self.base_url}/mcp',
            json={
                'jsonrpc': '2.0',
                'method': method,
                'params': params or {},
                'id': self.request_id
            }
        )

        response.raise_for_status()
        data = response.json()

        if 'error' in data:
            raise Exception(data['error']['message'])

        return data.get('result')

    def list_tools(self) -> Dict[str, Any]:
        return self.call('tools/list')

    def call_tool(self, name: str, arguments: Dict[str, Any]) -> Any:
        return self.call('tools/call', {
            'name': name,
            'arguments': arguments
        })

# Usage
client = MCPHttpClient(
    'https://gateway.example.com:8443',
    os.getenv('MCP_TOKEN')
)

tools = client.list_tools()
result = client.call_tool('weather/forecast', {'location': 'London'})
```

### SSE Client

**JavaScript:**

```javascript
class MCPSSEClient {
  constructor(url, token) {
    this.eventSource = new EventSource(url, {
      headers: {
        'Authorization': `Bearer ${token}`
      }
    });

    this.listeners = new Map();

    this.eventSource.addEventListener('message', (event) => {
      const data = JSON.parse(event.data);

      if (data.id && this.listeners.has(data.id)) {
        const callback = this.listeners.get(data.id);
        callback(data);
        this.listeners.delete(data.id);
      }
    });
  }

  async call(method, params) {
    const id = Date.now();

    // Send request via HTTP
    await fetch(`${this.baseUrl}/mcp`, {
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${this.token}`,
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({
        jsonrpc: '2.0',
        method,
        params,
        id
      })
    });

    // Wait for response via SSE
    return new Promise((resolve, reject) => {
      this.listeners.set(id, (data) => {
        if (data.error) {
          reject(new Error(data.error.message));
        } else {
          resolve(data.result);
        }
      });

      setTimeout(() => {
        if (this.listeners.has(id)) {
          this.listeners.delete(id);
          reject(new Error('Request timeout'));
        }
      }, 30000);
    });
  }

  close() {
    this.eventSource.close();
  }
}
```

### TCP Binary Client

**Python:**

```python
import socket
import struct
import json

class MCPTCPClient:
    def __init__(self, host: str, port: int, token: str):
        self.host = host
        self.port = port
        self.token = token
        self.sock = None
        self.message_id = 0

    def connect(self):
        self.sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.sock.connect((self.host, self.port))

        # Send authentication
        auth_msg = json.dumps({
            'jsonrpc': '2.0',
            'method': 'auth',
            'params': {'token': self.token},
            'id': 0
        }).encode('utf-8')

        self._send_frame(auth_msg)
        response = self._receive_frame()

        if json.loads(response).get('error'):
            raise Exception('Authentication failed')

    def _send_frame(self, data: bytes):
        # Frame format: [4 bytes length][N bytes data]
        length = struct.pack('>I', len(data))
        self.sock.sendall(length + data)

    def _receive_frame(self) -> bytes:
        # Read 4-byte length prefix
        length_data = self.sock.recv(4)
        if not length_data:
            raise ConnectionError('Connection closed')

        length = struct.unpack('>I', length_data)[0]

        # Read message data
        data = b''
        while len(data) < length:
            chunk = self.sock.recv(length - len(data))
            if not chunk:
                raise ConnectionError('Connection closed')
            data += chunk

        return data

    def call(self, method: str, params=None):
        self.message_id += 1

        request = json.dumps({
            'jsonrpc': '2.0',
            'method': method,
            'params': params or {},
            'id': self.message_id
        }).encode('utf-8')

        self._send_frame(request)
        response_data = self._receive_frame()
        response = json.loads(response_data)

        if 'error' in response:
            raise Exception(response['error']['message'])

        return response.get('result')

    def close(self):
        if self.sock:
            self.sock.close()

# Usage
client = MCPTCPClient('gateway.example.com', 8444, os.getenv('MCP_TOKEN'))
client.connect()

tools = client.call('tools/list')
print(tools)

client.close()
```

## Language Examples

### Go

```go
package main

import (
    "encoding/json"
    "fmt"
    "github.com/gorilla/websocket"
    "net/http"
)

type MCPClient struct {
    conn      *websocket.Conn
    messageID int
}

type JSONRPCRequest struct {
    JSONRPC string      `json:"jsonrpc"`
    Method  string      `json:"method"`
    Params  interface{} `json:"params,omitempty"`
    ID      int         `json:"id"`
}

type JSONRPCResponse struct {
    JSONRPC string          `json:"jsonrpc"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *RPCError       `json:"error,omitempty"`
    ID      int             `json:"id"`
}

type RPCError struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
}

func NewMCPClient(url, token string) (*MCPClient, error) {
    header := http.Header{}
    header.Add("Authorization", fmt.Sprintf("Bearer %s", token))

    conn, _, err := websocket.DefaultDialer.Dial(url, header)
    if err != nil {
        return nil, err
    }

    return &MCPClient{
        conn:      conn,
        messageID: 0,
    }, nil
}

func (c *MCPClient) Call(method string, params interface{}) (json.RawMessage, error) {
    c.messageID++

    req := JSONRPCRequest{
        JSONRPC: "2.0",
        Method:  method,
        Params:  params,
        ID:      c.messageID,
    }

    if err := c.conn.WriteJSON(req); err != nil {
        return nil, err
    }

    var resp JSONRPCResponse
    if err := c.conn.ReadJSON(&resp); err != nil {
        return nil, err
    }

    if resp.Error != nil {
        return nil, fmt.Errorf("RPC error: %s", resp.Error.Message)
    }

    return resp.Result, nil
}

func (c *MCPClient) ListTools() (json.RawMessage, error) {
    return c.Call("tools/list", nil)
}

func (c *MCPClient) Close() error {
    return c.conn.Close()
}

func main() {
    client, err := NewMCPClient(
        "wss://gateway.example.com:8443",
        os.Getenv("MCP_TOKEN"),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    tools, err := client.ListTools()
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("Tools:", string(tools))
}
```

### Rust

```rust
use serde::{Deserialize, Serialize};
use tokio_tungstenite::{connect_async, tungstenite::Message};
use futures_util::{SinkExt, StreamExt};

#[derive(Serialize, Deserialize)]
struct JSONRPCRequest {
    jsonrpc: String,
    method: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    params: Option<serde_json::Value>,
    id: u64,
}

#[derive(Serialize, Deserialize)]
struct JSONRPCResponse {
    jsonrpc: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    result: Option<serde_json::Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<RPCError>,
    id: u64,
}

#[derive(Serialize, Deserialize)]
struct RPCError {
    code: i32,
    message: String,
}

struct MCPClient {
    ws_stream: WebSocketStream<MaybeTlsStream<TcpStream>>,
    message_id: u64,
}

impl MCPClient {
    async fn new(url: &str, token: &str) -> Result<Self, Box<dyn std::error::Error>> {
        let mut request = url.into_client_request()?;
        request.headers_mut().insert(
            "Authorization",
            format!("Bearer {}", token).parse()?,
        );

        let (ws_stream, _) = connect_async(request).await?;

        Ok(MCPClient {
            ws_stream,
            message_id: 0,
        })
    }

    async fn call(
        &mut self,
        method: &str,
        params: Option<serde_json::Value>,
    ) -> Result<serde_json::Value, Box<dyn std::error::Error>> {
        self.message_id += 1;

        let request = JSONRPCRequest {
            jsonrpc: "2.0".to_string(),
            method: method.to_string(),
            params,
            id: self.message_id,
        };

        let msg = Message::Text(serde_json::to_string(&request)?);
        self.ws_stream.send(msg).await?;

        if let Some(msg) = self.ws_stream.next().await {
            let msg = msg?;
            let response: JSONRPCResponse = serde_json::from_str(msg.to_text()?)?;

            if let Some(error) = response.error {
                return Err(error.message.into());
            }

            Ok(response.result.unwrap_or(serde_json::Value::Null))
        } else {
            Err("Connection closed".into())
        }
    }
}
```

## Best Practices

### Connection Management

**Implement reconnection logic:**

```typescript
class ResilientMCPClient {
  private reconnectAttempts = 0;
  private maxReconnectAttempts = 5;
  private reconnectDelay = 1000;

  async connect() {
    try {
      await this.createConnection();
      this.reconnectAttempts = 0;
    } catch (error) {
      if (this.reconnectAttempts < this.maxReconnectAttempts) {
        this.reconnectAttempts++;
        const delay = this.reconnectDelay * Math.pow(2, this.reconnectAttempts);

        console.log(`Reconnecting in ${delay}ms...`);
        await new Promise(resolve => setTimeout(resolve, delay));

        return this.connect();
      }

      throw error;
    }
  }
}
```

### Request Timeouts

**Always set timeouts:**

```python
async def call_with_timeout(self, method: str, params=None, timeout=30):
    try:
        return await asyncio.wait_for(
            self.call(method, params),
            timeout=timeout
        )
    except asyncio.TimeoutError:
        raise Exception(f'Request to {method} timed out after {timeout}s')
```

### Error Handling

**Handle all error types:**

```typescript
try {
  const result = await client.callTool('weather/forecast', { location: 'NYC' });
} catch (error) {
  if (error.code === -32600) {
    console.error('Invalid request format');
  } else if (error.code === -32601) {
    console.error('Method not found');
  } else if (error.code === -32603) {
    console.error('Internal error');
  } else if (error.message.includes('timeout')) {
    console.error('Request timed out, retrying...');
  } else if (error.message.includes('ECONNREFUSED')) {
    console.error('Gateway unavailable');
  } else {
    console.error('Unknown error:', error);
  }
}
```

### Resource Cleanup

**Always clean up connections:**

```python
class MCPClient:
    async def __aenter__(self):
        await self.connect()
        return self

    async def __aexit__(self, exc_type, exc_val, exc_tb):
        await self.close()

# Usage
async with MCPClient(url, token) as client:
    tools = await client.list_tools()
    # Connection automatically closed
```

## Error Handling

### JSON-RPC Error Codes

| Code | Message | Description |
|------|---------|-------------|
| -32700 | Parse error | Invalid JSON |
| -32600 | Invalid Request | Invalid JSON-RPC |
| -32601 | Method not found | Method doesn't exist |
| -32602 | Invalid params | Invalid parameters |
| -32603 | Internal error | Server error |
| -32000 to -32099 | Server error | Implementation-defined errors |

### Gateway-Specific Errors

```typescript
interface MCPError {
  code: number;
  message: string;
  data?: {
    retryable?: boolean;
    backend_error?: string;
    circuit_breaker_state?: 'open' | 'half-open' | 'closed';
  };
}

function handleMCPError(error: MCPError) {
  if (error.data?.retryable) {
    // Retry with exponential backoff
    return retryWithBackoff(request);
  }

  if (error.data?.circuit_breaker_state === 'open') {
    // Circuit breaker is open, fail fast
    throw new Error('Service temporarily unavailable');
  }

  // Log and propagate error
  console.error('MCP Error:', error);
  throw error;
}
```

## Advanced Features

### Connection Pooling

```typescript
class MCPConnectionPool {
  private connections: MCPClient[] = [];
  private availableConnections: MCPClient[] = [];
  private maxConnections = 10;

  async getConnection(): Promise<MCPClient> {
    if (this.availableConnections.length > 0) {
      return this.availableConnections.pop()!;
    }

    if (this.connections.length < this.maxConnections) {
      const client = new MCPClient(this.url, this.token);
      await client.connect();
      this.connections.push(client);
      return client;
    }

    // Wait for available connection
    return new Promise((resolve) => {
      const interval = setInterval(() => {
        if (this.availableConnections.length > 0) {
          clearInterval(interval);
          resolve(this.availableConnections.pop()!);
        }
      }, 100);
    });
  }

  releaseConnection(client: MCPClient) {
    this.availableConnections.push(client);
  }
}
```

### Batching Requests

```typescript
class BatchingMCPClient {
  private batchQueue: any[] = [];
  private batchTimeout: NodeJS.Timeout | null = null;

  queueRequest(method: string, params: any): Promise<any> {
    return new Promise((resolve, reject) => {
      this.batchQueue.push({ method, params, resolve, reject });

      if (!this.batchTimeout) {
        this.batchTimeout = setTimeout(() => this.flushBatch(), 10);
      }
    });
  }

  private async flushBatch() {
    const batch = this.batchQueue.splice(0);
    this.batchTimeout = null;

    // Send batch request
    const requests = batch.map((item, index) => ({
      jsonrpc: '2.0',
      method: item.method,
      params: item.params,
      id: index
    }));

    const responses = await this.ws.send(JSON.stringify(requests));

    // Resolve individual promises
    responses.forEach((response: any, index: number) => {
      if (response.error) {
        batch[index].reject(response.error);
      } else {
        batch[index].resolve(response.result);
      }
    });
  }
}
```

### Streaming Responses

```typescript
async *streamToolCalls(toolName: string, args: any) {
  const id = ++this.messageId;

  this.ws.send(JSON.stringify({
    jsonrpc: '2.0',
    method: 'tools/call_stream',
    params: { name: toolName, arguments: args },
    id
  }));

  while (true) {
    const message = await this.waitForMessage(id);

    if (message.result?.done) {
      break;
    }

    yield message.result?.chunk;
  }
}

// Usage
for await (const chunk of client.streamToolCalls('llm/generate', { prompt: 'Hello' })) {
  process.stdout.write(chunk);
}
```

## See Also

- [Usage Guide](USAGE.md) - General usage documentation
- [Server Integration Guide](server-integration.md) - Adding MCP servers
- [Configuration Reference](configuration.md) - Complete configuration options
- [Protocol Documentation](protocol.md) - Wire protocol details
- [Authentication Guide](authentication.md) - Authentication methods
- [Error Handling Guide](ERROR_HANDLING.md) - Error handling patterns
