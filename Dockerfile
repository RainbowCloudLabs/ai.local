# ==========================================================================
# STAGE 1: Compilation Pipeline (Builder Stage)
# ==========================================================================
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

# Leverage Docker layer caching for dependency synchronization
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Compile Linux core server ONLY (CLI compilation is fully offloaded to GitHub Actions)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o dist/ai-local-server cmd/server/main.go

# ==========================================================================
# STAGE 2: Production Hardened Runtime Env (Runtime Stage)
# ==========================================================================
FROM alpine:3.19

# Install base CA certificates ensuring zero TLS handshake friction with upstream OpenRouter
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /

# Provision server core engine only
COPY --from=builder /app/dist/ai-local-server /usr/local/bin/ai-local-server

# Copy and weld system entrypoint initializer
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# Target configuration directory and volume persistence layer
RUN mkdir -p /etc/ai.local
VOLUME ["/etc/ai.local"]

# Expose Data-Plane Proxy (:8443) and Control-Plane gRPC (:50051)
EXPOSE 8443 50051

# Bind entrypoint runtime flow
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

# Default fallback flags delivered directly to entrypoint.sh
CMD ["-d", "/etc/ai.local"]
