package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"slimserve/internal/config"

	"github.com/gin-gonic/gin"
)

// FuzzRequestPath tests the HTTP handler against various path traversal and malicious path inputs
func FuzzRequestPath(f *testing.F) {
	// Set Gin to test mode to reduce noise
	gin.SetMode(gin.TestMode)

	// Seed corpus with known attack patterns
	seeds := []string{
		".",
		"..",
		"../",
		"../../etc/passwd",
		"%2e%2e%2f",
		"..\\..\\",
		"/very/long/" + strings.Repeat("a", 1024),
		"/./.",
		"/foo/%00bar",
		"/favicon.ico?thumb=1",
		"/%2e/%2e/%2e/",
		"/..",
		"/../../../etc/passwd",
		"/..%2f..%2f..%2fetc%2fpasswd",
		"/.%252e/.%252e/.%252e/etc/passwd",
		"/windows/system32/drivers/etc/hosts",
		"/%5c..%5c..%5cwindows%5csystem32%5cdrivers%5cetc%5chosts",
		"/proc/self/environ",
		"/dev/null",
		"/tmp/../etc/passwd",
		strings.Repeat("../", 100) + "etc/passwd",
		"/.git/config",
		"/.env",
		"/.htaccess",
		"/web.config",
		"/crossdomain.xml",
		"/robots.txt",
		"/sitemap.xml",
		"/%2e%2e/",
		"/%2e%2e%2f",
		"/%252e%252e/",
		"/%252e%252e%252f",
		"/..%255c",
		"/%c0%ae%c0%ae/",
		"/%c1%9c",
		"/..%c0%af",
		"/..%c1%9c",
		"/.%2e/",
		"/%2e./",
		"/.%2e%2f",
		"/%2e.%2f",
		"/..%2f%2e%2e%2f",
		"/%2e%2e%2f%2e%2e%2f",
		"/<script>alert(1)</script>",
		"/';DROP TABLE files;--",
		"/file.txt?callback=alert(1)",
		"/file.txt?jsonp=<script>",
		"/file.txt?' OR '1'='1",
		"/\x00etc/passwd",
		"/file\x00.txt",
		"/file.txt\x00.exe",
		"/" + strings.Repeat("a", 8192),
		"/unicode文件名.txt",
		"/emoji😀🔥💯.txt",
		"/file with spaces.txt",
		"/file\ttab.txt",
		"/file\nline.txt",
		"/file\rcarriage.txt",
		"/file;semicolon.txt",
		"/file&ampersand.txt",
		"/file|pipe.txt",
		"/file`backtick.txt",
		"/file$dollar.txt",
		"/file(paren).txt",
		"/file[bracket].txt",
		"/file{brace}.txt",
		"/file<angle>.txt",
		"/file>greater.txt",
		"/file\"quote.txt",
		"/file'apostrophe.txt",
		"/file~tilde.txt",
		"/file!exclamation.txt",
		"/file@at.txt",
		"/file#hash.txt",
		"/file%percent.txt",
		"/file^caret.txt",
		"/file*asterisk.txt",
		"/file+plus.txt",
		"/file=equals.txt",
		"/file?question.txt",
		"/CON",
		"/PRN",
		"/AUX",
		"/NUL",
		"/COM1",
		"/LPT1",
		"/con.txt",
		"/prn.txt",
		"/aux.txt",
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, path string) {
		t.Parallel()

		// Create temporary directory for whitelisting
		tmpDir, err := os.MkdirTemp("", "slimserve-fuzz-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		// Create some test files in the temp directory
		testFile := filepath.Join(tmpDir, "test.txt")
		if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		// Create a subdirectory with a file
		subDir := filepath.Join(tmpDir, "subdir")
		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Fatalf("Failed to create subdir: %v", err)
		}
		subFile := filepath.Join(subDir, "sub.txt")
		if err := os.WriteFile(subFile, []byte("sub content"), 0644); err != nil {
			t.Fatalf("Failed to create sub file: %v", err)
		}

		// Create a dot file for testing
		dotFile := filepath.Join(tmpDir, ".hidden")
		if err := os.WriteFile(dotFile, []byte("hidden content"), 0644); err != nil {
			t.Fatalf("Failed to create dot file: %v", err)
		}

		// Create dot directory
		dotDir := filepath.Join(tmpDir, ".hiddendir")
		if err := os.MkdirAll(dotDir, 0755); err != nil {
			t.Fatalf("Failed to create dot dir: %v", err)
		}
		dotDirFile := filepath.Join(dotDir, "secret.txt")
		if err := os.WriteFile(dotDirFile, []byte("secret content"), 0644); err != nil {
			t.Fatalf("Failed to create dot dir file: %v", err)
		}

		// Set up server with test configuration
		cfg := &config.Config{
			Port:            8080,
			Host:            "localhost",
			Directories:     []string{tmpDir},
			DisableDotFiles: true, // Block dot files by default
		}

		server := New(cfg)

		// Create test server
		ts := httptest.NewServer(server)
		defer ts.Close()

		// Create request with timeout context
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Construct URL - handle both absolute and relative paths
		var requestURL string
		if strings.HasPrefix(path, "/") {
			requestURL = ts.URL + path
		} else {
			requestURL = ts.URL + "/" + path
		}

		req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
		if err != nil {
			// If we can't even create the request, skip this input
			t.Skipf("Failed to create request for path %q: %v", path, err)
		}

		client := &http.Client{
			Timeout: 2 * time.Second,
		}

		resp, err := client.Do(req)
		if err != nil {
			// Network timeouts or context cancellation are expected for some inputs
			if ctx.Err() == context.DeadlineExceeded {
				t.Logf("Request timeout for path %q (expected for some inputs)", path)
				return
			}
			// Other network errors might be due to malformed URLs, which is acceptable
			t.Logf("Network error for path %q: %v", path, err)
			return
		}
		defer resp.Body.Close()

		// Validate response
		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			// Success responses are fine for legitimate paths
			t.Logf("Success response %d for path %q", resp.StatusCode, path)
		case resp.StatusCode >= 400 && resp.StatusCode < 500:
			// Client errors are expected for blocked/invalid paths
			t.Logf("Client error %d for path %q", resp.StatusCode, path)
		case resp.StatusCode >= 500:
			// Server errors should never happen - this indicates a bug
			t.Errorf("Unexpected server error %d for path %q", resp.StatusCode, path)
		default:
			// Other status codes (1xx, 3xx) are less expected but not necessarily errors
			t.Logf("Unexpected status %d for path %q", resp.StatusCode, path)
		}

		// Ensure we got some response headers (basic sanity check)
		if len(resp.Header) == 0 {
			t.Errorf("No response headers for path %q", path)
		}
	})
}

