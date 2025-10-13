# Tutorial: Client Integration

Build clients that connect to MCP Bridge Gateway using different protocols and languages.

## Prerequisites

- MCP Bridge Gateway running
- Basic programming knowledge
- Understanding of WebSocket, HTTP, or your chosen protocol
- 25-30 minutes

## What You'll Build

Client implementations in multiple languages:
- **JavaScript/TypeScript** - Browser and Node.js clients
- **Python** - WebSocket and HTTP clients
- **Go** - High-performance TCP Binary client
- **Curl/CLI** - Quick testing and scripting

Each client demonstrates:
- Connection management
- Authentication
- Request/response handling
- Error handling
- Automatic reconnection

## Architecture

```
┌─────────────────────────────────────────────┐
│  Your Client Application                     │
│  ┌─────────────────────────────────────┐   │
│  │  MCP Client Library                 │   │
│  │  • Connect to Gateway               │   │
│  │  • Handle Authentication            │   │
│  │  • Send/Receive Messages            │   │
│  │  • Automatic Reconnection           │   │
│  └──────────────┬──────────────────────┘   │
└─────────────────┼──────────────────────────┘
                  │
                  │ WebSocket/HTTP/TCP
                  ▼
         ┌────────────────┐
         │  MCP Gateway   │
         └────────────────┘
```

## Client 1: JavaScript/TypeScript Browser Client

### Installation

```bash
npm install @modelcontextprotocol/sdk
# or
yarn add @modelcontextprotocol/sdk
```

### Basic WebSocket Client

Create `client.ts`:

```typescript
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { WebSocketClientTransport } from '@modelcontextprotocol/sdk/client/websocket.js';

class MCPClient {
  private client: Client;
  private transport: WebSocketClientTransport;
  private reconnectAttempts = 0;
  private maxReconnectAttempts = 5;

  constructor(
    private url: string,
    private authToken: string
  ) {}

  async connect(): Promise<void> {
    try {
      // Create WebSocket transport
      this.transport = new WebSocketClientTransport(this.url, {
        headers: {
          'Authorization': `Bearer ${this.authToken}`
        }
      });

      // Create MCP client
      this.client = new Client({
        name: 'my-mcp-client',
        version: '1.0.0'
      }, {
        capabilities: {
          tools: {}
        }
      });

      // Connect transport
      await this.client.connect(this.transport);

      // Reset reconnect attempts on successful connection
      this.reconnectAttempts = 0;

      console.log('Connected to MCP Gateway');

      // Handle disconnection
      this.transport.onclose = () => this.handleDisconnect();
      this.transport.onerror = (error) => this.handleError(error);

    } catch (error) {
      console.error('Connection failed:', error);
      await this.handleDisconnect();
    }
  }

  async listTools(): Promise<any[]> {
    try {
      const response = await this.client.listTools();
      return response.tools;
    } catch (error) {
      console.error('Failed to list tools:', error);
      throw error;
    }
  }

  async callTool(name: string, args: Record<string, any>): Promise<any> {
    try {
      const response = await this.client.callTool({
        name,
        arguments: args
      });
      return response;
    } catch (error) {
      console.error(`Failed to call tool ${name}:`, error);
      throw error;
    }
  }

  private async handleDisconnect(): Promise<void> {
    console.log('Disconnected from gateway');

    if (this.reconnectAttempts < this.maxReconnectAttempts) {
      this.reconnectAttempts++;
      const delay = Math.min(1000 * Math.pow(2, this.reconnectAttempts), 30000);

      console.log(`Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts}/${this.maxReconnectAttempts})`);

      setTimeout(() => {
        this.connect();
      }, delay);
    } else {
      console.error('Max reconnection attempts reached');
    }
  }

  private handleError(error: Error): void {
    console.error('WebSocket error:', error);
  }

  async disconnect(): Promise<void> {
    if (this.client) {
      await this.client.close();
    }
  }
}

// Usage
const client = new MCPClient(
  'wss://gateway.example.com:8443',
  'your-auth-token'
);

await client.connect();

// List available tools
const tools = await client.listTools();
console.log('Available tools:', tools);

// Call a tool
const result = await client.callTool('calculate', {
  expression: '2 + 2'
});
console.log('Result:', result);

// Cleanup
await client.disconnect();
```

### React Hook Example

