# ==============================================================================
# Build Configuration
# ==============================================================================

# Executable names
SERVER_BIN := ai.local
CLI_BIN    := ai.local.cli

# Docker Image
DOCKER_IMAGE_NAME := ai.local
DOCKER_TAR        := ai.local-alpha.tar

# OCI export for router OS
OCI_TAR     := ai.local-oci.tar
OCI_TAR_GZ  := $(OCI_TAR).gz
OCI_PLATFORM ?= linux/amd64

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

.PHONY: docker-image
docker-image: ## Build the lightweight production Docker image locally
	@echo "==> Building Docker image $(DOCKER_IMAGE_NAME)..."
	@sudo docker build -t $(DOCKER_IMAGE_NAME):alpha -t $(DOCKER_IMAGE_NAME):latest .
	@echo "--> Success: Docker images tagged as $(DOCKER_IMAGE_NAME):alpha and $(DOCKER_IMAGE_NAME):latest"

.PHONY: docker-export
docker-export: docker-image ## Export the local Docker image into a redistributable .tar file
	@echo "==> Exporting $(DOCKER_IMAGE_NAME):latest to $(DOCKER_TAR)..."
	@sudo docker save -o $(DOCKER_TAR) $(DOCKER_IMAGE_NAME):latest
	@echo "--> Success: Physical image archive ready at ./$(DOCKER_TAR)"

.PHONY: docker-oci
docker-oci: ## Build OCI image tar (for router OS import)
	@echo "==> Building OCI archive $(OCI_TAR) for $(OCI_PLATFORM)..."
	@sudo docker buildx build \
		--platform $(OCI_PLATFORM) \
		--output type=oci,dest=$(OCI_TAR) \
		.
	@echo "--> Success: ./$(OCI_TAR)"

.PHONY: docker-oci-gzip
docker-oci-gzip: docker-oci ## Compress OCI tar to .tar.gz
	@echo "==> Compressing $(OCI_TAR) -> $(OCI_TAR_GZ)..."
	@gzip -f $(OCI_TAR)
	@echo "--> Success: ./$(OCI_TAR_GZ)"

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

# ==============================================================================
# Release (local) targets
# ==============================================================================

DIST_DIR      := dist
APP_NAME      := ai.local
CLI_NAME      := ai.local.cli
DOCKER_TAR    := ai.local-alpha.tar
OCI_TAR       := ai.local-oci.tar
OCI_TAR_GZ    := $(OCI_TAR).gz
LDFLAGS       := -w -s
OCI_PLATFORM ?= linux/amd64

.PHONY: release-local
release-local: release-clean release-binaries release-docker-tar release-oci-targz release-sha256 ## Build all local release artifacts into ./dist
	@echo "--> Local release artifacts ready in ./$(DIST_DIR)"

.PHONY: release-clean
release-clean: ## Clean local release dist directory
	@echo "==> Cleaning $(DIST_DIR)..."
	@rm -rf $(DIST_DIR)
	@mkdir -p $(DIST_DIR)

.PHONY: release-binaries
release-binaries: ## Build linux amd64/arm64 binaries for server and cli
	@echo "==> Building linux/amd64 binaries..."
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(APP_NAME)-linux-amd64 ./cmd/server
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(CLI_NAME)-linux-amd64 ./cmd/cli
	@echo "==> Building linux/arm64 binaries..."
	@CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(APP_NAME)-linux-arm64 ./cmd/server
	@CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(CLI_NAME)-linux-arm64 ./cmd/cli
	@echo "--> Success: built 4 binaries"

.PHONY: release-docker-tar
release-docker-tar: ## Build docker archive tar (linux/amd64)
	@echo "==> Building Docker archive: $(DIST_DIR)/$(DOCKER_TAR)..."
	@sudo docker buildx build \
		--platform linux/amd64 \
		--output type=docker,dest=$(DIST_DIR)/$(DOCKER_TAR) \
		.
	@echo "--> Success: $(DIST_DIR)/$(DOCKER_TAR)"

.PHONY: release-oci-targz
release-oci-targz: ## Build OCI tar and gzip it (for router OS)
	@echo "==> Building OCI tar: $(DIST_DIR)/$(OCI_TAR) for $(OCI_PLATFORM)..."
	@sudo docker buildx build \
		--platform $(OCI_PLATFORM) \
		--output type=oci,dest=$(DIST_DIR)/$(OCI_TAR) \
		.
	@echo "==> Compressing OCI tar..."
	@gzip -f $(DIST_DIR)/$(OCI_TAR)
	@echo "--> Success: $(DIST_DIR)/$(OCI_TAR_GZ)"

.PHONY: release-sha256
release-sha256: ## Generate SHA256SUMS for all dist artifacts
	@echo "==> Generating checksums..."
	@cd $(DIST_DIR) && sha256sum * > SHA256SUMS
	@echo "--> Success: $(DIST_DIR)/SHA256SUMS"

.PHONY: release-list
release-list: ## List local release artifacts
	@echo "==> $(DIST_DIR) contents:"
	@ls -lh $(DIST_DIR)

.PHONY: help
help: ## Display this help screen
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'
