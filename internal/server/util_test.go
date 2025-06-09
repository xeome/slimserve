package server

import (
	"os"
	"testing"
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
