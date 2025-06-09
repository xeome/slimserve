package server

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slimserve/internal/config"
	"slimserve/internal/security"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestThumbnailGeneration(t *testing.T) {
	// Create temporary directory with test image
	tmpDir, err := os.MkdirTemp("", "slimserve-thumb-test")
	if err != nil {
		t.Fatal("Failed to create temp dir:", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test PNG image (50x50)
	testImagePath := filepath.Join(tmpDir, "test.png")
	img := image.NewRGBA(image.Rect(0, 0, 50, 50))
	// Fill with red color
	for y := 0; y < 50; y++ {
		for x := 0; x < 50; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}

	file, err := os.Create(testImagePath)
	if err != nil {
		t.Fatal("Failed to create test image:", err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		t.Fatal("Failed to encode test image:", err)
	}
	file.Close()

	// Create handler
	cfg := &config.Config{
		Host:               "localhost",
		Port:               8080,
		Directories:        []string{tmpDir},
		DisableDotFiles:    true,
		ThumbMaxFileSizeMB: 20,
	}

	// Create RootFS instances
	var roots []*security.RootFS
	for _, dir := range cfg.Directories {
		root, err := security.NewRootFS(dir)
		require.NoError(t, err)
		roots = append(roots, root)
	}
	defer func() {
		for _, root := range roots {
			root.Close()
		}
	}()

	handler := NewHandler(cfg, roots)
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		path           string
		query          string
		expectedStatus int
		checkHeaders   map[string]string
	}{
		{
			name:           "serve_original_image",
			path:           "/test.png",
			query:          "",
			expectedStatus: http.StatusOK,
			checkHeaders:   map[string]string{"Content-Type": "image/png"},
		},
		{
			name:           "serve_thumbnail",
			path:           "/test.png",
			query:          "thumb=1",
			expectedStatus: http.StatusOK,
			checkHeaders:   map[string]string{"Content-Type": "image/jpeg"},
		},
		{
			name:           "thumbnail_non_image_fallback",
			path:           "/nonexistent.txt",
			query:          "thumb=1",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			requestURL := tt.path
			if tt.query != "" {
				requestURL += "?" + tt.query
			}

			c.Request = httptest.NewRequest("GET", requestURL, nil)
			c.Params = gin.Params{
				{Key: "path", Value: tt.path},
			}

			// Parse query manually for test context
			if tt.query != "" {
				parts := strings.Split(tt.query, "=")
				if len(parts) == 2 {
					c.Request.URL.RawQuery = tt.query
				}
			}

			handler.ServeFiles(c)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			// Check headers if specified
			for headerName, expectedValue := range tt.checkHeaders {
				actualValue := w.Header().Get(headerName)
				if !strings.Contains(actualValue, expectedValue) {
					t.Errorf("Expected header %s to contain '%s', got '%s'", headerName, expectedValue, actualValue)
				}
			}

			// For successful thumbnail requests, verify we got image data
			if tt.name == "serve_thumbnail" && w.Code == http.StatusOK {
				body := w.Body.Bytes()
				if len(body) == 0 {
					t.Error("Thumbnail response should contain image data")
				}

				// Try to decode the response as JPEG to verify it's a valid image
				_, err := jpeg.Decode(strings.NewReader(string(body)))
				if err != nil {
					t.Errorf("Thumbnail response should be valid JPEG: %v", err)
				}
			}
		})
	}
}

func TestThumbnailURLGeneration(t *testing.T) {
	tests := []struct {
		basePath string
		fileName string
		expected string
	}{
		{"/", "image.jpg", "/image.jpg?thumb=1"},
		{"/photos", "vacation.png", "/photos/vacation.png?thumb=1"},
		{"/docs/images", "diagram.gif", "/docs/images/diagram.gif?thumb=1"},
	}

	for _, tt := range tests {
		result := buildThumbnailURL(tt.basePath, tt.fileName)
		if result != tt.expected {
			t.Errorf("buildThumbnailURL(%q, %q) = %q, expected %q",
				tt.basePath, tt.fileName, result, tt.expected)
		}
	}
}

func TestServeThumbnailMethod(t *testing.T) {
	// Create temporary directory with various test files
	tmpDir, err := os.MkdirTemp("", "slimserve-thumb-method-test")
	if err != nil {
		t.Fatal("Failed to create temp dir:", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test image file
	testImagePath := filepath.Join(tmpDir, "test.jpg")
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{0, 255, 0, 255}) // Green
		}
	}

	file, err := os.Create(testImagePath)
	if err != nil {
		t.Fatal("Failed to create test image:", err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		t.Fatal("Failed to encode test image:", err)
	}
	file.Close()

	// Create test text file
	testTextPath := filepath.Join(tmpDir, "text.txt")
	if err := os.WriteFile(testTextPath, []byte("not an image"), 0644); err != nil {
		t.Fatal("Failed to create text file:", err)
	}

	// Create handler
	cfg := &config.Config{
		Host:               "localhost",
		Port:               8080,
		Directories:        []string{tmpDir},
		DisableDotFiles:    true,
		ThumbMaxFileSizeMB: 20,
	}

	// Create RootFS instances
	var roots []*security.RootFS
	for _, dir := range cfg.Directories {
		root, err := security.NewRootFS(dir)
		require.NoError(t, err)
		roots = append(roots, root)
	}
	defer func() {
		for _, root := range roots {
			root.Close()
		}
	}()

	handler := NewHandler(cfg, roots)
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name                   string
		requestPath            string
		expectedStatus         int
		shouldContainImageData bool
	}{
		{
			name:                   "thumbnail_valid_image",
			requestPath:            "/test.jpg",
			expectedStatus:         http.StatusOK,
			shouldContainImageData: true,
		},
		{
			name:                   "thumbnail_text_file_fallback",
			requestPath:            "/text.txt",
			expectedStatus:         http.StatusOK,
			shouldContainImageData: false,
		},
		{
			name:                   "thumbnail_nonexistent_file",
			requestPath:            "/nonexistent.png",
			expectedStatus:         http.StatusNotFound,
			shouldContainImageData: false,
		},
		{
			name:                   "thumbnail_directory_path",
			requestPath:            "/",
			expectedStatus:         http.StatusNotFound,
			shouldContainImageData: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", tt.requestPath, nil)

			// Clean path for handler
			cleanPath := filepath.Clean(tt.requestPath)
			if cleanPath == "." {
				cleanPath = "/"
			}

			// Call serveThumbnailFromRoot directly
			relPath := strings.TrimPrefix(cleanPath, "/")
			handler.serveThumbnailFromRoot(c, relPath)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.shouldContainImageData && w.Code == http.StatusOK {
				body := w.Body.Bytes()
				if len(body) == 0 {
					t.Error("Expected image data in response body")
				}
			}
		})
	}
}

