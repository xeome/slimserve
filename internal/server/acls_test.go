package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAccessControlSecurity(t *testing.T) {
	// Create temporary directories for testing access controls
	tmpRoot, err := os.MkdirTemp("", "slimserve-acls-test")
	if err != nil {
		t.Fatal("Failed to create temp root dir:", err)
	}
	defer os.RemoveAll(tmpRoot)

	// Create allowed directory structure
	allowedDir1 := filepath.Join(tmpRoot, "allowed1")
	allowedDir2 := filepath.Join(tmpRoot, "allowed2")
	siblingDir := filepath.Join(tmpRoot, "sibling")

	for _, dir := range []string{allowedDir1, allowedDir2, siblingDir} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Create test files
	testFile1 := filepath.Join(allowedDir1, "test1.txt")
	if err := os.WriteFile(testFile1, []byte("content1"), 0644); err != nil {
		t.Fatal("Failed to create test file 1:", err)
	}

	testFile2 := filepath.Join(allowedDir2, "test2.txt")
	if err := os.WriteFile(testFile2, []byte("content2"), 0644); err != nil {
		t.Fatal("Failed to create test file 2:", err)
	}

	siblingFile := filepath.Join(siblingDir, "secret.txt")
	if err := os.WriteFile(siblingFile, []byte("secret content"), 0644); err != nil {
		t.Fatal("Failed to create sibling file:", err)
	}

	// Create hidden file in allowed directory
	hiddenFile := filepath.Join(allowedDir1, ".secret")
	if err := os.WriteFile(hiddenFile, []byte("hidden content"), 0644); err != nil {
		t.Fatal("Failed to create hidden file:", err)
	}

	// Create server with multiple allowed roots
	srv := New([]string{allowedDir1, allowedDir2})
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		description    string
	}{
		{
			name:           "access_allowed_directory_1",
			path:           "/",
			expectedStatus: http.StatusOK,
			description:    "Access to first allowed directory should work",
		},
		{
			name:           "access_allowed_file_1",
			path:           "/test1.txt",
			expectedStatus: http.StatusOK,
			description:    "Access to file in first allowed directory should work",
		},
		{
			name:           "access_allowed_file_2",
			path:           "/test2.txt",
			expectedStatus: http.StatusOK,
			description:    "Access to file in second allowed directory should work",
		},
		{
			name:           "access_sibling_directory",
			path:           "/../sibling/secret.txt",
			expectedStatus: http.StatusForbidden,
			description:    "Access to sibling directory should be forbidden",
		},
		{
			name:           "access_hidden_file",
			path:           "/.secret",
			expectedStatus: http.StatusForbidden,
			description:    "Access to hidden file should be forbidden",
		},
		{
			name:           "path_traversal_to_sibling",
			path:           "/../sibling/",
			expectedStatus: http.StatusForbidden,
			description:    "Path traversal to sibling directory should be forbidden",
		},
		{
			name:           "path_traversal_multiple_dotdot",
			path:           "/../../etc/passwd",
			expectedStatus: http.StatusForbidden,
			description:    "Multiple .. path traversal should be forbidden",
		},
		{
			name:           "hidden_directory_access",
			path:           "/.hidden/file.txt",
			expectedStatus: http.StatusForbidden,
			description:    "Access to hidden directory should be forbidden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test request
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", tt.path, nil)

			// Use the server's HTTP handler (includes middleware)
			srv.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("%s: Expected status %d, got %d", tt.description, tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestMultipleAllowedRoots(t *testing.T) {
	// Create temporary directories
	tmpRoot, err := os.MkdirTemp("", "slimserve-multi-test")
	if err != nil {
		t.Fatal("Failed to create temp root dir:", err)
	}
	defer os.RemoveAll(tmpRoot)

	// Create multiple allowed directories
	root1 := filepath.Join(tmpRoot, "root1")
	root2 := filepath.Join(tmpRoot, "root2")

	for _, dir := range []string{root1, root2} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Create files with same name in both roots
	file1 := filepath.Join(root1, "shared.txt")
	if err := os.WriteFile(file1, []byte("content from root1"), 0644); err != nil {
		t.Fatal("Failed to create file in root1:", err)
	}

	file2 := filepath.Join(root2, "shared.txt")
	if err := os.WriteFile(file2, []byte("content from root2"), 0644); err != nil {
		t.Fatal("Failed to create file in root2:", err)
	}

	// Create unique files in each root
	unique1 := filepath.Join(root1, "unique1.txt")
	if err := os.WriteFile(unique1, []byte("unique content 1"), 0644); err != nil {
		t.Fatal("Failed to create unique file 1:", err)
	}

	unique2 := filepath.Join(root2, "unique2.txt")
	if err := os.WriteFile(unique2, []byte("unique content 2"), 0644); err != nil {
		t.Fatal("Failed to create unique file 2:", err)
	}

	// Create server with multiple roots
	srv := New([]string{root1, root2})
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		expectedBody   string
		description    string
	}{
		{
			name:           "shared_file_first_match",
			path:           "/shared.txt",
			expectedStatus: http.StatusOK,
			expectedBody:   "content from root1", // First root should match
			description:    "Shared file should serve from first matching root",
		},
		{
			name:           "unique_file_root1",
			path:           "/unique1.txt",
			expectedStatus: http.StatusOK,
			expectedBody:   "unique content 1",
			description:    "Unique file in root1 should be accessible",
		},
		{
			name:           "unique_file_root2",
			path:           "/unique2.txt",
			expectedStatus: http.StatusOK,
			expectedBody:   "unique content 2",
			description:    "Unique file in root2 should be accessible",
		},
		{
			name:           "nonexistent_file",
			path:           "/nonexistent.txt",
			expectedStatus: http.StatusNotFound,
			description:    "Nonexistent file should return 404",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test request
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", tt.path, nil)

			// Use the server's HTTP handler
			srv.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("%s: Expected status %d, got %d", tt.description, tt.expectedStatus, w.Code)
			}

			if tt.expectedBody != "" {
				body := w.Body.String()
				if body != tt.expectedBody {
					t.Errorf("%s: Expected body '%s', got '%s'", tt.description, tt.expectedBody, body)
				}
			}
		})
	}
}

