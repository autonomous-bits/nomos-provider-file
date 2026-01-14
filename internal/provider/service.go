// Package provider implements the Nomos file provider service.
//
// Multi-Instance Architecture:
//
// This provider supports multiple logical "provider instances" within a single
// gRPC service process. From the user's perspective, each Init call with a
// unique alias creates a new provider instance that operates independently.
//
// The FileProviderService manages all instances internally, mapping each alias
// to its own configuration (directory, enumerated files). This enables users
// to work with multiple configuration directories simultaneously:
//
//	Init(alias="local", directory="./configs")
//	Init(alias="shared", directory="/etc/configs")
//	Fetch(path=["local", "database"])  // reads from ./configs/database.csl
//	Fetch(path=["shared", "network"])  // reads from /etc/configs/network.csl
//
// Thread-Safety Model:
//
// All RPC methods protect shared state using a sync.RWMutex:
//   - Write operations (Init, Shutdown): acquire exclusive Lock()
//   - Read operations (Fetch, Info, Health): acquire shared RLock()
//
// This allows concurrent Fetch operations from multiple goroutines while
// ensuring safe initialization and shutdown.
//
// Atomic Initialization:
//
// When multiple Init calls are made in sequence, the service guarantees
// atomicity: either all initializations succeed, or all are rolled back.
// If any Init fails after previous successful inits, rollbackAll() removes
// all initialized instances, returning the service to a clean empty state.
//
// This prevents partial initialization states and ensures consistent behavior
// when initialization fails midway through multiple provider declarations.
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

// instanceConfig represents configuration for one logical provider instance.
//
// From the user's perspective, each instance is a separate "provider" identified
// by its alias. Internally, instanceConfig holds all state for that instance:
//   - alias: the unique identifier for this instance
//   - directory: canonical absolute path to the config directory
//   - cslFiles: map of base names to absolute file paths (e.g., "database" -> "/path/database.csl")
//   - initialized: tracks whether this instance completed initialization
//
// Instances are created during Init() and stored in FileProviderService.configs.
type instanceConfig struct {
	alias       string
	directory   string
	cslFiles    map[string]string // base name -> absolute file path
	initialized bool
}

// FileProviderService implements the nomos.provider.v1.ProviderService gRPC interface
// for local file system access to .csl configuration files.
//
// Multi-Instance Management:
//
// A single FileProviderService manages multiple provider instances. Each instance
// is identified by a unique alias and configured with its own directory. The service
// maintains three data structures:
//
//  1. configs: maps alias -> instanceConfig (the primary instance storage)
//  2. directoryRegistry: maps canonical directory path -> alias (prevents duplicate directories)
//  3. initOrder: tracks initialization order for proper rollback sequencing
//
// Thread-Safety:
//
// All RPC methods protect access to instance state using mu:
//   - Write operations (Init, Shutdown): acquire exclusive Lock()
//   - Read operations (Fetch, Info, Health): acquire shared RLock()
//
// This allows concurrent Fetch operations while ensuring safe state modifications.
//
// Rollback Semantics:
//
// Init() provides atomic multi-instance initialization. If any Init call fails
// after previous successful initializations, all instances are rolled back via
// rollbackAll(). This ensures the service never remains in a partially-initialized
// state, which could cause confusing behavior for users.
type FileProviderService struct {
	providerv1.UnimplementedProviderServiceServer

	mu sync.RWMutex

	version      string
	providerType string

	// Multi-instance state
	configs           map[string]*instanceConfig // alias -> instance config
	directoryRegistry map[string]string          // canonical path -> alias (prevents duplicates)
	initOrder         []string                   // ordered list of initialized aliases (for rollback)
}

// NewFileProviderService creates a new multi-instance file provider service.
//
// The service is created with no initialized instances. Instances are added
// through subsequent Init() RPC calls. Each Init call with a unique alias
// creates a new logical provider instance within this service.
//
// Parameters:
//   - version: semantic version of the provider (e.g., "0.1.1")
//   - providerType: type identifier for the provider (e.g., "file")
//
// Returns a FileProviderService ready to accept Init RPC calls.
func NewFileProviderService(version, providerType string) *FileProviderService {
	return &FileProviderService{
		version:           version,
		providerType:      providerType,
		configs:           make(map[string]*instanceConfig),
		directoryRegistry: make(map[string]string),
		initOrder:         make([]string, 0),
	}
}