func TestThumbnailErrorPaths(t *testing.T) {
	// Test various error conditions
	cfg := &config.Config{
		Host:            "localhost",
		Port:            8080,
		Directories:     []string{}, // Empty directories to force 404
		DisableDotFiles: true,
	}

	// Create RootFS instances
	var roots []*security.RootFS
	for _, dir := range cfg.Directories {
		root, err := security.NewRootFS(dir)
		require.NoError(t, err)
		roots = append(roots, root)
	}
	defer func() {
		for _, root := range roots {
			root.Close()
		}
	}()

	handler := NewHandler(cfg, roots)
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/any.jpg", nil)

	handler.serveThumbnailFromRoot(c, "any.jpg")

	// Should return 404 when no allowed roots are configured
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestImageFileDetection(t *testing.T) {
	tests := []struct {
		fileName string
		expected bool
	}{
		{"test.jpg", true},
		{"test.jpeg", true},
		{"test.png", true},
		{"test.gif", true},
		{"test.webp", true},
		{"test.svg", true},
		{"test.JPG", true}, // Case insensitive
		{"test.txt", false},
		{"test.pdf", false},
		{"test", false},       // No extension
		{".hidden.jpg", true}, // Hidden but still image
	}

	for _, tt := range tests {
		result := isImageFile(tt.fileName)
		if result != tt.expected {
			t.Errorf("isImageFile(%q) = %v, expected %v", tt.fileName, result, tt.expected)
		}
	}
}
