# MCP Binary Protocol Client Implementation Guide

This guide provides example implementations for MCP binary protocol clients in various languages.

## Python Client Example

```python
import socket
import struct
import json
import threading
from typing import Dict, Any, Optional

class MCPBinaryClient:
    MAGIC_BYTES = 0x4D435042  # "MCPB"
    VERSION = 0x0001
    
    # Message types
    REQUEST = 0x0001
    RESPONSE = 0x0002
    CONTROL = 0x0003
    HEALTH_CHECK = 0x0004
    ERROR = 0x0005
    
    def __init__(self, host: str, port: int):
        self.host = host
        self.port = port
        self.socket = None
        self.authenticated = False
        self.session_id = None
        self.request_id = 0
        self.pending_requests = {}
        self.reader_thread = None
        self.running = False
        
    def connect(self, auth_token: str) -> bool:
        """Connect and authenticate with the server."""
        try:
            self.socket = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            self.socket.connect((self.host, self.port))
            
            # Start reader thread
            self.running = True
            self.reader_thread = threading.Thread(target=self._read_loop)
            self.reader_thread.start()
            
            # Send initialization request
            init_request = {
                "jsonrpc": "2.0",
                "id": "init",
                "method": "initialize",
                "params": {"token": auth_token}
            }
            
            response = self._send_request_sync(init_request)
            if response and "result" in response:
                self.session_id = response["result"]["sessionId"]
                self.authenticated = True
                return True
                
        except Exception as e:
            print(f"Connection failed: {e}")
            
        return False
        
    def call_tool(self, tool_name: str, arguments: Dict[str, Any]) -> Optional[Dict[str, Any]]:
        """Call an MCP tool."""
        if not self.authenticated:
            raise Exception("Not authenticated")
            
        request = {
            "jsonrpc": "2.0",
            "id": f"req-{self.request_id}",
            "method": "tools/call",
            "params": {
                "name": tool_name,
                "arguments": arguments
            }
        }
        self.request_id += 1
        
        return self._send_request_sync(request)
        
    def _send_frame(self, msg_type: int, payload: bytes):
        """Send a frame over the socket."""
        header = struct.pack(">IHHJ", 
                           self.MAGIC_BYTES,
                           self.VERSION,
                           msg_type,
                           len(payload))
        self.socket.sendall(header + payload)
        
    def _read_frame(self) -> tuple[int, bytes]:
        """Read a frame from the socket."""
        # Read header
        header = self._recv_exact(12)
        magic, version, msg_type, payload_len = struct.unpack(">IHHJ", header)
        
        if magic != self.MAGIC_BYTES:
            raise ValueError("Invalid magic bytes")
            
        # Read payload
        payload = self._recv_exact(payload_len) if payload_len > 0 else b""
        
        return msg_type, payload
        
    def _recv_exact(self, n: int) -> bytes:
        """Receive exactly n bytes from socket."""
        data = b""
        while len(data) < n:
            chunk = self.socket.recv(n - len(data))
            if not chunk:
                raise ConnectionError("Socket closed")
            data += chunk
        return data
        
    def _send_request_sync(self, request: Dict[str, Any]) -> Optional[Dict[str, Any]]:
        """Send request and wait for response."""
        req_id = request["id"]
        event = threading.Event()
        self.pending_requests[req_id] = {"event": event, "response": None}
        
        # Send request
        payload = json.dumps(request).encode("utf-8")
        self._send_frame(self.REQUEST, payload)
        
        # Wait for response
        event.wait(timeout=30.0)
        
        result = self.pending_requests[req_id]["response"]
        del self.pending_requests[req_id]
        
        return result
        
    def _read_loop(self):
        """Background thread for reading frames."""
        while self.running:
            try:
                msg_type, payload = self._read_frame()
                
                if msg_type == self.RESPONSE:
                    response = json.loads(payload.decode("utf-8"))
                    req_id = response.get("id")
                    if req_id in self.pending_requests:
                        self.pending_requests[req_id]["response"] = response
                        self.pending_requests[req_id]["event"].set()
                        
                elif msg_type == self.HEALTH_CHECK:
                    # Respond to health check
                    self._send_frame(self.HEALTH_CHECK, b"")
                    
                elif msg_type == self.ERROR:
                    error_msg = payload.decode("utf-8")
                    print(f"Protocol error: {error_msg}")
                    
                elif msg_type == self.CONTROL:
                    control = json.loads(payload.decode("utf-8"))
                    if control.get("command") == "shutdown":
                        # Acknowledge shutdown
                        ack = {"command": "shutdown_ack", "status": "ok"}
                        self._send_frame(self.CONTROL, json.dumps(ack).encode("utf-8"))
                        self.running = False
                        
            except Exception as e:
                print(f"Read error: {e}")
                self.running = False
                
    def close(self):
        """Close the connection."""
        self.running = False
        if self.socket:
            self.socket.close()
        if self.reader_thread:
            self.reader_thread.join()

# Example usage
if __name__ == "__main__":
    client = MCPBinaryClient("localhost", 9223)
    
    if client.connect("your-auth-token"):
        print("Connected and authenticated!")
        
        # Call a tool
        result = client.call_tool("weather/get", {"location": "London"})
        print(f"Result: {result}")
        
        client.close()
```