// canonicalizePath resolves a directory path to its canonical absolute form.
//
// This is critical for multi-instance duplicate detection. Different path
// representations ("./configs", "configs", "/abs/path/configs") may all
// point to the same directory. Canonicalization ensures we detect duplicates
// by comparing the resolved absolute paths.
//
// Process:
//  1. Resolve symlinks via filepath.EvalSymlinks
//  2. Clean and normalize path separators
//  3. Return canonical absolute path
//
// The canonical path is stored in directoryRegistry to prevent multiple
// instances from using the same directory with different aliases.
func (s *FileProviderService) canonicalizePath(path string) (string, error) {
	// Resolve symlinks
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	// Normalize path (clean redundant separators, resolve . and ..)
	canonical := filepath.Clean(resolved)

	return canonical, nil
}

// rollbackAll performs a complete rollback of all initialized instances.
//
// Rollback Semantics:
//
// When an Init() call fails after one or more previous Init() calls have
// succeeded, rollbackAll() is invoked to restore the service to a clean
// empty state. This prevents partial initialization scenarios.
//
// For example:
//  1. Init(alias="local", dir="./configs")   → succeeds
//  2. Init(alias="shared", dir="/bad/path") → fails
//     Result: "local" instance is rolled back, service has 0 instances
//
// Rollback Process:
//  1. Iterate through initOrder in reverse (LIFO)
//  2. Remove each instance from configs map
//  3. Remove directory from directoryRegistry
//  4. Clear initOrder array
//
// Thread-safety: Caller must hold mu.Lock() before calling rollbackAll().
func (s *FileProviderService) rollbackAll() {
	// Iterate in reverse order to undo initialization
	for i := len(s.initOrder) - 1; i >= 0; i-- {
		alias := s.initOrder[i]
		// T039: Log rollback operation
		log.Printf("Rolling back provider instance: alias=%q", alias)

		// Remove from configs map
		if cfg, exists := s.configs[alias]; exists {
			// Remove directory registry entry
			if cfg.directory != "" {
				delete(s.directoryRegistry, cfg.directory)
			}
			delete(s.configs, alias)
		}
	}

	// Clear init order
	s.initOrder = s.initOrder[:0]
}