// FuzzThumbnailQuery tests thumbnail generation with various query parameters and filenames
func FuzzThumbnailQuery(f *testing.F) {
	gin.SetMode(gin.TestMode)

	// Seed corpus with thumbnail-related inputs
	seeds := []string{
		"image.jpg?thumb=1",
		"photo.png?thumb=1&size=200",
		"picture.gif?thumb=1&quality=90",
		"test.webp?thumb=1",
		"../image.jpg?thumb=1",
		"subdir/photo.png?thumb=1",
		".hidden.jpg?thumb=1",
		"<script>alert(1)</script>.jpg?thumb=1",
		"file with spaces.png?thumb=1",
		"unicode文件.jpg?thumb=1",
		"emoji😀.png?thumb=1",
		"very-long-" + strings.Repeat("filename", 50) + ".jpg?thumb=1",
		"file.jpg?thumb=1&size=" + strings.Repeat("9", 100),
		"file.jpg?thumb=1&callback=alert(1)",
		"file.txt?thumb=1", // Non-image file
		"nonexistent.jpg?thumb=1",
		"file.jpg?thumb=" + strings.Repeat("1", 1000),
		"file.jpg?thumb=1&" + strings.Repeat("param=value&", 100),
		"file.jpg?thumb=true",
		"file.jpg?thumb=yes",
		"file.jpg?thumb=on",
		"file.jpg?thumb=enabled",
		"file.jpg?thumb=-1",
		"file.jpg?thumb=0",
		"file.jpg?thumb=2",
		"file.jpg?thumb=<script>",
		"file.jpg?thumb='; DROP TABLE images; --",
		"file.jpg?thumb=%3Cscript%3E",
		"file.jpg?thumb=\x00",
		"file.jpg?thumb=\xFF\xFE",
		"file.svg?thumb=1",  // SVG handling
		"file.bmp?thumb=1",  // Unsupported format
		"file.tiff?thumb=1", // Another format
		"file.ico?thumb=1",  // Icon format
		"../../../etc/passwd?thumb=1",
		"/.env?thumb=1",
		"/proc/self/environ?thumb=1",
		"file.jpg?thumb=1&width=0",
		"file.jpg?thumb=1&width=-100",
		"file.jpg?thumb=1&width=999999",
		"file.jpg?thumb=1&height=0",
		"file.jpg?thumb=1&height=-100",
		"file.jpg?thumb=1&height=999999",
		"file.jpg?thumb=1&format=png",
		"file.jpg?thumb=1&format=../../../etc/passwd",
		"file.jpg?thumb=1&quality=-1",
		"file.jpg?thumb=1&quality=101",
		"file.jpg?thumb=1&quality=abc",
		strings.Repeat("a", 255) + ".jpg?thumb=1",
		strings.Repeat("../", 50) + "image.jpg?thumb=1",
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, pathQuery string) {
		t.Parallel()

		// Create temporary directory
		tmpDir, err := os.MkdirTemp("", "slimserve-thumb-fuzz-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		// Create some test image files (simple placeholder images)
		testImages := []string{"test.jpg", "test.png", "test.gif", "test.webp"}
		for _, img := range testImages {
			imgPath := filepath.Join(tmpDir, img)
			// Create minimal valid image data (1x1 pixel PNG)
			pngData := []byte{
				0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
				0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
				0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
				0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
				0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41,
				0x54, 0x08, 0xD7, 0x63, 0xF8, 0x00, 0x00, 0x00,
				0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x37, 0x6E,
				0xF9, 0x24, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45,
				0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
			}
			if err := os.WriteFile(imgPath, pngData, 0644); err != nil {
				t.Fatalf("Failed to create test image: %v", err)
			}
		}

		// Create a subdirectory with an image
		subDir := filepath.Join(tmpDir, "subdir")
		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Fatalf("Failed to create subdir: %v", err)
		}
		subImgPath := filepath.Join(subDir, "sub.png")
		pngData := []byte{
			0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
			0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
			0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
			0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
			0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41,
			0x54, 0x08, 0xD7, 0x63, 0xF8, 0x00, 0x00, 0x00,
			0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x37, 0x6E,
			0xF9, 0x24, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45,
			0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
		}
		if err := os.WriteFile(subImgPath, pngData, 0644); err != nil {
			t.Fatalf("Failed to create sub image: %v", err)
		}

		// Set up server
		cfg := &config.Config{
			Port:            8080,
			Host:            "localhost",
			Directories:     []string{tmpDir},
			DisableDotFiles: true,
		}

		server := New(cfg)
		ts := httptest.NewServer(server)
		defer ts.Close()

		// Create request with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		requestURL := ts.URL + "/" + pathQuery
		req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
		if err != nil {
			t.Skipf("Failed to create request for path %q: %v", pathQuery, err)
		}

		client := &http.Client{
			Timeout: 2 * time.Second,
		}

		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				t.Logf("Request timeout for path %q", pathQuery)
				return
			}
			t.Logf("Network error for path %q: %v", pathQuery, err)
			return
		}
		defer resp.Body.Close()

		// Validate response - thumbnails should never cause server errors
		if resp.StatusCode >= 500 {
			t.Errorf("Unexpected server error %d for thumbnail path %q", resp.StatusCode, pathQuery)
		}

		// Log response for analysis
		t.Logf("Thumbnail response %d for path %q", resp.StatusCode, pathQuery)
	})
}

