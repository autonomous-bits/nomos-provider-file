# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2025-11-02

### Added
- Initial implementation of file provider for Nomos
- gRPC server implementing `nomos.provider.v1.ProviderService` contract
- Directory-based .csl file access and parsing
- Health check and graceful shutdown support
- Cross-platform builds for darwin/arm64, darwin/amd64, linux/amd64
- Comprehensive test suite with >80% coverage
- CI/CD pipeline with GitHub Actions
- GoReleaser configuration for automated releases
- README with usage and installation instructions

[Unreleased]: https://github.com/autonomous-bits/nomos-provider-file/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/autonomous-bits/nomos-provider-file/releases/tag/v0.1.0
