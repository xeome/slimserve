package files

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkCacheSizeMB benchmarks the cache size calculation
func BenchmarkCacheSizeMB(b *testing.B) {
	testDir := b.TempDir()
	cacheDir := filepath.Join(testDir, "cache")

	// Create cache manager
	cm := NewCacheManager(cacheDir)

	// Create test cache files of different sizes
	fileSizes := []int{1024, 10240, 102400, 1048576} // 1KB, 10KB, 100KB, 1MB

	for i, size := range fileSizes {
		fileName := fmt.Sprintf("thumb_%d.jpg", i)
		filePath := filepath.Join(cacheDir, fileName)

		// Ensure cache directory exists
		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			b.Fatalf("Failed to create cache dir: %v", err)
		}

		// Create test file with specified size
		data := make([]byte, size)
		for j := range data {
			data[j] = byte(j % 256)
		}

		if err := os.WriteFile(filePath, data, 0644); err != nil {
			b.Fatalf("Failed to create test file: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := cm.SizeMB()
		if err != nil {
			b.Fatalf("SizeMB failed: %v", err)
		}
	}
}

// BenchmarkCacheCollectFiles benchmarks the cache file collection process
func BenchmarkCacheCollectFiles(b *testing.B) {
	testDir := b.TempDir()
	cacheDir := filepath.Join(testDir, "cache")

	// Create many test cache files to simulate real-world scenario
	numFiles := []int{10, 50, 100, 500}

	for _, count := range numFiles {
		b.Run(fmt.Sprintf("files_%d", count), func(b *testing.B) {
			// Setup: create test files
			subCacheDir := filepath.Join(cacheDir, fmt.Sprintf("test_%d", count))
			if err := os.MkdirAll(subCacheDir, 0755); err != nil {
				b.Fatalf("Failed to create cache dir: %v", err)
			}

			for i := 0; i < count; i++ {
				fileName := fmt.Sprintf("thumb_%d.jpg", i)
				filePath := filepath.Join(subCacheDir, fileName)

				// Create small test file
				data := make([]byte, 1024) // 1KB each
				if err := os.WriteFile(filePath, data, 0644); err != nil {
					b.Fatalf("Failed to create test file: %v", err)
				}
			}

			// Create cache manager for this subdirectory
			subCM := NewCacheManager(subCacheDir)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := subCM.collectCacheFiles()
				if err != nil {
					b.Fatalf("collectCacheFiles failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkCachePrune benchmarks the cache pruning operation
func BenchmarkCachePrune(b *testing.B) {
	testDir := b.TempDir()
	cacheDir := filepath.Join(testDir, "cache")

	// Create cache manager
	cm := NewCacheManager(cacheDir)

	// Create test files that exceed target size
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		b.Fatalf("Failed to create cache dir: %v", err)
	}

	// Create 20 files of 1MB each (20MB total)
	for i := 0; i < 20; i++ {
		fileName := fmt.Sprintf("thumb_%d.jpg", i)
		filePath := filepath.Join(cacheDir, fileName)

		// Create 1MB file
		data := make([]byte, 1024*1024)
		for j := range data {
			data[j] = byte(j % 256)
		}

		if err := os.WriteFile(filePath, data, 0644); err != nil {
			b.Fatalf("Failed to create test file: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Reset cache for each iteration by recreating files
		if i > 0 {
			// Recreate files that were pruned
			for j := 0; j < 20; j++ {
				fileName := fmt.Sprintf("thumb_%d.jpg", j)
				filePath := filepath.Join(cacheDir, fileName)

				if _, err := os.Stat(filePath); os.IsNotExist(err) {
					data := make([]byte, 1024*1024)
					if err := os.WriteFile(filePath, data, 0644); err != nil {
						b.Fatalf("Failed to recreate test file: %v", err)
					}
				}
			}
		}

		// Prune to 5MB (should remove ~15MB worth of files)
		_, _, err := cm.Prune(5)
		if err != nil {
			b.Fatalf("Prune failed: %v", err)
		}
	}
}

// BenchmarkCachePruneIfNeeded benchmarks the conditional pruning operation
func BenchmarkCachePruneIfNeeded(b *testing.B) {
	testDir := b.TempDir()
	cacheDir := filepath.Join(testDir, "cache")

	// Test scenarios: no pruning needed vs pruning needed
	scenarios := []struct {
		name         string
		maxCacheMB   int
		createSizeMB int
	}{
		{"no_pruning_needed", 100, 5}, // 5MB cache, 100MB limit
		{"pruning_needed", 10, 15},    // 15MB cache, 10MB limit
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			// Setup: create cache files
			subCacheDir := filepath.Join(cacheDir, scenario.name)
			if err := os.MkdirAll(subCacheDir, 0755); err != nil {
				b.Fatalf("Failed to create cache dir: %v", err)
			}

			// Create files totaling approximately the desired size
			numFiles := scenario.createSizeMB
			for i := 0; i < numFiles; i++ {
				fileName := fmt.Sprintf("thumb_%d.jpg", i)
				filePath := filepath.Join(subCacheDir, fileName)

				// Create 1MB file
				data := make([]byte, 1024*1024)
				if err := os.WriteFile(filePath, data, 0644); err != nil {
					b.Fatalf("Failed to create test file: %v", err)
				}
			}

			subCM := NewCacheManager(subCacheDir)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _, _, err := subCM.PruneIfNeeded(scenario.maxCacheMB)
				if err != nil {
					b.Fatalf("PruneIfNeeded failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkIsImageFile benchmarks the image file detection function
func BenchmarkIsImageFile(b *testing.B) {
	testFiles := []string{
		"image.jpg",
		"image.jpeg",
		"image.png",
		"image.gif",
		"image.webp",
		"document.pdf",
		"text.txt",
		"video.mp4",
		"archive.zip",
		"no_extension",
	}

	for _, filename := range testFiles {
		b.Run(filename, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				IsImageFile(filename)
			}
		})
	}
}
