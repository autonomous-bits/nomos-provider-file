# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.1] - 2026-01-14

### Fixed
- Fetch method now automatically detects single vs multi-instance mode for path resolution
- Single instance mode: path starts with filename directly (e.g., `["prod", "database", "name"]`)
- Multi-instance mode: path must include alias first (e.g., `["configs", "prod", "database", "name"]`)
- Fixed "provider instance not found" error when using single provider with file references

### Note
- This fix ensures backward compatibility with Nomos CLI's reference resolution behavior

## [0.2.0] - 2026-01-14

### Added
- Multiple provider instance support - enables users to work with multiple configuration directories simultaneously
- Atomic rollback on initialization failure - if any Init fails, all previous instances are rolled back to ensure clean state
- Enhanced error messages with alias context - all errors now include the provider instance alias for better debugging
- Comprehensive logging for Init, Fetch, and rollback operations
- Multi-instance path structure: `path=[alias, filename, ...]` for independent instance access

### Changed
- Init method can now be called multiple times with different aliases to create independent provider instances
- Fetch operations now require alias as first path component to identify target instance
- Health check returns STATUS_OK when at least one instance is initialized (previously required single instance)

### Note
- All changes are backward compatible with v0.1.0 behavior when using single instance

## [0.1.2] - 2025-12-28

### Changed
- Updated Go version to 1.25.5 across all CI workflows and documentation
- Updated Go module dependencies to latest versions

## [0.1.1] - 2025-11-02

### Added
- Windows build support for amd64 and arm64 architectures
- Linux arm64 build support (previously missing from releases)
- Archive-based distribution with documentation files included

### Changed
- Release artifacts now packaged as `.tar.gz` archives instead of raw binaries
- Each archive includes executable, LICENSE, README.md, and CHANGELOG.md
- Updated installation instructions for archive-based distribution
- Updated CI workflow to build Windows targets

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
- README with usage and installation instructions2...HEAD
[0.1.2]: https://github.com/autonomous-bits/nomos-provider-file/compare/v0.1.1...v0.1.2

[Unreleased]: https://github.com/autonomous-bits/nomos-provider-file/compare/v0.2.1...HEAD
[0.2.1]: https://github.com/autonomous-bits/nomos-provider-file/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/autonomous-bits/nomos-provider-file/compare/v0.1.2...v0.2.0
[0.1.2]: https://github.com/autonomous-bits/nomos-provider-file/compare/v0.1.1...v0.1.2
[0.1.0]: https://github.com/autonomous-bits/nomos-provider-file/releases/tag/v0.1.0