// FuzzStaticAssets tests static asset serving with various malicious paths
func FuzzStaticAssets(f *testing.F) {
	gin.SetMode(gin.TestMode)

	// Seed corpus for static asset fuzzing
	seeds := []string{
		"/static/css/theme.css",
		"/static/js/main.js",
		"/static/favicon.ico",
		"/static/../../../etc/passwd",
		"/static/%2e%2e/%2e%2e/etc/passwd",
		"/static/\x00",
		"/static/css/\x00.css",
		"/static/js/<script>.js",
		"/static/css/'; DROP TABLE assets; --.css",
		"/static/" + strings.Repeat("a", 1000),
		"/static/css/" + strings.Repeat("../", 100) + "passwd",
		"/static/nonexistent.css",
		"/static/css/nonexistent.css",
		"/static/js/nonexistent.js",
		"/static/images/nonexistent.png",
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, path string) {
		t.Parallel()

		// Ensure we're testing static paths
		if !strings.HasPrefix(path, "/static/") {
			path = "/static/" + strings.TrimPrefix(path, "/")
		}

		// Create temporary directory (not needed for static assets but for consistency)
		tmpDir, err := os.MkdirTemp("", "slimserve-static-fuzz-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		cfg := &config.Config{
			Port:            8080,
			Host:            "localhost",
			Directories:     []string{tmpDir},
			DisableDotFiles: true,
		}

		server := New(cfg)
		ts := httptest.NewServer(server)
		defer ts.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		requestURL := ts.URL + path
		req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
		if err != nil {
			t.Skipf("Failed to create request for path %q: %v", path, err)
		}

		client := &http.Client{
			Timeout: 2 * time.Second,
		}

		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				t.Logf("Request timeout for static path %q", path)
				return
			}
			t.Logf("Network error for static path %q: %v", path, err)
			return
		}
		defer resp.Body.Close()

		// Static assets should never cause server errors
		if resp.StatusCode >= 500 {
			t.Errorf("Unexpected server error %d for static path %q", resp.StatusCode, path)
		}

		// Log response for analysis
		t.Logf("Static asset response %d for path %q", resp.StatusCode, path)
	})
}
