# Contributing to nomos-provider-file

Thank you for your interest in contributing to the Nomos file provider!

## Development Setup

### Prerequisites

- Go 1.24.4 or later
- Make
- Git

### Getting Started

1. Fork the repository
2. Clone your fork:
   ```bash
   git clone https://github.com/YOUR_USERNAME/nomos-provider-file.git
   cd nomos-provider-file
   ```

3. Add the upstream remote:
   ```bash
   git remote add upstream https://github.com/autonomous-bits/nomos-provider-file.git
   ```

4. Install dependencies:
   ```bash
   go mod download
   ```

## Development Workflow

### Building

```bash
# Build for current platform
make build

# Build for all platforms
make build-all
```

### Testing

```bash
# Run tests
make test

# Run tests with coverage
make coverage
```

### Code Quality

```bash
# Format code
make fmt

# Run linter
make lint

# Run all checks
make check
```

## Pull Request Process

1. Create a new branch:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. Make your changes and commit using conventional commits:
   ```bash
   git commit -m "feat: add new feature"
   ```

   Commit message format:
   - `feat:` - New features
   - `fix:` - Bug fixes
   - `docs:` - Documentation changes
   - `test:` - Test updates
   - `refactor:` - Code refactoring
   - `chore:` - Maintenance tasks

3. Push to your fork:
   ```bash
   git push origin feature/your-feature-name
   ```

4. Open a Pull Request against the `main` branch

## Code Guidelines

- Follow Go best practices and idioms
- Write tests for new functionality
- Ensure all tests pass before submitting PR
- Update documentation for user-facing changes
- Keep commits focused and atomic

## Testing Guidelines

- Write unit tests for all new functions
- Include integration tests for gRPC endpoints
- Aim for >80% code coverage
- Test error paths and edge cases

## Reporting Issues

- Use the GitHub issue tracker
- Include clear reproduction steps
- Provide relevant logs and error messages
- Specify Go version and platform

## Questions?

Feel free to open an issue for questions or discussions about the project.

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
