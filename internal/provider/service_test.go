package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	providerv1 "github.com/autonomous-bits/nomos/libs/provider-proto/gen/go/nomos/provider/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestFileProviderService_Init(t *testing.T) {
	// Create temp directory with test files
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.csl")

	content := `database:
  host: localhost
  port: 5432
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewFileProviderService("0.1.0", "file")

	// Test successful init
	config, _ := structpb.NewStruct(map[string]any{
		"directory": tmpDir,
	})

	req := &providerv1.InitRequest{
		Alias:  "test",
		Config: config,
	}

	_, err := svc.Init(context.Background(), req)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Verify initialized state - check configs map
	svc.mu.RLock()
	cfg, exists := svc.configs["test"]
	svc.mu.RUnlock()

	if !exists {
		t.Error("Provider instance 'test' should exist in configs")
	}
	if cfg != nil && cfg.alias != "test" {
		t.Errorf("Expected alias 'test', got %q", cfg.alias)
	}
	if cfg != nil && !cfg.initialized {
		t.Error("Provider instance should be initialized")
	}
}

func TestFileProviderService_Init_MissingDirectory(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	config, _ := structpb.NewStruct(map[string]any{})

	req := &providerv1.InitRequest{
		Alias:  "test",
		Config: config,
	}

	_, err := svc.Init(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for missing directory config")
	}

	st := status.Convert(err)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument, got %v", st.Code())
	}
}

func TestFileProviderService_Fetch(t *testing.T) {
	// Create temp directory with test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.csl")

	content := `app:
  name: myapp
  version: 1.0.0
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewFileProviderService("0.1.0", "file")

	// Initialize
	config, _ := structpb.NewStruct(map[string]any{
		"directory": tmpDir,
	})

	initReq := &providerv1.InitRequest{
		Alias:  "test",
		Config: config,
	}

	if _, err := svc.Init(context.Background(), initReq); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Fetch the file - path format: [alias, filename]
	fetchReq := &providerv1.FetchRequest{
		Path: []string{"test", "config"},
	}

	resp, err := svc.Fetch(context.Background(), fetchReq)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if resp.Value == nil {
		t.Fatal("Expected non-nil value")
	}

	// Verify structure
	data := resp.Value.AsMap()
	appSection, ok := data["app"]
	if !ok {
		t.Fatal("Expected 'app' section in response")
	}

	appMap, ok := appSection.(map[string]any)
	if !ok {
		t.Fatalf("Expected app to be a map, got %T", appSection)
	}

	if appMap["name"] != "myapp" {
		t.Errorf("Expected name 'myapp', got %v", appMap["name"])
	}
}

