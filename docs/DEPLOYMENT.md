# SlimServe Docker Deployment

## Quick Start

### Build and Run with Docker Compose
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

### Manual Docker Build and Run
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

## Configuration Options

### Environment Variables
Override any configuration setting using environment variables:
- `SLIMSERVE_HOST` - Server host (default: 0.0.0.0)
- `SLIMSERVE_PORT` - Server port (default: 8080)
- `SLIMSERVE_DIRS` - Comma-separated list of directories to serve
- `SLIMSERVE_DISABLE_DOTFILES` - Disable dot files (true=disable, false=allow, default: true)
- `SLIMSERVE_LOG_LEVEL` - Log level (debug, info, warn, error)
- `SLIMSERVE_ENABLE_AUTH` - Enable session-based authentication (true/false)
- `SLIMSERVE_USERNAME` - Username for authentication
- `SLIMSERVE_PASSWORD` - Password for authentication
- `SLIMSERVE_IGNORE_PATTERNS` - Comma-separated list of glob patterns to ignore
- `CONFIG_FILE` - Path to JSON config file (default: `/etc/slimserve/config.json`)

### Configuration File
Mount a JSON configuration file to `/etc/slimserve/config.json`:
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
  "ignore_patterns": ["*.log", ".git/"]
}
```

### Priority Order
Configuration is loaded in this order (later overrides earlier):
1. Default values
2. JSON configuration file
3. Environment variables
4. Command-line flags

## Production Deployment

### Authentication

SlimServe supports cookie-based session authentication for securing access to files:

```bash
# Enable authentication with username and password
./slimserve -enable-auth -username myuser -password mypassword

# Or via environment variables
export SLIMSERVE_ENABLE_AUTH=true
export SLIMSERVE_USERNAME=myuser
export SLIMSERVE_PASSWORD=mypassword
./slimserve
```

**How it works:**
1. When authentication is enabled, users must first provide valid credentials via HTTP Basic Auth
2. Upon successful authentication, SlimServe sets a secure session cookie (`slimserve_session`)
3. Subsequent requests use the cookie for authentication - no need to repeatedly send credentials
4. Sessions are stored in memory and automatically invalidated when the server restarts
5. This approach eliminates browser credential caching issues common with traditional Basic Auth

**Important**: Always use HTTPS in production by placing SlimServe behind a reverse proxy with SSL termination. This protects both the initial login credentials and the session cookie from interception.

### Security Considerations
- Container runs as non-root user (UID 1001)
- Only serves explicitly whitelisted directories
- Dot files blocked by default (can be disabled with `-disable-dotfiles=false`)
- Path traversal protection built-in
- Cookie-based session authentication with secure defaults (HttpOnly, SameSite=Lax)
- Sessions automatically invalidated on server restart (no persistent storage)
- Always use HTTPS in production when authentication is enabled

### Resource Usage
- Final image size: ~24MB
- Memory usage: <50MB typical
- CPU usage: Minimal under normal load

## Testing the Deployment

### 1. Basic Functionality Test
```bash
# Create test data
mkdir -p data
echo "Hello SlimServe!" > data/test.txt
echo "<h1>Test HTML</h1>" > data/index.html

# Build and start
docker-compose up -d

# Test file serving
curl http://localhost:8080/test.txt
curl http://localhost:8080/index.html

# Test web interface (should show directory listing)
curl http://localhost:8080/
```

### 2. Configuration Testing

#### Test Environment Variables
```bash
# Stop existing container
docker-compose down

# Test custom port and log level
SLIMSERVE_PORT=9090 SLIMSERVE_LOG_LEVEL=debug docker-compose up -d

# Verify new port works
curl http://localhost:9090/

# Check logs show debug level
docker-compose logs slimserve
```

#### Test Config File Override
```bash
# Create custom config
cat > test-config.json << EOF
{
  "port": 8081,
  "directories": ["/data"],
  "disable_dot_files": false,
  "log_level": "warn"
}
EOF

