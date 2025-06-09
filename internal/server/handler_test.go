package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"slimserve/internal/config"
	"slimserve/internal/security"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Test helper functions

// setupTestHandler creates a test handler with temporary directory and test files
func setupTestHandler(t *testing.T) (*Handler, string, func()) {
	t.Helper()

	// Create temporary directory with test files
	tmpDir, err := os.MkdirTemp("", "slimserve-handler-test")
	if err != nil {
		t.Fatal("Failed to create temp dir:", err)
	}

	// Create test files and directories
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatal("Failed to create test file:", err)
	}

	testDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(testDir, 0755); err != nil {
		t.Fatal("Failed to create test directory:", err)
	}

	nestedFile := filepath.Join(testDir, "nested.txt")
	if err := os.WriteFile(nestedFile, []byte("nested content"), 0644); err != nil {
		t.Fatal("Failed to create nested file:", err)
	}

	// Create handler with RootFS
	cfg := &config.Config{
		Host:            "localhost",
		Port:            8080,
		Directories:     []string{tmpDir},
		DisableDotFiles: true,
	}

	var roots []*security.RootFS
	for _, dir := range cfg.Directories {
		root, err := security.NewRootFS(dir)
		require.NoError(t, err)
		roots = append(roots, root)
	}

	handler := NewHandler(cfg, roots)
	gin.SetMode(gin.TestMode)

	// Return cleanup function
	cleanup := func() {
		for _, root := range roots {
			root.Close()
		}
		os.RemoveAll(tmpDir)
	}

	return handler, tmpDir, cleanup
}

// createTestContext creates a Gin test context for the given path and method
func createTestContext(path, method string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	requestURL := path
	if requestURL == "" {
		requestURL = "/"
	}

	c.Request = httptest.NewRequest(method, requestURL, nil)
	c.Params = gin.Params{
		{Key: "path", Value: path},
	}

	return c, w
}

func TestHandler_ServeFiles(t *testing.T) {
	handler, _, cleanup := setupTestHandler(t)
	defer cleanup()

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		expectedBody   string
		checkContains  []string
	}{
		{
			name:           "root_directory_listing",
			path:           "/",
			expectedStatus: http.StatusOK,
			checkContains:  []string{"test.txt", "subdir", "<html"},
		},
		{
			name:           "empty_path_parameter",
			path:           "",
			expectedStatus: http.StatusOK,
			checkContains:  []string{"test.txt", "subdir", "<html"},
		},
		{
			name:           "serve_existing_file",
			path:           "/test.txt",
			expectedStatus: http.StatusOK,
			expectedBody:   "test content",
		},
		{
			name:           "serve_nested_file",
			path:           "/subdir/nested.txt",
			expectedStatus: http.StatusOK,
			expectedBody:   "nested content",
		},
		{
			name:           "subdirectory_listing",
			path:           "/subdir",
			expectedStatus: http.StatusOK,
			checkContains:  []string{"nested.txt", "<html"},
		},
		{
			name:           "subdirectory_listing_with_slash",
			path:           "/subdir/",
			expectedStatus: http.StatusOK,
			checkContains:  []string{"nested.txt", "<html"},
		},
		{
			name:           "non_existent_file",
			path:           "/nonexistent.txt",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "non_existent_directory",
			path:           "/nonexistent/",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "path_traversal_attempt",
			path:           "/../etc/passwd",
			expectedStatus: http.StatusNotFound, // os.Root returns 404 for out-of-bounds access
		},
		{
			name:           "path_traversal_with_dotdot",
			path:           "/subdir/../../../etc/passwd",
			expectedStatus: http.StatusNotFound, // os.Root returns 404 for out-of-bounds access
		},
		{
			name:           "path_traversal_simple",
			path:           "/..",
			expectedStatus: http.StatusNotFound, // os.Root returns 404 for out-of-bounds access
		},
		{
			name:           "path_traversal_in_middle",
			path:           "/test/../../../etc/passwd",
			expectedStatus: http.StatusNotFound, // os.Root returns 404 for out-of-bounds access
		},
		{
			name:           "static_favicon_ico",
			path:           "/static/favicon.ico",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "static_main_js",
			path:           "/static/js/main.js",
			expectedStatus: http.StatusOK,
			checkContains:  []string{"slimserveUI"},
		},
		{
			name:           "static_custom_css",
			path:           "/static/css/custom.css",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "static_heroicons_js",
			path:           "/static/js/heroicons.js",
			expectedStatus: http.StatusOK,
			checkContains:  []string{"window.heroicons", "outline"},
		},
		{
			name:           "non_existent_static_file",
			path:           "/static/nonexistent.js",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, w := createTestContext(tt.path, "GET")

			// Call handler
			handler.ServeFiles(c)

			// Check status code
			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			// Check response body if expected
			if tt.expectedBody != "" {
				body := w.Body.String()
				if body != tt.expectedBody {
					t.Errorf("Expected body '%s', got '%s'", tt.expectedBody, body)
				}
			}

			// Check if response contains expected strings
			if len(tt.checkContains) > 0 {
				body := w.Body.String()
				for _, expected := range tt.checkContains {
					if !strings.Contains(body, expected) {
						t.Errorf("Response body should contain '%s', got: %s", expected, body)
					}
				}
			}
		})
	}
}

