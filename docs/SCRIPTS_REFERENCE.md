# MCP Bridge Scripts Reference

Complete documentation for all automation scripts in the MCP Bridge project.

## Table of Contents

- [Script Overview](#script-overview)
- [Core Scripts](#core-scripts)
- [Migration Scripts](#migration-scripts)
- [Environment Variables](#environment-variables)
- [Exit Codes](#exit-codes)
- [Best Practices](#best-practices)

## Script Overview

MCP Bridge includes several automation scripts to simplify common operations:

| Script | Location | Purpose | Privileges |
|--------|----------|---------|------------|
| `quickstart.sh` | `/` | Developer quick setup | User |
| `install.sh` | `/scripts/` | System installation | Root (production) |
| `stop.sh` | `/scripts/` | Service shutdown | User/Root |
| `migrate.sh` | `/scripts/` | Version upgrades | User/Root |
| `rollback.sh` | `/scripts/` | Emergency rollback | User/Root |
| `migrate-config.sh` | `/tools/config-migrator/` | Config migration | User |

## Core Scripts

### quickstart.sh

**Purpose**: Rapid development environment setup with interactive wizard

**Synopsis**:
```bash
./quickstart.sh [OPTIONS]
```

**Options**:
| Option | Description | Default |
|--------|-------------|---------|
| `-v, --verbose` | Enable verbose output | false |
| `--skip-deps` | Skip dependency installation | false |
| `--skip-docker` | Skip Docker setup | false |
| `--skip-build` | Skip building binaries | false |
| `--demo` | Run demo after setup | false |
| `--env ENV` | Environment type | development |
| `-h, --help` | Show help message | - |

**Environment Variables**:
- `EMOJI_ENABLED`: Enable/disable emoji in output (default: true)
- `VERBOSE`: Enable verbose logging (default: false)

**Process Flow**:
1. System detection and validation
2. Dependency checking
3. Environment setup
4. Binary building
5. Docker services (optional)
6. Service startup
7. Health verification
8. Demo execution (optional)

**Output Files**:
- `quickstart.log`: Detailed execution log
- `.quickstart`: State file with last run info
- `configs/*.yaml`: Generated configurations
- `.env`: Environment variables
- `docker-compose.yml`: Docker services definition

**Example Usage**:
```bash
# Basic setup
./quickstart.sh

# Setup with demo
./quickstart.sh --demo

# Setup without Docker
./quickstart.sh --skip-docker

# Verbose debugging
./quickstart.sh --verbose
```

### scripts/install.sh

**Purpose**: Production-grade system installation with systemd integration

**Synopsis**:
```bash
./scripts/install.sh [OPTIONS]
```

**Options**:
| Option | Description | Default |
|--------|-------------|---------|
| `-e, --environment` | Environment (development/staging/production) | development |
| `-p, --prefix` | Installation prefix | /usr/local |
| `-v, --verbose` | Enable verbose output | false |
| `-y, --yes` | Auto-confirm prompts | false |
| `--skip-systemd` | Skip systemd service setup | false |
| `--skip-config` | Skip config file creation | false |
| `--dry-run` | Preview without changes | false |
| `--uninstall` | Remove MCP Bridge | false |

**Required Privileges**:
- Development: User
- Staging: User (recommended: root)
- Production: Root

**Installation Paths**:
```
$PREFIX/
├── bin/
│   ├── mcp-gateway
│   ├── mcp-router
│   └── mcp-migrate
└── share/
    └── mcp-bridge/
        └── docs/

/etc/mcp-bridge/        # Configurations
/var/lib/mcp-bridge/    # Data
/var/log/mcp-bridge/    # Logs
```

**Systemd Services** (staging/production):
- `mcp-gateway.service`
- `mcp-router.service`

**Example Usage**:
```bash
# Development installation
./scripts/install.sh --environment development

# Production installation
sudo ./scripts/install.sh --environment production --yes

# Custom prefix
./scripts/install.sh --prefix /opt/mcp --environment staging

# Uninstall
sudo ./scripts/install.sh --uninstall
```

### scripts/stop.sh

**Purpose**: Gracefully stop all MCP Bridge services

**Synopsis**:
```bash
./scripts/stop.sh [OPTIONS]
```

**Options**:
| Option | Description | Default |
|--------|-------------|---------|
| `-f, --force` | Force stop if graceful fails | false |
| `-q, --quiet` | Suppress output | false |
| `-h, --help` | Show help message | - |

**Stop Methods** (in order):
1. Systemd services (`systemctl stop`)
2. Docker Compose services (`docker-compose down`)
3. Kubernetes deployments (`kubectl scale --replicas=0`)
4. PID file tracking (`.gateway.pid`, `.router.pid`)
5. Process name matching (`pgrep/pkill`)
6. Port cleanup (`lsof`)

**Signals**:
- SIGTERM: Graceful shutdown (10s timeout)
- SIGKILL: Force kill (with `--force`)

**Example Usage**:
```bash
# Graceful stop
./scripts/stop.sh

# Force stop
./scripts/stop.sh --force

# Quiet mode
./scripts/stop.sh --quiet
```

## Migration Scripts

### scripts/migrate.sh

**Purpose**: Handle version upgrades and migrations

**Synopsis**:
```bash
./scripts/migrate.sh [OPTIONS] TARGET_VERSION
```

**Options**:
| Option | Description | Default |
|--------|-------------|---------|
| `-c, --current` | Current version | auto-detect |
| `-d, --dry-run` | Preview changes | false |
| `-f, --force` | Force migration | false |
| `-r, --rollback` | Rollback to previous | false |
| `-s, --skip-backup` | Skip backup creation | false |

**Migration Process**:
1. Version detection
2. Pre-migration checks
3. Backup creation
4. Migration execution
5. Post-migration tasks
6. Health verification

**State Files**:
- `.migration-state`: Current migration state
- `migrations/`: Migration scripts
- `backups/`: Backup archives

**Example Usage**:
```bash
# Upgrade to version 1.1.0
./scripts/migrate.sh 1.1.0

# Dry run
./scripts/migrate.sh --dry-run 1.1.0

# Rollback
./scripts/migrate.sh --rollback
```

### scripts/rollback.sh

**Purpose**: Emergency rollback from failed upgrades

**Synopsis**:
```bash
./scripts/rollback.sh [OPTIONS]
```

**Options**:
| Option | Description | Default |
|--------|-------------|---------|
| `-b, --backup` | Specific backup file | auto-detect |
| `-a, --auto` | Auto-detect latest backup | true |
| `-f, --force` | Force rollback | false |
| `-d, --dry-run` | Preview rollback | false |
| `-s, --skip-health` | Skip health checks | false |

**Rollback Process**:
1. Backup validation
2. Service shutdown
3. Backup restoration
4. Service restart
5. Health verification

**Example Usage**:
```bash
# Auto rollback to latest
./scripts/rollback.sh

# Rollback to specific backup
./scripts/rollback.sh --backup backups/backup-20240120.tar.gz

# Dry run
./scripts/rollback.sh --dry-run
```

### tools/config-migrator/migrate-config.sh

**Purpose**: Migrate configuration files between versions

**Synopsis**:
```bash
./tools/config-migrator/migrate-config.sh [OPTIONS] CONFIG_FILE
```

**Options**:
| Option | Description | Default |
|--------|-------------|---------|
| `-d, --dry-run` | Show changes without applying | false |
| `-v, --validate` | Validate config only | false |
| `-c, --config` | Config file to migrate | required |
| `-o, --output` | Output file | input file |

**Migrations Applied**:
- Add missing required sections
- Update deprecated fields
- Migrate to new formats
- Add default values

**Example Usage**:
```bash
# Migrate gateway config
./tools/config-migrator/migrate-config.sh configs/gateway.yaml

# Dry run
./tools/config-migrator/migrate-config.sh --dry-run configs/router.yaml

# Validate only
./tools/config-migrator/migrate-config.sh --validate configs/gateway.yaml
```

## Environment Variables

### Global Variables

| Variable | Description | Default | Scripts |
|----------|-------------|---------|---------|
| `PROJECT_ROOT` | Project root directory | Script parent dir | All |
| `VERBOSE` | Enable verbose output | false | All |
| `DRY_RUN` | Preview mode | false | Most |
| `LOG_LEVEL` | Logging verbosity | info | Services |

### Service Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `GATEWAY_PORT` | Gateway service port | 8080 |
| `ROUTER_PORT` | Router service port | 8081 |
| `METRICS_PORT` | Metrics endpoint port | 9090 |
| `REDIS_URL` | Redis connection string | localhost:6379 |
| `AUTH_TOKEN` | Authentication token | dev-token-12345 |

### Installation Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `INSTALL_PREFIX` | Installation directory | /usr/local |
| `CONFIG_DIR` | Configuration directory | /etc/mcp-bridge |
| `DATA_DIR` | Data directory | /var/lib/mcp-bridge |
| `LOG_DIR` | Log directory | /var/log/mcp-bridge |

## Exit Codes

Standard exit codes used across all scripts:

| Code | Meaning | Description |
|------|---------|-------------|
| 0 | Success | Operation completed successfully |
| 1 | General Error | Unspecified error occurred |
| 2 | Misuse | Invalid arguments or options |
| 3 | Cannot Execute | Permission denied or not executable |
| 4 | Not Found | Required file or command not found |
| 5 | Config Error | Configuration file error |
| 6 | Service Error | Service start/stop failure |
| 7 | Network Error | Network connectivity issue |
| 8 | Dependency Error | Missing required dependency |
| 9 | State Error | Invalid state for operation |
| 10 | Health Check Failed | Service health verification failed |

## Best Practices

### Script Development

1. **Idempotency**: Scripts should be safe to run multiple times
2. **Error Handling**: Use `set -euo pipefail` for strict error handling
3. **Logging**: Write to both stdout and log files
4. **Dry Run**: Always provide `--dry-run` option
5. **Help Text**: Include comprehensive `--help` output
6. **Exit Codes**: Use consistent, documented exit codes

### Script Usage

1. **Always Review First**: Read script help before running
2. **Use Dry Run**: Test with `--dry-run` in production
3. **Check Logs**: Review log files for details
4. **Backup First**: Create backups before migrations
5. **Test in Staging**: Validate in staging before production

### Security

1. **Privilege Escalation**: Only request root when necessary
2. **Input Validation**: Validate all user inputs
3. **Secure Defaults**: Use secure settings by default
4. **Sensitive Data**: Never log passwords or tokens
5. **File Permissions**: Set appropriate file permissions

### Maintenance

1. **Version Control**: Track all script changes
2. **Testing**: Test scripts in CI/CD pipeline
3. **Documentation**: Keep documentation in sync
4. **Deprecation**: Provide migration paths for changes
5. **Compatibility**: Maintain backward compatibility

## Troubleshooting

### Common Issues

**Script Not Executable**:
```bash
chmod +x script.sh
```

**Permission Denied**:
```bash
# Check if root is required
sudo ./script.sh
```

**Command Not Found**:
```bash
# Check PATH
echo $PATH
# Or use full path
/usr/local/bin/mcp-gateway
```

**Port Already in Use**:
```bash
# Find process using port
lsof -i :8080
# Stop services
./scripts/stop.sh --force
```

### Debug Mode

Enable debug output:
```bash
# Set environment variable
export VERBOSE=true
./quickstart.sh

# Or use flag
./quickstart.sh --verbose
```

### Log Locations

- `quickstart.log`: Quickstart execution
- `migration.log`: Migration operations
- `rollback.log`: Rollback operations
- `/var/log/mcp-bridge/`: System logs (production)
- `journalctl -u mcp-*`: Systemd logs

## Contributing

When adding new scripts:

1. Follow existing patterns and conventions
2. Include comprehensive help text
3. Add to this documentation
4. Test on multiple platforms
5. Include in CI/CD tests

See [CONTRIBUTING.md](../CONTRIBUTING.md) for details.