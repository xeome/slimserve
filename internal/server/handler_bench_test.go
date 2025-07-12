package server

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slimserve/internal/config"
	"slimserve/internal/security"
	"testing"

	"github.com/gin-gonic/gin"
)

// setupBenchmarkHandler creates a handler with test data for benchmarking
func setupBenchmarkHandler(b *testing.B, numFiles, numDirs int) (*Handler, string) {
	testDir := b.TempDir()
	
	// Create test directory structure
	for i := 0; i < numDirs; i++ {
		dirPath := filepath.Join(testDir, fmt.Sprintf("dir_%d", i))
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			b.Fatalf("Failed to create test directory: %v", err)
		}
		
		// Create files in each directory
		for j := 0; j < numFiles; j++ {
			filePath := filepath.Join(dirPath, fmt.Sprintf("file_%d.txt", j))
			content := fmt.Sprintf("This is test file %d in directory %d", j, i)
			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				b.Fatalf("Failed to create test file: %v", err)
			}
		}
	}
	
	// Create some image files for thumbnail testing
	for i := 0; i < 5; i++ {
		imagePath := filepath.Join(testDir, fmt.Sprintf("image_%d.png", i))
		img := image.NewRGBA(image.Rect(0, 0, 200, 200))
		for y := 0; y < 200; y++ {
			for x := 0; x < 200; x++ {
				img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), uint8(i * 50), 255})
			}
		}
		
		file, err := os.Create(imagePath)
		if err != nil {
			b.Fatalf("Failed to create test image: %v", err)
		}
		
		if err := png.Encode(file, img); err != nil {
			file.Close()
			b.Fatalf("Failed to encode test image: %v", err)
		}
		file.Close()
	}
	
	// Create RootFS and handler
	root, err := security.NewRootFS(testDir)
	if err != nil {
		b.Fatalf("Failed to create RootFS: %v", err)
	}
	
	cfg := &config.Config{
		Directories:         []string{testDir},
		DisableDotFiles:     true,
		MaxThumbCacheMB:     100,
		ThumbJpegQuality:    85,
		ThumbMaxFileSizeMB:  10,
	}
	
	handler := NewHandler(cfg, []*security.RootFS{root})
	return handler, testDir
}

// BenchmarkServeFiles benchmarks the main file serving function
func BenchmarkServeFiles(b *testing.B) {
	gin.SetMode(gin.TestMode)
	
	scenarios := []struct {
		name     string
		numFiles int
		numDirs  int
		path     string
	}{
		{"small_dir", 10, 1, "/"},
		{"medium_dir", 50, 1, "/"},
		{"large_dir", 200, 1, "/"},
		{"nested_dirs", 20, 10, "/"},
	}
	
	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			handler, _ := setupBenchmarkHandler(b, scenario.numFiles, scenario.numDirs)
			
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest("GET", scenario.path, nil)
				c.Params = gin.Params{{Key: "path", Value: scenario.path}}
				
				handler.ServeFiles(c)
				
				if w.Code != http.StatusOK {
					b.Fatalf("Expected status 200, got %d", w.Code)
				}
			}
		})
	}
}

// BenchmarkServeFileFromRoot benchmarks individual file serving
func BenchmarkServeFileFromRoot(b *testing.B) {
	gin.SetMode(gin.TestMode)
	handler, testDir := setupBenchmarkHandler(b, 10, 1)
	
	// Test different file sizes
	fileSizes := []int{1024, 10240, 102400, 1048576} // 1KB, 10KB, 100KB, 1MB
	
	for _, size := range fileSizes {
		b.Run(fmt.Sprintf("size_%dB", size), func(b *testing.B) {
			// Create test file of specific size
			fileName := fmt.Sprintf("test_file_%d.txt", size)
			filePath := filepath.Join(testDir, fileName)
			data := make([]byte, size)
			for i := range data {
				data[i] = byte(i % 256)
			}
			
			if err := os.WriteFile(filePath, data, 0644); err != nil {
				b.Fatalf("Failed to create test file: %v", err)
			}
			
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest("GET", "/"+fileName, nil)
				c.Params = gin.Params{{Key: "path", Value: "/" + fileName}}
				
				handler.ServeFiles(c)
				
				if w.Code != http.StatusOK {
					b.Fatalf("Expected status 200, got %d", w.Code)
				}
			}
		})
	}
}

// BenchmarkServeThumbnailFromRoot benchmarks thumbnail serving
func BenchmarkServeThumbnailFromRoot(b *testing.B) {
	gin.SetMode(gin.TestMode)
	handler, testDir := setupBenchmarkHandler(b, 5, 1)
	
	// Set cache directory for thumbnails
	cacheDir := filepath.Join(testDir, "thumb_cache")
	os.Setenv("SLIMSERVE_CACHE_DIR", cacheDir)
	defer os.Unsetenv("SLIMSERVE_CACHE_DIR")
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/image_0.png?thumb=1", nil)
		c.Params = gin.Params{{Key: "path", Value: "/image_0.png"}}
		
		handler.ServeFiles(c)
		
		if w.Code != http.StatusOK {
			b.Fatalf("Expected status 200, got %d", w.Code)
		}
	}
}

// BenchmarkContainsDotFile benchmarks dot file detection
func BenchmarkContainsDotFile(b *testing.B) {
	handler, _ := setupBenchmarkHandler(b, 1, 1)
	
	testPaths := []string{
		"/normal/path/file.txt",
		"/path/with/.dotfile",
		"/.hidden/file.txt",
		"/very/long/path/with/many/segments/file.txt",
		"/path/with/.hidden/and/.more/.dotfiles",
		"/normal/file",
	}
	
	for _, path := range testPaths {
		b.Run(fmt.Sprintf("path_%s", filepath.Base(path)), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				handler.containsDotFile(path)
			}
		})
	}
}

// BenchmarkTryServeFromRoots benchmarks the root traversal logic
func BenchmarkTryServeFromRoots(b *testing.B) {
	gin.SetMode(gin.TestMode)
	handler, _ := setupBenchmarkHandler(b, 20, 3)
	
	testPaths := []string{
		"dir_0/file_0.txt",
		"dir_1/file_5.txt", 
		"dir_2/file_10.txt",
		"nonexistent/file.txt",
		"image_0.png",
	}
	
	for _, relPath := range testPaths {
		b.Run(fmt.Sprintf("path_%s", filepath.Base(relPath)), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest("GET", "/"+relPath, nil)
				
				cleanPath := "/" + relPath
				handler.tryServeFromRoots(c, relPath, cleanPath)
			}
		})
	}
}
