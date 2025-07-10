package files

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerate(t *testing.T) {
	// Create test image file
	testDir := t.TempDir()
	testImagePath := filepath.Join(testDir, "test.png")

	// Create a simple 32x32 test image
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	// Fill with red color
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}

	file, err := os.Create(testImagePath)
	if err != nil {
		t.Fatalf("Failed to create test image: %v", err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		t.Fatalf("Failed to encode test image: %v", err)
	}
	file.Close()

	// Set custom cache dir for testing
	customCacheDir := filepath.Join(testDir, "cache")
	os.Setenv("SLIMSERVE_CACHE_DIR", customCacheDir)
	defer os.Unsetenv("SLIMSERVE_CACHE_DIR")

	// Test thumbnail generation
	thumbPath, err := Generate(testImagePath, 16)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Verify thumbnail exists
	if _, err := os.Stat(thumbPath); os.IsNotExist(err) {
		t.Fatalf("Thumbnail file was not created: %s", thumbPath)
	}

	// Verify thumbnail is in expected cache directory
	if !strings.HasPrefix(thumbPath, customCacheDir) {
		t.Errorf("Thumbnail not in expected cache dir. Got: %s, expected prefix: %s", thumbPath, customCacheDir)
	}

	// Verify thumbnail dimensions
	thumbFile, err := os.Open(thumbPath)
	if err != nil {
		t.Fatalf("Failed to open thumbnail: %v", err)
	}
	defer thumbFile.Close()

	thumbImg, _, err := image.Decode(thumbFile)
	if err != nil {
		t.Fatalf("Failed to decode thumbnail: %v", err)
	}

	bounds := thumbImg.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Should be 16x16 since original was square
	if width != 16 || height != 16 {
		t.Errorf("Unexpected thumbnail dimensions: %dx%d, expected 16x16", width, height)
	}
}

func TestGenerateCache(t *testing.T) {
	testDir := t.TempDir()
	testImagePath := filepath.Join(testDir, "test.png")

	// Create test image
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	file, err := os.Create(testImagePath)
	if err != nil {
		t.Fatalf("Failed to create test image: %v", err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		t.Fatalf("Failed to encode test image: %v", err)
	}
	file.Close()

	// Set custom cache dir
	customCacheDir := filepath.Join(testDir, "cache")
	os.Setenv("SLIMSERVE_CACHE_DIR", customCacheDir)
	defer os.Unsetenv("SLIMSERVE_CACHE_DIR")

	// First generation
	thumbPath1, err := Generate(testImagePath, 10)
	if err != nil {
		t.Fatalf("First generate failed: %v", err)
	}

	// Get modification time of first thumbnail
	info1, err := os.Stat(thumbPath1)
	if err != nil {
		t.Fatalf("Failed to stat first thumbnail: %v", err)
	}

	// Wait a bit to ensure different timestamps
	time.Sleep(10 * time.Millisecond)

	// Second generation should use cache
	thumbPath2, err := Generate(testImagePath, 10)
	if err != nil {
		t.Fatalf("Second generate failed: %v", err)
	}

	// Should return same path
	if thumbPath1 != thumbPath2 {
		t.Errorf("Cache not used: got different paths %s != %s", thumbPath1, thumbPath2)
	}

	// Modification time should be the same (cached)
	info2, err := os.Stat(thumbPath2)
	if err != nil {
		t.Fatalf("Failed to stat second thumbnail: %v", err)
	}

	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Errorf("Thumbnail was regenerated instead of using cache")
	}
}

func TestGenerateLargeFile(t *testing.T) {
	testDir := t.TempDir()
	testImagePath := filepath.Join(testDir, "large.png")

	// Create a file that appears large (we'll just write 6MB of data)
	file, err := os.Create(testImagePath)
	if err != nil {
		t.Fatalf("Failed to create large file: %v", err)
	}
	defer file.Close()

	// Write 11MB of data to exceed the 10MB limit
	data := make([]byte, 11*1024*1024)
	file.Write(data)
	file.Close()

	// Should fail due to size limit
	_, err = Generate(testImagePath, 100)
	if err == nil {
		t.Error("Expected error for large file, but got none")
	}

	if !errors.Is(err, ErrFileTooLarge) {
		t.Errorf("Expected ErrFileTooLarge, got: %v", err)
	}
}

