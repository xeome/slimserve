# Fuzz Test Corpus

This directory contains seed data for Go fuzz tests.

The Go fuzzing engine will automatically discover and use files in this directory
as additional seed inputs for the fuzz tests.

## Structure

- Each file contains input data for fuzzing
- Files are automatically detected by Go's fuzzing framework
- Additional seed data can be added manually or generated during fuzzing

## Usage

The fuzz tests will use both:
1. Hardcoded seeds in the test functions
2. Any files placed in this corpus directory

## Security Focus

The fuzz tests focus on:
- Path traversal attacks (../, %2e%2e%2f, etc.)
- Directory access control validation
- Dot file blocking when configured
- Thumbnail generation security
- Static asset serving protection
- Unicode and special character handling
- Malformed request handling

## Running Fuzz Tests

```bash
# Run all fuzz tests (short duration)
go test ./internal/server -fuzz=Fuzz -fuzztime=30s

# Run specific fuzz test
go test ./internal/server -fuzz=FuzzRequestPath -fuzztime=1m

# Run with longer duration for more comprehensive testing
go test ./internal/server -fuzz=FuzzThumbnailQuery -fuzztime=5m