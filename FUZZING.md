# SlimServe Security Fuzzing

This document describes the comprehensive fuzzing test infrastructure implemented for SlimServe's security validation.

## Overview

SlimServe includes exhaustive Go fuzz tests that validate security boundaries against various attack patterns including:

- Path traversal attacks (`../`, `%2e%2e%2f`, etc.)
- Directory access control validation
- Dot file blocking when configured
- Thumbnail generation security
- Static asset serving protection
- Unicode and special character handling
- Malformed request handling

## Fuzz Tests

### 1. FuzzRequestPath

**Location**: [`internal/server/fuzz_request_path_test.go`](internal/server/fuzz_request_path_test.go:20)

Tests the main HTTP handler against various malicious path inputs:

- **Path traversal**: `../`, `../../etc/passwd`, URL-encoded variants
- **Directory whitelisting**: Attempts to access outside allowed directories
- **Dot file blocking**: Hidden files and directories (`.env`, `.git/config`)
- **Unicode attacks**: Special characters, null bytes, control characters
- **Windows-specific**: Reserved names (`CON`, `PRN`, `AUX`)
- **Injection attempts**: SQL injection, XSS in paths
- **Very long paths**: Buffer overflow attempts

**Expected behavior**: Never returns 5xx server errors, always returns 2xx for legitimate paths, 4xx for blocked paths.

### 2. FuzzThumbnailQuery

**Location**: [`internal/server/fuzz_request_path_test.go`](internal/server/fuzz_request_path_test.go:194)

Tests thumbnail generation with various query parameters and filenames:

- **Path traversal in image paths**: `../../../etc/passwd?thumb=1`
- **Malicious query parameters**: XSS, injection, oversized values
- **Invalid image formats**: Non-image files with `?thumb=1`
- **Parameter manipulation**: Invalid sizes, qualities, formats
- **Unicode filenames**: Emoji, international characters
- **Very long filenames**: Buffer overflow attempts

**Expected behavior**: Never causes server errors during thumbnail generation, gracefully handles unsupported formats.

### 3. FuzzStaticAssets

**Location**: [`internal/server/fuzz_request_path_test.go`](internal/server/fuzz_request_path_test.go:400)

Tests static asset serving with malicious paths:

- **Path traversal from static root**: `/static/../../../etc/passwd`
- **Embedded filesystem attacks**: Attempts to break out of embedded assets
- **Invalid static paths**: Non-existent assets, malformed paths
- **Control characters**: Null bytes, special characters in paths

**Expected behavior**: Never serves files outside the embedded static filesystem, returns appropriate 404 for missing assets.

## Usage

### Running Fuzz Tests

```bash
# Run all tests
go test ./internal/server

# Run specific fuzz test for 30 seconds
go test ./internal/server -fuzz=FuzzRequestPath -fuzztime=30s

# Run all fuzz tests using Makefile
make fuzz-short    # 30 seconds each
make fuzz-long     # 5 minutes each
make fuzz-go       # Generic fuzzing for 30 seconds
```

### Makefile Targets

- **`make fuzz-short`**: Run all fuzz tests for 30 seconds each
- **`make fuzz-long`**: Extended fuzzing for 5 minutes each

### Interpreting Results

The fuzzing tests use Go's built-in fuzzing framework and will:

1. **Start with seed corpus**: Predefined attack patterns
2. **Generate mutations**: Automatically create variations
3. **Track coverage**: Find new code paths
4. **Report crashes**: Any panics or unexpected errors
5. **Generate interesting inputs**: Save inputs that trigger new behaviors

### Expected Output

```
fuzz: elapsed: 30s, execs: 30881 (578/sec), new interesting: 50 (total: 182)
PASS
```

- **execs**: Number of test cases executed
- **new interesting**: Inputs that triggered new code paths
- **total**: Total unique behaviors discovered

## Security Validation

### What the Tests Validate

1. **Path Traversal Protection**: Ensures `../` attacks are blocked
2. **Directory Whitelisting**: Validates only allowed directories are accessible
3. **Dot File Blocking**: Confirms hidden files are protected when configured
4. **Error Handling**: No 500 errors on malformed inputs
5. **Resource Limits**: No crashes on very long paths or large inputs
6. **Encoding Attacks**: Handles URL-encoded attack attempts

### What Would Indicate Problems

- **Server errors (5xx)**: Indicates unhandled input causing crashes
- **Successful path traversal**: Accessing files outside allowed directories
- **Dot file leakage**: Serving hidden files when `DisableDotFiles=true`
- **Panic/crashes**: Unhandled exceptions during fuzzing

## Test Coverage

The fuzz tests complement existing unit tests by:

- **Testing edge cases**: Automated discovery of unusual inputs
- **Stress testing**: High-volume automated testing
- **Mutation testing**: Variations of known attack patterns
- **Coverage expansion**: Finding untested code paths

## Corpus Management

### Seed Data Location

- **Corpus directory**: [`testdata/fuzz_corpus/`](testdata/fuzz_corpus/)
- **Hardcoded seeds**: Embedded in each fuzz function
- **Generated corpus**: Go automatically saves interesting inputs

### Adding New Test Cases

1. Add new attack patterns to the `seeds` slice in each fuzz function
2. Place sample inputs in `testdata/fuzz_corpus/`
3. Run tests to verify new patterns are covered

## Integration with CI/CD

The fuzz tests are designed to run quickly for CI while supporting longer runs for security audits:

```bash
# Quick CI validation (30 seconds)
make fuzz-short

# Security audit (longer duration)
make fuzz-long
```

## Security Considerations

This fuzzing infrastructure helps ensure SlimServe's security against:

- **Path traversal attacks**: Common web vulnerability
- **Directory access control bypass**: Unauthorized file access
- **Information disclosure**: Hidden file leakage
- **Denial of service**: Resource exhaustion attacks
- **Injection attacks**: XSS, SQL injection attempts in file paths

The comprehensive test coverage provides confidence in SlimServe's security boundaries and helps detect regressions during development.