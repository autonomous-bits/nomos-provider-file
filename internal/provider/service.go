// Package provider implements the Nomos file provider service.
//
// Single-Instance Architecture:
//
// This provider is designed to work with Nomos's architecture where each source
// declaration (alias) spawns a SEPARATE provider process. Therefore, each provider
// process only ever has ONE configuration initialized.
//
// Example Nomos usage:
//   User's config.csl:
//     source: { alias: "dev", type: "file", directory: "./dev" }
//     source: { alias: "prod", type: "file", directory: "./prod" }
//
//   Nomos spawns:
//     - provider-file process #1 → Init(alias="dev", directory="./dev")
//     - provider-file process #2 → Init(alias="prod", directory="./prod")
//
//   When fetching:
//     - Process #1 receives Fetch(path=["database"]) → reads ./dev/database.csl
//     - Process #2 receives Fetch(path=["database"]) → reads ./prod/database.csl
//
// Thread-Safety:
//
// All RPC methods protect shared state using a sync.RWMutex:
//   - Write operations (Init, Shutdown): acquire exclusive Lock()
//   - Read operations (Fetch, Info, Health): acquire shared RLock()
//
// This allows concurrent Fetch operations from multiple goroutines while
// ensuring safe initialization and shutdown.
package provider

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	providerv1 "github.com/autonomous-bits/nomos/libs/provider-proto/gen/go/nomos/provider/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// providerConfig represents the provider's single configuration.
type providerConfig struct {
	alias       string
	directory   string
	cslFiles    map[string]string // base name -> absolute file path
	initialized bool
}

// FileProviderService implements the nomos.provider.v1.ProviderService gRPC interface
// for local file system access to .csl configuration files.
//
// Single-Instance Design:
//
// Each provider process serves exactly ONE configuration. When used by Nomos,
// a separate provider process is spawned for each source alias, so there's
// no need to track multiple instances within a single process.
//
// Thread-Safety:
//
// All RPC methods protect access to the configuration using mu:
//   - Write operations (Init, Shutdown): acquire exclusive Lock()
//   - Read operations (Fetch, Info, Health): acquire shared RLock()
type FileProviderService struct {
	providerv1.UnimplementedProviderServiceServer

	mu sync.RWMutex

	version      string
	providerType string
	config       *providerConfig
}

// NewFileProviderService creates a new file provider service.
//
// The service starts uninitialized. Call Init() to configure it.
func NewFileProviderService(version, providerType string) *FileProviderService {
	return &FileProviderService{
		version:      version,
		providerType: providerType,
		config:       nil,
	}
}

// Init initializes the provider with the given configuration.
//
// Since each provider process serves one configuration, Init should only be
// called ONCE. Subsequent Init calls will return an error.
//
// Required configuration:
//   - req.Alias: identifier for this provider instance (for logging)
//   - req.Config["directory"]: path to directory containing .csl files
//
// Validation:
//   - Directory must exist and be readable
//   - Directory must contain at least one .csl file
//   - Init can only be called once per process
func (s *FileProviderService) Init(ctx context.Context, req *providerv1.InitRequest) (*providerv1.InitResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already initialized
	if s.config != nil && s.config.initialized {
		return nil, status.Errorf(codes.FailedPrecondition, "provider already initialized as %q", s.config.alias)
	}

	// Validate alias
	if req.Alias == "" {
		return nil, status.Error(codes.InvalidArgument, "alias cannot be empty")
	}

	// Extract directory from config
	configMap := req.Config.AsMap()
	dirValue, ok := configMap["directory"]
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "missing required config key 'directory'")
	}

	dirStr, ok := dirValue.(string)
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "directory must be a string, got %T", dirValue)
	}

	// Resolve to absolute path
	var absPath string
	if !filepath.IsAbs(dirStr) && req.SourceFilePath != "" {
		sourceDir := filepath.Dir(req.SourceFilePath)
		absPath = filepath.Join(sourceDir, dirStr)
	} else {
		var err error
		absPath, err = filepath.Abs(dirStr)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "failed to resolve path: %v", err)
		}
	}

	// Verify directory exists
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, status.Errorf(codes.NotFound, "directory does not exist: %s", absPath)
		}
		return nil, status.Errorf(codes.Internal, "failed to stat directory: %v", err)
	}

	if !info.IsDir() {
		return nil, status.Errorf(codes.InvalidArgument, "path is not a directory: %s", absPath)
	}

	// Enumerate CSL files
	cslFiles, err := s.enumerateCSLFiles(absPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to enumerate .csl files: %v", err)
	}

	// Create configuration
	s.config = &providerConfig{
		alias:       req.Alias,
		directory:   absPath,
		cslFiles:    cslFiles,
		initialized: true,
	}

	log.Printf("Initialized provider: alias=%q directory=%q files=%d", req.Alias, absPath, len(cslFiles))

	return &providerv1.InitResponse{}, nil
}

