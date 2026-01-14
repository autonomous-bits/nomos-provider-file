package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/autonomous-bits/nomos-provider-file/internal/provider"
	providerv1 "github.com/autonomous-bits/nomos/libs/provider-proto/gen/go/nomos/provider/v1"
	"google.golang.org/grpc"
)

const (
	version      = "0.2.1"
	providerType = "file"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("Provider failed: %v", err)
	}
}

func run() error {
	// Create listener on random port
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}

	port := lis.Addr().(*net.TCPAddr).Port

	// Print port to stdout (compiler expects this format)
	fmt.Printf("PROVIDER_PORT=%d\n", port)

	// Create gRPC server
	server := grpc.NewServer()

	// Create and register provider service
	svc := provider.NewFileProviderService(version, providerType)
	providerv1.RegisterProviderServiceServer(server, svc)

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received shutdown signal, stopping server...")
		server.GracefulStop()
	}()

	// Start serving
	log.Printf("File provider v%s listening on %s", version, lis.Addr())

	if err := server.Serve(lis); err != nil {
		return fmt.Errorf("server failed: %w", err)
	}

	return nil
}
