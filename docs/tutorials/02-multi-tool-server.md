# Tutorial: Building a Multi-Tool MCP Server

Create an MCP server with multiple tools that handle different input types and return structured data.

## Prerequisites

- Completed [Your First MCP Server](01-first-mcp-server.md) tutorial
- Basic knowledge of Python or Node.js
- MCP Bridge Gateway installed
- 20-25 minutes

## What You'll Build

A productivity MCP server with 4 tools:
- `calculate` - Perform mathematical calculations
- `get_time` - Get current time in different timezones
- `convert_units` - Convert between different units (temperature, distance, weight)
- `generate_password` - Generate secure random passwords

Each tool demonstrates different patterns:
- Simple numeric inputs
- String/enum parameters
- Optional parameters with defaults
- Structured output formats

## Step 1: Create the Multi-Tool Server

Create `productivity_server.py`:

```python
#!/usr/bin/env python3
import json
import sys
import math
import secrets
import string
from datetime import datetime
from zoneinfo import ZoneInfo

# Tool implementations

def calculate(expression: str) -> dict:
    """Safely evaluate mathematical expressions"""
    try:
        # Safe evaluation - only allow math operations
        allowed_names = {
            k: v for k, v in math.__dict__.items()
            if not k.startswith("__")
        }
        result = eval(expression, {"__builtins__": {}}, allowed_names)
        return {
            "expression": expression,
            "result": result,
            "type": type(result).__name__
        }
    except Exception as e:
        return {
            "expression": expression,
            "error": str(e),
            "result": None
        }

def get_time(timezone: str = "UTC") -> dict:
    """Get current time in specified timezone"""
    try:
        tz = ZoneInfo(timezone)
        now = datetime.now(tz)
        return {
            "timezone": timezone,
            "time": now.strftime("%H:%M:%S"),
            "date": now.strftime("%Y-%m-%d"),
            "iso8601": now.isoformat(),
            "unix_timestamp": int(now.timestamp())
        }
    except Exception as e:
        return {
            "timezone": timezone,
            "error": str(e),
            "time": None
        }

def convert_units(value: float, from_unit: str, to_unit: str, unit_type: str) -> dict:
    """Convert between different units"""
    conversions = {
        "temperature": {
            ("celsius", "fahrenheit"): lambda x: (x * 9/5) + 32,
            ("fahrenheit", "celsius"): lambda x: (x - 32) * 5/9,
            ("celsius", "kelvin"): lambda x: x + 273.15,
            ("kelvin", "celsius"): lambda x: x - 273.15,
        },
        "distance": {
            ("meters", "feet"): lambda x: x * 3.28084,
            ("feet", "meters"): lambda x: x / 3.28084,
            ("kilometers", "miles"): lambda x: x * 0.621371,
            ("miles", "kilometers"): lambda x: x / 0.621371,
        },
        "weight": {
            ("kilograms", "pounds"): lambda x: x * 2.20462,
            ("pounds", "kilograms"): lambda x: x / 2.20462,
            ("grams", "ounces"): lambda x: x * 0.035274,
            ("ounces", "grams"): lambda x: x / 0.035274,
        }
    }

    try:
        converter = conversions[unit_type].get((from_unit, to_unit))
        if converter:
            result = converter(value)
            return {
                "original": {"value": value, "unit": from_unit},
                "converted": {"value": result, "unit": to_unit},
                "unit_type": unit_type
            }
        else:
            return {
                "error": f"Conversion from {from_unit} to {to_unit} not supported",
                "result": None
            }
    except Exception as e:
        return {
            "error": str(e),
            "result": None
        }

def generate_password(length: int = 16, include_symbols: bool = True) -> dict:
    """Generate a secure random password"""
    try:
        characters = string.ascii_letters + string.digits
        if include_symbols:
            characters += string.punctuation

        password = ''.join(secrets.choice(characters) for _ in range(length))

        return {
            "password": password,
            "length": length,
            "includes_symbols": include_symbols,
            "strength": "strong" if length >= 16 else "medium" if length >= 12 else "weak"
        }
    except Exception as e:
        return {
            "error": str(e),
            "password": None
        }

# MCP Protocol handling

def handle_request(request):
    """Handle MCP protocol requests"""
    method = request.get('method')
    params = request.get('params', {})
    request_id = request.get('id')

    # Initialize
    if method == 'initialize':
        return {
            "jsonrpc": "2.0",
            "result": {
                "protocolVersion": "1.0",
                "capabilities": {"tools": {}},
                "serverInfo": {
                    "name": "productivity-server",
                    "version": "1.0.0"
                }
            },
            "id": request_id
        }

    # List tools
    elif method == 'tools/list':
        return {
            "jsonrpc": "2.0",
            "result": {
                "tools": [
                    {
                        "name": "calculate",
                        "description": "Evaluate mathematical expressions",
                        "inputSchema": {
                            "type": "object",
                            "properties": {
                                "expression": {
                                    "type": "string",
                                    "description": "Mathematical expression to evaluate (e.g., '2 + 2', 'sqrt(16)', 'sin(pi/2)')"
                                }
                            },
                            "required": ["expression"]
                        }
                    },
                    {
                        "name": "get_time",
                        "description": "Get current time in specified timezone",
                        "inputSchema": {
                            "type": "object",
                            "properties": {
                                "timezone": {
                                    "type": "string",
                                    "description": "Timezone name (e.g., 'UTC', 'America/New_York', 'Europe/London')",
                                    "default": "UTC"
                                }
                            }
                        }
                    },
                    {
                        "name": "convert_units",
                        "description": "Convert between different units",
                        "inputSchema": {
                            "type": "object",
                            "properties": {
                                "value": {
                                    "type": "number",
                                    "description": "Value to convert"
                                },
                                "from_unit": {
                                    "type": "string",
                                    "description": "Source unit"
                                },
                                "to_unit": {
                                    "type": "string",
                                    "description": "Target unit"
                                },
                                "unit_type": {
                                    "type": "string",
                                    "enum": ["temperature", "distance", "weight"],
                                    "description": "Type of unit conversion"
                                }
                            },
                            "required": ["value", "from_unit", "to_unit", "unit_type"]
                        }
                    },
                    {
                        "name": "generate_password",
                        "description": "Generate a secure random password",
                        "inputSchema": {
                            "type": "object",
                            "properties": {
                                "length": {
                                    "type": "integer",
                                    "description": "Password length",
                                    "minimum": 8,
                                    "maximum": 128,
                                    "default": 16
                                },
                                "include_symbols": {
                                    "type": "boolean",
                                    "description": "Include special characters",
                                    "default": true
                                }
                            }
                        }
                    }
                ]
            },
            "id": request_id
        }

    # Call tool
    elif method == 'tools/call':
        tool_name = params.get('name')
        arguments = params.get('arguments', {})

        # Route to appropriate tool
        if tool_name == 'calculate':
            result = calculate(arguments.get('expression'))
        elif tool_name == 'get_time':
            result = get_time(arguments.get('timezone', 'UTC'))
        elif tool_name == 'convert_units':
            result = convert_units(
                arguments.get('value'),
                arguments.get('from_unit'),
                arguments.get('to_unit'),
                arguments.get('unit_type')
            )
        elif tool_name == 'generate_password':
            result = generate_password(
                arguments.get('length', 16),
                arguments.get('include_symbols', True)
            )
        else:
            return {
                "jsonrpc": "2.0",
                "error": {
                    "code": -32602,
                    "message": f"Unknown tool: {tool_name}"
                },
                "id": request_id
            }

        return {
            "jsonrpc": "2.0",
            "result": {
                "content": [{
                    "type": "text",
                    "text": json.dumps(result, indent=2)
                }]
            },
            "id": request_id
        }

    # Unknown method
    return {
        "jsonrpc": "2.0",
        "error": {
            "code": -32601,
            "message": f"Method not found: {method}"
        },
        "id": request_id
    }

def main():
    """Main server loop"""
    for line in sys.stdin:
        try:
            request = json.loads(line.strip())
            response = handle_request(request)
            print(json.dumps(response), flush=True)
        except Exception as e:
            error_response = {
                "jsonrpc": "2.0",
                "error": {
                    "code": -32603,
                    "message": f"Internal error: {str(e)}"
                },
                "id": None
            }
            print(json.dumps(error_response), flush=True)

if __name__ == '__main__':
    main()
```

