.PHONY: build test clean install lint fmt vet

BINARY_NAME=nomos-provider-file
VERSION?=0.1.0
BUILD_DIR=dist

# Build for current platform
build:
	@echo "Building $(BINARY_NAME)..."
	@go build -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/provider

# Build for all supported platforms
build-all: clean
	@echo "Building for all platforms..."
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY_NAME)-$(VERSION)-darwin-arm64 ./cmd/provider
	GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY_NAME)-$(VERSION)-darwin-amd64 ./cmd/provider
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY_NAME)-$(VERSION)-linux-amd64 ./cmd/provider
	@echo "Generating checksums..."
	@cd $(BUILD_DIR) && shasum -a 256 $(BINARY_NAME)-* > SHA256SUMS

# Run tests
test:
	@echo "Running tests..."
	@go test -v -race -cover ./...

# Run tests with coverage report
coverage:
	@echo "Running tests with coverage..."
	@go test -v -race -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html

# Install binary to GOBIN
install:
	@echo "Installing $(BINARY_NAME)..."
	@go install ./cmd/provider

# Run linter
lint:
	@echo "Running linter..."
	@golangci-lint run ./...

# Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...
	@goimports -w .

# Run go vet
vet:
	@echo "Running go vet..."
	@go vet ./...

# Run all checks
check: fmt vet lint test

# Display help
help:
	@echo "Available targets:"
	@echo "  build       - Build binary for current platform"
	@echo "  build-all   - Build binaries for all supported platforms"
	@echo "  test        - Run tests"
	@echo "  coverage    - Run tests with coverage report"
	@echo "  clean       - Remove build artifacts"
	@echo "  install     - Install binary to GOBIN"
	@echo "  lint        - Run linter"
	@echo "  fmt         - Format code"
	@echo "  vet         - Run go vet"
	@echo "  check       - Run all checks (fmt, vet, lint, test)"
	@echo "  help        - Display this help message"
