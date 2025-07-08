# SlimServe Makefile
# NOTE: If you add or move HTML templates, JS files, or Go-embedded templates,
# update the 'content' array in tailwind.config.js to ensure all Tailwind CSS classes are included.
.PHONY: help build test clean fuzz-go fuzz-short fuzz-long docker-build docker-run

all: build

help:
	@echo "Available targets:"
	@echo "  build         - Build the SlimServe binary"
	@echo "  test          - Run all tests"
	@echo "  fuzz-go       - Run Go fuzz tests (short duration)"
	@echo "  fuzz-short    - Run fuzz tests for 30 seconds"
	@echo "  fuzz-long     - Run fuzz tests for 5 minutes"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-run    - Run Docker container"
	@echo "  clean         - Clean build artifacts"
	@echo "  help          - Show this help message"

build:
	go build -o slimserve cmd/slimserve/main.go

test:
	go test ./...

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
	docker build -t slimserve:latest .

docker-run:
	docker run --rm -p 8080:8080 slimserve:latest

clean:
	rm -f slimserve
