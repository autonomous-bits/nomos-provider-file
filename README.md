# nomos-provider-file

Reference implementation of an external Nomos provider for file system access.

## Overview

This provider implements the Nomos Provider gRPC service contract to supply configuration data from local file system directories. It reads `.csl` files from a configured directory and makes them available to the Nomos compiler via gRPC.

## Features

- **Directory-based access**: Reads all `.csl` files from a configured directory
- **gRPC interface**: Implements `nomos.provider.v1.ProviderService`
- **Health checks**: Built-in health monitoring
- **Graceful shutdown**: Clean resource cleanup on termination

## Installation

### From GitHub Releases

Download the appropriate binary for your platform:

```bash
# macOS ARM64
curl -LO https://github.com/autonomous-bits/nomos-provider-file/releases/download/v0.1.0/nomos-provider-file-0.1.0-darwin-arm64

# macOS AMD64
curl -LO https://github.com/autonomous-bits/nomos-provider-file/releases/download/v0.1.0/nomos-provider-file-0.1.0-darwin-amd64

# Linux AMD64
curl -LO https://github.com/autonomous-bits/nomos-provider-file/releases/download/v0.1.0/nomos-provider-file-0.1.0-linux-amd64
```

Verify the checksum:

```bash
curl -LO https://github.com/autonomous-bits/nomos-provider-file/releases/download/v0.1.0/SHA256SUMS
shasum -a 256 -c SHA256SUMS
```

Make it executable and move to installation directory:

```bash
chmod +x nomos-provider-file-*
sudo mv nomos-provider-file-* /usr/local/bin/nomos-provider-file
```

### From Source

```bash
git clone https://github.com/autonomous-bits/nomos-provider-file.git
cd nomos-provider-file
go build -o nomos-provider-file ./cmd/provider
```

## Usage

### With Nomos CLI

Declare the provider in your `.csl` file:

```
source:
  alias: 'configs'
  type: 'file'
  version: '0.1.0'
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

- Go 1.24.4 or later
- Protocol Buffers compiler (for regenerating proto stubs)

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

The first path component is the file base name (without `.csl` extension):

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
