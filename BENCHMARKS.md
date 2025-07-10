# SlimServe Performance Benchmarks

This document describes the comprehensive benchmarking suite for SlimServe's critical code paths.

## Overview

SlimServe includes extensive benchmarks for performance-critical operations including:

- **Cache Operations**: Cache key generation, file collection, pruning, and size calculations
- **Thumbnail Generation**: Image processing, scaling, encoding, and caching
- **File Serving**: File delivery, directory listing, and path validation
- **Request Routing**: HTTP request handling, middleware processing, and authentication
- **Concurrent Operations**: Multi-threaded performance under load

## Running Benchmarks

### Quick Start

```bash
# Run all benchmarks
make bench

# Run specific benchmark categories
make bench-cache      # Cache operations only
make bench-thumbnail  # Thumbnail generation only  
make bench-server     # Server/handler operations only

# Run comprehensive benchmarks with detailed output
make bench-all
```

### Manual Execution

```bash
# Run specific benchmark functions
go test -bench=BenchmarkGenerateCacheKey -benchmem ./internal/files
go test -bench=BenchmarkServeFiles -benchmem ./internal/server
go test -bench=BenchmarkAccessControl -benchmem ./internal/server

# Run with custom parameters
go test -bench=. -benchmem -benchtime=10s ./internal/files
go test -bench=. -benchmem -cpu=1,2,4,8 ./internal/server
```

## Benchmark Categories

### Cache Operations (`internal/files`)

- **BenchmarkCacheSizeMB**: Cache size calculation performance
- **BenchmarkCacheCollectFiles**: File collection with varying file counts (10-500 files)
- **BenchmarkCachePrune**: Cache pruning operations with large datasets
- **BenchmarkCachePruneIfNeeded**: Conditional pruning logic
- **BenchmarkIsImageFile**: File type detection performance

### Thumbnail Generation (`internal/files`)

- **BenchmarkGenerateCacheKey**: Cache key generation with different dimensions
- **BenchmarkGenerateCacheKeyLargeFile**: Cache key generation for large files
- **BenchmarkThumbnailGeneration**: Complete thumbnail generation pipeline

### File Serving (`internal/server`)

- **BenchmarkServeFiles**: Directory listing with varying sizes
- **BenchmarkServeFileFromRoot**: Individual file serving with different file sizes
- **BenchmarkServeThumbnailFromRoot**: Thumbnail serving performance
- **BenchmarkContainsDotFile**: Dot file detection
- **BenchmarkTryServeFromRoots**: Multi-root file resolution

### Request Processing (`internal/server`)

- **BenchmarkAccessControlMiddleware**: Path validation and security checks
- **BenchmarkSessionAuthMiddleware**: Authentication middleware performance
- **BenchmarkCreateUnifiedHandler**: Complete request routing
- **BenchmarkPathValidation**: Path cleaning and validation
- **BenchmarkRouteMatching**: Route pattern matching
- **BenchmarkConcurrentRequests**: Concurrent request handling
- **BenchmarkMiddlewareChain**: Complete middleware stack

## Performance Targets

### Expected Performance (AMD Ryzen 5 7600, 12 cores)

| Operation | Target Performance | Memory Usage |
|-----------|-------------------|--------------|
| Cache Key Generation | ~9,000 ns/op | ~2KB/op |
| File Serving (1KB) | ~14,000 ns/op | ~15KB/op |
| File Serving (1MB) | ~340,000 ns/op | ~2MB/op |
| Directory Listing (50 files) | ~156,000 ns/op | ~137KB/op |
| Thumbnail Generation (256px) | ~5,600,000 ns/op | ~2.2MB/op |
| Access Control Check | ~2,000 ns/op | ~6.5KB/op |
| Route Matching | ~0.2 ns/op | 0 allocs |

### Performance Monitoring

Monitor these key metrics for performance regressions:

1. **Cache Key Generation**: Should remain under 10μs per operation
2. **File Serving**: Linear scaling with file size, ~300ns per KB
3. **Directory Listing**: Should handle 200+ files under 200μs
4. **Thumbnail Generation**: Should complete 256px thumbnails under 6ms
5. **Middleware Processing**: Should process requests under 2μs per middleware

## Optimization Guidelines

### Cache Operations
- Cache key generation is CPU-bound (hashing operations)
- File collection scales linearly with file count
- Pruning performance depends on filesystem operations

### Thumbnail Generation
- Image decoding dominates processing time
- Memory usage scales with image dimensions
- JPEG encoding is faster than PNG for thumbnails

### File Serving
- Small files (< 100KB) are memory-bound
- Large files (> 1MB) are I/O-bound
- Directory listing scales with entry count

### Request Processing
- Route matching is highly optimized (sub-nanosecond)
- Middleware overhead is minimal (~2μs per request)
- Concurrent performance scales well with CPU cores

## Continuous Integration

Add benchmark monitoring to CI/CD:

```bash
# Run benchmarks and save baseline
go test -bench=. -benchmem ./... > benchmarks.txt

# Compare against baseline (requires benchcmp tool)
go test -bench=. -benchmem ./... | benchcmp benchmarks.txt /dev/stdin
```

## Profiling

For detailed performance analysis:

```bash
# CPU profiling
go test -bench=BenchmarkThumbnailGeneration -cpuprofile=cpu.prof ./internal/files
go tool pprof cpu.prof

# Memory profiling  
go test -bench=BenchmarkServeFiles -memprofile=mem.prof ./internal/server
go tool pprof mem.prof

# Trace analysis
go test -bench=BenchmarkConcurrentRequests -trace=trace.out ./internal/server
go tool trace trace.out
```

## Contributing

When adding new features:

1. Add corresponding benchmarks for performance-critical code
2. Ensure benchmarks cover realistic usage scenarios
3. Document expected performance characteristics
4. Run benchmarks before and after changes to detect regressions

For benchmark naming conventions:
- Use descriptive names: `BenchmarkOperationScenario`
- Include sub-benchmarks for different parameters
- Add memory benchmarking with `-benchmem`
- Test edge cases and realistic workloads
