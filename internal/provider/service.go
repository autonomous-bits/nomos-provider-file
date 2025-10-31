package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	providerv1 "github.com/autonomous-bits/nomos/libs/provider-proto/gen/go/nomos/provider/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// FileProviderService implements the nomos.provider.v1.ProviderService gRPC interface
// for local file system access to .csl configuration files.
type FileProviderService struct {
	providerv1.UnimplementedProviderServiceServer

	mu sync.RWMutex

	version      string
	providerType string

	// State set by Init
	alias     string
	directory string
	cslFiles  map[string]string // base name -> absolute file path

	initialized bool
}

// NewFileProviderService creates a new file provider service.
func NewFileProviderService(version, providerType string) *FileProviderService {
	return &FileProviderService{
		version:      version,
		providerType: providerType,
	}
}

// Init initializes the provider with configuration.
func (s *FileProviderService) Init(ctx context.Context, req *providerv1.InitRequest) (*providerv1.InitResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.initialized {
		return nil, status.Error(codes.FailedPrecondition, "provider already initialized")
	}

	s.alias = req.Alias

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
		// Resolve relative to the source file's directory
		sourceDir := filepath.Dir(req.SourceFilePath)
		absPath = filepath.Join(sourceDir, dirStr)
	} else {
		// Absolute path or no source file path - resolve from current directory
		var err error
		absPath, err = filepath.Abs(dirStr)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "failed to resolve path to absolute: %v", err)
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

	// Enumerate .csl files
	if err := s.enumerateCSLFiles(absPath); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to enumerate .csl files: %v", err)
	}

	s.directory = absPath
	s.initialized = true

	return &providerv1.InitResponse{}, nil
}

// enumerateCSLFiles scans the directory for .csl files and builds the file map.
func (s *FileProviderService) enumerateCSLFiles(dirPath string) error {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
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
			return fmt.Errorf("duplicate file base name %q", baseName)
		}

		cslFiles[baseName] = filepath.Join(dirPath, fileName)
	}

	if len(cslFiles) == 0 {
		return fmt.Errorf("no .csl files found in directory")
	}

	s.cslFiles = cslFiles
	return nil
}

// Fetch retrieves data from a .csl file.
func (s *FileProviderService) Fetch(ctx context.Context, req *providerv1.FetchRequest) (*providerv1.FetchResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.initialized {
		return nil, status.Error(codes.FailedPrecondition, "provider not initialized")
	}

	if len(req.Path) == 0 {
		return nil, status.Error(codes.InvalidArgument, "path cannot be empty")
	}

	// First path component is the file base name
	baseName := req.Path[0]

	filePath, exists := s.cslFiles[baseName]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "file %q not found in provider %q", baseName, s.alias)
	}

	// Parse the .csl file
	data, err := parseCSLFile(filePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to parse .csl file %q: %v", filePath, err)
	}

	// If additional path components provided, navigate to that path
	if len(req.Path) > 1 {
		var current any = data
		for i, key := range req.Path[1:] {
			m, ok := current.(map[string]any)
			if !ok {
				return nil, status.Errorf(codes.InvalidArgument,
					"cannot navigate to path %v: element at index %d is not a map", req.Path, i+1)
			}

			val, exists := m[key]
			if !exists {
				return nil, status.Errorf(codes.NotFound, "path element %q not found in file %q", key, baseName)
			}

			current = val
		}
		data = current
	}

	// Convert to protobuf Struct
	value, err := toProtoStruct(data)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to convert data to protobuf: %v", err)
	}

	return &providerv1.FetchResponse{Value: value}, nil
}

// Info returns provider metadata.
func (s *FileProviderService) Info(ctx context.Context, req *providerv1.InfoRequest) (*providerv1.InfoResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return &providerv1.InfoResponse{
		Alias:   s.alias,
		Version: s.version,
		Type:    s.providerType,
	}, nil
}

// Health checks provider health status.
func (s *FileProviderService) Health(ctx context.Context, req *providerv1.HealthRequest) (*providerv1.HealthResponse, error) {
	s.mu.RLock()
	initialized := s.initialized
	s.mu.RUnlock()

	if !initialized {
		return &providerv1.HealthResponse{
			Status:  providerv1.HealthResponse_STATUS_DEGRADED,
			Message: "provider not initialized",
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

	// Clean up resources if needed
	s.initialized = false
	s.cslFiles = nil

	return &providerv1.ShutdownResponse{}, nil
}

// toProtoStruct converts a Go value to a protobuf Struct.
func toProtoStruct(v any) (*structpb.Struct, error) {
	// Handle map type
	if m, ok := v.(map[string]any); ok {
		return structpb.NewStruct(m)
	}

	// If not a map, wrap it in a struct with "value" key
	wrapped := map[string]any{"value": v}
	return structpb.NewStruct(wrapped)
}