# Update docker-compose.yml to mount test-config.json or run manually:
docker run --rm -p 8081:8081 \
  -v $(pwd)/data:/data \
  -v $(pwd)/test-config.json:/etc/slimserve/config.json:ro \
  slimserve:latest

# Test on new port
curl http://localhost:8081/
```

### 3. Security Testing

#### Test Directory Whitelisting
```bash
# Try to access files outside mounted volume (should fail)
curl http://localhost:8080/../etc/passwd

# Should return 403 or 404, not file contents
```

#### Test Dot File Protection
```bash
# Create dot file
echo "secret" > data/.env

# Test with dot files disabled (default)
curl http://localhost:8080/.env  # Should be blocked

# Test with dot files enabled
SLIMSERVE_DISABLE_DOTFILES=false docker-compose up -d
curl http://localhost:8080/.env  # Should work now
```

### 4. Health Check Testing
```bash
# Check container health status
docker-compose ps

# Should show "healthy" status after ~30 seconds
# If unhealthy, check logs:
docker-compose logs slimserve
```

### 5. Performance Testing
```bash
# Simple load test with curl
for i in {1..10}; do
  curl -s http://localhost:8080/ > /dev/null &
done
wait

# Check logs for performance
docker-compose logs slimserve | tail -20
```

### 6. Volume Mounting Test
```bash
# Create subdirectory
mkdir -p data/subdir
echo "nested file" > data/subdir/nested.txt

# Test nested file access
curl http://localhost:8080/subdir/nested.txt

# Test directory listing includes subdirectory
curl http://localhost:8080/ | grep subdir
```

### 7. Complete Integration Test Script
```bash
#!/bin/bash
set -e

echo "Testing SlimServe Docker deployment..."

# Clean start
docker-compose down 2>/dev/null || true
mkdir -p data

# Create test files
echo "Hello World" > data/hello.txt
echo "<h1>SlimServe Test</h1>" > data/index.html
mkdir -p data/images
echo "fake image" > data/images/test.jpg

# Test 1: Basic startup
echo "Test 1: Basic startup..."
docker-compose up -d
sleep 5

# Test 2: Health check
echo "Test 2: Health check..."
if curl -f http://localhost:8080/ > /dev/null 2>&1; then
  echo "✓ Health check passed"
else
  echo "✗ Health check failed"
  exit 1
fi

# Test 3: File serving
echo "Test 3: File serving..."
RESPONSE=$(curl -s http://localhost:8080/hello.txt)
if [ "$RESPONSE" = "Hello World" ]; then
  echo "✓ File serving works"
else
  echo "✗ File serving failed"
  exit 1
fi

# Test 4: Directory listing
echo "Test 4: Directory listing..."
if curl -s http://localhost:8080/ | grep -q "hello.txt"; then
  echo "✓ Directory listing works"
else
  echo "✗ Directory listing failed"
  exit 1
fi

# Test 5: Custom config
echo "Test 5: Custom configuration..."
docker-compose down
SLIMSERVE_LOG_LEVEL=debug docker-compose up -d
sleep 3

if docker-compose logs slimserve | grep -q "level.*debug"; then
  echo "✓ Environment variable configuration works"
else
  echo "✗ Environment variable configuration failed"
fi

echo "All tests passed! 🎉"
docker-compose down
```

### Common Issues
- **Port already in use**: Change `SLIMSERVE_PORT` or stop conflicting services
- **Permission denied**: Ensure data directory is readable by UID 1001
- **Config file not found**: Check volume mount path and file permissions
- **404 errors**: Verify directories are correctly whitelisted in config
## Troubleshooting

### Check logs
```bash
docker-compose logs slimserve
```

### Verify configuration
The entrypoint script logs which config file is being used on startup.

### Test health
```bash
curl http://localhost:8080/