// enumerateCSLFiles scans the directory for .csl files.
func (s *FileProviderService) enumerateCSLFiles(dirPath string) (map[string]string, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	cslFiles := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		fileName := entry.Name()
		if !strings.HasSuffix(fileName, ".csl") {
			continue
		}

		baseName := strings.TrimSuffix(fileName, ".csl")
		if _, exists := cslFiles[baseName]; exists {
			return nil, fmt.Errorf("duplicate file base name %q", baseName)
		}

		cslFiles[baseName] = filepath.Join(dirPath, fileName)
	}

	if len(cslFiles) == 0 {
		return nil, fmt.Errorf("no .csl files found in directory")
	}

	return cslFiles, nil
}

// Fetch retrieves configuration data from a .csl file.
//
// Path Structure:
//   path[0]: file base name (without .csl extension)
//   path[1+]: optional nested keys within the file
//
// Examples:
//   path=["database"]           → reads database.csl (entire file)
//   path=["database", "host"]   → reads database.csl, extracts "host" key
//   path=["prod", "database"]   → reads prod.csl, extracts "database" key
func (s *FileProviderService) Fetch(ctx context.Context, req *providerv1.FetchRequest) (*providerv1.FetchResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if initialized
	if s.config == nil || !s.config.initialized {
		return nil, status.Error(codes.FailedPrecondition, "provider not initialized")
	}

	// Validate path
	if len(req.Path) == 0 {
		return nil, status.Error(codes.InvalidArgument, "path cannot be empty")
	}

	if req.Path[0] == "" {
		return nil, status.Error(codes.InvalidArgument, "path[0] cannot be empty")
	}

	// path[0] is the filename
	baseName := req.Path[0]

	// Look up file
	filePath, exists := s.config.cslFiles[baseName]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "file %q not found", baseName)
	}

	// Parse the file
	data, err := parseCSLFile(filePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to parse file: %v", err)
	}

	// Navigate to nested path if provided
	if len(req.Path) > 1 {
		current := data
		for i, key := range req.Path[1:] {
			m, ok := current.(map[string]any)
			if !ok {
				return nil, status.Errorf(codes.InvalidArgument,
					"cannot navigate: element at index %d is not a map", i+1)
			}

			val, exists := m[key]
			if !exists {
				return nil, status.Errorf(codes.NotFound, "key %q not found", key)
			}

			current = val
		}
		data = current
	}

	// Convert to protobuf
	value, err := toProtoStruct(data)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to convert data: %v", err)
	}

	return &providerv1.FetchResponse{Value: value}, nil
}

// Info returns provider metadata.
func (s *FileProviderService) Info(ctx context.Context, req *providerv1.InfoRequest) (*providerv1.InfoResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return &providerv1.InfoResponse{
		Version: s.version,
		Type:    s.providerType,
	}, nil
}

// Health checks provider health.
func (s *FileProviderService) Health(ctx context.Context, req *providerv1.HealthRequest) (*providerv1.HealthResponse, error) {
	s.mu.RLock()
	initialized := s.config != nil && s.config.initialized
	s.mu.RUnlock()

	if !initialized {
		return &providerv1.HealthResponse{
			Status:  providerv1.HealthResponse_STATUS_DEGRADED,
			Message: "not initialized",
		}, nil
	}

	return &providerv1.HealthResponse{
		Status:  providerv1.HealthResponse_STATUS_OK,
		Message: "healthy",
	}, nil
}

// Shutdown gracefully shuts down the provider.
func (s *FileProviderService) Shutdown(ctx context.Context, req *providerv1.ShutdownRequest) (*providerv1.ShutdownResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.config = nil

	return &providerv1.ShutdownResponse{}, nil
}

// toProtoStruct converts a Go value to a protobuf Struct.
func toProtoStruct(v any) (*structpb.Struct, error) {
	if m, ok := v.(map[string]any); ok {
		return structpb.NewStruct(m)
	}
	wrapped := map[string]any{"value": v}
	return structpb.NewStruct(wrapped)
}
