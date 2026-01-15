package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	providerv1 "github.com/autonomous-bits/nomos/libs/provider-proto/gen/go/nomos/provider/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestInit(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.csl")
	if err := os.WriteFile(testFile, []byte("app: { name: test }"), 0644); err != nil {
		t.Fatal(err)
	}

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

	// Verify initialized
	if svc.config == nil {
		t.Fatal("Expected config to be set")
	}
	if !svc.config.initialized {
		t.Fatal("Expected config to be initialized")
	}
}

func TestInit_AlreadyInitialized(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.csl")
	if err := os.WriteFile(testFile, []byte("app: { name: test }"), 0644); err != nil {
		t.Fatal(err)
	}

	config, _ := structpb.NewStruct(map[string]any{
		"directory": tmpDir,
	})

	req := &providerv1.InitRequest{
		Alias:  "test",
		Config: config,
	}

	// First init should succeed
	if _, err := svc.Init(context.Background(), req); err != nil {
		t.Fatalf("First Init failed: %v", err)
	}

	// Second init should fail
	_, err := svc.Init(context.Background(), req)
	if err == nil {
		t.Fatal("Expected second Init to fail")
	}

	st := status.Convert(err)
	if st.Code() != codes.FailedPrecondition {
		t.Errorf("Expected FailedPrecondition, got %v", st.Code())
	}
}

func TestInit_MissingDirectory(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	config, _ := structpb.NewStruct(map[string]any{
		"directory": "/nonexistent/path",
	})

	req := &providerv1.InitRequest{
		Alias:  "test",
		Config: config,
	}

	_, err := svc.Init(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for nonexistent directory")
	}

	st := status.Convert(err)
	if st.Code() != codes.NotFound {
		t.Errorf("Expected NotFound, got %v", st.Code())
	}
}

func TestFetch(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.csl")
	content := `app:
  name: myapp
  version: 1.0.0
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
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

	data := resp.Value.AsMap()
	app := data["app"].(map[string]any)
	if app["name"] != "myapp" {
		t.Errorf("Expected name 'myapp', got %v", app["name"])
	}
}

func TestFetch_NestedPath(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "database.csl")
	content := `connection:
  host: localhost
  port: 5432
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
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

	// Fetch nested value
	fetchReq := &providerv1.FetchRequest{
		Path: []string{"database", "connection", "host"},
	}

	resp, err := svc.Fetch(context.Background(), fetchReq)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	data := resp.Value.AsMap()
	if data["value"] != "localhost" {
		t.Errorf("Expected 'localhost', got %v", data["value"])
	}
}

func TestFetch_NotFound(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.csl")
	if err := os.WriteFile(testFile, []byte("app: { name: test }"), 0644); err != nil {
		t.Fatal(err)
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

	// Try to fetch nonexistent file
	fetchReq := &providerv1.FetchRequest{
		Path: []string{"nonexistent"},
	}

	_, err := svc.Fetch(context.Background(), fetchReq)
	if err == nil {
		t.Fatal("Expected error for nonexistent file")
	}

	st := status.Convert(err)
	if st.Code() != codes.NotFound {
		t.Errorf("Expected NotFound, got %v", st.Code())
	}
}

func TestFetch_NotInitialized(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	fetchReq := &providerv1.FetchRequest{
		Path: []string{"config"},
	}

	_, err := svc.Fetch(context.Background(), fetchReq)
	if err == nil {
		t.Fatal("Expected error when not initialized")
	}

	st := status.Convert(err)
	if st.Code() != codes.FailedPrecondition {
		t.Errorf("Expected FailedPrecondition, got %v", st.Code())
	}
}

func TestHealth(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	// Before init
	resp, err := svc.Health(context.Background(), &providerv1.HealthRequest{})
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}

	if resp.Status != providerv1.HealthResponse_STATUS_DEGRADED {
		t.Errorf("Expected DEGRADED before init, got %v", resp.Status)
	}

	// After init
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.csl")
	if err := os.WriteFile(testFile, []byte("app: { name: test }"), 0644); err != nil {
		t.Fatal(err)
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
		t.Errorf("Expected OK after init, got %v", resp.Status)
	}
}

func TestInfo(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	resp, err := svc.Info(context.Background(), &providerv1.InfoRequest{})
	if err != nil {
		t.Fatalf("Info failed: %v", err)
	}

	if resp.Version != "0.1.0" {
		t.Errorf("Expected version 0.1.0, got %v", resp.Version)
	}

	if resp.Type != "file" {
		t.Errorf("Expected type 'file', got %v", resp.Type)
	}
}

func TestShutdown(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.csl")
	if err := os.WriteFile(testFile, []byte("app: { name: test }"), 0644); err != nil {
		t.Fatal(err)
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

	// Shutdown
	_, err := svc.Shutdown(context.Background(), &providerv1.ShutdownRequest{})
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	// Verify cleared
	if svc.config != nil {
		t.Error("Expected config to be nil after shutdown")
	}
}

func TestInit_EmptyDirectory(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	tmpDir := t.TempDir()

	config, _ := structpb.NewStruct(map[string]any{
		"directory": tmpDir,
	})

	req := &providerv1.InitRequest{
		Alias:  "test",
		Config: config,
	}

	_, err := svc.Init(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for empty directory")
	}

	if !strings.Contains(err.Error(), "no .csl files found") {
		t.Errorf("Expected 'no .csl files found' error, got: %v", err)
	}
}

func TestInit_EmptyAlias(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.csl")
	if err := os.WriteFile(testFile, []byte("app: { name: test }"), 0644); err != nil {
		t.Fatal(err)
	}

	config, _ := structpb.NewStruct(map[string]any{
		"directory": tmpDir,
	})

	req := &providerv1.InitRequest{
		Alias:  "",
		Config: config,
	}

	_, err := svc.Init(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for empty alias")
	}

	st := status.Convert(err)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument, got %v", st.Code())
	}
}

func TestFetch_EmptyPath(t *testing.T) {
	svc := NewFileProviderService("0.1.0", "file")

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.csl")
	if err := os.WriteFile(testFile, []byte("app: { name: test }"), 0644); err != nil {
		t.Fatal(err)
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

	fetchReq := &providerv1.FetchRequest{
		Path: []string{},
	}

	_, err := svc.Fetch(context.Background(), fetchReq)
	if err == nil {
		t.Fatal("Expected error for empty path")
	}

	st := status.Convert(err)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument, got %v", st.Code())
	}
}
