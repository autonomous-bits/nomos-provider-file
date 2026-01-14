# Nomos File Provider

Reference implementation of an external Nomos provider for file system access.

[![License](https://img.shields.io/badge/License-MIT-blue.svg)](/LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.25%2B-blue)](https://go.dev/)

## Overview

This provider implements the Nomos Provider gRPC service contract to supply configuration data from local file system directories. It reads `.csl` files from a configured directory and makes them available to the Nomos compiler via gRPC.

## Features

- **Directory-based access**: Reads all `.csl` files from a configured directory
- **gRPC interface**: Implements `nomos.provider.v1.ProviderService`
- **Multi-instance support**: Work with multiple configuration directories simultaneously
- **Atomic initialization**: Automatic rollback if any instance fails to initialize
- **Health checks**: Built-in health monitoring
- **Graceful shutdown**: Clean resource cleanup on termination

## Multi-Instance Support

**New in v0.1.1**: The file provider now supports multiple independent instances within a single service process. Each instance is identified by a unique alias and operates on its own directory.

### Key Capabilities

- **Independent Instances**: Initialize multiple provider instances with different directories
- **Isolated Operations**: Each instance manages its own set of .csl files independently
- **Atomic Guarantees**: If any initialization fails, all instances are rolled back to ensure clean state
- **Enhanced Errors**: All error messages include the alias to identify which instance caused the error

### Multi-Instance Usage Example

```csl
# Define multiple provider instances
source:
  alias: 'local'
  type: 'file'
  version: '0.1.1'
  directory: './configs'

source:
  alias: 'shared'
  type: 'file'
  version: '0.1.1'
  directory: '/etc/shared-configs'

# Import from different instances
import:local:database    # reads ./configs/database.csl
import:shared:network    # reads /etc/shared-configs/network.csl
```

### Path Structure

With multi-instance support, fetch paths follow this structure:

```
path[0]: alias        # identifies the provider instance
path[1]: filename     # base name without .csl extension
path[2+]: nested keys # optional: navigate within the file
```

Examples:
```go
// Fetch entire file from "local" instance
path: ["local", "database"]

// Fetch nested key from "shared" instance  
path: ["shared", "network", "ports", "http"]
```

### Error Handling

All errors include the alias for better debugging:

```
❌ provider instance "local" not found
❌ file "database" not found in provider instance "local"
❌ path element "host" not found in file "database" (provider instance "local")
```

### Initialization Guarantees

The provider ensures atomic initialization:

```csl
source:
  alias: 'instance1'
  directory: './valid-path'    # ✅ succeeds

source:
  alias: 'instance2'
  directory: './invalid-path'  # ❌ fails
  
# Result: Both instances are rolled back
# Service returns to clean empty state
# Error message: "rolled back all 1 instance(s)"
```

This prevents partial initialization states and ensures consistent behavior.

## Usage

### With Nomos CLI

Declare the provider in your `.csl` file:

```csl
# Single instance (v0.1.0 compatible)
source:
  alias: 'configs'
  type: 'file'
  version: '0.1.1'
  directory: './configs'

import:configs:database
```

Run `nomos init` to install the provider:

```bash
nomos init
```

Then build your configuration:

```bash
nomos build -p ./config.csl
```

### Standalone Testing

The provider can be run standalone for testing:

```bash
./nomos-provider-file
```

The provider will:
1. Start a gRPC server on a random available port
2. Print `PROVIDER_PORT=<port>` to stdout
3. Wait for RPC calls

## Configuration

The provider accepts the following configuration in the `Init` RPC call:

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `directory` | string | Yes | Absolute or relative path to directory containing `.csl` files |

## Development

### Prerequisites

- Go 1.25+ or later
- Protocol Buffers compiler (for regenerating proto stubs)
- Local clone of the Nomos repository (for now, until modules are published)

### Note on Dependencies

This provider currently uses `replace` directives in `go.mod` to reference the Nomos libraries locally. For production releases, these dependencies need to be published as proper Go modules or vendored into the repository.

### Building

```bash
make build
```

### Testing

```bash
make test
```

### Running Locally

```bash
go run ./cmd/provider
```

## Protocol

This provider implements the `nomos.provider.v1.ProviderService` gRPC contract:

- **Init**: Initialize the provider with a directory path
- **Fetch**: Retrieve a `.csl` file by base name (without extension)
- **Info**: Return provider metadata (alias, version, type)
- **Health**: Check provider health status
- **Shutdown**: Gracefully shut down the provider

### Fetch Path Format

**Multi-Instance Format (v0.1.1+)**:

```
path: ["alias", "filename"]            → fetches from specific instance
path: ["alias", "filename", "key"]     → fetches nested key
path: ["local", "database"]            → fetches ./configs/database.csl
path: ["shared", "network", "ports"]   → fetches /etc/configs/network.csl -> ports
```

**Single Instance Format (v0.1.0 compatible)**:

```
path: ["database"]      → fetches database.csl
path: ["network"]       → fetches network.csl
path: ["app", "config"] → fetches app.csl and navigates to config key
```

## Architecture

```
┌──────────────┐          gRPC           ┌─────────────────┐
│    Nomos     │ ──────────────────────▶ │ Provider        │
│   Compiler   │   Init/Fetch/Info/etc   │ (subprocess)    │
└──────────────┘                         └─────────────────┘
                                                  │
                                                  ▼
                                         ┌─────────────────┐
                                         │  File System    │
                                         │  (.csl files)   │
                                         └─────────────────┘
```

The provider:
1. Is started as a subprocess by the Nomos compiler
2. Listens on a random TCP port
3. Parses `.csl` files from the configured directory
4. Returns structured data via gRPC

## Versioning

This project follows [Semantic Versioning](https://semver.org/):

- **MAJOR**: Breaking changes to gRPC contract or behavior
- **MINOR**: New features, backward compatible
- **PATCH**: Bug fixes, backward compatible

## License

See [LICENSE](LICENSE) file for details.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines.
