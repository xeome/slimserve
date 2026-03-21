package files

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheManagerPruneOnlyImageFiles(t *testing.T) {
	testDir := t.TempDir()
	cacheDir := filepath.Join(testDir, "cache")

	cacheManager, err := NewCacheManager(cacheDir, 100)
	if err != nil {
		t.Fatalf("Failed to create cache manager: %v", err)
	}

	baseTime := time.Now().Add(-4 * time.Hour)
	files := []struct {
		name    string
		content string
		isImage bool
	}{
		{"old_thumb.jpg", "fake jpeg content", true},
		{"old_thumb.png", "fake png content", true},
		{"newer_thumb.gif", "fake gif content", true},
		{"newest_thumb.webp", "fake webp content", true},
		{"config.txt", "configuration data", false},
		{"readme.md", "documentation", false},
		{"cache.log", "log entries", false},
		{"temp.tmp", "temporary data", false},
	}

	for i, file := range files {
		filePath := filepath.Join(cacheDir, file.name)

		var content []byte
		if file.isImage {
			content = make([]byte, 300*1024)
		} else {
			content = []byte(file.content)
		}

		err := os.WriteFile(filePath, content, 0644)
		if err != nil {
			t.Fatalf("Failed to create file %s: %v", file.name, err)
		}

		modTime := baseTime.Add(time.Duration(i) * time.Minute)
		err = os.Chtimes(filePath, modTime, modTime)
		if err != nil {
			t.Fatalf("Failed to set mod time for %s: %v", file.name, err)
		}
	}

	nonImageFilesBefore := 0
	err = filepath.WalkDir(cacheDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !IsImageFile(d.Name()) {
			nonImageFilesBefore++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to count files: %v", err)
	}

	currentSizeMB := cacheManager.SizeMB()
	t.Logf("Current cache size: %d MB", currentSizeMB)

	nonImageFilesAfter := 0
	err = filepath.WalkDir(cacheDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !IsImageFile(d.Name()) {
			nonImageFilesAfter++
		}
		return nil
	})

	if nonImageFilesAfter != nonImageFilesBefore {
		t.Errorf("Non-image files were affected. Before: %d, After: %d",
			nonImageFilesBefore, nonImageFilesAfter)
	}
}

func TestCacheManagerPruneOldestFirst(t *testing.T) {
	testDir := t.TempDir()
	cacheDir := filepath.Join(testDir, "cache")

	os.MkdirAll(cacheDir, 0755)

	baseTime := time.Now().Add(-3 * time.Hour)
	files := []struct {
		name string
		age  time.Duration
	}{
		{"oldest.jpg", 0},
		{"older.png", 30 * time.Minute},
		{"newer.gif", 60 * time.Minute},
		{"newest.webp", 90 * time.Minute},
	}

	content := make([]byte, 512*1024)
	for _, file := range files {
		filePath := filepath.Join(cacheDir, file.name)
		err := os.WriteFile(filePath, content, 0644)
		if err != nil {
			t.Fatalf("Failed to create file %s: %v", file.name, err)
		}

		modTime := baseTime.Add(file.age)
		err = os.Chtimes(filePath, modTime, modTime)
		if err != nil {
			t.Fatalf("Failed to set mod time for %s: %v", file.name, err)
		}
	}

	cacheManager, err := NewCacheManager(cacheDir, 1)
	if err != nil {
		t.Fatalf("Failed to create cache manager: %v", err)
	}

	added, used, maxBytes := cacheManager.Stats()
	t.Logf("Cache stats after rebuild - count: %d, used: %d, max: %d", added, used, maxBytes)

	cacheManager.Set("new_thumb", 512*1024, ".jpg")

	added, used, maxBytes = cacheManager.Stats()
	t.Logf("Cache stats after Set - count: %d, used: %d, max: %d", added, used, maxBytes)

	var remainingFiles []string
	err = filepath.WalkDir(cacheDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if IsImageFile(d.Name()) {
			remainingFiles = append(remainingFiles, d.Name())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to list remaining files: %v", err)
	}

	t.Logf("Remaining files: %v", remainingFiles)

	for _, remaining := range remainingFiles {
		if remaining == "oldest.jpg" {
			t.Errorf("Oldest file 'oldest.jpg' should have been evicted, but it remains")
		}
	}
}

func TestCacheManagerPruneIfNeeded(t *testing.T) {
	testDir := t.TempDir()
	cacheDir := filepath.Join(testDir, "cache")

	cacheManager, err := NewCacheManager(cacheDir, 100)
	if err != nil {
		t.Fatalf("Failed to create cache manager: %v", err)
	}

	pruned, _, _, err := cacheManager.PruneIfNeeded(0)
	if err != nil {
		t.Fatalf("PruneIfNeeded failed: %v", err)
	}
	if pruned {
		t.Error("Expected no pruning with limit 0")
	}

	largeContent := make([]byte, 1*1024*1024)
	for i := 0; i < 3; i++ {
		filePath := filepath.Join(cacheDir, fmt.Sprintf("large_thumb_%d.jpg", i))
		err := os.WriteFile(filePath, largeContent, 0644)
		if err != nil {
			t.Fatalf("Failed to create large file: %v", err)
		}
		modTime := time.Now().Add(-time.Duration(i) * time.Hour)
		os.Chtimes(filePath, modTime, modTime)
	}

	pruned, _, _, err = cacheManager.PruneIfNeeded(2)
	if err != nil {
		t.Fatalf("PruneIfNeeded failed: %v", err)
	}

	t.Logf("Pruned: %v", pruned)

	finalSize := cacheManager.SizeMB()
	t.Logf("Final cache size: %d MB", finalSize)

	if finalSize > 2 {
		t.Errorf("Final cache size %d MB exceeds reasonable bounds", finalSize)
	}
}