func TestHandler_ServeDirectory_And_Ignore(t *testing.T) {
	// Create temporary directory with test files
	tmpDir, err := os.MkdirTemp("", "slimserve-dir-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create test files and directories
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content1"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".dotfile"), []byte("secret"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file.log"), []byte("log"), 0644))
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "subdir", "nested.txt"), []byte("nested"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".slimserveignore"), []byte("*.log\n"), 0644))

	cfg := &config.Config{
		Directories:     []string{tmpDir},
		DisableDotFiles: true,
		IgnorePatterns:  []string{}, // Test .slimserveignore first
	}
	root, err := security.NewRootFS(tmpDir)
	require.NoError(t, err)
	defer root.Close()
	handler := NewHandler(cfg, []*security.RootFS{root})
	gin.SetMode(gin.TestMode)

	// Test case 1: Listing root directory, expecting .dotfile and *.log to be ignored
	c, w := createTestContext("/", "GET")
	handler.ServeFiles(c)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	require.Contains(t, body, "file1.txt")
	require.Contains(t, body, "subdir")
	require.NotContains(t, body, ".dotfile")
	require.NotContains(t, body, "file.log")
	require.NotContains(t, body, ".slimserveignore")

	// Test case 2: Direct access to ignored file should be forbidden
	c, w = createTestContext("/file.log", "GET")
	handler.ServeFiles(c)
	require.Equal(t, http.StatusForbidden, w.Code)

	c, w = createTestContext("/.slimserveignore", "GET")
	handler.ServeFiles(c)
	require.Equal(t, http.StatusForbidden, w.Code)

	// Test case 3: Direct access to dotfile should be forbidden
	c, w = createTestContext("/.dotfile", "GET")
	handler.ServeFiles(c)
	require.Equal(t, http.StatusForbidden, w.Code)
}
func TestHandler_HeadRequest_StaticAndDirectory(t *testing.T) {
	// Config: static assets via embedded FS, directory via tempdir
	cfg := &config.Config{
		Host:            "localhost",
		Port:            8080,
		Directories:     []string{"."}, // Dir testing only - static test below uses embedded
		DisableDotFiles: true,
	}

	// Create RootFS instances
	var roots []*security.RootFS
	for _, dir := range cfg.Directories {
		root, err := security.NewRootFS(dir)
		require.NoError(t, err)
		roots = append(roots, root)
	}
	defer func() {
		for _, root := range roots {
			root.Close()
		}
	}()

	handler := NewHandler(cfg, roots)
	gin.SetMode(gin.TestMode)

	t.Run("HEAD static asset returns 200 and correct headers", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("HEAD", "/static/css/theme.css", nil)
		c.Params = gin.Params{
			{Key: "path", Value: "/static/css/theme.css"},
		}
		handler.ServeFiles(c)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d", w.Code)
		}
		ctype := w.Header().Get("Content-Type")
		if !strings.Contains(ctype, "text/css") {
			t.Errorf("Expected Content-Type text/css, got %q", ctype)
		}
		if w.Body.Len() != 0 {
			t.Errorf("Expected zero-length body for HEAD, got %d bytes", w.Body.Len())
		}
	})

	t.Run("HEAD on directory returns 200 and headers but no body", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "slimserve-head-dir-test")
		if err != nil {
			t.Fatal("Failed to create temp dir:", err)
		}
		defer os.RemoveAll(tmpDir)
		cfg := &config.Config{
			Host:            "localhost",
			Port:            8081,
			Directories:     []string{tmpDir},
			DisableDotFiles: true,
		}

		// Create RootFS instances
		var roots []*security.RootFS
		for _, dir := range cfg.Directories {
			root, err := security.NewRootFS(dir)
			require.NoError(t, err)
			roots = append(roots, root)
		}
		defer func() {
			for _, root := range roots {
				root.Close()
			}
		}()

		handler := NewHandler(cfg, roots)

		subDir := filepath.Join(tmpDir, "subdir-abc")
		err = os.Mkdir(subDir, 0755)
		if err != nil {
			t.Fatal("Failed to make subdir:", err)
		}
		testFile := filepath.Join(subDir, "foo.txt")
		if err := os.WriteFile(testFile, []byte("bar"), 0644); err != nil {
			t.Fatal("Failed to write test file:", err)
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("HEAD", "/subdir-abc", nil)
		c.Params = gin.Params{
			{Key: "path", Value: "/subdir-abc"},
		}
		handler.ServeFiles(c)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 for HEAD dir, got %d", w.Code)
		}
		ctype := w.Header().Get("Content-Type")
		if !strings.Contains(ctype, "text/html") {
			t.Errorf("Expected Content-Type text/html for dir, got %q", ctype)
		}
		if w.Body.Len() != 0 {
			t.Errorf("Expected zero-length body for HEAD directory, got %d bytes", w.Body.Len())
		}
	})
}
