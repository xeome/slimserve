# SlimServe Dockerfile
# Multi-stage build for minimal production image

# Builder stage
FROM golang:1.24-alpine AS builder

# Add build dependencies for CGO (if needed)
RUN apk add --no-cache build-base git ca-certificates

# Set build environment for static binary
ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

# Create app directory
WORKDIR /app

# Copy go modules files first for better caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary with optimization flags
RUN go build \
    -buildvcs=false \
    -trimpath \
    -ldflags="-s -w -X main.version=docker" \
    -o slimserve \
    cmd/slimserve/main.go

# Final stage - minimal runtime image
FROM alpine:3.20

# Install minimal runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    wget \
    && rm -rf /var/cache/apk/*

# Create non-root user
RUN addgroup -g 1001 -S slimserve && \
    adduser -u 1001 -S slimserve -G slimserve

# Create data directory
RUN mkdir -p /data && \
    chown slimserve:slimserve /data

# Copy binary and entrypoint script from builder
COPY --from=builder /app/slimserve /usr/local/bin/slimserve
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

# Make binary and entrypoint script executable
RUN chmod +x /usr/local/bin/slimserve && \
    chmod +x /usr/local/bin/docker-entrypoint.sh

# Create config directory
RUN mkdir -p /etc/slimserve && \
    chown slimserve:slimserve /etc/slimserve

# Switch to non-root user
USER slimserve

# Set working directory
WORKDIR /data

# Expose default port
EXPOSE 8080

# Add health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/ || exit 1

# Set default environment variables
ENV CONFIG_FILE=/etc/slimserve/config.json

# Add metadata labels
LABEL maintainer="xeome" \
      description="Lightweight, efficient file server. Environment variables: SLIMSERVE_HOST, SLIMSERVE_PORT, SLIMSERVE_DIRS, SLIMSERVE_DISABLE_DOTFILES, SLIMSERVE_LOG_LEVEL, SLIMSERVE_ENABLE_AUTH, SLIMSERVE_USERNAME, SLIMSERVE_PASSWORD, CONFIG_FILE" \
      version="1.0" \
      org.opencontainers.image.source="https://github.com/xeome/slimserve"

# Use entrypoint script for flexible configuration
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]

# Default arguments (can be overridden)
CMD ["-port", "8080", "-dirs", "/data"]