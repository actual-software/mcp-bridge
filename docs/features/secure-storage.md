# Secure Token Storage

The MCP Router provides platform-native secure credential storage to protect authentication tokens and sensitive configuration data.

## Overview

Instead of storing tokens in plain text files or environment variables, the router uses:
- **macOS**: Keychain Services
- **Windows**: Credential Manager
- **Linux**: Secret Service (via D-Bus)
- **Fallback**: Encrypted file storage with AES-256-GCM

## Quick Start

### Storing a Token

```bash
# Option 1: Interactive setup (recommended)
mcp-router setup
# Follow prompts to securely store your token

# Option 2: Direct storage
mcp-router token set --name gateway-token --value "your-secret-token"

# Option 3: From environment variable
export MCP_AUTH_TOKEN="your-secret-token"
mcp-router token import --name gateway-token --env MCP_AUTH_TOKEN
```

### Using Stored Tokens

In your configuration file:
```yaml
gateway:
  auth:
    type: bearer
    token_secure_key: "gateway-token"  # References the stored token
```

## Platform-Specific Details

### macOS Keychain

Tokens are stored in the user's login keychain with the following attributes:
- **Service**: `com.poiley.mcp-router`
- **Account**: Your token name
- **Access Control**: Only accessible by mcp-router

View stored tokens:
```bash
# List tokens (requires authorization)
security find-generic-password -s com.poiley.mcp-router

# View specific token
mcp-router token get --name gateway-token
```

### Windows Credential Manager

Tokens are stored in Windows Credential Manager:
- **Target**: `MCP-Router:TokenName`
- **Type**: Generic Credential
- **Persistence**: Local Machine

View in Control Panel:
1. Open Control Panel → User Accounts → Credential Manager
2. Select "Windows Credentials"
3. Look for entries starting with "MCP-Router:"

### Linux Secret Service

Uses the FreeDesktop.org Secret Service API:
- **Collection**: Default (usually "Login")
- **Label**: `MCP Router: TokenName`
- **Attributes**: `{"application": "mcp-router", "token_name": "..."}`

Requirements:
- D-Bus session bus
- Secret service provider (gnome-keyring, KWallet, or similar)

Verify setup:
```bash
# Check D-Bus
echo $DBUS_SESSION_BUS_ADDRESS

# Check secret service
dbus-send --session --print-reply --dest=org.freedesktop.secrets \
  /org/freedesktop/secrets org.freedesktop.DBus.Introspectable.Introspect
```

### Encrypted File Fallback

When native storage is unavailable, tokens are stored in:
- **Location**: `~/.config/mcp-router/tokens.enc`
- **Encryption**: AES-256-GCM
- **Key Derivation**: PBKDF2 with 100,000 iterations
- **Master Key**: Derived from machine ID + user ID

## Configuration Options

### Basic Token Reference
```yaml
gateway:
  auth:
    type: bearer
    token_secure_key: "prod-token"
```

### OAuth2 with Secure Storage
```yaml
gateway:
  auth:
    type: oauth2
    client_id: "your-client-id"
    client_secret_secure_key: "oauth-secret"
    token_endpoint: "https://auth.example.com/token"
```

### Multiple Tokens
```yaml
gateway:
  auth:
    type: bearer
    token_secure_key: "primary-token"

backends:
  - name: "service-a"
    auth:
      token_secure_key: "service-a-token"
  - name: "service-b"
    auth:
      token_secure_key: "service-b-token"
```

## Token Management Commands

### List Tokens
```bash
mcp-router token list
```

Output:
```
NAME            CREATED              LAST_USED
gateway-token   2024-01-15 10:30:00  2024-01-15 14:22:00
oauth-secret    2024-01-14 09:15:00  2024-01-15 14:22:00
```

### Add Token
```bash
# Interactive (recommended for security)
mcp-router token set --name prod-token

# From file
mcp-router token set --name prod-token --file /secure/path/token.txt

# From stdin
echo -n "secret" | mcp-router token set --name prod-token --stdin
```

### Update Token
```bash
mcp-router token update --name prod-token
```

### Delete Token
```bash
mcp-router token delete --name prod-token
```

### Export Token (with caution)
```bash
# Requires confirmation
mcp-router token get --name prod-token --export
```

## Security Best Practices

### 1. Use Unique Token Names
```yaml
# Good: Descriptive, unique names
token_secure_key: "prod-gateway-bearer-2024"

# Bad: Generic names
token_secure_key: "token1"
```

### 2. Regular Token Rotation
```bash
# Rotate token monthly
mcp-router token rotate --name prod-token --backup
```

### 3. Audit Token Access
```bash
# View token access logs
mcp-router token audit --name prod-token --days 7
```

### 4. Restrict Token Permissions
On macOS, tokens can be restricted to specific applications:
```bash
mcp-router token set --name prod-token --restrict-access
```

## Troubleshooting

### "Secure storage unavailable"
```bash
# Check platform support
mcp-router token doctor

# Common fixes:
# macOS: Unlock keychain
security unlock-keychain

# Linux: Start secret service
systemctl --user start gnome-keyring-daemon

# Windows: Check credential manager service
sc query VaultSvc
```

### "Access denied"
```bash
# Reset permissions
mcp-router token repair --name token-name

# Re-import with correct permissions
mcp-router token set --name token-name --force
```

### Migration from Environment Variables
```bash
# Migrate all environment variables to secure storage
mcp-router token migrate-env \
  --env MCP_AUTH_TOKEN=gateway-token \
  --env MCP_OAUTH_SECRET=oauth-secret
```

### Backup and Restore
```bash
# Backup tokens (encrypted)
mcp-router token backup --output tokens.backup

# Restore tokens
mcp-router token restore --input tokens.backup
```

## Integration with CI/CD

### GitHub Actions
```yaml
- name: Setup MCP Router
  uses: poiley/mcp-router-action@v1
  with:
    token-name: 'ci-token'
    token-value: ${{ secrets.MCP_AUTH_TOKEN }}
```

### Jenkins
```groovy
withCredentials([string(credentialsId: 'mcp-token', variable: 'TOKEN')]) {
    sh 'mcp-router token import --name ci-token --env TOKEN'
}
```

### Kubernetes Secrets
```bash
# Import from Kubernetes secret
kubectl get secret mcp-tokens -o json | \
  jq -r '.data["gateway-token"]' | \
  base64 -d | \
  mcp-router token set --name gateway-token --stdin
```

## Advanced Configuration

### Custom Storage Backend
```yaml
advanced:
  secure_storage:
    backend: "custom"
    custom_command: "/usr/local/bin/vault-helper"
    custom_args: ["get", "secret/mcp/{name}"]
```

### Hardware Security Module (HSM)
```yaml
advanced:
  secure_storage:
    backend: "pkcs11"
    pkcs11_module: "/usr/lib/softhsm/libsofthsm2.so"
    pkcs11_slot: 0
    pkcs11_pin_secure_key: "hsm-pin"
```

## API Reference

### TokenStore Interface
```go
type TokenStore interface {
    Store(name, token string) error
    Retrieve(name string) (string, error)
    Delete(name string) error
    List() ([]TokenInfo, error)
}
```

### Custom Implementation
```go
import "github.com/poiley/mcp-bridge/pkg/secure"

// Implement custom token storage
type MyTokenStore struct{}

func (s *MyTokenStore) Store(name, token string) error {
    // Custom implementation
}

// Register custom store
secure.RegisterBackend("mystore", NewMyTokenStore)
```