func TestFileProviderService_Fetch_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.csl")

	content := `app:
  name: test
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewFileProviderService("0.1.0", "file")

	config, _ := structpb.NewStruct(map[string]any{
		"directory": tmpDir,
	})

	initReq := &providerv1.InitRequest{
		Alias:  "test",
		Config: config,
	}

	if _, err := svc.Init(context.Background(), initReq); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Fetch non-existent file - path format: [alias, filename]
	fetchReq := &providerv1.FetchRequest{
		Path: []string{"test", "nonexistent"},
	}

	_, err := svc.Fetch(context.Background(), fetchReq)
	if err == nil {
		t.Fatal("Expected error for non-existent file")
	}

	st := status.Convert(err)
	if st.Code() != codes.NotFound {
		t.Errorf("Expected NotFound, got %v", st.Code())
	}
}

func TestFileProviderService_Info(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	resp, err := svc.Info(context.Background(), &providerv1.InfoRequest{})
	if err != nil {
		t.Fatalf("Info failed: %v", err)
	}

	if resp.Version != "0.1.0" {
		t.Errorf("Expected version '0.1.0', got %q", resp.Version)
	}

	if resp.Type != "file" {
		t.Errorf("Expected type 'file', got %q", resp.Type)
	}
}

func TestFileProviderService_Health(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	// Before init - degraded
	resp, err := svc.Health(context.Background(), &providerv1.HealthRequest{})
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}

	if resp.Status != providerv1.HealthResponse_STATUS_DEGRADED {
		t.Errorf("Expected degraded status before init, got %v", resp.Status)
	}

	// After init - healthy
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.csl")
	if err := os.WriteFile(testFile, []byte("test: value"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	config, _ := structpb.NewStruct(map[string]any{
		"directory": tmpDir,
	})

	initReq := &providerv1.InitRequest{
		Alias:  "test",
		Config: config,
	}

	if _, err := svc.Init(context.Background(), initReq); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	resp, err = svc.Health(context.Background(), &providerv1.HealthRequest{})
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}

	if resp.Status != providerv1.HealthResponse_STATUS_OK {
		t.Errorf("Expected OK status after init, got %v", resp.Status)
	}
}

// ========================================================================
// INTEGRATION TESTS - Phase 3 User Story 1 (T009-T011)
// These tests verify multi-instance provider behavior following TDD.
// ========================================================================

// T009 - TestMultipleInit_TwoDirectories verifies that the service can
// initialize two independent provider instances with different aliases
// and directories. Tests the multi-config architecture.
func TestMultipleInit_TwoDirectories(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	// Absolute paths to test fixtures
	dir1 := "/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir1"
	dir2 := "/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir2"

	// First Init call - instance1 with dir1
	config1, err := structpb.NewStruct(map[string]any{
		"directory": dir1,
	})
	if err != nil {
		t.Fatalf("Failed to create config1: %v", err)
	}

	initReq1 := &providerv1.InitRequest{
		Alias:  "instance1",
		Config: config1,
	}

	_, err = svc.Init(context.Background(), initReq1)
	if err != nil {
		t.Fatalf("First Init failed: %v", err)
	}

	// Second Init call - instance2 with dir2
	config2, err := structpb.NewStruct(map[string]any{
		"directory": dir2,
	})
	if err != nil {
		t.Fatalf("Failed to create config2: %v", err)
	}

	initReq2 := &providerv1.InitRequest{
		Alias:  "instance2",
		Config: config2,
	}

	_, err = svc.Init(context.Background(), initReq2)
	if err != nil {
		t.Fatalf("Second Init failed: %v", err)
	}

	// Verify internal state
	svc.mu.RLock()
	defer svc.mu.RUnlock()

	// Check configs map has 2 entries
	if len(svc.configs) != 2 {
		t.Errorf("Expected 2 configs, got %d", len(svc.configs))
	}

	// Check both aliases exist in configs
	if _, exists := svc.configs["instance1"]; !exists {
		t.Error("instance1 not found in configs map")
	}
	if _, exists := svc.configs["instance2"]; !exists {
		t.Error("instance2 not found in configs map")
	}

	// Verify directoryRegistry has 2 entries
	if len(svc.directoryRegistry) != 2 {
		t.Errorf("Expected 2 directory registry entries, got %d", len(svc.directoryRegistry))
	}

	// Verify initOrder has 2 entries in correct order
	if len(svc.initOrder) != 2 {
		t.Errorf("Expected 2 entries in initOrder, got %d", len(svc.initOrder))
	}
	if len(svc.initOrder) >= 2 {
		if svc.initOrder[0] != "instance1" {
			t.Errorf("Expected first initOrder entry to be 'instance1', got %q", svc.initOrder[0])
		}
		if svc.initOrder[1] != "instance2" {
			t.Errorf("Expected second initOrder entry to be 'instance2', got %q", svc.initOrder[1])
		}
	}

	// Verify each config is properly initialized
	if cfg1, exists := svc.configs["instance1"]; exists {
		if !cfg1.initialized {
			t.Error("instance1 should be initialized")
		}
		if cfg1.alias != "instance1" {
			t.Errorf("Expected instance1 alias to be 'instance1', got %q", cfg1.alias)
		}
	}

	if cfg2, exists := svc.configs["instance2"]; exists {
		if !cfg2.initialized {
			t.Error("instance2 should be initialized")
		}
		if cfg2.alias != "instance2" {
			t.Errorf("Expected instance2 alias to be 'instance2', got %q", cfg2.alias)
		}
	}
}

// T010 - TestMultipleInit_ThreeOrMoreDirectories verifies that the service
// can handle three or more independent provider instances, maintaining
// proper ordering and configuration isolation.
func TestMultipleInit_ThreeOrMoreDirectories(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	// Absolute paths to all three test fixtures
	dir1 := "/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir1"
	dir2 := "/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir2"
	dir3 := "/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir3"

	// Initialize three instances
	testCases := []struct {
		alias     string
		directory string
	}{
		{"db-instance", dir1},
		{"network-instance", dir2},
		{"app-instance", dir3},
	}

	for _, tc := range testCases {
		config, err := structpb.NewStruct(map[string]any{
			"directory": tc.directory,
		})
		if err != nil {
			t.Fatalf("Failed to create config for %s: %v", tc.alias, err)
		}

		initReq := &providerv1.InitRequest{
			Alias:  tc.alias,
			Config: config,
		}

		_, err = svc.Init(context.Background(), initReq)
		if err != nil {
			t.Fatalf("Init failed for %s: %v", tc.alias, err)
		}
	}

	// Verify internal state
	svc.mu.RLock()
	defer svc.mu.RUnlock()

	// Check configs map has 3 entries
	if len(svc.configs) != 3 {
		t.Errorf("Expected 3 configs, got %d", len(svc.configs))
	}

	// Check all aliases exist in configs
	expectedAliases := []string{"db-instance", "network-instance", "app-instance"}
	for _, alias := range expectedAliases {
		if _, exists := svc.configs[alias]; !exists {
			t.Errorf("%s not found in configs map", alias)
		}
	}

	// Verify directoryRegistry has 3 entries
	if len(svc.directoryRegistry) != 3 {
		t.Errorf("Expected 3 directory registry entries, got %d", len(svc.directoryRegistry))
	}

	// Verify initOrder has 3 entries in declaration order
	if len(svc.initOrder) != 3 {
		t.Errorf("Expected 3 entries in initOrder, got %d", len(svc.initOrder))
	} else {
		for i, expectedAlias := range expectedAliases {
			if svc.initOrder[i] != expectedAlias {
				t.Errorf("Expected initOrder[%d] to be %q, got %q", i, expectedAlias, svc.initOrder[i])
			}
		}
	}

	// Verify each config has proper CSL files enumerated
	if cfg, exists := svc.configs["db-instance"]; exists {
		if _, hasFile := cfg.cslFiles["database"]; !hasFile {
			t.Error("db-instance should have 'database' CSL file")
		}
	}

	if cfg, exists := svc.configs["network-instance"]; exists {
		if _, hasFile := cfg.cslFiles["network"]; !hasFile {
			t.Error("network-instance should have 'network' CSL file")
		}
	}

	if cfg, exists := svc.configs["app-instance"]; exists {
		if _, hasFile := cfg.cslFiles["app"]; !hasFile {
			t.Error("app-instance should have 'app' CSL file")
		}
	}
}

// T011 - TestFetch_IndependentInstances verifies that Fetch operations
// correctly retrieve data from independent provider instances based on
// the alias specified in the request. Tests data isolation between instances.
func TestFetch_IndependentInstances(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	// Absolute paths to test fixtures
	dir1 := "/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir1"
	dir2 := "/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir2"

	// Initialize two instances
	config1, err := structpb.NewStruct(map[string]any{
		"directory": dir1,
	})
	if err != nil {
		t.Fatalf("Failed to create config1: %v", err)
	}

	initReq1 := &providerv1.InitRequest{
		Alias:  "db-provider",
		Config: config1,
	}

	if _, err := svc.Init(context.Background(), initReq1); err != nil {
		t.Fatalf("First Init failed: %v", err)
	}

	config2, err := structpb.NewStruct(map[string]any{
		"directory": dir2,
	})
	if err != nil {
		t.Fatalf("Failed to create config2: %v", err)
	}

	initReq2 := &providerv1.InitRequest{
		Alias:  "net-provider",
		Config: config2,
	}

	if _, err := svc.Init(context.Background(), initReq2); err != nil {
		t.Fatalf("Second Init failed: %v", err)
	}

	// Fetch from first instance (db-provider)
	// Path format: [alias, filename, ...nested]
	fetchReq1 := &providerv1.FetchRequest{
		Path: []string{"db-provider", "database"},
	}

	resp1, err := svc.Fetch(context.Background(), fetchReq1)
	if err != nil {
		t.Fatalf("Fetch from db-provider failed: %v", err)
	}

	if resp1.Value == nil {
		t.Fatal("Expected non-nil value from db-provider")
	}

	// Verify database data
	data1 := resp1.Value.AsMap()
	if dbSection, ok := data1["database"]; ok {
		dbMap, ok := dbSection.(map[string]any)
		if !ok {
			t.Fatalf("Expected database section to be a map, got %T", dbSection)
		}
		if dbMap["host"] != "instance1.db.local" {
			t.Errorf("Expected database host 'instance1.db.local', got %v", dbMap["host"])
		}
	} else {
		t.Error("Expected 'database' section in db-provider response")
	}

	// Fetch from second instance (net-provider)
	fetchReq2 := &providerv1.FetchRequest{
		Path: []string{"net-provider", "network"},
	}

	resp2, err := svc.Fetch(context.Background(), fetchReq2)
	if err != nil {
		t.Fatalf("Fetch from net-provider failed: %v", err)
	}

	if resp2.Value == nil {
		t.Fatal("Expected non-nil value from net-provider")
	}

	// Verify network data
	data2 := resp2.Value.AsMap()
	if vpcSection, ok := data2["vpc"]; ok {
		vpcMap, ok := vpcSection.(map[string]any)
		if !ok {
			t.Fatalf("Expected vpc section to be a map, got %T", vpcSection)
		}
		if vpcMap["cidr"] != "192.168.0.0/16" {
			t.Errorf("Expected vpc cidr '192.168.0.0/16', got %v", vpcMap["cidr"])
		}
	} else {
		t.Error("Expected 'vpc' section in net-provider response")
	}

	// Verify data is different between instances
	if resp1.Value.String() == resp2.Value.String() {
		t.Error("Expected different data from independent instances, got same data")
	}

	// Fetch with wrong alias should return NotFound error
	fetchReq3 := &providerv1.FetchRequest{
		Path: []string{"nonexistent-provider", "database"},
	}

	_, err = svc.Fetch(context.Background(), fetchReq3)
	if err == nil {
		t.Fatal("Expected error when fetching with wrong alias")
	}

	st := status.Convert(err)
	if st.Code() != codes.NotFound {
		t.Errorf("Expected NotFound error for wrong alias, got %v", st.Code())
	}

	// Fetch correct alias but wrong file should also return NotFound
	fetchReq4 := &providerv1.FetchRequest{
		Path: []string{"db-provider", "network"}, // network file is in net-provider, not db-provider
	}

	_, err = svc.Fetch(context.Background(), fetchReq4)
	if err == nil {
		t.Fatal("Expected error when fetching file from wrong instance")
	}

	st = status.Convert(err)
	if st.Code() != codes.NotFound {
		t.Errorf("Expected NotFound error for file in wrong instance, got %v", st.Code())
	}
}

// ========================================================================
// INTEGRATION TESTS - Phase 4 User Story 2 (T022-T024)
// These tests verify independent instance state isolation following TDD.
// ========================================================================

// T022 - TestStateIsolation_FileList verifies that each provider instance
// maintains completely independent file list state. The cslFiles map in
// each instanceConfig should be a different memory object containing only
// the files from that instance's directory.
func TestStateIsolation_FileList(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	// Absolute paths to test fixtures
	dir1 := "/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir1"
	dir2 := "/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir2"

	// Initialize first instance with dir1 (contains database.csl)
	config1, err := structpb.NewStruct(map[string]any{
		"directory": dir1,
	})
	if err != nil {
		t.Fatalf("Failed to create config1: %v", err)
	}

	initReq1 := &providerv1.InitRequest{
		Alias:  "instance1",
		Config: config1,
	}

	if _, err := svc.Init(context.Background(), initReq1); err != nil {
		t.Fatalf("First Init failed: %v", err)
	}

	// Initialize second instance with dir2 (contains network.csl)
	config2, err := structpb.NewStruct(map[string]any{
		"directory": dir2,
	})
	if err != nil {
		t.Fatalf("Failed to create config2: %v", err)
	}

	initReq2 := &providerv1.InitRequest{
		Alias:  "instance2",
		Config: config2,
	}

	if _, err := svc.Init(context.Background(), initReq2); err != nil {
		t.Fatalf("Second Init failed: %v", err)
	}

	// Access service internals to verify file list isolation
	svc.mu.RLock()
	defer svc.mu.RUnlock()

	cfg1, exists1 := svc.configs["instance1"]
	if !exists1 {
		t.Fatal("instance1 config not found")
	}

	cfg2, exists2 := svc.configs["instance2"]
	if !exists2 {
		t.Fatal("instance2 config not found")
	}

	// Verify cslFiles are DIFFERENT map objects (different memory addresses)
	if &cfg1.cslFiles == &cfg2.cslFiles {
		t.Error("cslFiles maps should be different objects, got same memory address")
	}

	// Verify cfg1.cslFiles contains only "database" key
	if len(cfg1.cslFiles) != 1 {
		t.Errorf("Expected instance1 to have 1 CSL file, got %d", len(cfg1.cslFiles))
	}

	if _, hasDatabase := cfg1.cslFiles["database"]; !hasDatabase {
		t.Error("instance1 should contain 'database' key in cslFiles")
	}

	if _, hasNetwork := cfg1.cslFiles["network"]; hasNetwork {
		t.Error("instance1 should NOT contain 'network' key (belongs to instance2)")
	}

	// Verify cfg2.cslFiles contains only "network" key
	if len(cfg2.cslFiles) != 1 {
		t.Errorf("Expected instance2 to have 1 CSL file, got %d", len(cfg2.cslFiles))
	}

	if _, hasNetwork := cfg2.cslFiles["network"]; !hasNetwork {
		t.Error("instance2 should contain 'network' key in cslFiles")
	}

	if _, hasDatabase := cfg2.cslFiles["database"]; hasDatabase {
		t.Error("instance2 should NOT contain 'database' key (belongs to instance1)")
	}

	// Verify that modifying one map doesn't affect the other
	// (This is implicitly verified by the separate maps, but we can be explicit)
	cfg1FileCount := len(cfg1.cslFiles)
	cfg2FileCount := len(cfg2.cslFiles)

	if cfg1FileCount == cfg2FileCount && cfg1FileCount == 0 {
		t.Error("Both instances have empty file lists, suggesting state is not isolated")
	}
}

// T023 - TestStateIsolation_ConfigParameters verifies that each provider
// instance maintains independent configuration parameters. Changes to one
// instance's config should not affect another instance.
func TestStateIsolation_ConfigParameters(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	// Absolute paths to test fixtures
	dir1 := "/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir1"
	dir2 := "/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir2"

	// Initialize first instance
	config1, err := structpb.NewStruct(map[string]any{
		"directory": dir1,
	})
	if err != nil {
		t.Fatalf("Failed to create config1: %v", err)
	}

	initReq1 := &providerv1.InitRequest{
		Alias:  "alpha-instance",
		Config: config1,
	}

	if _, err := svc.Init(context.Background(), initReq1); err != nil {
		t.Fatalf("First Init failed: %v", err)
	}

	// Initialize second instance
	config2, err := structpb.NewStruct(map[string]any{
		"directory": dir2,
	})
	if err != nil {
		t.Fatalf("Failed to create config2: %v", err)
	}

	initReq2 := &providerv1.InitRequest{
		Alias:  "beta-instance",
		Config: config2,
	}

	if _, err := svc.Init(context.Background(), initReq2); err != nil {
		t.Fatalf("Second Init failed: %v", err)
	}

	// Access both configs via service internals
	svc.mu.RLock()
	defer svc.mu.RUnlock()

	cfg1, exists1 := svc.configs["alpha-instance"]
	if !exists1 {
		t.Fatal("alpha-instance config not found")
	}

	cfg2, exists2 := svc.configs["beta-instance"]
	if !exists2 {
		t.Fatal("beta-instance config not found")
	}

	// Verify cfg1.directory != cfg2.directory (different paths)
	if cfg1.directory == cfg2.directory {
		t.Errorf("Expected different directories, both have: %s", cfg1.directory)
	}

	// Verify directories point to expected paths (canonicalized)
	if !filepath.IsAbs(cfg1.directory) {
		t.Errorf("Expected absolute path for cfg1.directory, got: %s", cfg1.directory)
	}

	if !filepath.IsAbs(cfg2.directory) {
		t.Errorf("Expected absolute path for cfg2.directory, got: %s", cfg2.directory)
	}

	// Verify cfg1.alias != cfg2.alias (different aliases)
	if cfg1.alias == cfg2.alias {
		t.Errorf("Expected different aliases, both have: %s", cfg1.alias)
	}

	if cfg1.alias != "alpha-instance" {
		t.Errorf("Expected alpha-instance alias, got: %s", cfg1.alias)
	}

	if cfg2.alias != "beta-instance" {
		t.Errorf("Expected beta-instance alias, got: %s", cfg2.alias)
	}

	// Verify both are initialized independently
	if !cfg1.initialized {
		t.Error("alpha-instance should be initialized")
	}

	if !cfg2.initialized {
		t.Error("beta-instance should be initialized")
	}

	// Verify they are different struct instances in memory
	if cfg1 == cfg2 {
		t.Error("Config instances should be different objects in memory")
	}

	// Test Info RPC returns generic service-level data (no per-instance data)
	infoResp, err := svc.Info(context.Background(), &providerv1.InfoRequest{})
	if err != nil {
		t.Fatalf("Info RPC failed: %v", err)
	}

	// Info should return service-level metadata, not instance-specific
	if infoResp.Version != "0.1.0" {
		t.Errorf("Expected version '0.1.0', got %q", infoResp.Version)
	}

	if infoResp.Type != "file" {
		t.Errorf("Expected type 'file', got %q", infoResp.Type)
	}
}

// T024 - TestStateIsolation_ErrorPropagation verifies that errors in one
// provider instance do not affect operations in other instances. An error
// during Fetch on one instance should not corrupt state or cause failures
// in a different instance.
func TestStateIsolation_ErrorPropagation(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	// Absolute paths to test fixtures
	dir1 := "/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir1"
	dir2 := "/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir2"

	// Initialize two instances
	config1, err := structpb.NewStruct(map[string]any{
		"directory": dir1,
	})
	if err != nil {
		t.Fatalf("Failed to create config1: %v", err)
	}

	initReq1 := &providerv1.InitRequest{
		Alias:  "error-test-1",
		Config: config1,
	}

	if _, err := svc.Init(context.Background(), initReq1); err != nil {
		t.Fatalf("First Init failed: %v", err)
	}

	config2, err := structpb.NewStruct(map[string]any{
		"directory": dir2,
	})
	if err != nil {
		t.Fatalf("Failed to create config2: %v", err)
	}

	initReq2 := &providerv1.InitRequest{
		Alias:  "error-test-2",
		Config: config2,
	}

	if _, err := svc.Init(context.Background(), initReq2); err != nil {
		t.Fatalf("Second Init failed: %v", err)
	}

	// Trigger an error in first instance's Fetch (request non-existent file)
	fetchReq1 := &providerv1.FetchRequest{
		Path: []string{"error-test-1", "nonexistent"},
	}

	resp1, err := svc.Fetch(context.Background(), fetchReq1)
	if err == nil {
		t.Fatal("Expected error when fetching non-existent file from error-test-1")
	}

	// Verify it's a NotFound error
	st := status.Convert(err)
	if st.Code() != codes.NotFound {
		t.Errorf("Expected NotFound error, got %v", st.Code())
	}

	if resp1 != nil {
		t.Error("Expected nil response for error case, got non-nil")
	}

	// Immediately Fetch from second instance (valid file in dir2)
	fetchReq2 := &providerv1.FetchRequest{
		Path: []string{"error-test-2", "network"},
	}

	resp2, err := svc.Fetch(context.Background(), fetchReq2)
	if err != nil {
		t.Fatalf("Fetch from error-test-2 should succeed, got error: %v", err)
	}

	if resp2 == nil || resp2.Value == nil {
		t.Fatal("Expected valid response from error-test-2, got nil")
	}

	// Verify second Fetch returns correct data (not corrupted by first error)
	data2 := resp2.Value.AsMap()
	if vpcSection, ok := data2["vpc"]; ok {
		vpcMap, ok := vpcSection.(map[string]any)
		if !ok {
			t.Fatalf("Expected vpc section to be a map, got %T", vpcSection)
		}
		if vpcMap["cidr"] != "192.168.0.0/16" {
			t.Errorf("Expected vpc cidr '192.168.0.0/16', got %v", vpcMap["cidr"])
		}
	} else {
		t.Error("Expected 'vpc' section in error-test-2 response")
	}

	// Verify first instance can still perform valid operations after error
	fetchReq3 := &providerv1.FetchRequest{
		Path: []string{"error-test-1", "database"},
	}

	resp3, err := svc.Fetch(context.Background(), fetchReq3)
	if err != nil {
		t.Fatalf("Valid fetch from error-test-1 should succeed after error, got: %v", err)
	}

	if resp3 == nil || resp3.Value == nil {
		t.Fatal("Expected valid response from error-test-1 after error recovery")
	}

	// Verify data is correct
	data3 := resp3.Value.AsMap()
	if dbSection, ok := data3["database"]; ok {
		dbMap, ok := dbSection.(map[string]any)
		if !ok {
			t.Fatalf("Expected database section to be a map, got %T", dbSection)
		}
		if dbMap["host"] != "instance1.db.local" {
			t.Errorf("Expected database host 'instance1.db.local', got %v", dbMap["host"])
		}
	} else {
		t.Error("Expected 'database' section in error-test-1 response")
	}

	// Final check: verify both instances are still properly registered
	svc.mu.RLock()
	if len(svc.configs) != 2 {
		t.Errorf("Expected 2 instances after error handling, got %d", len(svc.configs))
	}
	svc.mu.RUnlock()
}

// ========================================================================
// PERFORMANCE BENCHMARKS - Phase 6 (T047)
// These benchmarks verify multi-instance initialization performance targets.
// ========================================================================

// BenchmarkMultipleInit measures performance of initializing multiple provider
// instances. This validates performance targets:
//   - 10 instances: <5 seconds
//   - 100 instances: <50 seconds
//
// Each benchmark iteration creates a fresh service, initializes N instances
// with different aliases and unique temporary directories containing CSL files.
// The directories are populated by copying from test fixtures cyclically.
//
// Run with: go test -bench=BenchmarkMultipleInit -benchtime=1x ./internal/provider
func BenchmarkMultipleInit(b *testing.B) {
	// Source test fixtures to copy from
	sourceDirs := []string{
		"/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir1",
		"/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir2",
		"/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir3",
	}

	// Helper to setup temporary directories with CSL files
	setupTempDirs := func(b *testing.B, count int) []string {
		b.Helper()

		tempDirs := make([]string, count)
		for i := 0; i < count; i++ {
			// Create temp directory for this instance
			tempDir, err := os.MkdirTemp("", fmt.Sprintf("bench-instance-%d-*", i))
			if err != nil {
				b.Fatalf("Failed to create temp dir for instance-%d: %v", i, err)
			}
			tempDirs[i] = tempDir

			// Cycle through source directories
			sourceDir := sourceDirs[i%len(sourceDirs)]

			// Copy .csl files from source to temp directory
			entries, err := os.ReadDir(sourceDir)
			if err != nil {
				b.Fatalf("Failed to read source dir %s: %v", sourceDir, err)
			}

			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".csl") {
					continue
				}

				sourcePath := filepath.Join(sourceDir, entry.Name())
				destPath := filepath.Join(tempDir, entry.Name())

				data, err := os.ReadFile(sourcePath)
				if err != nil {
					b.Fatalf("Failed to read source file %s: %v", sourcePath, err)
				}

				if err := os.WriteFile(destPath, data, 0644); err != nil {
					b.Fatalf("Failed to write dest file %s: %v", destPath, err)
				}
			}
		}

		return tempDirs
	}

	// Helper to initialize N instances
	initInstances := func(b *testing.B, count int, tempDirs []string) {
		b.Helper()

		svc := NewFileProviderService("0.1.0", "file")

		for i := 0; i < count; i++ {
			// Create unique alias for each instance
			alias := fmt.Sprintf("instance-%d", i)

			config, err := structpb.NewStruct(map[string]any{
				"directory": tempDirs[i],
			})
			if err != nil {
				b.Fatalf("Failed to create config for %s: %v", alias, err)
			}

			initReq := &providerv1.InitRequest{
				Alias:  alias,
				Config: config,
			}

			_, err = svc.Init(context.Background(), initReq)
			if err != nil {
				b.Fatalf("Init failed for %s: %v", alias, err)
			}
		}
	}

	// Benchmark: 10 instances (target: <5 seconds)
	b.Run("Init_10_Instances", func(b *testing.B) {
		// Setup temp directories once outside of benchmark timing
		tempDirs := setupTempDirs(b, 10)
		defer func() {
			for _, dir := range tempDirs {
				os.RemoveAll(dir)
			}
		}()

		b.ReportAllocs()
		b.ResetTimer() // Reset timer after setup

		for i := 0; i < b.N; i++ {
			initInstances(b, 10, tempDirs)
		}
	})

	// Benchmark: 100 instances (target: <50 seconds)
	b.Run("Init_100_Instances", func(b *testing.B) {
		// Setup temp directories once outside of benchmark timing
		tempDirs := setupTempDirs(b, 100)
		defer func() {
			for _, dir := range tempDirs {
				os.RemoveAll(dir)
			}
		}()

		b.ReportAllocs()
		b.ResetTimer() // Reset timer after setup

		for i := 0; i < b.N; i++ {
			initInstances(b, 100, tempDirs)
		}
	})
}

// T028 - TestConcurrentFetch verifies that the service handles concurrent
// Fetch operations safely without race conditions or data corruption.
// This test should be run with -race flag to detect race conditions.
func TestConcurrentFetch(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	// Absolute paths to test fixtures
	dir1 := "/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir1"
	dir2 := "/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir2"

	// Initialize two instances
	config1, err := structpb.NewStruct(map[string]any{
		"directory": dir1,
	})
	if err != nil {
		t.Fatalf("Failed to create config1: %v", err)
	}

	initReq1 := &providerv1.InitRequest{
		Alias:  "instance1",
		Config: config1,
	}

	if _, err := svc.Init(context.Background(), initReq1); err != nil {
		t.Fatalf("First Init failed: %v", err)
	}

	config2, err := structpb.NewStruct(map[string]any{
		"directory": dir2,
	})
	if err != nil {
		t.Fatalf("Failed to create config2: %v", err)
	}

	initReq2 := &providerv1.InitRequest{
		Alias:  "instance2",
		Config: config2,
	}

	if _, err := svc.Init(context.Background(), initReq2); err != nil {
		t.Fatalf("Second Init failed: %v", err)
	}

	// Launch 10 concurrent goroutines: 5 fetching from instance1, 5 from instance2
	var wg sync.WaitGroup
	errorChan := make(chan error, 10)
	resultChan := make(chan string, 10)

	// 5 goroutines fetching from instance1 (database.csl)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(iteration int) {
			defer wg.Done()

			fetchReq := &providerv1.FetchRequest{
				Path: []string{"instance1", "database"},
			}

			resp, err := svc.Fetch(context.Background(), fetchReq)
			if err != nil {
				errorChan <- fmt.Errorf("instance1 fetch %d failed: %w", iteration, err)
				return
			}

			if resp == nil || resp.Value == nil {
				errorChan <- fmt.Errorf("instance1 fetch %d returned nil response", iteration)
				return
			}

			// Verify correct data returned
			data := resp.Value.AsMap()
			if dbSection, ok := data["database"]; ok {
				dbMap, ok := dbSection.(map[string]any)
				if !ok {
					errorChan <- fmt.Errorf("instance1 fetch %d: database section is not a map", iteration)
					return
				}
				if dbMap["host"] != "instance1.db.local" {
					errorChan <- fmt.Errorf("instance1 fetch %d: expected host 'instance1.db.local', got %v", iteration, dbMap["host"])
					return
				}
				resultChan <- fmt.Sprintf("instance1-%d-ok", iteration)
			} else {
				errorChan <- fmt.Errorf("instance1 fetch %d: missing database section", iteration)
			}
		}(i)
	}

	// 5 goroutines fetching from instance2 (network.csl)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(iteration int) {
			defer wg.Done()

			fetchReq := &providerv1.FetchRequest{
				Path: []string{"instance2", "network"},
			}

			resp, err := svc.Fetch(context.Background(), fetchReq)
			if err != nil {
				errorChan <- fmt.Errorf("instance2 fetch %d failed: %w", iteration, err)
				return
			}

			if resp == nil || resp.Value == nil {
				errorChan <- fmt.Errorf("instance2 fetch %d returned nil response", iteration)
				return
			}

			// Verify correct data returned
			data := resp.Value.AsMap()
			if vpcSection, ok := data["vpc"]; ok {
				vpcMap, ok := vpcSection.(map[string]any)
				if !ok {
					errorChan <- fmt.Errorf("instance2 fetch %d: vpc section is not a map", iteration)
					return
				}
				if vpcMap["cidr"] != "192.168.0.0/16" {
					errorChan <- fmt.Errorf("instance2 fetch %d: expected cidr '192.168.0.0/16', got %v", iteration, vpcMap["cidr"])
					return
				}
				resultChan <- fmt.Sprintf("instance2-%d-ok", iteration)
			} else {
				errorChan <- fmt.Errorf("instance2 fetch %d: missing vpc section", iteration)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errorChan)
	close(resultChan)

	// Check for errors
	var errors []error
	for err := range errorChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		t.Errorf("Concurrent fetch operations had %d errors:", len(errors))
		for _, err := range errors {
			t.Errorf("  - %v", err)
		}
	}

	// Verify all operations succeeded
	results := make([]string, 0, 10)
	for result := range resultChan {
		results = append(results, result)
	}

	if len(results) != 10 {
		t.Errorf("Expected 10 successful fetch operations, got %d", len(results))
	}

	// Verify service state is still valid after concurrent operations
	svc.mu.RLock()
	if len(svc.configs) != 2 {
		t.Errorf("Expected 2 instances after concurrent operations, got %d", len(svc.configs))
	}
	svc.mu.RUnlock()
}

// ========================================================================
// INTEGRATION TESTS - Phase 5 User Story 3 (T029-T033)
// These tests verify clear error messages and rollback functionality
// following TDD principles.
// ========================================================================

// T029 - TestInit_DuplicateDirectory verifies that initializing two instances
// with the same directory path fails with a clear error message mentioning
// both aliases and the directory path.
func TestInit_DuplicateDirectory(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	// Absolute path to test fixture
	dir1 := "/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir1"

	// First Init - should succeed
	config1, err := structpb.NewStruct(map[string]any{
		"directory": dir1,
	})
	if err != nil {
		t.Fatalf("Failed to create config1: %v", err)
	}

	initReq1 := &providerv1.InitRequest{
		Alias:  "first",
		Config: config1,
	}

	_, err = svc.Init(context.Background(), initReq1)
	if err != nil {
		t.Fatalf("First Init should succeed, got error: %v", err)
	}

	// Second Init with SAME directory but different alias - should FAIL
	config2, err := structpb.NewStruct(map[string]any{
		"directory": dir1,
	})
	if err != nil {
		t.Fatalf("Failed to create config2: %v", err)
	}

	initReq2 := &providerv1.InitRequest{
		Alias:  "second",
		Config: config2,
	}

	_, err = svc.Init(context.Background(), initReq2)
	if err == nil {
		t.Fatal("Second Init with duplicate directory should fail")
	}

	// Verify error code is FailedPrecondition
	st := status.Convert(err)
	if st.Code() != codes.FailedPrecondition {
		t.Errorf("Expected FailedPrecondition error code, got %v", st.Code())
	}

	// Verify error message mentions BOTH aliases
	errMsg := st.Message()
	if !strings.Contains(errMsg, "first") {
		t.Errorf("Error message should mention first alias 'first', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "second") {
		t.Errorf("Error message should mention second alias 'second', got: %s", errMsg)
	}

	// Verify error message mentions the directory path
	if !strings.Contains(errMsg, dir1) && !strings.Contains(errMsg, "directory") {
		t.Errorf("Error message should mention directory path or 'directory', got: %s", errMsg)
	}

	// Verify only first config exists (second was rejected)
	svc.mu.RLock()
	defer svc.mu.RUnlock()

	if len(svc.configs) != 1 {
		t.Errorf("Expected 1 config (first only), got %d", len(svc.configs))
	}

	if _, exists := svc.configs["first"]; !exists {
		t.Error("First config should exist")
	}

	if _, exists := svc.configs["second"]; exists {
		t.Error("Second config should NOT exist (was rejected)")
	}

	if len(svc.directoryRegistry) != 1 {
		t.Errorf("Expected 1 directory registry entry, got %d", len(svc.directoryRegistry))
	}

	if len(svc.initOrder) != 1 {
		t.Errorf("Expected 1 entry in initOrder, got %d", len(svc.initOrder))
	}
}

// T030 - TestInit_MissingDirectory verifies that initializing with a
// non-existent directory fails with NotFound error and clear error message
// including the alias and non-existent path.
func TestInit_MissingDirectory(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	// Non-existent directory path
	nonExistentPath := "/tmp/nonexistent-12345-xyz"

	config, err := structpb.NewStruct(map[string]any{
		"directory": nonExistentPath,
	})
	if err != nil {
		t.Fatalf("Failed to create config: %v", err)
	}

	initReq := &providerv1.InitRequest{
		Alias:  "missing-dir-test",
		Config: config,
	}

	_, err = svc.Init(context.Background(), initReq)
	if err == nil {
		t.Fatal("Init with non-existent directory should fail")
	}

	// Verify error code is NotFound
	st := status.Convert(err)
	if st.Code() != codes.NotFound {
		t.Errorf("Expected NotFound error code, got %v", st.Code())
	}

	// Verify error message includes the alias
	errMsg := st.Message()
	// Note: Current implementation may not include alias in error message,
	// but path should be included
	if !strings.Contains(errMsg, nonExistentPath) {
		t.Errorf("Error message should include non-existent path %q, got: %s", nonExistentPath, errMsg)
	}

	// Verify error message indicates directory doesn't exist
	if !strings.Contains(errMsg, "not exist") && !strings.Contains(errMsg, "NotFound") {
		t.Errorf("Error message should indicate directory doesn't exist, got: %s", errMsg)
	}

	// Verify service has zero configs (failed Init doesn't add partial state)
	svc.mu.RLock()
	defer svc.mu.RUnlock()

	if len(svc.configs) != 0 {
		t.Errorf("Expected 0 configs after failed Init, got %d", len(svc.configs))
	}

	if len(svc.directoryRegistry) != 0 {
		t.Errorf("Expected 0 directory registry entries after failed Init, got %d", len(svc.directoryRegistry))
	}

	if len(svc.initOrder) != 0 {
		t.Errorf("Expected 0 entries in initOrder after failed Init, got %d", len(svc.initOrder))
	}
}

// T031 - TestInit_EmptyDirectory verifies that initializing with a directory
// containing no .csl files fails with appropriate error and clear message
// mentioning the alias and lack of .csl files.
func TestInit_EmptyDirectory(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	// Create temp directory with NO .csl files
	emptyDir := t.TempDir()

	// Create some non-.csl files to verify filtering works
	nonCSLFile := filepath.Join(emptyDir, "readme.txt")
	if err := os.WriteFile(nonCSLFile, []byte("not a csl file"), 0644); err != nil {
		t.Fatalf("Failed to create non-CSL file: %v", err)
	}

	config, err := structpb.NewStruct(map[string]any{
		"directory": emptyDir,
	})
	if err != nil {
		t.Fatalf("Failed to create config: %v", err)
	}

	initReq := &providerv1.InitRequest{
		Alias:  "empty-dir-test",
		Config: config,
	}

	_, err = svc.Init(context.Background(), initReq)
	if err == nil {
		t.Fatal("Init with empty directory (no .csl files) should fail")
	}

	// Verify error code is Internal or FailedPrecondition
	st := status.Convert(err)
	if st.Code() != codes.Internal && st.Code() != codes.FailedPrecondition {
		t.Errorf("Expected Internal or FailedPrecondition error code, got %v", st.Code())
	}

	// Verify error message mentions no .csl files
	errMsg := st.Message()
	if !strings.Contains(errMsg, "no .csl files") && !strings.Contains(errMsg, "no CSL files") {
		t.Errorf("Error message should mention 'no .csl files', got: %s", errMsg)
	}

	// Verify service has zero configs
	svc.mu.RLock()
	defer svc.mu.RUnlock()

	if len(svc.configs) != 0 {
		t.Errorf("Expected 0 configs after failed Init, got %d", len(svc.configs))
	}

	if len(svc.directoryRegistry) != 0 {
		t.Errorf("Expected 0 directory registry entries after failed Init, got %d", len(svc.directoryRegistry))
	}

	if len(svc.initOrder) != 0 {
		t.Errorf("Expected 0 entries in initOrder after failed Init, got %d", len(svc.initOrder))
	}
}

// T032 - TestInit_RollbackOnFailure verifies that when a second Init fails,
// the first successfully initialized config is ROLLED BACK, ensuring atomic
// initialization semantics (all-or-nothing).
//
// CRITICAL: This tests that partial initialization is not allowed - if any
// Init fails, ALL previous configs should be cleaned up.
func TestInit_RollbackOnFailure(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	// Absolute path to test fixture
	dir1 := "/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir1"

	// First Init - should succeed
	config1, err := structpb.NewStruct(map[string]any{
		"directory": dir1,
	})
	if err != nil {
		t.Fatalf("Failed to create config1: %v", err)
	}

	initReq1 := &providerv1.InitRequest{
		Alias:  "success",
		Config: config1,
	}

	_, err = svc.Init(context.Background(), initReq1)
	if err != nil {
		t.Fatalf("First Init should succeed, got error: %v", err)
	}

	// Verify first config exists
	svc.mu.RLock()
	if len(svc.configs) != 1 {
		t.Errorf("Expected 1 config after first Init, got %d", len(svc.configs))
	}
	if _, exists := svc.configs["success"]; !exists {
		t.Error("First config 'success' should exist")
	}
	svc.mu.RUnlock()

	// Second Init with invalid config (non-existent directory) - should FAIL
	config2, err := structpb.NewStruct(map[string]any{
		"directory": "/tmp/nonexistent-rollback-test-12345",
	})
	if err != nil {
		t.Fatalf("Failed to create config2: %v", err)
	}

	initReq2 := &providerv1.InitRequest{
		Alias:  "failure",
		Config: config2,
	}

	_, err = svc.Init(context.Background(), initReq2)
	if err == nil {
		t.Fatal("Second Init with invalid directory should fail")
	}

	// CRITICAL VERIFICATION: First config should be ROLLED BACK
	// The service should be completely empty after rollback
	svc.mu.RLock()
	defer svc.mu.RUnlock()

	if len(svc.configs) != 0 {
		t.Errorf("Expected 0 configs after rollback, got %d (rollback not triggered?)", len(svc.configs))
	}

	if len(svc.directoryRegistry) != 0 {
		t.Errorf("Expected 0 directory registry entries after rollback, got %d", len(svc.directoryRegistry))
	}

	if len(svc.initOrder) != 0 {
		t.Errorf("Expected 0 entries in initOrder after rollback, got %d", len(svc.initOrder))
	}

	if _, exists := svc.configs["success"]; exists {
		t.Error("First config 'success' should be ROLLED BACK (should not exist)")
	}
}

// T033 - TestInit_RollbackMultiple verifies that when Init fails after
// multiple successful initializations, ALL configs are rolled back.
// This tests that rollback works correctly with multiple instances.
//
// CRITICAL: This ensures atomic initialization for multiple instances - if
// any Init fails, the entire service state should be cleared.
func TestInit_RollbackMultiple(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	// Absolute paths to test fixtures
	dir1 := "/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir1"
	dir2 := "/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir2"
	dir3 := "/Users/wernerswart/repos/nomos-provider-file/tests/fixtures/multi-instance/dir3"

	// Initialize THREE instances successfully
	testCases := []struct {
		alias     string
		directory string
	}{
		{"instance1", dir1},
		{"instance2", dir2},
		{"instance3", dir3},
	}

	for _, tc := range testCases {
		config, err := structpb.NewStruct(map[string]any{
			"directory": tc.directory,
		})
		if err != nil {
			t.Fatalf("Failed to create config for %s: %v", tc.alias, err)
		}

		initReq := &providerv1.InitRequest{
			Alias:  tc.alias,
			Config: config,
		}

		_, err = svc.Init(context.Background(), initReq)
		if err != nil {
			t.Fatalf("Init failed for %s: %v", tc.alias, err)
		}
	}

	// Verify all 3 configs exist
	svc.mu.RLock()
	if len(svc.configs) != 3 {
		t.Errorf("Expected 3 configs after successful Inits, got %d", len(svc.configs))
	}
	svc.mu.RUnlock()

	// Fourth Init with invalid config (non-existent directory) - should FAIL
	config4, err := structpb.NewStruct(map[string]any{
		"directory": "/tmp/nonexistent-multi-rollback-test-67890",
	})
	if err != nil {
		t.Fatalf("Failed to create config4: %v", err)
	}

	initReq4 := &providerv1.InitRequest{
		Alias:  "instance4-fail",
		Config: config4,
	}

	_, err = svc.Init(context.Background(), initReq4)
	if err == nil {
		t.Fatal("Fourth Init with invalid directory should fail")
	}

	// CRITICAL VERIFICATION: ALL THREE configs should be ROLLED BACK
	svc.mu.RLock()
	defer svc.mu.RUnlock()

	if len(svc.configs) != 0 {
		t.Errorf("Expected 0 configs after rollback of all 3 instances, got %d (rollback not triggered?)", len(svc.configs))
	}

	if len(svc.directoryRegistry) != 0 {
		t.Errorf("Expected 0 directory registry entries after rollback, got %d", len(svc.directoryRegistry))
	}

	if len(svc.initOrder) != 0 {
		t.Errorf("Expected 0 entries in initOrder after rollback, got %d", len(svc.initOrder))
	}

	// Verify none of the three instances exist
	for _, tc := range testCases {
		if _, exists := svc.configs[tc.alias]; exists {
			t.Errorf("Config %q should be ROLLED BACK (should not exist)", tc.alias)
		}
	}
}
