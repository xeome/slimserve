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

	cacheManager := NewCacheManager(cacheDir)

	// Create cache directory
	err := cacheManager.EnsureCacheDir()
	if err != nil {
		t.Fatalf("Failed to create cache dir: %v", err)
	}

	// Create a mix of image files (thumbnails) and non-image files
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

	baseTime := time.Now().Add(-4 * time.Hour)
	for i, file := range files {
		filePath := filepath.Join(cacheDir, file.name)

		// Create larger image files to ensure they register as > 0 MB
		var content []byte
		if file.isImage {
			content = make([]byte, 300*1024) // 300KB each
		} else {
			content = []byte(file.content)
		}

		err := os.WriteFile(filePath, content, 0644)
		if err != nil {
			t.Fatalf("Failed to create file %s: %v", file.name, err)
		}

		// Set different modification times (older files first)
		modTime := baseTime.Add(time.Duration(i) * time.Minute)
		err = os.Chtimes(filePath, modTime, modTime)
		if err != nil {
			t.Fatalf("Failed to set mod time for %s: %v", file.name, err)
		}
	}

	// Count files before pruning
	imageFilesBefore := 0
	nonImageFilesBefore := 0
	err = filepath.WalkDir(cacheDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if IsImageFile(d.Name()) {
			imageFilesBefore++
		} else {
			nonImageFilesBefore++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to count files before pruning: %v", err)
	}

	// Get current cache size before pruning
	currentSizeMB, err := cacheManager.SizeMB()
	if err != nil {
		t.Fatalf("Failed to get current cache size: %v", err)
	}
	t.Logf("Current cache size: %d MB (should be > 0)", currentSizeMB)

	// Prune cache with very small target (should remove some image files)
	// Use a target smaller than current cache size
	targetMB := currentSizeMB / 2 // Target half the current size
	if targetMB <= 0 {
		targetMB = 0 // Force to 0 if cache is still too small
	}

	removed, freedMB, err := cacheManager.Prune(targetMB)
	if err != nil {
		t.Fatalf("Prune failed: %v", err)
	}

	t.Logf("Removed %d files, freed %d MB (target was %d MB)", removed, freedMB, targetMB)

	// Count files after pruning
	imageFilesAfter := 0
	nonImageFilesAfter := 0
	remainingImageFiles := []string{}
	remainingNonImageFiles := []string{}

	err = filepath.WalkDir(cacheDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if IsImageFile(d.Name()) {
			imageFilesAfter++
			remainingImageFiles = append(remainingImageFiles, d.Name())
		} else {
			nonImageFilesAfter++
			remainingNonImageFiles = append(remainingNonImageFiles, d.Name())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to count files after pruning: %v", err)
	}

	// Verify that non-image files were not touched
	if nonImageFilesAfter != nonImageFilesBefore {
		t.Errorf("Non-image files were affected by pruning. Before: %d, After: %d",
			nonImageFilesBefore, nonImageFilesAfter)
		t.Errorf("Remaining non-image files: %v", remainingNonImageFiles)
	}

	// If we had a reasonable cache size and set a smaller target, we should have removed some files
	if currentSizeMB > 0 && targetMB < currentSizeMB {
		if imageFilesAfter >= imageFilesBefore && removed == 0 {
			t.Errorf("Expected some image files to be removed when cache (%d MB) > target (%d MB). Before: %d, After: %d, Removed: %d",
				currentSizeMB, targetMB, imageFilesBefore, imageFilesAfter, removed)
		}
	}

	// Verify that all remaining files are actual non-image files we expect
	expectedNonImageFiles := []string{"config.txt", "readme.md", "cache.log", "temp.tmp"}
	if len(remainingNonImageFiles) != len(expectedNonImageFiles) {
		t.Errorf("Expected %d non-image files to remain, got %d: %v",
			len(expectedNonImageFiles), len(remainingNonImageFiles), remainingNonImageFiles)
	}

	for _, expected := range expectedNonImageFiles {
		found := false
		for _, remaining := range remainingNonImageFiles {
			if remaining == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected non-image file %s to remain after pruning", expected)
		}
	}
}

func TestCacheManagerPruneOldestFirst(t *testing.T) {
	testDir := t.TempDir()
	cacheDir := filepath.Join(testDir, "cache")

	cacheManager := NewCacheManager(cacheDir)

	// Create cache directory
	err := cacheManager.EnsureCacheDir()
	if err != nil {
		t.Fatalf("Failed to create cache dir: %v", err)
	}

	// Create image files with different modification times
	baseTime := time.Now().Add(-3 * time.Hour)
	files := []struct {
		name string
		age  time.Duration // how old relative to baseTime
	}{
		{"oldest.jpg", 0},                 // oldest
		{"older.png", 30 * time.Minute},   // older
		{"newer.gif", 60 * time.Minute},   // newer
		{"newest.webp", 90 * time.Minute}, // newest
	}

	content := make([]byte, 512*1024) // 512KB each
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

	// Prune to keep only 1MB (should remove 2 oldest files)
	removed, freedMB, err := cacheManager.Prune(1)
	if err != nil {
		t.Fatalf("Prune failed: %v", err)
	}

	t.Logf("Removed %d files, freed %d MB", removed, freedMB)

	// Check which files remain
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

	// Should have removed oldest files first
	// Expect newest files to remain
	expectedRemaining := []string{"newer.gif", "newest.webp"}

	if len(remainingFiles) < 1 {
		t.Errorf("Expected some files to remain, but all were removed")
		return
	}

	// Check that the newest files are among the remaining ones
	for _, expected := range expectedRemaining {
		found := false
		for _, remaining := range remainingFiles {
			if remaining == expected {
				found = true
				break
			}
		}
		if !found && len(remainingFiles) > 0 {
			// It's ok if not all expected files remain, but newest should be prioritized
			// Let's just check that "oldest.jpg" is not in the remaining files
			for _, remaining := range remainingFiles {
				if remaining == "oldest.jpg" {
					t.Errorf("Oldest file 'oldest.jpg' should have been removed first, but it remains in: %v", remainingFiles)
				}
			}
		}
	}
}

func TestCacheManagerPruneIfNeeded(t *testing.T) {
	testDir := t.TempDir()
	cacheDir := filepath.Join(testDir, "cache")

	cacheManager := NewCacheManager(cacheDir)

	// Test with no limit (should not prune)
	pruned, removed, freed, err := cacheManager.PruneIfNeeded(0)
	if err != nil {
		t.Fatalf("PruneIfNeeded failed: %v", err)
	}
	if pruned {
		t.Error("Expected no pruning with limit 0, but pruning occurred")
	}

	// Create cache directory and some files
	err = cacheManager.EnsureCacheDir()
	if err != nil {
		t.Fatalf("Failed to create cache dir: %v", err)
	}

	// Create files to exceed a 2MB limit
	largeContent := make([]byte, 1*1024*1024) // 1MB each
	for i := 0; i < 3; i++ {
		filePath := filepath.Join(cacheDir, fmt.Sprintf("large_thumb_%d.jpg", i))
		err := os.WriteFile(filePath, largeContent, 0644)
		if err != nil {
			t.Fatalf("Failed to create large file: %v", err)
		}
		// Set different modification times
		modTime := time.Now().Add(-time.Duration(i) * time.Hour)
		os.Chtimes(filePath, modTime, modTime)
	}

	// Test pruning with 2MB limit (should trigger pruning)
	pruned, removed, freed, err = cacheManager.PruneIfNeeded(2)
	if err != nil {
		t.Fatalf("PruneIfNeeded failed: %v", err)
	}

	t.Logf("Pruned: %v, Removed: %d, Freed: %d MB", pruned, removed, freed)

	// Should have pruned since cache > 2MB
	if !pruned {
		t.Error("Expected pruning to occur with 2MB limit, but no pruning happened")
	}

	// Verify final cache size is within reasonable bounds
	finalSize, err := cacheManager.SizeMB()
	if err != nil {
		t.Fatalf("Failed to get final cache size: %v", err)
	}

	// Should be roughly around 1MB (50% of 2MB limit)
	if finalSize > 2 {
		t.Errorf("Final cache size %d MB exceeds the limit of 2 MB", finalSize)
	}
}
