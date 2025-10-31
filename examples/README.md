# Examples

This directory contains example configuration files to test the provider.

## Running the Provider

1. Build the provider:
   ```bash
   make build
   ```

2. Start the provider:
   ```bash
   ./dist/nomos-provider-file
   ```

   You should see output like:
   ```
   PROVIDER_PORT=52341
   File provider v0.1.0 listening on 127.0.0.1:52341
   ```

## Testing with gRPC

You can test the provider using a gRPC client tool like `grpcurl`:

### Initialize the provider

```bash
grpcurl -plaintext -d '{
  "alias": "configs",
  "config": {
    "directory": "./examples/configs"
  }
}' localhost:PORT nomos.provider.v1.ProviderService/Init
```

### Fetch database config

```bash
grpcurl -plaintext -d '{
  "path": ["database"]
}' localhost:PORT nomos.provider.v1.ProviderService/Fetch
```

### Check provider health

```bash
grpcurl -plaintext localhost:PORT nomos.provider.v1.ProviderService/Health
```

### Get provider info

```bash
grpcurl -plaintext localhost:PORT nomos.provider.v1.ProviderService/Info
```

## Configuration Files

- `configs/database.csl` - Database configuration with connection pool settings
- `configs/network.csl` - Network/VPC configuration

## Notes

- Replace `PORT` with the actual port number printed by the provider on startup
- The provider expects absolute or relative paths in the `directory` config
- All `.csl` files in the directory are automatically enumerated on Init
