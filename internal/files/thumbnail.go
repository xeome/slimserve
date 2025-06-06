package files

import (
	"crypto/sha1"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"mime"
	"os"
	"path/filepath"
	"strings"
)

// Generate creates a thumbnail for the given source file path with the specified maximum dimension.
// The thumbnail is cached in the system temp directory under slimserve/thumbcache.
// Cache directory can be overridden via SLIMSERVE_CACHE_DIR environment variable.
//
// Returns the path to the generated thumbnail file, or an error if generation fails.
// Cached filename scheme: <sha1 of filePath + mtime + maxDim>.<ext>
//
// Supported formats: JPEG, PNG, GIF (detected via MIME type)
// Files larger than 5MB are skipped to prevent memory issues.
func Generate(srcPath string, maxDim int) (string, error) {
	// Check file size first - skip if > 5MB
	info, err := os.Stat(srcPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat source file: %w", err)
	}

	const maxFileSize = 5 * 1024 * 1024 // 5MB
	if info.Size() > maxFileSize {
		return "", fmt.Errorf("file too large for thumbnail generation: %d bytes", info.Size())
	}

	// Determine cache directory
	cacheDir := os.Getenv("SLIMSERVE_CACHE_DIR")
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "slimserve", "thumbcache")
	}

	// Ensure cache directory exists
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Generate cache key based on file path, modification time, and max dimension
	cacheKey := fmt.Sprintf("%s_%d_%d", srcPath, info.ModTime().Unix(), maxDim)
	hash := sha1.Sum([]byte(cacheKey))

	// Determine output format based on source file
	ext := strings.ToLower(filepath.Ext(srcPath))
	var outputExt string
	switch ext {
	case ".jpg", ".jpeg":
		outputExt = ".jpg"
	case ".png":
		outputExt = ".png"
	case ".gif":
		outputExt = ".gif"
	default:
		// For unsupported formats, try to detect MIME type
		mimeType := mime.TypeByExtension(ext)
		if !strings.HasPrefix(mimeType, "image/") {
			return "", fmt.Errorf("unsupported image format: %s", ext)
		}
		outputExt = ".jpg" // Default to JPEG for unknown image types
	}

	thumbPath := filepath.Join(cacheDir, fmt.Sprintf("%x%s", hash, outputExt))

	// Check if cached thumbnail exists and is newer than source
	if thumbInfo, err := os.Stat(thumbPath); err == nil {
		if thumbInfo.ModTime().After(info.ModTime()) || thumbInfo.ModTime().Equal(info.ModTime()) {
			return thumbPath, nil
		}
	}

	// Generate thumbnail
	if err := generateThumbnail(srcPath, thumbPath, maxDim, outputExt); err != nil {
		return "", fmt.Errorf("failed to generate thumbnail: %w", err)
	}

	return thumbPath, nil
}

// generateThumbnail creates a thumbnail by scaling the source image proportionally
// so that max(width, height) = maxDim, using nearest-neighbor scaling.
func generateThumbnail(srcPath, thumbPath string, maxDim int, outputExt string) error {
	// Open source file
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	// Decode image
	srcImg, _, err := image.Decode(srcFile)
	if err != nil {
		return fmt.Errorf("failed to decode image: %w", err)
	}

	// Calculate new dimensions maintaining aspect ratio
	bounds := srcImg.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	var newWidth, newHeight int
	if width > height {
		newWidth = maxDim
		newHeight = height * maxDim / width
	} else {
		newHeight = maxDim
		newWidth = width * maxDim / height
	}

	// Skip if image is already smaller than target
	if width <= maxDim && height <= maxDim {
		newWidth = width
		newHeight = height
	}

	// Create new image with calculated dimensions
	thumbImg := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))

	// Scale using nearest neighbor
	for y := 0; y < newHeight; y++ {
		for x := 0; x < newWidth; x++ {
			srcX := x * width / newWidth
			srcY := y * height / newHeight
			thumbImg.Set(x, y, srcImg.At(srcX, srcY))
		}
	}

	// Create output file
	thumbFile, err := os.Create(thumbPath)
	if err != nil {
		return fmt.Errorf("failed to create thumbnail file: %w", err)
	}
	defer thumbFile.Close()

	// Encode based on output format
	switch outputExt {
	case ".jpg":
		err = jpeg.Encode(thumbFile, thumbImg, &jpeg.Options{Quality: 85})
	case ".png":
		err = png.Encode(thumbFile, thumbImg)
	case ".gif":
		err = gif.Encode(thumbFile, thumbImg, nil)
	default:
		return fmt.Errorf("unsupported output format: %s", outputExt)
	}

	if err != nil {
		return fmt.Errorf("failed to encode thumbnail: %w", err)
	}

	return nil
}