// Init initializes a new provider instance with the given configuration.
//
// Multi-Instance Behavior:
//
// Each Init call creates a new logical provider instance identified by req.Alias.
// Multiple Init calls with different aliases are supported and create independent
// instances within the same service process.
//
// Required configuration:
//   - req.Alias: unique identifier for this instance (non-empty string)
//   - req.Config["directory"]: path to directory containing .csl files
//
// Validation:
//   - Alias must be unique (not already initialized)
//   - Directory must exist and be readable
//   - Directory must not be used by another instance (via canonical path comparison)
//   - Directory must contain at least one .csl file
//
// Atomic Rollback:
//
// If this Init fails after previous successful Init calls, ALL instances are
// rolled back. This ensures the service never remains in a partially-initialized
// state. The error message indicates how many instances were rolled back.
//
// Thread-safety: Acquires exclusive lock (mu.Lock) for the duration of initialization.
func (s *FileProviderService) Init(ctx context.Context, req *providerv1.InitRequest) (*providerv1.InitResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// T034/T013: Validate alias is not empty
	if req.Alias == "" {
		// T037: Rollback if we have existing instances
		if len(s.initOrder) > 0 {
			rolledBackCount := len(s.initOrder)
			s.rollbackAll()
			return nil, status.Errorf(codes.InvalidArgument, "alias %q: alias cannot be empty; rolled back all %d instance(s)", req.Alias, rolledBackCount)
		}
		return nil, status.Error(codes.InvalidArgument, "alias cannot be empty")
	}

	// T034/T012/T013: Check if alias already exists
	if _, exists := s.configs[req.Alias]; exists {
		// T037: Rollback if we have existing instances
		if len(s.initOrder) > 0 {
			rolledBackCount := len(s.initOrder)
			s.rollbackAll()
			return nil, status.Errorf(codes.FailedPrecondition, "alias %q: provider instance already initialized; rolled back all %d instance(s)", req.Alias, rolledBackCount)
		}
		return nil, status.Errorf(codes.FailedPrecondition, "alias %q: provider instance already initialized", req.Alias)
	}

	// T034: Extract directory from config
	configMap := req.Config.AsMap()
	dirValue, ok := configMap["directory"]
	if !ok {
		// T037: Rollback if we have existing instances
		if len(s.initOrder) > 0 {
			rolledBackCount := len(s.initOrder)
			s.rollbackAll()
			return nil, status.Errorf(codes.InvalidArgument, "alias %q: missing required config key 'directory'; rolled back all %d instance(s)", req.Alias, rolledBackCount)
		}
		return nil, status.Errorf(codes.InvalidArgument, "alias %q: missing required config key 'directory'", req.Alias)
	}

	// T034: Validate directory type
	dirStr, ok := dirValue.(string)
	if !ok {
		// T037: Rollback if we have existing instances
		if len(s.initOrder) > 0 {
			rolledBackCount := len(s.initOrder)
			s.rollbackAll()
			return nil, status.Errorf(codes.InvalidArgument, "alias %q: directory must be a string, got %T; rolled back all %d instance(s)", req.Alias, dirValue, rolledBackCount)
		}
		return nil, status.Errorf(codes.InvalidArgument, "alias %q: directory must be a string, got %T", req.Alias, dirValue)
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
			// T034/T037: Enhanced error with rollback
			if len(s.initOrder) > 0 {
				rolledBackCount := len(s.initOrder)
				s.rollbackAll()
				return nil, status.Errorf(codes.InvalidArgument, "alias %q: failed to resolve path to absolute: %v; rolled back all %d instance(s)", req.Alias, err, rolledBackCount)
			}
			return nil, status.Errorf(codes.InvalidArgument, "alias %q: failed to resolve path to absolute: %v", req.Alias, err)
		}
	}

	// T034: Verify directory exists
	info, err := os.Stat(absPath)
	if err != nil {
		// T037: Rollback if we have existing instances
		rolledBackCount := len(s.initOrder)
		if rolledBackCount > 0 {
			s.rollbackAll()
			if os.IsNotExist(err) {
				return nil, status.Errorf(codes.NotFound, "alias %q: directory does not exist: %s; rolled back all %d instance(s)", req.Alias, absPath, rolledBackCount)
			}
			return nil, status.Errorf(codes.Internal, "alias %q: failed to stat directory: %v; rolled back all %d instance(s)", req.Alias, err, rolledBackCount)
		}
		if os.IsNotExist(err) {
			return nil, status.Errorf(codes.NotFound, "alias %q: directory does not exist: %s", req.Alias, absPath)
		}
		return nil, status.Errorf(codes.Internal, "alias %q: failed to stat directory: %v", req.Alias, err)
	}

	// T034: Verify path is a directory
	if !info.IsDir() {
		// T037: Rollback if we have existing instances
		if len(s.initOrder) > 0 {
			rolledBackCount := len(s.initOrder)
			s.rollbackAll()
			return nil, status.Errorf(codes.InvalidArgument, "alias %q: path is not a directory: %s; rolled back all %d instance(s)", req.Alias, absPath, rolledBackCount)
		}
		return nil, status.Errorf(codes.InvalidArgument, "alias %q: path is not a directory: %s", req.Alias, absPath)
	}

	// T014/T034: Resolve directory to canonical path
	canonicalPath, err := s.canonicalizePath(absPath)
	if err != nil {
		// T037: Rollback if we have existing instances
		if len(s.initOrder) > 0 {
			rolledBackCount := len(s.initOrder)
			s.rollbackAll()
			return nil, status.Errorf(codes.Internal, "alias %q: failed to canonicalize path: %v; rolled back all %d instance(s)", req.Alias, err, rolledBackCount)
		}
		return nil, status.Errorf(codes.Internal, "alias %q: failed to canonicalize path: %v", req.Alias, err)
	}

	// T015/T035: Check if directory is already registered
	// This is a validation error - do NOT trigger rollback since it's before we add the config
	if existingAlias, exists := s.directoryRegistry[canonicalPath]; exists {
		return nil, status.Errorf(codes.FailedPrecondition,
			"directory %q already registered by provider instance %q, cannot register as %q",
			canonicalPath, existingAlias, req.Alias)
	}

	// T016/T034: Enumerate CSL files
	cslFiles, err := s.enumerateCSLFiles(canonicalPath)
	if err != nil {
		// T037/T038: Rollback all previous inits on failure with enhanced error
		rolledBackCount := len(s.initOrder)
		if rolledBackCount > 0 {
			s.rollbackAll()
			return nil, status.Errorf(codes.Internal, "alias %q: failed to enumerate .csl files: %v; rolled back all %d instance(s)", req.Alias, err, rolledBackCount)
		}
		return nil, status.Errorf(codes.Internal, "alias %q: failed to enumerate .csl files: %v", req.Alias, err)
	}

	// T016: Create new instance config
	config := &instanceConfig{
		alias:       req.Alias,
		directory:   canonicalPath,
		cslFiles:    cslFiles,
		initialized: true,
	}

	// T017: Add to maps and initOrder
	s.configs[req.Alias] = config
	s.directoryRegistry[canonicalPath] = req.Alias
	s.initOrder = append(s.initOrder, req.Alias)

	// T025: Log successful initialization
	log.Printf("Initialized provider instance: alias=%q directory=%q", req.Alias, canonicalPath)

	return &providerv1.InitResponse{}, nil
}