Make it executable:

```bash
chmod +x productivity_server.py
```

## Step 2: Test Each Tool Locally

```bash
# Test calculate
echo '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"calculate","arguments":{"expression":"2 + 2 * 3"}},"id":1}' | python productivity_server.py

# Test get_time
echo '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"get_time","arguments":{"timezone":"America/New_York"}},"id":2}' | python productivity_server.py

# Test convert_units
echo '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"convert_units","arguments":{"value":100,"from_unit":"celsius","to_unit":"fahrenheit","unit_type":"temperature"}},"id":3}' | python productivity_server.py

# Test generate_password
echo '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"generate_password","arguments":{"length":20,"include_symbols":true}},"id":4}' | python productivity_server.py
```

## Step 3: Configure Gateway

Update `gateway.yaml`:

```yaml
version: 1

server:
  host: 0.0.0.0
  port: 8443
  protocol: websocket
  tls:
    enabled: false  # Development only

auth:
  type: bearer
  per_message_auth: false

discovery:
  provider: static
  static:
    endpoints:
      default:
        - url: ws://localhost:9000

rate_limit:
  enabled: true
  requests_per_sec: 100
  burst: 200

metrics:
  enabled: true
  endpoint: "0.0.0.0:9090"

logging:
  level: info
  format: json
```

