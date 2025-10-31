package provider

import (
	"context"
	"os"
	"path/filepath"
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

	// Verify initialized state
	if !svc.initialized {
		t.Error("Provider should be initialized")
	}
	if svc.alias != "test" {
		t.Errorf("Expected alias 'test', got %q", svc.alias)
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

	// Fetch the file
	fetchReq := &providerv1.FetchRequest{
		Path: []string{"config"},
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

	// Fetch non-existent file
	fetchReq := &providerv1.FetchRequest{
		Path: []string{"nonexistent"},
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
	os.WriteFile(testFile, []byte("test: value"), 0644)

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