Create `useMCPClient.ts`:

```typescript
import { useState, useEffect, useCallback } from 'react';
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { WebSocketClientTransport } from '@modelcontextprotocol/sdk/client/websocket.js';

interface UseMCPClientOptions {
  url: string;
  authToken: string;
  autoConnect?: boolean;
}

export function useMCPClient({ url, authToken, autoConnect = true }: UseMCPClientOptions) {
  const [client, setClient] = useState<Client | null>(null);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [tools, setTools] = useState<any[]>([]);

  const connect = useCallback(async () => {
    try {
      const transport = new WebSocketClientTransport(url, {
        headers: { 'Authorization': `Bearer ${authToken}` }
      });

      const newClient = new Client({
        name: 'react-mcp-client',
        version: '1.0.0'
      }, {
        capabilities: { tools: {} }
      });

      await newClient.connect(transport);

      setClient(newClient);
      setConnected(true);
      setError(null);

      // Load tools
      const response = await newClient.listTools();
      setTools(response.tools);

    } catch (err) {
      setError(err as Error);
      setConnected(false);
    }
  }, [url, authToken]);

  const callTool = useCallback(async (name: string, args: Record<string, any>) => {
    if (!client) throw new Error('Not connected');
    return await client.callTool({ name, arguments: args });
  }, [client]);

  const disconnect = useCallback(async () => {
    if (client) {
      await client.close();
      setClient(null);
      setConnected(false);
    }
  }, [client]);

  useEffect(() => {
    if (autoConnect) {
      connect();
    }
    return () => {
      disconnect();
    };
  }, [autoConnect, connect, disconnect]);

  return {
    client,
    connected,
    error,
    tools,
    connect,
    disconnect,
    callTool
  };
}

// Usage in component
function MyComponent() {
  const { connected, tools, callTool, error } = useMCPClient({
    url: 'wss://gateway.example.com:8443',
    authToken: process.env.MCP_TOKEN!
  });

  const handleCalculate = async () => {
    const result = await callTool('calculate', { expression: '2 + 2' });
    console.log(result);
  };

  if (error) return <div>Error: {error.message}</div>;
  if (!connected) return <div>Connecting...</div>;

  return (
    <div>
      <h2>Available Tools</h2>
      <ul>
        {tools.map(tool => (
          <li key={tool.name}>{tool.name}: {tool.description}</li>
        ))}
      </ul>
      <button onClick={handleCalculate}>Calculate</button>
    </div>
  );
}
```

## Client 2: Python WebSocket Client

### Installation

```bash
pip install websockets pyjwt
```

### Basic Python Client

Create `mcp_client.py`:

