# SlimServe

[![Go Report Card](https://goreportcard.com/badge/github.com/xeome/slimserve)](https://goreportcard.com/report/github.com/xeome/slimserve)
[![Coverage](https://img.shields.io/codecov/c/github/xeome/slimserve?label=coverage)](https://codecov.io/gh/xeome/slimserve)
[![Docker Pulls](https://img.shields.io/docker/pulls/xeome/slimserve)](https://hub.docker.com/r/xeome/slimserve)
[![License](https://img.shields.io/badge/license-GPLv3-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.24.4-blue.svg)](https://golang.org/dl/)

> A lightweight, suckless HTTP file server designed with simplicity and security in mind.

SlimServe is a minimalistic and efficient file-serving application that provides seamless file sharing over HTTP with minimal dependencies, configuration, and resource usage. Built with a suckless philosophy—simple, fast, and easy to use.

## Features

- 🚀 **Single-binary deployment** with embedded assets (no external dependencies)
- 🔧 **Zero configuration** by default with sensible defaults
- 🔒 **Secure file serving** with directory whitelisting and path traversal protection
- 🎨 **Modern responsive web interface** with grid/list views and dark mode
- 🖼️ **On-demand thumbnail generation** for images (JPEG, PNG, GIF, WebP)
- 📝 **Structured logging** with configurable levels
- 🔐 **Configurable dot-file protection** and cookie-based session authentication
- 🐳 **Docker deployment support** (23.9MB production image)
- 🧪 **Comprehensive security fuzzing** infrastructure
- 🌐 **Cross-platform support** (Linux, BSD, macOS, Windows)
- ⚡ **Lightweight and fast** - minimal resource usage

## Architecture

```mermaid
flowchart TD
    Main[cmd/slimserve/main.go] --> Config[internal/config/loader.go]
    Config --> Logger[internal/logger/logger.go]
    Logger --> Server[internal/server/server.go]
    Server --> GinEngine[gin.Engine]
    GinEngine --> Middleware[logging & access control]
    GinEngine --> Handler[internal/server/handler.go]
    Handler --> FS[internal/security/rootfs.go]
    Handler --> Templates[web/embed.go]
    Handler --> Thumbnail[internal/files/thumbnail.go]
```

## Quick Start

### Binary Installation

```bash
# Build from source
git clone https://github.com/xeome/slimserve.git
cd slimserve
make build

# Run with default settings
./slimserve -dirs ./data
```

### Docker Compose (Recommended)

```bash
# Create data directory
mkdir -p data

# Build and start the service
docker-compose up -d

# View logs
docker-compose logs -f slimserve

# Stop the service
docker-compose down
```

### Manual Docker Run

```bash
# Build the image
docker build -t slimserve:latest .

# Run with mounted directory
docker run --rm -p 8080:8080 -v $(pwd)/data:/data slimserve:latest

# Run with custom config file
docker run --rm -p 8080:8080 \
  -v $(pwd)/data:/data \
  -v $(pwd)/my-config.json:/etc/slimserve/config.json:ro \
  slimserve:latest
```

## Configuration

SlimServe supports multiple configuration methods with the following priority order:

| Source                | Priority    | Description                                 |
| --------------------- | ----------- | ------------------------------------------- |
| CLI flags             | 4 (highest) | Command-line arguments                      |
| Environment variables | 3           | `SLIMSERVE_*` prefixed variables            |
| JSON config file      | 2           | Configuration file (default: `config.json`) |
| Defaults              | 1 (lowest)  | Built-in sensible defaults                  |

### Environment Variables

Override any configuration setting using environment variables:

- `SLIMSERVE_HOST` - Server host (default: `0.0.0.0`)
- `SLIMSERVE_PORT` - Server port (default: `8080`)
- `SLIMSERVE_DIRS` - Comma-separated list of directories to serve
- `SLIMSERVE_DISABLE_DOTFILES` - Disable dot files (`true`=disable, `false`=allow, default: `true`)
- `SLIMSERVE_LOG_LEVEL` - Log level (`debug`, `info`, `warn`, `error`)
- `SLIMSERVE_ENABLE_AUTH` - Enable session-based authentication (`true`/`false`)
- `SLIMSERVE_USERNAME` - Username for authentication
- `SLIMSERVE_PASSWORD` - Password for authentication
- `SLIMSERVE_THUMB_CACHE_MB` - Thumbnail cache size in MB (default: `100`)
- `CONFIG_FILE` - Path to JSON config file

### Configuration File

Create a JSON configuration file (see [`example-config.json`](example-config.json)):

```json
{
  "host": "0.0.0.0",
  "port": 8080,
  "directories": ["/data", "/shared"],
  "disable_dot_files": true,
  "log_level": "info",
  "enable_auth": false,
  "username": "",
  "password": "",
  "thumb_cache_mb": 100
}
```

## Usage

SlimServe can be run directly with command-line flags or configured via a JSON file.

### Command-line flags

| Flag                | Environment Variable         | Default   | Description                             |
| ------------------- | ---------------------------- | --------- | --------------------------------------- |
| `-host`             | `SLIMSERVE_HOST`             | `0.0.0.0` | Host address to bind to                 |
| `-port`             | `SLIMSERVE_PORT`             | `8080`    | Port to listen on                       |
| `-dirs`             | `SLIMSERVE_DIRS`             | `.`       | Directories to serve (comma-separated)  |
| `-config`           | `SLIMSERVE_CONFIG`           | -         | Path to JSON configuration file         |
| `-log-level`        | `SLIMSERVE_LOG_LEVEL`        | `info`    | Logging level: debug, info, warn, error |
| `-disable-dotfiles` | `SLIMSERVE_DISABLE_DOTFILES` | `true`    | Disable serving dot-files for security  |
| `-enable-auth`      | `SLIMSERVE_ENABLE_AUTH`      | `false`   | Enable session-based authentication     |
| `-username`         | `SLIMSERVE_USERNAME`         | -         | Username for authentication             |
| `-password`         | `SLIMSERVE_PASSWORD`         | -         | Password for authentication             |
| `-thumb-cache-mb`   | `SLIMSERVE_THUMB_CACHE_MB`   | `100`     | Thumbnail cache size in MB              |

### Example usage

```bash
# Serve current directory
./slimserve

# Serve specific directories on custom port
./slimserve -port 3000 -dirs "/home/user/docs,/var/www"

# Use configuration file
./slimserve -config config.json

# Enable debug logging and allow dot-files
./slimserve -log-level debug -disable-dotfiles=false

# Enable session-based authentication
./slimserve -enable-auth -username alice -password secret
```

## Security Features

- **Path Traversal Protection**: Uses Go 1.24's `os.Root` for traversal-resistant file operations
- **Directory Whitelisting**: Only serves explicitly configured directories
- **Dot-file Protection**: Configurable blocking of hidden files (enabled by default)
- **Non-root Container**: Docker container runs as UID 1001 for security
- **Cookie-based Session Authentication**: In-memory session management with automatic logout on server restart
- **Security Fuzzing**: Comprehensive fuzzing tests for vulnerability detection

## Thumbnail Generation

SlimServe automatically generates thumbnails for supported image formats:

- **Supported formats**: JPEG, PNG, GIF, WebP
- **On-demand generation**: Thumbnails created only when requested
- **Intelligent caching**: Four-step cache-key algorithm detects file changes
- **Automatic pruning**: Configurable cache size with LRU eviction
- **Performance optimized**: Efficient image scaling algorithms

Configure thumbnail cache size:
```bash
export SLIMSERVE_THUMB_CACHE_MB=200  # 200MB cache
```

## Development

### Building

```bash
# Build binary
make build

# Run tests
make test

# Run security fuzz tests
make fuzz-short    # 30 seconds
make fuzz-long     # 5 minutes each test

# Build Docker image
make docker-build

# Clean build artifacts
make clean
```

### Testing

The project includes comprehensive testing:

- **Unit tests**: 229 test cases with 88.6% coverage
- **Fuzz testing**: Security-focused fuzzing for path traversal, thumbnails, and static assets
- **Integration tests**: End-to-end Docker deployment testing

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run fuzz tests
go test ./internal/server -fuzz=FuzzRequestPath -fuzztime=30s
```

See [`FUZZING.md`](FUZZING.md) for detailed security testing information.

## Docker Deployment

### Production Deployment

The Docker image is optimized for production use:

- **Image size**: ~24MB (Alpine-based)
- **Memory usage**: <50MB typical
- **CPU usage**: Minimal under normal load
- **Security**: Runs as non-root user (UID 1001)

### Health Checks

Built-in health check endpoint:
```bash
curl http://localhost:8080/
```

Docker health check is automatically configured with 30-second intervals.

## API

SlimServe provides a simple HTTP API:

- `GET /` - Directory listing or file serving
- `GET /path/to/file` - Serve specific file
- `GET /path/to/image?thumb=1` - Serve thumbnail for images
- `GET /path/to/dir/` - Directory listing with navigation

All responses include appropriate MIME types and security headers.

## Performance

- **Startup time**: <100ms typical
- **Memory footprint**: <50MB under normal load
- **Concurrent connections**: Handles hundreds of simultaneous requests
- **Static asset serving**: Embedded assets served from memory
- **Thumbnail caching**: Efficient caching reduces regeneration overhead

## Contributing

We welcome contributions! Please:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes and add tests
4. Ensure code passes: `go fmt`, `go vet`, `make test`
5. Run security tests: `make fuzz-short`
6. Commit your changes (`git commit -m 'Add amazing feature'`)
7. Push to the branch (`git push origin feature/amazing-feature`)
8. Open a Pull Request

### Code Style

- Follow standard Go formatting (`go fmt`)
- Add tests for new functionality
- Update documentation as needed
- Run `go vet` before submitting

## Roadmap

- [ ] WebDAV support for file uploads
- [ ] Video thumbnail generation
- [ ] Plugin system for custom handlers
- [ ] Advanced authentication (OAuth, LDAP)
- [ ] File search and indexing
- [ ] Real-time file change notifications

## License

This project is licensed under the GNU General Public License v3.0 - see the [LICENSE](LICENSE) file for details.

## Acknowledgements

SlimServe is built with these excellent open-source projects:

- [Gin](https://github.com/gin-gonic/gin) - HTTP web framework
- [Zerolog](https://github.com/rs/zerolog) - Structured logging
- [Tailwind CSS](https://tailwindcss.com/) - Utility-first CSS framework
- [Alpine.js](https://alpinejs.dev/) - Lightweight JavaScript framework
- [Heroicons](https://heroicons.com/) - Beautiful hand-crafted SVG icons

---

**SlimServe** - Simple, secure, and suckless file serving. 🚀