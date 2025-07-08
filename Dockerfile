# SlimServe Dockerfile
# Multi-stage build for minimal production image

FROM golang:1.24-alpine AS builder

RUN apk add --no-cache build-base git ca-certificates

ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build \
    -buildvcs=false \
    -trimpath \
    -ldflags="-s -w -X main.version=docker" \
    -o slimserve \
    cmd/slimserve/main.go

FROM alpine:3.20

RUN apk add --no-cache \
    ca-certificates \
    wget \
    && rm -rf /var/cache/apk/*

RUN addgroup -g 1001 -S slimserve && \
    adduser -u 1001 -S slimserve -G slimserve

RUN mkdir -p /data && \
    chown slimserve:slimserve /data

COPY --from=builder /app/slimserve /usr/local/bin/slimserve
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

RUN chmod +x /usr/local/bin/slimserve && \
    chmod +x /usr/local/bin/docker-entrypoint.sh

RUN mkdir -p /etc/slimserve && \
    chown slimserve:slimserve /etc/slimserve

USER slimserve
WORKDIR /data
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/ || exit 1

ENV CONFIG_FILE=/etc/slimserve/config.json

LABEL maintainer="xeome" \
      description="Lightweight, efficient file server with admin interface. Environment variables: SLIMSERVE_HOST, SLIMSERVE_PORT, SLIMSERVE_DIRS, SLIMSERVE_DISABLE_DOTFILES, SLIMSERVE_LOG_LEVEL, SLIMSERVE_ENABLE_AUTH, SLIMSERVE_USERNAME, SLIMSERVE_PASSWORD, SLIMSERVE_ENABLE_ADMIN, SLIMSERVE_ADMIN_USERNAME, SLIMSERVE_ADMIN_PASSWORD, SLIMSERVE_MAX_UPLOAD_SIZE_MB, CONFIG_FILE" \
      version="1.0" \
      org.opencontainers.image.source="https://github.com/xeome/slimserve"

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["-port", "8080", "-dirs", "/data"]