```python
#!/usr/bin/env python3
import asyncio
import websockets
import json
import logging
from typing import Dict, Any, Optional, List
from dataclasses import dataclass

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

@dataclass
class MCPClientConfig:
    url: str
    auth_token: str
    max_reconnect_attempts: int = 5
    reconnect_delay: float = 1.0

class MCPClient:
    def __init__(self, config: MCPClientConfig):
        self.config = config
        self.websocket: Optional[websockets.WebSocketClientProtocol] = None
        self.request_id = 0
        self.reconnect_attempts = 0
        self.running = False

    async def connect(self):
        """Connect to MCP Gateway"""
        try:
            headers = {
                'Authorization': f'Bearer {self.config.auth_token}'
            }

            self.websocket = await websockets.connect(
                self.config.url,
                extra_headers=headers,
                ping_interval=30,
                ping_timeout=10
            )

            # Initialize connection
            await self._send_request('initialize', {
                'protocolVersion': '1.0',
                'capabilities': {'tools': {}},
                'clientInfo': {
                    'name': 'python-mcp-client',
                    'version': '1.0.0'
                }
            })

            self.reconnect_attempts = 0
            self.running = True
            logger.info('Connected to MCP Gateway')

        except Exception as e:
            logger.error(f'Connection failed: {e}')
            await self._handle_reconnect()

    async def _send_request(self, method: str, params: Dict[str, Any] = None) -> Dict[str, Any]:
        """Send JSON-RPC request"""
        self.request_id += 1
        request = {
            'jsonrpc': '2.0',
            'method': method,
            'params': params or {},
            'id': self.request_id
        }

        await self.websocket.send(json.dumps(request))
        response = await self.websocket.recv()
        return json.loads(response)

    async def list_tools(self) -> List[Dict[str, Any]]:
        """List available tools"""
        response = await self._send_request('tools/list')
        return response.get('result', {}).get('tools', [])

    async def call_tool(self, name: str, arguments: Dict[str, Any]) -> Any:
        """Call a tool"""
        response = await self._send_request('tools/call', {
            'name': name,
            'arguments': arguments
        })

        if 'error' in response:
            raise Exception(f"Tool call failed: {response['error']}")

        return response.get('result')

    async def _handle_reconnect(self):
        """Handle reconnection with exponential backoff"""
        if self.reconnect_attempts >= self.config.max_reconnect_attempts:
            logger.error('Max reconnection attempts reached')
            return

        self.reconnect_attempts += 1
        delay = min(
            self.config.reconnect_delay * (2 ** self.reconnect_attempts),
            30.0
        )

        logger.info(f'Reconnecting in {delay}s (attempt {self.reconnect_attempts})')
        await asyncio.sleep(delay)
        await self.connect()

    async def disconnect(self):
        """Disconnect from gateway"""
        self.running = False
        if self.websocket:
            await self.websocket.close()
            logger.info('Disconnected from gateway')

    async def run_forever(self):
        """Keep connection alive and handle incoming messages"""
        while self.running:
            try:
                message = await self.websocket.recv()
                data = json.loads(message)
                logger.info(f'Received: {data}')
            except websockets.exceptions.ConnectionClosed:
                logger.warning('Connection closed')
                await self._handle_reconnect()
            except Exception as e:
                logger.error(f'Error: {e}')

# Usage
async def main():
    config = MCPClientConfig(
        url='wss://gateway.example.com:8443',
        auth_token='your-auth-token'
    )

    client = MCPClient(config)
    await client.connect()

    try:
        # List tools
        tools = await client.list_tools()
        print(f'Available tools: {tools}')

        # Call a tool
        result = await client.call_tool('calculate', {
            'expression': '2 + 2'
        })
        print(f'Result: {result}')

    finally:
        await client.disconnect()

if __name__ == '__main__':
    asyncio.run(main())
```

## Client 3: Python HTTP Client

Create `mcp_http_client.py`:

```python
#!/usr/bin/env python3
import requests
from typing import Dict, Any, Optional
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

class MCPHTTPClient:
    def __init__(self, base_url: str, auth_token: str, timeout: int = 30):
        self.base_url = base_url.rstrip('/')
        self.auth_token = auth_token
        self.timeout = timeout
        self.session = requests.Session()
        self.session.headers.update({
            'Authorization': f'Bearer {auth_token}',
            'Content-Type': 'application/json'
        })
        self.request_id = 0

    def _send_request(self, method: str, params: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
        """Send JSON-RPC request via HTTP"""
        self.request_id += 1
        payload = {
            'jsonrpc': '2.0',
            'method': method,
            'params': params or {},
            'id': self.request_id
        }

        try:
            response = self.session.post(
                f'{self.base_url}/api/v1/mcp',
                json=payload,
                timeout=self.timeout
            )
            response.raise_for_status()
            return response.json()
        except requests.exceptions.RequestException as e:
            logger.error(f'Request failed: {e}')
            raise

    def list_tools(self) -> list:
        """List available tools"""
        response = self._send_request('tools/list')
        return response.get('result', {}).get('tools', [])

    def call_tool(self, name: str, arguments: Dict[str, Any]) -> Any:
        """Call a tool"""
        response = self._send_request('tools/call', {
            'name': name,
            'arguments': arguments
        })

        if 'error' in response:
            raise Exception(f"Tool call failed: {response['error']}")

        return response.get('result')

    def close(self):
        """Close session"""
        self.session.close()

# Usage
client = MCPHTTPClient(
    base_url='https://gateway.example.com:8080',
    auth_token='your-auth-token'
)

try:
    # List tools
    tools = client.list_tools()
    print(f'Available tools: {tools}')

    # Call tool
    result = client.call_tool('calculate', {'expression': '2 + 2'})
    print(f'Result: {result}')
finally:
    client.close()
```

## Client 4: Go TCP Binary Client

Create `client.go`:

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "net"
    "time"
)

type MCPClient struct {
    conn      net.Conn
    requestID int
}

