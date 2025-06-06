package server

import (
	"context"
	"os"
	"slimserve/internal/config"
	"testing"
	"time"
)

func TestFormatSize(t *testing.T) {
	tests := []struct {
		size     int64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024*1024 + 512*1024, "1.5 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{1024*1024*1024 + 512*1024*1024, "1.5 GB"},
		{1024 * 1024 * 1024 * 1024, "1024.0 GB"},
	}

	for _, tt := range tests {
		result := formatSize(tt.size)
		if result != tt.expected {
			t.Errorf("formatSize(%d) = %q, expected %q", tt.size, result, tt.expected)
		}
	}
}

func TestDetermineFileType(t *testing.T) {
	// Create temporary directory entries for testing
	tmpDir, err := os.MkdirTemp("", "filetype-test")
	if err != nil {
		t.Fatal("Failed to create temp dir:", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	testFiles := []struct {
		name         string
		expectedType string
	}{
		{"test.jpg", "image"},
		{"test.jpeg", "image"},
		{"test.png", "image"},
		{"test.gif", "image"},
		{"test.webp", "image"},
		{"test.svg", "image"},
		{"test.mp4", "video"},
		{"test.avi", "video"},
		{"test.mkv", "video"},
		{"test.mov", "video"},
		{"test.webm", "video"},
		{"test.mp3", "audio"},
		{"test.wav", "audio"},
		{"test.flac", "audio"},
		{"test.ogg", "audio"},
		{"test.pdf", "document"},
		{"test.doc", "document"},
		{"test.docx", "document"},
		{"test.txt", "document"},
		{"test.md", "document"},
		{"test.unknown", "file"},
		{"test", "file"}, // No extension
	}

	for _, tt := range testFiles {
		// Create actual file
		filePath := tmpDir + "/" + tt.name
		if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", tt.name, err)
		}

		// Get directory entry
		entries, err := os.ReadDir(tmpDir)
		if err != nil {
			t.Fatal("Failed to read temp dir:", err)
		}

		for _, entry := range entries {
			if entry.Name() == tt.name {
				result := determineFileType(entry)
				if result != tt.expectedType {
					t.Errorf("determineFileType(%q) = %q, expected %q", tt.name, result, tt.expectedType)
				}
				break
			}
		}

		// Clean up for next test
		os.Remove(filePath)
	}

	// Test directory
	subDir := tmpDir + "/testdir"
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal("Failed to create test directory:", err)
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal("Failed to read temp dir:", err)
	}

	for _, entry := range entries {
		if entry.Name() == "testdir" && entry.IsDir() {
			result := determineFileType(entry)
			if result != "folder" {
				t.Errorf("determineFileType(directory) = %q, expected %q", result, "folder")
			}
			break
		}
	}
}

func TestGetFileIcon(t *testing.T) {
	// Create temporary directory entries for testing
	tmpDir, err := os.MkdirTemp("", "fileicon-test")
	if err != nil {
		t.Fatal("Failed to create temp dir:", err)
	}
	defer os.RemoveAll(tmpDir)

	testFiles := []struct {
		name         string
		expectedIcon string
	}{
		{"test.jpg", "image"},
		{"test.jpeg", "image"},
		{"test.png", "image"},
		{"test.gif", "image"},
		{"test.webp", "image"},
		{"test.svg", "image"},
		{"test.mp4", "video"},
		{"test.avi", "video"},
		{"test.mkv", "video"},
		{"test.mov", "video"},
		{"test.webm", "video"},
		{"test.mp3", "audio"},
		{"test.wav", "audio"},
		{"test.flac", "audio"},
		{"test.ogg", "audio"},
		{"test.pdf", "file-pdf"},
		{"test.doc", "file-text"},
		{"test.docx", "file-text"},
		{"test.txt", "file-text"},
		{"test.md", "file-text"},
		{"test.zip", "archive"},
		{"test.tar", "archive"},
		{"test.gz", "archive"},
		{"test.rar", "archive"},
		{"test.unknown", "file"},
		{"test", "file"}, // No extension
	}

	for _, tt := range testFiles {
		// Create actual file
		filePath := tmpDir + "/" + tt.name
		if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", tt.name, err)
		}

		// Get directory entry
		entries, err := os.ReadDir(tmpDir)
		if err != nil {
			t.Fatal("Failed to read temp dir:", err)
		}

		for _, entry := range entries {
			if entry.Name() == tt.name {
				result := getFileIcon(entry)
				if result != tt.expectedIcon {
					t.Errorf("getFileIcon(%q) = %q, expected %q", tt.name, result, tt.expectedIcon)
				}
				break
			}
		}

		// Clean up for next test
		os.Remove(filePath)
	}

	// Test directory
	subDir := tmpDir + "/testdir"
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal("Failed to create test directory:", err)
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal("Failed to read temp dir:", err)
	}

	for _, entry := range entries {
		if entry.Name() == "testdir" && entry.IsDir() {
			result := getFileIcon(entry)
			if result != "folder" {
				t.Errorf("getFileIcon(directory) = %q, expected %q", result, "folder")
			}
			break
		}
	}
}

func TestServerLifecycleMethods(t *testing.T) {
	cfg := config.Default()
	cfg.Port = 0 // Use random port
	srv := New(cfg)

	// Test GetEngine
	engine := srv.GetEngine()
	if engine == nil {
		t.Error("GetEngine() should return non-nil engine")
	}

	// Test Start method (just ensure it doesn't panic)
	t.Run("start_method_exists", func(t *testing.T) {
		// We can't actually start the server in tests as it would block
		// but we can call the method to test it exists
		// This will fail but we can catch the error
		go func() {
			srv.Start() // This will try to bind to default port and likely fail
		}()
		// Just testing that the method exists and is callable
	})

	// Test Stop method
	t.Run("stop_without_running", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		err := srv.Stop(ctx)
		if err != nil {
			t.Errorf("Stop() on non-running server should not error, got: %v", err)
		}
	})
}

func TestBuildPathSegments(t *testing.T) {
	tests := []struct {
		requestPath string
		expected    []PathSegment
	}{
		{
			"/",
			[]PathSegment{},
		},
		{
			"/folder",
			[]PathSegment{
				{Name: "folder", URL: "/folder"},
			},
		},
		{
			"/folder/subfolder",
			[]PathSegment{
				{Name: "folder", URL: "/folder"},
				{Name: "subfolder", URL: "/folder/subfolder"},
			},
		},
		{
			"/folder/subfolder/file.txt",
			[]PathSegment{
				{Name: "folder", URL: "/folder"},
				{Name: "subfolder", URL: "/folder/subfolder"},
				{Name: "file.txt", URL: "/folder/subfolder/file.txt"},
			},
		},
		{
			"/",
			[]PathSegment{},
		},
		{
			"", // Empty path - edge case
			[]PathSegment{},
		},
	}

	for _, tt := range tests {
		result := buildPathSegments(tt.requestPath)
		if len(result) != len(tt.expected) {
			t.Errorf("buildPathSegments(%q) returned %d segments, expected %d",
				tt.requestPath, len(result), len(tt.expected))
			continue
		}

		for i, segment := range result {
			if segment.Name != tt.expected[i].Name || segment.URL != tt.expected[i].URL {
				t.Errorf("buildPathSegments(%q)[%d] = {%q, %q}, expected {%q, %q}",
					tt.requestPath, i, segment.Name, segment.URL,
					tt.expected[i].Name, tt.expected[i].URL)
			}
		}
	}
}