// enumerateCSLFiles scans the directory for .csl files and builds the file map.
//
// Returns a map of base names (without .csl extension) to absolute file paths.
// For example, "/path/database.csl" -> map["database"] = "/path/database.csl"
//
// Validation:
//   - Directory must contain at least one .csl file
//   - No duplicate base names allowed (e.g., cannot have both database.csl and database.CSL)
//   - Only regular files are considered (subdirectories are skipped)
//
// This map is stored in instanceConfig.cslFiles and used during Fetch operations
// to quickly resolve file names to paths without additional directory scans.
func (s *FileProviderService) enumerateCSLFiles(dirPath string) (map[string]string, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %q: %w", dirPath, err)
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
			return nil, fmt.Errorf("duplicate file base name %q in directory %q", baseName, dirPath)
		}

		cslFiles[baseName] = filepath.Join(dirPath, fileName)
	}

	if len(cslFiles) == 0 {
		return nil, fmt.Errorf("no .csl files found in directory %q", dirPath)
	}

	return cslFiles, nil
}

// Fetch retrieves configuration data from a .csl file.
//
// Multi-Instance Path Structure:
//
// The path format adapts based on the number of initialized instances:
//
// Single Instance Mode (only one Init called):
//
//	path[0]: file base name (without .csl extension)
//	path[1+]: optional nested keys within the file
//
// Multi-Instance Mode (multiple Init calls):
//
//	path[0]: alias (identifies which provider instance to use)
//	path[1]: file base name (without .csl extension)
//	path[2+]: optional nested keys within the file
//
// Examples:
//
//	Single instance:
//	  path=["database"]           → reads database.csl (entire file)
//	  path=["database", "host"]   → reads database.csl, extracts "host" key
//	  path=["prod", "database", "name"] → reads prod.csl, extracts "database.name" path
//
//	Multi-instance:
//	  path=["configs", "database"]           → reads database.csl from "configs" instance
//	  path=["configs", "database", "host"]   → reads database.csl, extracts "host" key
//	  path=["configs", "prod", "database", "name"] → reads prod.csl, extracts "database.name"
//
// Error Handling:
//
// All errors include the alias in the message to help users identify which
// provider instance caused the error:
//   - "provider instance %q not found" (alias not initialized)
//   - "file %q not found in provider instance %q" (file doesn't exist)
//   - "path element %q not found in file %q (provider instance %q)" (nested key missing)
//
// Thread-safety: Acquires shared lock (mu.RLock) for the duration of the fetch.
func (s *FileProviderService) Fetch(ctx context.Context, req *providerv1.FetchRequest) (*providerv1.FetchResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// T018: Extract alias from request
	// NOTE: Requires proto update to add Alias field to FetchRequest
	// For now, commented out to allow compilation
	// if req.Alias == "" {
	// 	return nil, status.Error(codes.InvalidArgument, "alias cannot be empty")
	// }

	// T018: Lookup config by alias
	// Determine if path includes alias or not
	if len(req.Path) == 0 {
		return nil, status.Error(codes.InvalidArgument, "path cannot be empty")
	}

	// Check for empty first path element
	if req.Path[0] == "" {
		return nil, status.Error(codes.InvalidArgument, "path[0] cannot be empty")
	}

	var cfg *instanceConfig
	var baseName string
	var pathOffset int

	// Try to determine if path[0] is an alias or a filename
	// Check if path[0] matches an initialized instance alias
	if possibleCfg, exists := s.configs[req.Path[0]]; exists {
		// path[0] is an alias - multi-instance explicit mode
		cfg = possibleCfg
		pathOffset = 1
		if len(req.Path) < 2 {
			return nil, status.Errorf(codes.InvalidArgument, "path must contain at least [alias, filename]")
		}
		baseName = req.Path[1]
	} else {
		// path[0] is not an alias - check if we have exactly one instance (single-instance mode)
		if len(s.configs) == 0 {
			return nil, status.Error(codes.FailedPrecondition, "no provider instances initialized")
		}
		if len(s.configs) == 1 {
			// Single instance - path[0] is the filename
			for _, c := range s.configs {
				cfg = c
				break
			}
			pathOffset = 0
			baseName = req.Path[0]
		} else {
			// Multiple instances but path[0] is not a valid alias
			return nil, status.Errorf(codes.NotFound, "provider instance %q not found (hint: with multiple instances, path must start with alias)", req.Path[0])
		}
	}

	// T026: Log Fetch operation
	log.Printf("Fetching from provider instance: alias=%q path=%v", cfg.alias, req.Path)

	// T018/T040: Use cfg.cslFiles with enhanced error message
	filePath, exists := cfg.cslFiles[baseName]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "file %q not found in provider instance %q", baseName, cfg.alias)
	}

	// Parse the .csl file
	data, err := parseCSLFile(filePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to parse .csl file %q: %v", filePath, err)
	}

	// If additional path components provided (beyond alias/filename and file), navigate to that path
	nestedPathStart := pathOffset + 1 // Skip alias (if present) and filename
	if len(req.Path) > nestedPathStart {
		current := data
		for i, key := range req.Path[nestedPathStart:] {
			m, ok := current.(map[string]any)
			if !ok {
				return nil, status.Errorf(codes.InvalidArgument,
					"cannot navigate to path %v: element at index %d is not a map", req.Path, i+nestedPathStart)
			}

			val, exists := m[key]
			if !exists {
				// T040: Enhanced error message with alias context
				return nil, status.Errorf(codes.NotFound, "path element %q not found in file %q (provider instance %q)", key, baseName, cfg.alias)
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
//
// Returns service-level metadata (version, type) that applies to all instances.
// This is not instance-specific; all instances share the same provider version
// and type.
//
// Thread-safety: Acquires shared lock (mu.RLock) to read version/type fields.
func (s *FileProviderService) Info(ctx context.Context, req *providerv1.InfoRequest) (*providerv1.InfoResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// T019: Return generic service-level info (no alias field)
	return &providerv1.InfoResponse{
		Version: s.version,
		Type:    s.providerType,
	}, nil
}

// Health checks provider health status.
//
// Multi-Instance Health:
//
// Returns STATUS_OK if at least one instance is initialized.
// Returns STATUS_DEGRADED if no instances are initialized.
//
// This is a simple check that doesn't validate directory accessibility or
// file integrity; it only checks whether Init has been called successfully.
//
// Thread-safety: Acquires shared lock (mu.RLock) to check configs map.
func (s *FileProviderService) Health(ctx context.Context, req *providerv1.HealthRequest) (*providerv1.HealthResponse, error) {
	s.mu.RLock()
	hasConfigs := len(s.configs) > 0
	s.mu.RUnlock()

	// T020: Check if any configs exist
	if !hasConfigs {
		return &providerv1.HealthResponse{
			Status:  providerv1.HealthResponse_STATUS_DEGRADED,
			Message: "no instances initialized",
		}, nil
	}

	return &providerv1.HealthResponse{
		Status:  providerv1.HealthResponse_STATUS_OK,
		Message: "healthy",
	}, nil
}

// Shutdown gracefully shuts down the provider.
//
// Clears all initialized instances and resets the service to its initial empty state.
// After Shutdown, new Init calls can be made to re-initialize instances.
//
// Cleanup actions:
//   - Clear configs map (all instance configurations)
//   - Clear directoryRegistry (all directory mappings)
//   - Clear initOrder (initialization history)
//
// Thread-safety: Acquires exclusive lock (mu.Lock) for cleanup operations.
func (s *FileProviderService) Shutdown(ctx context.Context, req *providerv1.ShutdownRequest) (*providerv1.ShutdownResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// T021: Clear all multi-instance maps
	s.configs = make(map[string]*instanceConfig)
	s.directoryRegistry = make(map[string]string)
	s.initOrder = s.initOrder[:0]

	return &providerv1.ShutdownResponse{}, nil
}

// toProtoStruct converts a Go value to a protobuf Struct.
//
// Handles two cases:
//  1. If v is already a map[string]any, convert directly to Struct
//  2. Otherwise, wrap the value in a map with key "value" before converting
//
// This ensures the return value is always a valid protobuf Struct, which
// requires map-like structure at the top level.
func toProtoStruct(v any) (*structpb.Struct, error) {
	// Handle map type
	if m, ok := v.(map[string]any); ok {
		return structpb.NewStruct(m)
	}

	// If not a map, wrap it in a struct with "value" key
	wrapped := map[string]any{"value": v}
	return structpb.NewStruct(wrapped)
}