type JSONRPCRequest struct {
    JSONRPC string                 `json:"jsonrpc"`
    Method  string                 `json:"method"`
    Params  map[string]interface{} `json:"params,omitempty"`
    ID      int                    `json:"id"`
}

type JSONRPCResponse struct {
    JSONRPC string                 `json:"jsonrpc"`
    Result  map[string]interface{} `json:"result,omitempty"`
    Error   *JSONRPCError          `json:"error,omitempty"`
    ID      int                    `json:"id"`
}

type JSONRPCError struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
}

func NewMCPClient(address string) (*MCPClient, error) {
    conn, err := net.DialTimeout("tcp", address, 10*time.Second)
    if err != nil {
        return nil, fmt.Errorf("connection failed: %w", err)
    }

    return &MCPClient{
        conn:      conn,
        requestID: 0,
    }, nil
}

func (c *MCPClient) sendRequest(method string, params map[string]interface{}) (*JSONRPCResponse, error) {
    c.requestID++

    request := JSONRPCRequest{
        JSONRPC: "2.0",
        Method:  method,
        Params:  params,
        ID:      c.requestID,
    }

    // Encode and send
    if err := json.NewEncoder(c.conn).Encode(request); err != nil {
        return nil, fmt.Errorf("send failed: %w", err)
    }

    // Read response
    var response JSONRPCResponse
    if err := json.NewDecoder(c.conn).Decode(&response); err != nil {
        return nil, fmt.Errorf("receive failed: %w", err)
    }

    if response.Error != nil {
        return nil, fmt.Errorf("RPC error %d: %s", response.Error.Code, response.Error.Message)
    }

    return &response, nil
}

func (c *MCPClient) ListTools(ctx context.Context) ([]interface{}, error) {
    response, err := c.sendRequest("tools/list", nil)
    if err != nil {
        return nil, err
    }

    tools, ok := response.Result["tools"].([]interface{})
    if !ok {
        return nil, fmt.Errorf("invalid tools response")
    }

    return tools, nil
}

func (c *MCPClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
    params := map[string]interface{}{
        "name":      name,
        "arguments": args,
    }

    response, err := c.sendRequest("tools/call", params)
    if err != nil {
        return nil, err
    }

    return response.Result, nil
}

func (c *MCPClient) Close() error {
    return c.conn.Close()
}

func main() {
    client, err := NewMCPClient("gateway.example.com:8444")
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    ctx := context.Background()

    // List tools
    tools, err := client.ListTools(ctx)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Available tools: %+v\n", tools)

    // Call tool
    result, err := client.CallTool(ctx, "calculate", map[string]interface{}{
        "expression": "2 + 2",
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Result: %+v\n", result)
}
```

## Client 5: CLI with Curl

### Simple Request

```bash
# Set variables
GATEWAY_URL="https://gateway.example.com:8080/api/v1/mcp"
AUTH_TOKEN="your-auth-token"

# List tools
curl -X POST "$GATEWAY_URL" \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "tools/list",
    "id": 1
  }' | jq .

# Call tool
curl -X POST "$GATEWAY_URL" \
  -H "Authorization: Bearer $AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "tools/call",
    "params": {
      "name": "calculate",
      "arguments": {"expression": "2 + 2"}
    },
    "id": 2
  }' | jq .
```

### Bash Script Wrapper

Create `mcp-cli.sh`:

```bash
#!/bin/bash

GATEWAY_URL="${MCP_GATEWAY_URL:-https://gateway.example.com:8080/api/v1/mcp}"
AUTH_TOKEN="${MCP_AUTH_TOKEN}"
REQUEST_ID=1

if [ -z "$AUTH_TOKEN" ]; then
    echo "Error: MCP_AUTH_TOKEN not set"
    exit 1
fi

mcp_request() {
    local method=$1
    local params=${2:-"{}"}

    curl -s -X POST "$GATEWAY_URL" \
        -H "Authorization: Bearer $AUTH_TOKEN" \
        -H "Content-Type: application/json" \
        -d "{
            \"jsonrpc\": \"2.0\",
            \"method\": \"$method\",
            \"params\": $params,
            \"id\": $REQUEST_ID
        }"

    REQUEST_ID=$((REQUEST_ID + 1))
}