## Node.js/TypeScript Client Example

```typescript
import net from 'net';
import { EventEmitter } from 'events';

interface MCPRequest {
  jsonrpc: '2.0';
  id: string;
  method: string;
  params?: any;
}

interface MCPResponse {
  jsonrpc: '2.0';
  id: string;
  result?: any;
  error?: {
    code: number;
    message: string;
  };
}

class MCPBinaryClient extends EventEmitter {
  private static readonly MAGIC_BYTES = 0x4D435042;
  private static readonly VERSION = 0x0001;
  
  private static readonly MSG_REQUEST = 0x0001;
  private static readonly MSG_RESPONSE = 0x0002;
  private static readonly MSG_CONTROL = 0x0003;
  private static readonly MSG_HEALTH_CHECK = 0x0004;
  private static readonly MSG_ERROR = 0x0005;
  
  private socket: net.Socket | null = null;
  private authenticated = false;
  private sessionId: string | null = null;
  private requestId = 0;
  private pendingRequests = new Map<string, {
    resolve: (value: any) => void;
    reject: (error: any) => void;
  }>();
  
  constructor(private host: string, private port: number) {
    super();
  }
  
  async connect(authToken: string): Promise<boolean> {
    return new Promise((resolve, reject) => {
      this.socket = new net.Socket();
      
      this.socket.connect(this.port, this.host, () => {
        this.setupSocketHandlers();
        
        // Send initialization
        this.sendRequest({
          jsonrpc: '2.0',
          id: 'init',
          method: 'initialize',
          params: { token: authToken }
        }).then(response => {
          if (response.result) {
            this.sessionId = response.result.sessionId;
            this.authenticated = true;
            resolve(true);
          } else {
            reject(new Error('Authentication failed'));
          }
        }).catch(reject);
      });
      
      this.socket.on('error', reject);
    });
  }
  
  async callTool(toolName: string, args: any): Promise<any> {
    if (!this.authenticated) {
      throw new Error('Not authenticated');
    }
    
    const response = await this.sendRequest({
      jsonrpc: '2.0',
      id: `req-${this.requestId++}`,
      method: 'tools/call',
      params: {
        name: toolName,
        arguments: args
      }
    });
    
    if (response.error) {
      throw new Error(response.error.message);
    }
    
    return response.result;
  }
  
  private sendRequest(request: MCPRequest): Promise<MCPResponse> {
    return new Promise((resolve, reject) => {
      this.pendingRequests.set(request.id, { resolve, reject });
      
      const payload = Buffer.from(JSON.stringify(request), 'utf-8');
      this.sendFrame(MCPBinaryClient.MSG_REQUEST, payload);
      
      // Timeout after 30 seconds
      setTimeout(() => {
        if (this.pendingRequests.has(request.id)) {
          this.pendingRequests.delete(request.id);
          reject(new Error('Request timeout'));
        }
      }, 30000);
    });
  }
  
  private sendFrame(msgType: number, payload: Buffer) {
    const header = Buffer.allocUnsafe(12);
    header.writeUInt32BE(MCPBinaryClient.MAGIC_BYTES, 0);
    header.writeUInt16BE(MCPBinaryClient.VERSION, 4);
    header.writeUInt16BE(msgType, 6);
    header.writeUInt32BE(payload.length, 8);
    
    this.socket!.write(Buffer.concat([header, payload]));
  }
  
  private setupSocketHandlers() {
    let buffer = Buffer.alloc(0);
    
    this.socket!.on('data', (data) => {
      buffer = Buffer.concat([buffer, data]);
      
      while (buffer.length >= 12) {
        // Parse header
        const magic = buffer.readUInt32BE(0);
        if (magic !== MCPBinaryClient.MAGIC_BYTES) {
          this.emit('error', new Error('Invalid magic bytes'));
          this.close();
          return;
        }
        
        const version = buffer.readUInt16BE(4);
        const msgType = buffer.readUInt16BE(6);
        const payloadLen = buffer.readUInt32BE(8);
        
        if (buffer.length < 12 + payloadLen) {
          break; // Wait for more data
        }
        
        // Extract frame
        const payload = buffer.slice(12, 12 + payloadLen);
        buffer = buffer.slice(12 + payloadLen);
        
        this.handleFrame(msgType, payload);
      }
    });
    
    this.socket!.on('close', () => {
      this.emit('close');
      this.authenticated = false;
    });
    
    this.socket!.on('error', (err) => {
      this.emit('error', err);
    });
  }
  
  private handleFrame(msgType: number, payload: Buffer) {
    switch (msgType) {
      case MCPBinaryClient.MSG_RESPONSE:
        const response = JSON.parse(payload.toString('utf-8')) as MCPResponse;
        const pending = this.pendingRequests.get(response.id);
        if (pending) {
          this.pendingRequests.delete(response.id);
          pending.resolve(response);
        }
        break;
        
      case MCPBinaryClient.MSG_HEALTH_CHECK:
        // Respond to health check
        this.sendFrame(MCPBinaryClient.MSG_HEALTH_CHECK, Buffer.alloc(0));
        break;
        
      case MCPBinaryClient.MSG_ERROR:
        const error = payload.toString('utf-8');
        this.emit('error', new Error(`Protocol error: ${error}`));
        break;
        
      case MCPBinaryClient.MSG_CONTROL:
        const control = JSON.parse(payload.toString('utf-8'));
        if (control.command === 'shutdown') {
          const ack = { command: 'shutdown_ack', status: 'ok' };
          this.sendFrame(MCPBinaryClient.MSG_CONTROL, 
                        Buffer.from(JSON.stringify(ack), 'utf-8'));
          this.close();
        }
        break;
    }
  }
  
  close() {
    if (this.socket) {
      this.socket.destroy();
      this.socket = null;
    }
    this.authenticated = false;
  }
}

// Example usage
async function example() {
  const client = new MCPBinaryClient('localhost', 9223);
  
  try {
    await client.connect('your-auth-token');
    console.log('Connected and authenticated!');
    
    const result = await client.callTool('weather/get', { location: 'London' });
    console.log('Result:', result);
    
    client.close();
  } catch (error) {
    console.error('Error:', error);
  }
}
```

