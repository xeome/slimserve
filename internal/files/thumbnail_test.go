package files

import (
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

	if !strings.Contains(err.Error(), "file too large") {
		t.Errorf("Expected 'file too large' error, got: %v", err)
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
