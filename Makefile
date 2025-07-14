# SlimServe Makefile
.PHONY: help build build-dev build-release cov test clean fuzz-go fuzz-short fuzz-long docker-build docker-run version bench bench-cache bench-thumbnail bench-server bench-all

# Version information
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT_HASH ?= $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
BUILD_USER ?= $(shell whoami)@$(shell hostname)

# Go build flags
LDFLAGS = -X slimserve/internal/version.Version=$(VERSION) \
          -X slimserve/internal/version.CommitHash=$(COMMIT_HASH) \
          -X slimserve/internal/version.BuildDate=$(BUILD_DATE) \
          -X slimserve/internal/version.BuildUser=$(BUILD_USER)

all: build

help:
	@echo "Available targets:"
	@echo "  build         - Build the SlimServe binary (development)"
	@echo "  build-dev     - Build the SlimServe binary with debug info"
	@echo "  build-release - Build optimized release binary"
	@echo "  version       - Show version information"
	@echo "  cov           - Run tests with coverage"
	@echo "  test          - Run all tests"
	@echo "  bench         - Run all benchmarks"
	@echo "  bench-cache   - Run cache operation benchmarks"
	@echo "  bench-thumbnail - Run thumbnail generation benchmarks"
	@echo "  bench-server  - Run server/handler benchmarks"
	@echo "  bench-all     - Run comprehensive benchmarks with detailed output"
	@echo "  fuzz-go       - Run Go fuzz tests (short duration)"
	@echo "  fuzz-short    - Run fuzz tests for 30 seconds"
	@echo "  fuzz-long     - Run fuzz tests for 5 minutes"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-run    - Run Docker container"
	@echo "  clean         - Clean build artifacts"
	@echo "  help          - Show this help message"

build: build-dev

build-dev:
	@echo "Building SlimServe (development)..."
	@echo "Version: $(VERSION)"
	@echo "Commit: $(COMMIT_HASH)"
	@echo "Build Date: $(BUILD_DATE)"
	@echo "Build User: $(BUILD_USER)"
	go build -ldflags "$(LDFLAGS)" -o slimserve cmd/slimserve/main.go

build-release:
	@echo "Building SlimServe (release)..."
	@echo "Version: $(VERSION)"
	@echo "Commit: $(COMMIT_HASH)"
	@echo "Build Date: $(BUILD_DATE)"
	@echo "Build User: $(BUILD_USER)"
	go build -ldflags "$(LDFLAGS) -s -w" -o slimserve cmd/slimserve/main.go

version:
	@echo "Version: $(VERSION)"
	@echo "Commit: $(COMMIT_HASH)"
	@echo "Build Date: $(BUILD_DATE)"
	@echo "Build User: $(BUILD_USER)"

cov:
	go test -cover ./...

test:
	go test ./...

# Benchmark targets
bench:
	@echo "Running all benchmarks..."
	go test -bench=. -benchmem ./internal/files ./internal/server

bench-cache:
	@echo "Running cache operation benchmarks..."
	go test -bench=BenchmarkCache -benchmem ./internal/files

bench-thumbnail:
	@echo "Running thumbnail generation benchmarks..."
	go test -bench=BenchmarkGenerate -benchmem ./internal/files
	go test -bench=BenchmarkThumbnail -benchmem ./internal/files

bench-server:
	@echo "Running server/handler benchmarks..."
	go test -bench=. -benchmem ./internal/server

bench-all:
	@echo "Running comprehensive benchmarks with detailed output..."
	@echo "=== Cache Operations ==="
	go test -bench=BenchmarkCache -benchmem -benchtime=5s ./internal/files
	@echo ""
	@echo "=== Thumbnail Generation ==="
	go test -bench=BenchmarkGenerate -benchmem -benchtime=5s ./internal/files
	go test -bench=BenchmarkThumbnail -benchmem -benchtime=5s ./internal/files
	@echo ""
	@echo "=== Server Operations ==="
	go test -bench=BenchmarkServe -benchmem -benchtime=5s ./internal/server
	@echo ""
	@echo "=== Middleware ==="
	go test -bench=BenchmarkMiddleware -benchmem -benchtime=5s ./internal/server
	go test -bench=BenchmarkAccess -benchmem -benchtime=5s ./internal/server
	go test -bench=BenchmarkRoute -benchmem -benchtime=5s ./internal/server
	go test -bench=BenchmarkPath -benchmem -benchtime=5s ./internal/server
	go test -bench=BenchmarkConcurrent -benchmem -benchtime=5s ./internal/server

fuzz-short:
	@echo "Running path fuzzing..."
	go test ./internal/server -fuzz=FuzzRequestPath -fuzztime=30s
	@echo "Running thumbnail fuzzing..."
	go test ./internal/server -fuzz=FuzzThumbnailQuery -fuzztime=30s
	@echo "Running static asset fuzzing..."
	go test ./internal/server -fuzz=FuzzStaticAssets -fuzztime=30s

fuzz-long:
	@echo "Running extended path fuzzing..."
	go test ./internal/server -fuzz=FuzzRequestPath -fuzztime=5m
	@echo "Running extended thumbnail fuzzing..."
	go test ./internal/server -fuzz=FuzzThumbnailQuery -fuzztime=5m
	@echo "Running extended static asset fuzzing..."
	go test ./internal/server -fuzz=FuzzStaticAssets -fuzztime=5m

docker-build:
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT_HASH=$(COMMIT_HASH) --build-arg BUILD_DATE=$(BUILD_DATE) --build-arg BUILD_USER=$(BUILD_USER) -t slimserve:latest .

docker-run:
	docker run --rm -p 8080:8080 slimserve:latest

clean:
	rm -f slimserve
