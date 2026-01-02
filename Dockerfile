# syntax=docker/dockerfile:1

# =============================================================================
# Builder Stage
# =============================================================================
# Pin Go version and Alpine variant explicitly for reproducible builds
# To fully pin, add digest: golang:1.24-alpine3.21@sha256:<digest>
FROM golang:1.25-alpine3.23 AS builder

# Install build dependencies for CGO (required by go-sqlite3)
# Note: go-sqlite3 bundles SQLite source, no need for sqlite-dev
RUN apk add --no-cache \
    gcc \
    musl-dev

WORKDIR /src

# Cache dependencies - copy go.mod/sum first for better layer caching
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build the binary with CGO enabled (required for go-sqlite3)
# Flags:
#   -tags sqlite_omit_load_extension: security hardening, disable extension loading
#   -ldflags="-s -w": strip debug info and symbol table
#   -extldflags '-static': fully static binary (no runtime deps)
#   -trimpath: remove file system paths for reproducibility
RUN CGO_ENABLED=1 GOOS=linux \
    go build \
    -tags sqlite_omit_load_extension \
    -ldflags="-s -w -extldflags '-static'" \
    -trimpath \
    -o /bin/lightd \
    ./cmd/lightd

# =============================================================================
# Runtime Stage
# =============================================================================
# Using Alpine for tzdata and ca-certificates
# Binary is fully static, no runtime libs needed
FROM alpine:3.23

# Installations
# - ca-certificates: HTTPS connections
# - tzdata: timezone support for scheduler
# - curl: healthcheck
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    curl

# Create non-root user for security
RUN addgroup -g 1000 lightd && \
    adduser -u 1000 -G lightd -s /bin/sh -D lightd

WORKDIR /app

# Copy binary from builder
COPY --from=builder /bin/lightd /usr/local/bin/lightd

# Create directory structure:
#   /app/data   - SQLite database (mount as volume for persistence)
#   /app/config - config.yaml and lua scripts (mount to customize)
RUN mkdir -p /app/data /app/config && chown -R lightd:lightd /app

# Copy default config (env-variable driven)
COPY --chown=lightd:lightd config.docker.yaml /app/config/config.yaml

# Switch to non-root user
USER lightd

# Expose ports
# 8080 - Webhook events
# 9090 - Health check
EXPOSE 8080 9090

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -sf http://localhost:9090/health || exit 1

# Run the application with default config path
# Override with: docker run lightd -c /path/to/config.yaml
ENTRYPOINT ["lightd", "-c", "/app/config/config.yaml"]