func TestGenerateUnsupportedFormat(t *testing.T) {
	testDir := t.TempDir()
	testFile := filepath.Join(testDir, "test.txt")

	// Create a text file
	if err := os.WriteFile(testFile, []byte("not an image"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Should fail for unsupported format
	_, err := Generate(testFile, 100)
	if err == nil {
		t.Error("Expected error for unsupported format, but got none")
	}
}

func TestGenerateAspectRatio(t *testing.T) {
	testDir := t.TempDir()
	testImagePath := filepath.Join(testDir, "rect.png")

	// Create a rectangular image (40x20)
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	file, err := os.Create(testImagePath)
	if err != nil {
		t.Fatalf("Failed to create test image: %v", err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		t.Fatalf("Failed to encode test image: %v", err)
	}
	file.Close()

	// Set custom cache dir
	customCacheDir := filepath.Join(testDir, "cache")
	os.Setenv("SLIMSERVE_CACHE_DIR", customCacheDir)
	defer os.Unsetenv("SLIMSERVE_CACHE_DIR")

	// Generate thumbnail with max dimension 20
	thumbPath, err := Generate(testImagePath, 20)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Check thumbnail dimensions
	thumbFile, err := os.Open(thumbPath)
	if err != nil {
		t.Fatalf("Failed to open thumbnail: %v", err)
	}
	defer thumbFile.Close()

	thumbImg, _, err := image.Decode(thumbFile)
	if err != nil {
		t.Fatalf("Failed to decode thumbnail: %v", err)
	}

	bounds := thumbImg.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Should be 20x10 (scaled down proportionally, width was limiting factor)
	if width != 20 || height != 10 {
		t.Errorf("Unexpected thumbnail dimensions: %dx%d, expected 20x10", width, height)
	}
}

func TestGenerateWithCacheLimit(t *testing.T) {
	testDir := t.TempDir()
	customCacheDir := filepath.Join(testDir, "cache")
	os.Setenv("SLIMSERVE_CACHE_DIR", customCacheDir)
	defer os.Unsetenv("SLIMSERVE_CACHE_DIR")

	tests := []struct {
		name         string
		cacheLimitMB int
		expectError  bool
		errorType    error
	}{
		{
			name:         "cache limit not exceeded",
			cacheLimitMB: 100, // Large limit
			expectError:  false,
		},
		{
			name:         "cache limit exceeded - should prune and succeed",
			cacheLimitMB: 2, // Small limit (2MB) - will trigger pruning but still succeed
			expectError:  false,
		},
		{
			name:         "unlimited cache",
			cacheLimitMB: 0, // No limit
			expectError:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// For the cache limit exceeded test, pre-populate cache with large files
			if strings.Contains(test.name, "cache limit exceeded") {
				// Create multiple large fake thumbnail files in cache to exceed 2MB limit
				err := os.MkdirAll(customCacheDir, 0755)
				if err != nil {
					t.Fatalf("Failed to create cache dir: %v", err)
				}

				// Create multiple 1MB files to ensure cache exceeds limit
				for i := 0; i < 3; i++ {
					largeCacheFile := filepath.Join(customCacheDir, fmt.Sprintf("fake_large_thumb_%d.jpg", i))
					largeData := make([]byte, 1*1024*1024) // 1MB file each
					err = os.WriteFile(largeCacheFile, largeData, 0644)
					if err != nil {
						t.Fatalf("Failed to create large cache file: %v", err)
					}
					// Set different modification times to test pruning order
					modTime := time.Now().Add(-time.Duration(i) * time.Hour)
					os.Chtimes(largeCacheFile, modTime, modTime)
				}

				// Verify cache size is above limit
				cacheManager := NewCacheManager(customCacheDir)
				cacheSize, err := cacheManager.SizeMB()
				if err != nil {
					t.Fatalf("Failed to get cache size: %v", err)
				}
				t.Logf("Cache size before test: %d MB, limit: %d MB", cacheSize, test.cacheLimitMB)
				if cacheSize <= int64(test.cacheLimitMB) {
					t.Fatalf("Cache size %d MB should be > limit %d MB for this test", cacheSize, test.cacheLimitMB)
				}
			}

			// Create test image
			testImagePath := filepath.Join(testDir, "test_"+test.name+".png")
			img := image.NewRGBA(image.Rect(0, 0, 32, 32))
			// Fill with red color
			for y := 0; y < 32; y++ {
				for x := 0; x < 32; x++ {
					img.Set(x, y, color.RGBA{255, 0, 0, 255})
				}
			}

			file, err := os.Create(testImagePath)
			if err != nil {
				t.Fatalf("Failed to create test image: %v", err)
			}
			defer file.Close()

			if err := png.Encode(file, img); err != nil {
				t.Fatalf("Failed to encode test image: %v", err)
			}
			file.Close()

			// Generate thumbnail with cache limit
			thumbPath, err := GenerateWithCacheLimit(testImagePath, 16, test.cacheLimitMB, 85, 10)

			if test.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if !errors.Is(err, test.errorType) {
					t.Errorf("expected error %v, got %v", test.errorType, err)
					return
				}
				// For error case, thumbnail path should be empty
				if thumbPath != "" {
					t.Errorf("expected empty thumbnail path on error, got %s", thumbPath)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
				// Verify thumbnail was created
				if _, err := os.Stat(thumbPath); os.IsNotExist(err) {
					t.Errorf("thumbnail file was not created at %s", thumbPath)
				}

				// For cache limit exceeded test, verify cache size is within limit after generation
				if strings.Contains(test.name, "cache limit exceeded") {
					cacheManager := NewCacheManager(customCacheDir)
					finalCacheSize, err := cacheManager.SizeMB()
					if err != nil {
						t.Errorf("Failed to get final cache size: %v", err)
					} else {
						t.Logf("Final cache size: %d MB, limit: %d MB", finalCacheSize, test.cacheLimitMB)
						if finalCacheSize > int64(test.cacheLimitMB) {
							t.Errorf("Cache size %d MB exceeds limit %d MB after generation", finalCacheSize, test.cacheLimitMB)
						}
					}
				}
			}
		})
	}
}

func TestCacheSizeMB(t *testing.T) {
	testDir := t.TempDir()
	customCacheDir := filepath.Join(testDir, "cache")

	// Initially empty cache should have size 0
	cacheManager := NewCacheManager(customCacheDir)
	size, err := cacheManager.SizeMB()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if size != 0 {
		t.Errorf("expected empty cache size 0, got %d", size)
	}

	// Set cache dir for thumbnail generation
	os.Setenv("SLIMSERVE_CACHE_DIR", customCacheDir)
	defer os.Unsetenv("SLIMSERVE_CACHE_DIR")

	// Create test image and thumbnail
	testImagePath := filepath.Join(testDir, "test.png")
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	file, err := os.Create(testImagePath)
	if err != nil {
		t.Fatalf("Failed to create test image: %v", err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		t.Fatalf("Failed to encode test image: %v", err)
	}
	file.Close()

	_, err = GenerateWithCacheLimit(testImagePath, 16, 0, 85, 10) // No limit
	if err != nil {
		t.Errorf("unexpected error creating thumbnail: %v", err)
	}

	// Cache should now have some size
	size, err = cacheManager.SizeMB()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// Size should be small but non-negative (thumbnail files are typically small, may be 0 MB due to rounding)
	if size < 0 {
		t.Errorf("expected non-negative cache size, got %d", size)
	}
}

func TestBackwardCompatibility(t *testing.T) {
	testDir := t.TempDir()
	customCacheDir := filepath.Join(testDir, "cache")
	os.Setenv("SLIMSERVE_CACHE_DIR", customCacheDir)
	defer os.Unsetenv("SLIMSERVE_CACHE_DIR")

	// Create test image
	testImagePath := filepath.Join(testDir, "test.png")
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	file, err := os.Create(testImagePath)
	if err != nil {
		t.Fatalf("Failed to create test image: %v", err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		t.Fatalf("Failed to encode test image: %v", err)
	}
	file.Close()

	// Test that the original Generate function still works (should have no cache limit)
	thumbPath1, err := Generate(testImagePath, 16)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Test that GenerateWithCacheLimit with 0 limit works the same
	thumbPath2, err := GenerateWithCacheLimit(testImagePath, 16, 0, 85, 10)
	if err != nil {
		t.Fatalf("GenerateWithCacheLimit failed: %v", err)
	}

	// Both should generate thumbnails (cache paths may differ due to timing, but both should exist)
	if _, err := os.Stat(thumbPath1); os.IsNotExist(err) {
		t.Errorf("thumbnail from Generate was not created at %s", thumbPath1)
	}
	if _, err := os.Stat(thumbPath2); os.IsNotExist(err) {
		t.Errorf("thumbnail from GenerateWithCacheLimit was not created at %s", thumbPath2)
	}
}

// Benchmark tests for performance-critical code paths

// BenchmarkGenerateCacheKey benchmarks the cache key generation function
func BenchmarkGenerateCacheKey(b *testing.B) {
	// Create test image file
	testDir := b.TempDir()
	testImagePath := filepath.Join(testDir, "test.png")

	// Create a simple test image
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 0, 255})
		}
	}

	file, err := os.Create(testImagePath)
	if err != nil {
		b.Fatalf("Failed to create test image: %v", err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		b.Fatalf("Failed to encode test image: %v", err)
	}
	file.Close()

	// Benchmark different dimensions to test various scenarios
	dimensions := []int{64, 128, 256, 512, 1024}

	for _, maxDim := range dimensions {
		b.Run(fmt.Sprintf("dim_%d", maxDim), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := generateCacheKey(testImagePath, maxDim)
				if err != nil {
					b.Fatalf("generateCacheKey failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkGenerateCacheKeyLargeFile benchmarks cache key generation with large files
func BenchmarkGenerateCacheKeyLargeFile(b *testing.B) {
	// Create a larger test image file (simulating real-world scenarios)
	testDir := b.TempDir()
	testImagePath := filepath.Join(testDir, "large_test.png")

	// Create a larger image (1000x1000)
	img := image.NewRGBA(image.Rect(0, 0, 1000, 1000))
	for y := 0; y < 1000; y++ {
		for x := 0; x < 1000; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), uint8((x + y) % 256), 255})
		}
	}

	file, err := os.Create(testImagePath)
	if err != nil {
		b.Fatalf("Failed to create test image: %v", err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		b.Fatalf("Failed to encode test image: %v", err)
	}
	file.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := generateCacheKey(testImagePath, 256)
		if err != nil {
			b.Fatalf("generateCacheKey failed: %v", err)
		}
	}
}

// BenchmarkThumbnailGeneration benchmarks the complete thumbnail generation process
func BenchmarkThumbnailGeneration(b *testing.B) {
	// Create test image file
	testDir := b.TempDir()
	testImagePath := filepath.Join(testDir, "bench_test.png")

	// Create test image
	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), uint8((x * y) % 256), 255})
		}
	}

	file, err := os.Create(testImagePath)
	if err != nil {
		b.Fatalf("Failed to create test image: %v", err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		b.Fatalf("Failed to encode test image: %v", err)
	}
	file.Close()

	// Set custom cache dir
	customCacheDir := filepath.Join(testDir, "cache")
	os.Setenv("SLIMSERVE_CACHE_DIR", customCacheDir)
	defer os.Unsetenv("SLIMSERVE_CACHE_DIR")

	// Test different thumbnail sizes
	sizes := []int{64, 128, 256, 512}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size_%d", size), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Use a unique path for each iteration to avoid cache hits
				uniquePath := fmt.Sprintf("%s_%d_%d", testImagePath, size, i)
				// Copy the test file to unique path
				if err := copyFile(testImagePath, uniquePath); err != nil {
					b.Fatalf("Failed to copy test file: %v", err)
				}
				defer os.Remove(uniquePath)

				_, err := Generate(uniquePath, size)
				if err != nil {
					b.Fatalf("Generate failed: %v", err)
				}
			}
		})
	}
}

// Helper function to copy files for benchmarking
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = destFile.ReadFrom(sourceFile)
	return err
}