func TestAccessControlMiddleware(t *testing.T) {
	// Create temporary directory structure
	tmpRoot, err := os.MkdirTemp("", "slimserve-middleware-test")
	if err != nil {
		t.Fatal("Failed to create temp root dir:", err)
	}
	defer os.RemoveAll(tmpRoot)

	allowedDir := filepath.Join(tmpRoot, "allowed")
	if err := os.Mkdir(allowedDir, 0755); err != nil {
		t.Fatal("Failed to create allowed dir:", err)
	}

	// Create test file
	testFile := filepath.Join(allowedDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatal("Failed to create test file:", err)
	}

	// Create hidden file
	hiddenFile := filepath.Join(allowedDir, ".hidden")
	if err := os.WriteFile(hiddenFile, []byte("hidden content"), 0644); err != nil {
		t.Fatal("Failed to create hidden file:", err)
	}

	// Test direct middleware functionality
	srv := New([]string{allowedDir})
	middleware := srv.accessControlMiddleware()

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		shouldAbort    bool
	}{
		{
			name:           "valid_path",
			path:           "/test.txt",
			expectedStatus: http.StatusOK,
			shouldAbort:    false,
		},
		{
			name:           "hidden_file_path",
			path:           "/.hidden",
			expectedStatus: http.StatusForbidden,
			shouldAbort:    true,
		},
		{
			name:           "path_traversal",
			path:           "/../outside",
			expectedStatus: http.StatusForbidden,
			shouldAbort:    true,
		},
		{
			name:           "dotdot_in_path",
			path:           "/subdir/../..",
			expectedStatus: http.StatusForbidden,
			shouldAbort:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", tt.path, nil)
			c.Params = gin.Params{
				{Key: "path", Value: tt.path},
			}

			// Create a test router with middleware
			router := gin.New()
			router.Use(middleware)
			router.GET("/*path", func(c *gin.Context) {
				c.String(http.StatusOK, "next handler called")
			})

			// Create request
			req := httptest.NewRequest("GET", tt.path, nil)
			w = httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.shouldAbort {
				body := w.Body.String()
				if body == "next handler called" {
					t.Errorf("Expected middleware to abort, but next handler was called")
				}
			} else {
				body := w.Body.String()
				if body != "next handler called" {
					t.Errorf("Expected next handler to be called, but got: %s", body)
				}
			}
		})
	}
}
