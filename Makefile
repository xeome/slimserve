# SlimServe Makefile
# NOTE: If you add or move HTML templates, JS files, or Go-embedded templates,
# update the 'content' array in tailwind.config.js to ensure all Tailwind CSS classes are included.
.PHONY: help build test clean

# Default target
all: build

help:
	@echo "Available targets:"
	@echo "  build         - Build the SlimServe binary"
	@echo "  test          - Run all tests"
	@echo "  clean         - Clean build artifacts"
	@echo "  help          - Show this help message"

# Build the main binary
build:
	go build -o slimserve cmd/slimserve/main.go

# Run tests
test:
	go test ./...

# Clean build artifacts
clean:
	rm -f slimserve