## Error Handling Best Practices

1. **Connection Errors**: Always handle socket connection failures gracefully
2. **Timeouts**: Implement request timeouts to avoid hanging
3. **Protocol Errors**: Validate magic bytes and handle invalid frames
4. **Reconnection**: Consider implementing automatic reconnection logic
5. **Health Checks**: Respond promptly to health checks to maintain connection

## Performance Tips

1. **Connection Pooling**: Reuse connections for multiple requests
2. **Async Operations**: Use async/await or promises for non-blocking I/O
3. **Buffering**: Implement proper buffering for partial frame reads
4. **Pipelining**: Send multiple requests without waiting for responses
5. **Binary Efficiency**: The binary protocol is more efficient than WebSocket for high-volume scenarios

## Testing Your Client

```python
# Simple test script
def test_client():
    client = MCPBinaryClient("localhost", 9223)
    
    # Test connection
    assert client.connect("test-token"), "Failed to connect"
    
    # Test tool call
    result = client.call_tool("echo", {"message": "Hello, MCP!"})
    assert result is not None, "No response received"
    
    # Test health check handling
    time.sleep(65)  # Wait for server health check
    
    # Test graceful shutdown
    client.close()
    
    print("All tests passed!")
```

## Debugging Tips

1. Use packet capture tools (Wireshark, tcpdump) to inspect frames
2. Log all sent and received frames during development
3. Implement frame validation before processing
4. Use structured logging for better debugging
5. Test edge cases: large payloads, rapid requests, connection drops