# Commands
list_tools() {
    mcp_request "tools/list" | jq '.result.tools'
}

call_tool() {
    local name=$1
    local args=$2
    mcp_request "tools/call" "{\"name\":\"$name\",\"arguments\":$args}" | jq '.result'
}

# Usage
case "${1:-}" in
    list)
        list_tools
        ;;
    call)
        call_tool "$2" "${3:-{}}"
        ;;
    *)
        echo "Usage: $0 {list|call <tool-name> <args-json>}"
        exit 1
        ;;
esac
```

Usage:

```bash
chmod +x mcp-cli.sh

# List tools
./mcp-cli.sh list

# Call tool
./mcp-cli.sh call calculate '{"expression":"2+2"}'
```

## Best Practices

### 1. Connection Management

```typescript
class ConnectionManager {
  private reconnectDelay = 1000;
  private maxDelay = 30000;

  async connectWithRetry(): Promise<void> {
    while (true) {
      try {
        await this.connect();
        return;
      } catch (error) {
        await this.sleep(this.reconnectDelay);
        this.reconnectDelay = Math.min(this.reconnectDelay * 2, this.maxDelay);
      }
    }
  }

  private sleep(ms: number): Promise<void> {
    return new Promise(resolve => setTimeout(resolve, ms));
  }
}
```

### 2. Error Handling

```python
class MCPError(Exception):
    def __init__(self, code: int, message: str):
        self.code = code
        self.message = message
        super().__init__(f"MCP Error {code}: {message}")

def handle_response(response: dict):
    if 'error' in response:
        error = response['error']
        raise MCPError(error['code'], error['message'])
    return response['result']
```

### 3. Request Timeout

```typescript
async function callToolWithTimeout(
  client: MCPClient,
  name: string,
  args: any,
  timeoutMs: number = 30000
): Promise<any> {
  return Promise.race([
    client.callTool(name, args),
    new Promise((_, reject) =>
      setTimeout(() => reject(new Error('Timeout')), timeoutMs)
    )
  ]);
}
```

### 4. Token Refresh

```typescript
class AuthManager {
  private token: string;
  private expiresAt: number;

  async getToken(): Promise<string> {
    if (Date.now() >= this.expiresAt - 60000) {
      await this.refreshToken();
    }
    return this.token;
  }

  private async refreshToken(): Promise<void> {
    // Implement token refresh logic
  }
}
```

## Testing Your Client

### Unit Tests (Jest)

```typescript
import { MCPClient } from './client';

describe('MCPClient', () => {
  let client: MCPClient;

  beforeEach(() => {
    client = new MCPClient('wss://localhost:8443', 'test-token');
  });

  afterEach(async () => {
    await client.disconnect();
  });

  it('should connect successfully', async () => {
    await expect(client.connect()).resolves.not.toThrow();
  });

  it('should list tools', async () => {
    await client.connect();
    const tools = await client.listTools();
    expect(Array.isArray(tools)).toBe(true);
  });

  it('should handle connection errors', async () => {
    const badClient = new MCPClient('wss://invalid:9999', 'token');
    await expect(badClient.connect()).rejects.toThrow();
  });
});
```

## Troubleshooting

### Connection Refused

```bash
# Check if gateway is running
curl http://localhost:9090/healthz

# Test WebSocket connection
wscat -c ws://localhost:8443
```

### Authentication Failures

```bash
# Verify token format
echo $MCP_AUTH_TOKEN | base64 -d

# Test with curl
curl -v -H "Authorization: Bearer $MCP_AUTH_TOKEN" \
  https://gateway.example.com:8080/api/v1/mcp
```

### WebSocket Errors

Check browser console or enable debug logging:

```typescript
// Enable debug logging
const client = new MCPClient(url, token, { debug: true });
```

## Next Steps

- [Protocol Selection Guide](09-protocols.md) - Choose the right protocol
- [Authentication & Security](08-authentication.md) - Secure your client
- [Performance Tuning](12-performance-tuning.md) - Optimize client performance

## Summary

You've successfully:
- ✅ Created clients in JavaScript, Python, and Go
- ✅ Implemented WebSocket, HTTP, and TCP Binary protocols
- ✅ Added connection management and error handling
- ✅ Built CLI tools with curl and bash
- ✅ Learned best practices for production clients

Your applications can now integrate with MCP Bridge Gateway!
