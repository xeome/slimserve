package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slimserve/internal/config"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHandler_ServeFiles(t *testing.T) {
	// Create temporary directory with test files
	tmpDir, err := os.MkdirTemp("", "slimserve-handler-test")
	if err != nil {
		t.Fatal("Failed to create temp dir:", err)
	}
	defer os.RemoveAll(tmpDir)

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

	// Create handler
	cfg := &config.Config{
		Host:            "localhost",
		Port:            8080,
		Directories:     []string{tmpDir},
		DisableDotFiles: true,
	}
	handler := NewHandler(cfg)

	// Set gin to test mode
	gin.SetMode(gin.TestMode)

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
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "path_traversal_with_dotdot",
			path:           "/subdir/../../../etc/passwd",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "path_traversal_simple",
			path:           "/..",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "path_traversal_in_middle",
			path:           "/test/../../../etc/passwd",
			expectedStatus: http.StatusForbidden,
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
			// Create test context
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			// Handle empty path case for HTTP request
			requestURL := tt.path
			if requestURL == "" {
				requestURL = "/"
			}
			c.Request = httptest.NewRequest("GET", requestURL, nil)
			c.Params = gin.Params{
				{Key: "path", Value: tt.path},
			}

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

func TestHandler_ServeDirectory(t *testing.T) {
	// Create temporary directory with test files
	tmpDir, err := os.MkdirTemp("", "slimserve-dir-test")
	if err != nil {
		t.Fatal("Failed to create temp dir:", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files with different characteristics
	files := []struct {
		name    string
		content string
	}{
		{"file1.txt", "content1"},
		{"file2.md", "# Markdown content"},
		{"data.json", `{"key": "value"}`},
	}

	for _, f := range files {
		filePath := filepath.Join(tmpDir, f.name)
		if err := os.WriteFile(filePath, []byte(f.content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", f.name, err)
		}
	}

	// Create subdirectory
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal("Failed to create subdirectory:", err)
	}

	// Create handler
	cfg := &config.Config{
		Host:            "localhost",
		Port:            8080,
		Directories:     []string{tmpDir},
		DisableDotFiles: true,
	}
	handler := NewHandler(cfg)
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		requestPath   string
		expectedFiles []string
		expectedDirs  []string
	}{
		{
			name:          "list_root_directory",
			requestPath:   "/",
			expectedFiles: []string{"file1.txt", "file2.md", "data.json"},
			expectedDirs:  []string{"subdir"},
		},
		{
			name:          "list_subdirectory",
			requestPath:   "/subdir",
			expectedFiles: []string{},
			expectedDirs:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", tt.requestPath, nil)

			// Determine full path for directory
			var fullPath string
			if tt.requestPath == "/" {
				fullPath = tmpDir
			} else {
				fullPath = filepath.Join(tmpDir, strings.TrimPrefix(tt.requestPath, "/"))
			}

			// Call serveDirectory directly
			handler.serveDirectory(c, fullPath, tt.requestPath)

			// Check status
			if w.Code != http.StatusOK {
				t.Errorf("Expected status 200, got %d", w.Code)
			}

			// Check content type
			contentType := w.Header().Get("Content-Type")
			if contentType != "text/html" {
				t.Errorf("Expected Content-Type 'text/html', got '%s'", contentType)
			}

			body := w.Body.String()

			// Check for expected files
			for _, expectedFile := range tt.expectedFiles {
				if !strings.Contains(body, expectedFile) {
					t.Errorf("Response should contain file '%s'", expectedFile)
				}
			}

			// Check for expected directories
			for _, expectedDir := range tt.expectedDirs {
				if !strings.Contains(body, expectedDir) {
					t.Errorf("Response should contain directory '%s'", expectedDir)
				}
			}

			// Check that it's valid HTML
			if !strings.Contains(body, "<html") {
				t.Error("Response should be HTML")
			}
		})
	}
}

func TestHandler_ServeDirectory_Error(t *testing.T) {
	// Test error handling in serveDirectory
	cfg := &config.Config{
		Host:            "localhost",
		Port:            8080,
		Directories:     []string{"/nonexistent-directory"},
		DisableDotFiles: true,
	}
	handler := NewHandler(cfg)
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)

	// Call serveDirectory with non-existent directory
	handler.serveDirectory(c, "/nonexistent-directory", "/")

	// Should return 500 because directory doesn't exist
	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500 for non-existent directory, got %d", w.Code)
	}
}
func TestHandler_HeadRequest_StaticAndDirectory(t *testing.T) {
	// Config: static assets via embedded FS, directory via tempdir
	cfg := &config.Config{
		Host:            "localhost",
		Port:            8080,
		Directories:     []string{"."}, // Dir testing only - static test below uses embedded
		DisableDotFiles: true,
	}
	handler := NewHandler(cfg)
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
		handler := NewHandler(cfg)

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
