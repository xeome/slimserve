package files

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkCacheSizeMB(b *testing.B) {
	testDir := b.TempDir()
	cacheDir := filepath.Join(testDir, "cache")

	cm, err := NewCacheManager(cacheDir, 100)
	if err != nil {
		b.Fatalf("Failed to create cache manager: %v", err)
	}

	fileSizes := []int{1024, 10240, 102400, 1048576}

	for i, size := range fileSizes {
		fileName := fmt.Sprintf("thumb_%d.jpg", i)
		filePath := filepath.Join(cacheDir, fileName)

		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			b.Fatalf("Failed to create cache dir: %v", err)
		}

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
		cm.SizeMB()
	}
}

func BenchmarkCachePruneIfNeeded(b *testing.B) {
	testDir := b.TempDir()
	cacheDir := filepath.Join(testDir, "cache")

	scenarios := []struct {
		name         string
		maxCacheMB   int
		createSizeMB int
	}{
		{"no_pruning_needed", 100, 5},
		{"pruning_needed", 10, 15},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			subCacheDir := filepath.Join(cacheDir, scenario.name)
			if err := os.MkdirAll(subCacheDir, 0755); err != nil {
				b.Fatalf("Failed to create cache dir: %v", err)
			}

			numFiles := scenario.createSizeMB
			for i := 0; i < numFiles; i++ {
				fileName := fmt.Sprintf("thumb_%d.jpg", i)
				filePath := filepath.Join(subCacheDir, fileName)

				data := make([]byte, 1024*1024)
				if err := os.WriteFile(filePath, data, 0644); err != nil {
					b.Fatalf("Failed to create test file: %v", err)
				}
			}

			subCM, err := NewCacheManager(subCacheDir, scenario.maxCacheMB)
			if err != nil {
				b.Fatalf("Failed to create cache manager: %v", err)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				subCM.PruneIfNeeded(scenario.maxCacheMB)
			}
		})
	}
}

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
