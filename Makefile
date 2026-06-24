# ==============================================================================
# Build Configuration
# ==============================================================================

# Executable names
SERVER_BIN := ai.local
CLI_BIN    := ai.local.cli

# Main entry points (Adjust these paths if your main.go files are located elsewhere)
SERVER_PKG := ./cmd/server
CLI_PKG    := ./cmd/cli

# Proto paths
PROTO_SRC   := grpc/proto/ailocalcli.proto

# Compilation flags
GO_ENV     := CGO_ENABLED=0
LDFLAGS    := -w -s

# ==============================================================================
# Development Targets
# ==============================================================================

.PHONY: all
all: build-server build-cli ## Build both server and CLI binaries

.PHONY: proto
proto: ## Compile protobuffer definitions into the proto/ directory
	@echo "==> Compiling protocol buffers..."
	@mkdir -p proto
	@protoc --go_out=. --go-grpc_out=. $(PROTO_SRC)
	@echo "--> Success: Protobuf compiled to ./proto/"

.PHONY: build-server
build-server: ## Build the ai.local gateway server binary
	@echo "==> Building $(SERVER_BIN)..."
	@$(GO_ENV) go build -ldflags "$(LDFLAGS)" -o $(SERVER_BIN) $(SERVER_PKG)
	@echo "--> Success: ./$(SERVER_BIN)"

.PHONY: build-cli
build-cli: ## Build the ai.local.cli tool binary
	@echo "==> Building $(CLI_BIN)..."
	@$(GO_ENV) go build -ldflags "$(LDFLAGS)" -o $(CLI_BIN) $(CLI_PKG)
	@echo "--> Success: ./$(CLI_BIN)"

.PHONY: clean
clean: ## Remove compiled binaries and scratch files
	@echo "==> Cleaning build artifacts..."
	@rm -f $(SERVER_BIN) $(CLI_BIN)
	@go clean
	@echo "--> Clean complete."

.PHONY: fmt
fmt: ## Run go fmt against all source code
	@echo "==> Formatting code..."
	@go fmt ./...

.PHONY: vet
vet: ## Run go vet against all source code
	@echo "==> Vetting code..."
	@go vet ./...

.PHONY: help
help: ## Display this help screen
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'