## Step 4: Start Server and Gateway

```bash
# Terminal 1: Start productivity server
python productivity_server.py | websocat -s 0.0.0.0:9000

# Terminal 2: Start gateway
mcp-gateway --config gateway.yaml
```

## Step 5: Test via Gateway

```bash
# List all tools
wscat -c ws://localhost:8443
> {"jsonrpc":"2.0","method":"tools/list","id":1}

# Calculate
> {"jsonrpc":"2.0","method":"tools/call","params":{"name":"calculate","arguments":{"expression":"sqrt(144) + pi"}},"id":2}

# Get time
> {"jsonrpc":"2.0","method":"tools/call","params":{"name":"get_time","arguments":{"timezone":"Asia/Tokyo"}},"id":3}

# Convert temperature
> {"jsonrpc":"2.0","method":"tools/call","params":{"name":"convert_units","arguments":{"value":32,"from_unit":"fahrenheit","to_unit":"celsius","unit_type":"temperature"}},"id":4}

# Generate password
> {"jsonrpc":"2.0","method":"tools/call","params":{"name":"generate_password","arguments":{"length":24,"include_symbols":true}},"id":5}
```

## Step 6: Node.js Implementation (Alternative)

Create `productivity_server.js`:

```javascript
#!/usr/bin/env node
const readline = require('readline');
const crypto = require('crypto');

// Tool implementations
const tools = {
  calculate: ({ expression }) => {
    try {
      // Safe evaluation using Function
      const result = Function('"use strict"; return (' + expression + ')')();
      return {
        expression,
        result,
        type: typeof result
      };
    } catch (error) {
      return { expression, error: error.message, result: null };
    }
  },

  get_time: ({ timezone = 'UTC' }) => {
    try {
      const now = new Date();
      return {
        timezone,
        time: now.toLocaleTimeString('en-US', { timeZone: timezone }),
        date: now.toLocaleDateString('en-US', { timeZone: timezone }),
        iso8601: now.toISOString(),
        unix_timestamp: Math.floor(now.getTime() / 1000)
      };
    } catch (error) {
      return { timezone, error: error.message, time: null };
    }
  },

  convert_units: ({ value, from_unit, to_unit, unit_type }) => {
    const conversions = {
      temperature: {
        'celsius-fahrenheit': v => (v * 9/5) + 32,
        'fahrenheit-celsius': v => (v - 32) * 5/9,
        'celsius-kelvin': v => v + 273.15,
        'kelvin-celsius': v => v - 273.15,
      },
      distance: {
        'meters-feet': v => v * 3.28084,
        'feet-meters': v => v / 3.28084,
        'kilometers-miles': v => v * 0.621371,
        'miles-kilometers': v => v / 0.621371,
      }
    };

    const key = `${from_unit}-${to_unit}`;
    const converter = conversions[unit_type]?.[key];

    if (converter) {
      return {
        original: { value, unit: from_unit },
        converted: { value: converter(value), unit: to_unit },
        unit_type
      };
    }
    return { error: `Conversion not supported`, result: null };
  },

  generate_password: ({ length = 16, include_symbols = true }) => {
    const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
    const symbols = '!@#$%^&*()_+-=[]{}|;:,.<>?';
    const charset = include_symbols ? chars + symbols : chars;

    const password = Array.from(crypto.randomBytes(length))
      .map(byte => charset[byte % charset.length])
      .join('');

    return {
      password,
      length,
      includes_symbols: include_symbols,
      strength: length >= 16 ? 'strong' : length >= 12 ? 'medium' : 'weak'
    };
  }
};

// MCP Protocol handler
function handleRequest(request) {
  const { method, params = {}, id } = request;

  if (method === 'initialize') {
    return {
      jsonrpc: '2.0',
      result: {
        protocolVersion: '1.0',
        capabilities: { tools: {} },
        serverInfo: { name: 'productivity-server', version: '1.0.0' }
      },
      id
    };
  }

  if (method === 'tools/list') {
    return {
      jsonrpc: '2.0',
      result: {
        tools: [
          {
            name: 'calculate',
            description: 'Evaluate mathematical expressions',
            inputSchema: {
              type: 'object',
              properties: {
                expression: { type: 'string', description: 'Math expression' }
              },
              required: ['expression']
            }
          },
          // ... other tools
        ]
      },
      id
    };
  }

  if (method === 'tools/call') {
    const { name, arguments: args } = params;
    const tool = tools[name];

    if (!tool) {
      return {
        jsonrpc: '2.0',
        error: { code: -32602, message: `Unknown tool: ${name}` },
        id
      };
    }

    const result = tool(args);
    return {
      jsonrpc: '2.0',
      result: {
        content: [{ type: 'text', text: JSON.stringify(result, null, 2) }]
      },
      id
    };
  }

  return {
    jsonrpc: '2.0',
    error: { code: -32601, message: `Method not found: ${method}` },
    id
  };
}

// Main loop
const rl = readline.createInterface({ input: process.stdin });
rl.on('line', (line) => {
  try {
    const request = JSON.parse(line);
    const response = handleRequest(request);
    console.log(JSON.stringify(response));
  } catch (error) {
    console.log(JSON.stringify({
      jsonrpc: '2.0',
      error: { code: -32603, message: error.message },
      id: null
    }));
  }
});
```

