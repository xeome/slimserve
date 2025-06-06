package server

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServerIntegration(t *testing.T) {
	// Create temporary directory with test files
	tmpDir, err := os.MkdirTemp("", "slimserve-test")
	if err != nil {
		t.Fatal("Failed to create temp dir:", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	testFile1 := filepath.Join(tmpDir, "test1.txt")
	testFile2 := filepath.Join(tmpDir, "test2.txt")

	if err := os.WriteFile(testFile1, []byte("content1"), 0644); err != nil {
		t.Fatal("Failed to create test file 1:", err)
	}

	if err := os.WriteFile(testFile2, []byte("content2"), 0644); err != nil {
		t.Fatal("Failed to create test file 2:", err)
	}

	// Create server
	srv := New([]string{tmpDir})

	// Find available port
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal("Failed to get available port:", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Start server in goroutine
	addr := fmt.Sprintf(":%d", port)
	go func() {
		if err := srv.Run(addr); err != nil {
			t.Logf("Server error: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	baseURL := fmt.Sprintf("http://localhost:%d", port)

	t.Run("directory_listing", func(t *testing.T) {
		// Test directory listing
		url := fmt.Sprintf("%s/", baseURL)
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal("Failed to GET /:", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal("Failed to read response body:", err)
		}

		bodyStr := string(body)

		// Check that HTML contains both test files
		if !strings.Contains(bodyStr, "test1.txt") {
			t.Error("Response should contain test1.txt")
		}

		if !strings.Contains(bodyStr, "test2.txt") {
			t.Error("Response should contain test2.txt")
		}

		// Check that it's HTML
		if !strings.Contains(bodyStr, "<html") {
			t.Error("Response should be HTML")
		}
	})

	t.Run("file_serving", func(t *testing.T) {
		// Test individual file serving
		fileUrl := fmt.Sprintf("%s/test1.txt", baseURL)
		fileResp, err := http.Get(fileUrl)
		if err != nil {
			t.Fatal("Failed to GET /test1.txt:", err)
		}
		defer fileResp.Body.Close()

		if fileResp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200 for file, got %d", fileResp.StatusCode)
		}

		fileBody, err := io.ReadAll(fileResp.Body)
		if err != nil {
			t.Fatal("Failed to read file response body:", err)
		}

		if string(fileBody) != "content1" {
			t.Errorf("Expected file content 'content1', got '%s'", string(fileBody))
		}
	})

	t.Run("non_existent_file_404", func(t *testing.T) {
		// Test 404 for non-existent file
		url := fmt.Sprintf("%s/nonexistent.txt", baseURL)
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal("Failed to GET /nonexistent.txt:", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("Expected status 404 for non-existent file, got %d", resp.StatusCode)
		}
	})

	t.Run("path_traversal_forbidden", func(t *testing.T) {
		// Test path traversal attempt returns 403
		url := fmt.Sprintf("%s/../go.mod", baseURL)
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal("Failed to GET /../go.mod:", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("Expected status 403 for path traversal attempt, got %d", resp.StatusCode)
		}
	})
}
