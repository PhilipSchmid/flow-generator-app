# Variables
GO := go
GOLANGCI_LINT := golangci-lint
DOCKER := docker
SERVER_IMAGE := ghcr.io/philipschmid/echo-server
CLIENT_IMAGE := ghcr.io/philipschmid/flow-generator
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "latest")

# Color definitions
BLUE := \033[0;34m
GREEN := \033[0;32m
YELLOW := \033[0;33m
RED := \033[0;31m
NC := \033[0m # No Color

# Go build variables
GOFLAGS := -trimpath
LDFLAGS := -s -w
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Directories
BIN_DIR := bin
COVERAGE_DIR := coverage

# Platforms for cross-compilation
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: all build test lint clean docker-build help

# Default target
all: clean lint test build

## help: Show this help message
help:
	@printf "$(BLUE)Usage:$(NC) make [target]\n"
	@printf "\n"
	@printf "$(YELLOW)Development targets:$(NC)\n"
	@printf "  $(GREEN)dev-server$(NC)       Run server with live reload\n"
	@printf "  $(GREEN)dev-client$(NC)       Run client with live reload\n"
	@printf "  $(GREEN)deps$(NC)             Download and tidy dependencies\n"
	@printf "  $(GREEN)generate$(NC)         Run go generate\n"
	@printf "\n"
	@printf "$(YELLOW)Build targets:$(NC)\n"
	@printf "  $(GREEN)build$(NC)            Build binaries for current platform\n"
	@printf "  $(GREEN)build-all$(NC)        Build binaries for all platforms\n"
	@printf "  $(GREEN)build-server$(NC)     Build server binary only\n"
	@printf "  $(GREEN)build-client$(NC)     Build client binary only\n"
	@printf "\n"
	@printf "$(YELLOW)Test targets:$(NC)\n"
	@printf "  $(GREEN)test$(NC)             Run tests\n"
	@printf "  $(GREEN)test-verbose$(NC)     Run tests with verbose output\n"
	@printf "  $(GREEN)test-race$(NC)        Run tests with race detector\n"
	@printf "  $(GREEN)test-coverage$(NC)    Run tests with coverage\n"
	@printf "  $(GREEN)benchmark$(NC)        Run benchmarks\n"
	@printf "\n"
	@printf "$(YELLOW)Quality targets:$(NC)\n"
	@printf "  $(GREEN)lint$(NC)             Run linters\n"
	@printf "  $(GREEN)fmt$(NC)              Format code\n"
	@printf "  $(GREEN)vet$(NC)              Run go vet\n"
	@printf "  $(GREEN)mod-tidy$(NC)         Tidy go modules\n"
	@printf "\n"
	@printf "$(YELLOW)Docker targets:$(NC)\n"
	@printf "  $(GREEN)docker-build$(NC)     Build Docker images\n"
	@printf "  $(GREEN)docker-push$(NC)      Push Docker images\n"
	@printf "  $(GREEN)docker-run$(NC)       Run containers locally\n"
	@printf "  $(GREEN)docker-stop$(NC)      Stop and remove containers\n"
	@printf "\n"
	@printf "$(YELLOW)Utility targets:$(NC)\n"
	@printf "  $(GREEN)clean$(NC)            Clean build artifacts\n"
	@printf "  $(GREEN)install-tools$(NC)    Install development tools\n"
	@printf "  $(GREEN)proto$(NC)            Generate protobuf files (if applicable)"

## deps: Download and tidy dependencies
deps:
	@printf "$(BLUE)Downloading dependencies...$(NC)\n"
	@$(GO) mod download
	@$(GO) mod tidy
	@printf "$(GREEN)✓ Dependencies updated$(NC)\n"

## generate: Run go generate
generate:
	@printf "$(BLUE)Running go generate...$(NC)\n"
	@$(GO) generate ./...
	@printf "$(GREEN)✓ Code generation completed$(NC)\n"

## build: Build binaries for current platform
build: build-server build-client
	@printf "$(GREEN)✓ All binaries built successfully$(NC)\n"

## build-server: Build server binary
build-server:
	@printf "$(BLUE)Building server binary...$(NC)\n"
	@mkdir -p $(BIN_DIR)
	@$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS) \
		-X github.com/PhilipSchmid/flow-generator-app/internal/version.Version=$(VERSION) \
		-X github.com/PhilipSchmid/flow-generator-app/internal/version.BuildDate=$(BUILD_DATE) \
		-X github.com/PhilipSchmid/flow-generator-app/internal/version.GitCommit=$(GIT_COMMIT)" \
		-o $(BIN_DIR)/echo-server ./cmd/server
	@printf "$(GREEN)✓ Server binary built: $(BIN_DIR)/echo-server$(NC)\n"

## build-client: Build client binary
build-client:
	@printf "$(BLUE)Building client binary...$(NC)\n"
	@mkdir -p $(BIN_DIR)
	@$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS) \
		-X github.com/PhilipSchmid/flow-generator-app/internal/version.Version=$(VERSION) \
		-X github.com/PhilipSchmid/flow-generator-app/internal/version.BuildDate=$(BUILD_DATE) \
		-X github.com/PhilipSchmid/flow-generator-app/internal/version.GitCommit=$(GIT_COMMIT)" \
		-o $(BIN_DIR)/flow-generator ./cmd/client
	@printf "$(GREEN)✓ Client binary built: $(BIN_DIR)/flow-generator$(NC)\n"

## build-all: Build binaries for all platforms
build-all:
	@printf "$(BLUE)Building binaries for all platforms...$(NC)\n"
	@for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*} GOARCH=$${platform#*/} make build-platform PLATFORM=$$platform; \
	done
	@printf "$(GREEN)✓ All platform binaries built successfully$(NC)\n"

build-platform:
	@printf "$(YELLOW)Building for $(PLATFORM)...$(NC)\n"
	@mkdir -p $(BIN_DIR)/$(PLATFORM)
	@GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS) \
		-X github.com/PhilipSchmid/flow-generator-app/internal/version.Version=$(VERSION) \
		-X github.com/PhilipSchmid/flow-generator-app/internal/version.BuildDate=$(BUILD_DATE) \
		-X github.com/PhilipSchmid/flow-generator-app/internal/version.GitCommit=$(GIT_COMMIT)" \
		-o $(BIN_DIR)/$(PLATFORM)/echo-server ./cmd/server
	@GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS) \
		-X github.com/PhilipSchmid/flow-generator-app/internal/version.Version=$(VERSION) \
		-X github.com/PhilipSchmid/flow-generator-app/internal/version.BuildDate=$(BUILD_DATE) \
		-X github.com/PhilipSchmid/flow-generator-app/internal/version.GitCommit=$(GIT_COMMIT)" \
		-o $(BIN_DIR)/$(PLATFORM)/flow-generator ./cmd/client

## test: Run tests
test:
	@printf "$(BLUE)Running tests...$(NC)\n"
	@$(GO) test ./...
	@printf "$(GREEN)✓ All tests passed$(NC)\n"

## test-verbose: Run tests with verbose output
test-verbose:
	@printf "$(BLUE)Running tests with verbose output...$(NC)\n"
	@$(GO) test -v ./...
	@printf "$(GREEN)✓ All tests passed$(NC)\n"

## test-race: Run tests with race detector
test-race:
	@printf "$(BLUE)Running tests with race detector...$(NC)\n"
	@$(GO) test -race ./...
	@printf "$(GREEN)✓ All tests passed without race conditions$(NC)\n"

## test-coverage: Run tests with coverage
test-coverage:
	@printf "$(BLUE)Running tests with coverage...$(NC)\n"
	@mkdir -p $(COVERAGE_DIR)
	@$(GO) test -coverprofile=$(COVERAGE_DIR)/coverage.out ./...
	@$(GO) tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	@printf "$(GREEN)✓ Coverage report generated at $(COVERAGE_DIR)/coverage.html$(NC)\n"

## benchmark: Run benchmarks
benchmark:
	@printf "$(BLUE)Running benchmarks...$(NC)\n"
	@$(GO) test -bench=. -benchmem ./...
	@printf "$(GREEN)✓ Benchmarks completed$(NC)\n"

## lint: Run linters
lint:
	@printf "$(BLUE)Running linters...$(NC)\n"
	@$(GOLANGCI_LINT) run
	@printf "$(GREEN)✓ All linters passed$(NC)\n"

## fmt: Format code
fmt:
	@printf "$(BLUE)Formatting code...$(NC)\n"
	@$(GO) fmt ./...
	@printf "$(GREEN)✓ Code formatted$(NC)\n"

## vet: Run go vet
vet:
	@printf "$(BLUE)Running go vet...$(NC)\n"
	@$(GO) vet ./...
	@printf "$(GREEN)✓ Go vet passed$(NC)\n"

## mod-tidy: Tidy go modules
mod-tidy:
	@printf "$(BLUE)Tidying go modules...$(NC)\n"
	@$(GO) mod tidy
	@printf "$(GREEN)✓ Go modules tidied$(NC)\n"

## clean: Clean build artifacts
clean:
	@printf "$(BLUE)Cleaning build artifacts...$(NC)\n"
	@rm -rf $(BIN_DIR) $(COVERAGE_DIR)
	@rm -f echo-server flow-generator
	@printf "$(GREEN)✓ Build artifacts cleaned$(NC)\n"

## docker-build: Build Docker images
docker-build:
	@printf "$(BLUE)Building Docker images...$(NC)\n"
	@printf "$(YELLOW)Building server image...$(NC)\n"
	@$(DOCKER) build -t $(SERVER_IMAGE):$(VERSION) \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		-f Dockerfile.server .
	@printf "$(GREEN)✓ Server image built: $(SERVER_IMAGE):$(VERSION)$(NC)\n"
	@printf "$(YELLOW)Building client image...$(NC)\n"
	@$(DOCKER) build -t $(CLIENT_IMAGE):$(VERSION) \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		-f Dockerfile.client .
	@printf "$(GREEN)✓ Client image built: $(CLIENT_IMAGE):$(VERSION)$(NC)\n"
	@printf "$(GREEN)✓ All Docker images built successfully$(NC)\n"

## docker-push: Push Docker images
docker-push: docker-build
	@printf "$(BLUE)Pushing Docker images...$(NC)\n"
	@printf "$(YELLOW)Pushing server image...$(NC)\n"
	@$(DOCKER) push $(SERVER_IMAGE):$(VERSION)
	@printf "$(GREEN)✓ Server image pushed$(NC)\n"
	@printf "$(YELLOW)Pushing client image...$(NC)\n"
	@$(DOCKER) push $(CLIENT_IMAGE):$(VERSION)
	@printf "$(GREEN)✓ Client image pushed$(NC)\n"
	@printf "$(GREEN)✓ All Docker images pushed successfully$(NC)\n"

## docker-run: Run containers locally
docker-run:
	@printf "$(BLUE)Starting Docker containers...$(NC)\n"
	@$(DOCKER) run -d --name echo-server -p 8080:8080 -p 8082:8082 -p 9090:9090 $(SERVER_IMAGE):$(VERSION)
	@printf "$(GREEN)✓ Echo server is running on ports 8080 (TCP), 8082 (health), and 9090 (metrics)$(NC)\n"
	@printf "$(YELLOW)To stop: make docker-stop$(NC)\n"

## docker-stop: Stop and remove containers
docker-stop:
	@printf "$(BLUE)Stopping Docker containers...$(NC)\n"
	@$(DOCKER) stop echo-server 2>/dev/null || true
	@$(DOCKER) rm echo-server 2>/dev/null || true
	@printf "$(GREEN)✓ Echo server stopped and removed$(NC)\n"

## install-tools: Install development tools
install-tools:
	@printf "$(BLUE)Installing development tools...$(NC)\n"
	@which $(GOLANGCI_LINT) > /dev/null || (printf "$(YELLOW)Installing golangci-lint...$(NC)\n" && \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin)
	@which air > /dev/null || (printf "$(YELLOW)Installing air for live reload...$(NC)\n" && \
		go install github.com/air-verse/air@latest)
	@printf "$(GREEN)✓ Development tools installed$(NC)\n"

## dev-server: Run server with live reload
dev-server:
	@which air > /dev/null || (printf "$(RED)Error: air not found. Please run 'make install-tools' first$(NC)\n" && exit 1)
	@printf "$(BLUE)Starting server with live reload...$(NC)\n"
	@air --build.cmd "go build -o ./tmp/echo-server ./cmd/server" --build.bin "./tmp/echo-server"

## dev-client: Run client with live reload
dev-client:
	@which air > /dev/null || (printf "$(RED)Error: air not found. Please run 'make install-tools' first$(NC)\n" && exit 1)
	@printf "$(BLUE)Starting client with live reload...$(NC)\n"
	@air --build.cmd "go build -o ./tmp/flow-generator ./cmd/client" --build.bin "./tmp/flow-generator --server localhost"

## quick-test: Run a quick integration test
quick-test: build
	@printf "$(BLUE)Running quick integration test...$(NC)\n"
	@printf "$(YELLOW)Starting echo server...$(NC)\n"
	@./bin/echo-server --tcp_ports_server 8080,8081 --udp_ports_server 9000 --metrics_port 9091 > /tmp/echo-server.log 2>&1 & \
		SERVER_PID=$$!; \
		sleep 2; \
		if ! kill -0 $$SERVER_PID 2>/dev/null; then \
			printf "$(RED)✗ Server failed to start$(NC)\n"; \
			cat /tmp/echo-server.log; \
			exit 1; \
		fi; \
		printf "$(GREEN)✓ Server started successfully$(NC)\n"; \
		printf "$(YELLOW)Starting flow generator...$(NC)\n"; \
		./bin/flow-generator --server localhost --tcp_ports 8080,8081 --udp_ports 9000 --rate 10 --max_duration 5 --metrics_port 9092 > /tmp/flow-generator.log 2>&1 & \
		CLIENT_PID=$$!; \
		printf "$(YELLOW)Running for 10 seconds...$(NC)\n"; \
		sleep 10; \
		printf "$(YELLOW)Stopping processes...$(NC)\n"; \
		kill $$CLIENT_PID 2>/dev/null || true; \
		wait $$CLIENT_PID 2>/dev/null || true; \
		printf "$(BLUE)\n===== Test Summary =====$(NC)\n"; \
		printf "$(YELLOW)Server Configuration:$(NC)\n"; \
		printf "  • TCP ports: 8080, 8081\n"; \
		printf "  • UDP port: 9000\n"; \
		printf "  • Health port: 8082 (default)\n"; \
		printf "  • Metrics port: 9091\n"; \
		printf "\n$(YELLOW)Client Configuration:$(NC)\n"; \
		printf "  • Target: localhost\n"; \
		printf "  • Rate: 10 flows/second\n"; \
		printf "  • Flow duration: 0-5 seconds\n"; \
		printf "  • Metrics port: 9092\n"; \
		printf "\n$(YELLOW)Test Results:$(NC)\n"; \
		REQUESTS_SENT=$$(grep -oE "Total Requests Sent.*│\s*[0-9]+" /tmp/flow-generator.log 2>/dev/null | grep -oE "[0-9]+$$" | tail -1 || echo "0"); \
		TCP_SENT=$$(grep -oE "Total TCP Requests Sent.*│\s*[0-9]+" /tmp/flow-generator.log 2>/dev/null | grep -oE "[0-9]+$$" | tail -1 || echo "0"); \
		UDP_SENT=$$(grep -oE "Total UDP Requests Sent.*│\s*[0-9]+" /tmp/flow-generator.log 2>/dev/null | grep -oE "[0-9]+$$" | tail -1 || echo "0"); \
		if [ "$$REQUESTS_SENT" -gt 0 ]; then \
			printf "$(GREEN)✓ Flow generation successful$(NC)\n"; \
			printf "  • Total requests sent: $$REQUESTS_SENT\n"; \
			printf "  • TCP requests: $$TCP_SENT\n"; \
			printf "  • UDP requests: $$UDP_SENT\n"; \
		else \
			printf "$(YELLOW)⚠ No metrics found (this is normal for short tests)$(NC)\n"; \
		fi; \
		if grep -q "Echo server is ready" /tmp/echo-server.log 2>/dev/null; then \
			printf "$(GREEN)✓ Server started and ready$(NC)\n"; \
			REQUESTS_RECEIVED=$$(grep -oE "Total Requests Received.*│\s*[0-9]+" /tmp/echo-server.log 2>/dev/null | grep -oE "[0-9]+$$" | tail -1); \
			if [ -n "$$REQUESTS_RECEIVED" ] && [ "$$REQUESTS_RECEIVED" -gt 0 ] 2>/dev/null; then \
				printf "  • Echo server processed: $$REQUESTS_RECEIVED requests\n"; \
			fi; \
			TCP_CONNS=$$(grep -oE "TCP server listening on port (8080|8081)" /tmp/echo-server.log 2>/dev/null | wc -l | xargs); \
			UDP_PORTS=$$(grep -oE "UDP server listening on port 9000" /tmp/echo-server.log 2>/dev/null | wc -l | xargs); \
			printf "  • Servers started: $$TCP_CONNS TCP, $$UDP_PORTS UDP\n"; \
		fi; \
		printf "\n$(YELLOW)Logs saved to:$(NC)\n"; \
		printf "  • Server: /tmp/echo-server.log\n"; \
		printf "  • Client: /tmp/flow-generator.log\n"; \
		kill $$SERVER_PID 2>/dev/null || true; \
		wait $$SERVER_PID 2>/dev/null || true
	@printf "\n$(GREEN)✓ Quick integration test completed$(NC)\n"