## Best Practices

### 1. Input Validation

Always validate tool inputs:

```python
def validate_inputs(arguments, schema):
    """Validate arguments against schema"""
    required = schema.get('required', [])
    for field in required:
        if field not in arguments:
            raise ValueError(f"Missing required field: {field}")

    # Type validation
    for field, value in arguments.items():
        expected_type = schema['properties'][field]['type']
        # Validate type...
```

### 2. Error Handling

Return structured errors:

```python
try:
    result = perform_operation()
    return {"success": True, "data": result}
except Exception as e:
    return {"success": False, "error": str(e), "data": None}
```

### 3. Documentation

Document each tool clearly:

```python
{
    "name": "tool_name",
    "description": "Clear description of what it does",
    "inputSchema": {
        "type": "object",
        "properties": {
            "param": {
                "type": "string",
                "description": "What this parameter does",
                "examples": ["example1", "example2"]
            }
        }
    }
}
```

## Next Steps

- [Authentication & Security](08-authentication.md) - Secure your tools
- [Docker Compose Setup](05-docker-compose.md) - Deploy with Docker
- [Kubernetes Deployment](04-kubernetes-deployment.md) - Deploy to K8s

## Summary

This tutorial covered:
- Created a multi-tool MCP server
- Implemented 4 different tools
- Handled different input types
- Returned structured data
- Integrated with MCP Bridge Gateway


