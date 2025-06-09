package files

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // import for side effects
	"os"
	"path/filepath"
	"runtime"
	"slimserve/internal/logger"
	"syscall"
	"time"

	"github.com/cespare/xxhash/v2"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // import for side effects
)

var (
	// ErrCacheSizeExceeded is deprecated and no longer returned, kept for API compatibility
	ErrCacheSizeExceeded = errors.New("thumbnail cache size limit exceeded")
	// ErrFileTooLarge is returned when a source image exceeds the size limit for thumbnailing.
	ErrFileTooLarge = errors.New("file too large for thumbnail generation")
)

// Generate creates a thumbnail for the given source file path with the specified maximum dimension.
// It is a wrapper around GenerateWithCacheLimit for backward compatibility.
func Generate(srcPath string, maxDim int) (string, error) {
	// Call the new function with default values for new parameters
	return GenerateWithCacheLimit(srcPath, maxDim, 0, 85)
}

// GenerateWithCacheLimit creates a thumbnail with cache size checking and configurable generation options.
// It now supports forcing JPEG output, configurable JPEG quality, and a conditional scaling algorithm.
func GenerateWithCacheLimit(srcPath string, maxDim, maxCacheMB, jpegQuality int) (string, error) {
	start := time.Now()
	logger.Debugf("Starting thumbnail generation for %s (max dimension: %d)", srcPath, maxDim)
	defer func() {
		// Aggressively trigger GC to release memory after thumbnail generation
		runtime.GC()
		logger.Debugf("Forced garbage collection after thumbnail generation for %s", srcPath)
	}()

	// Check file size first - skip if > 10MB
	info, err := os.Stat(srcPath)
	if err != nil {
		logger.Errorf("Failed to stat source file %s: %v", srcPath, err)
		return "", fmt.Errorf("failed to stat source file: %w", err)
	}

	const maxFileSize = 10 * 1024 * 1024 // 10MB
	if info.Size() > maxFileSize {
		logger.Errorf("File too large for thumbnail generation: %s (%d bytes)", srcPath, info.Size())
		return "", ErrFileTooLarge
	}

	cacheDir := os.Getenv("SLIMSERVE_CACHE_DIR")
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "slimserve", "thumbcache")
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	cacheKey, err := generateCacheKey(srcPath, maxDim)
	if err != nil {
		return "", fmt.Errorf("failed to generate cache key: %w", err)
	}

	// All thumbnails are now JPEG
	outputExt := ".jpg"
	thumbPath := filepath.Join(cacheDir, fmt.Sprintf("%s%s", cacheKey, outputExt))

	if thumbInfo, err := os.Stat(thumbPath); err == nil {
		if thumbInfo.ModTime().After(info.ModTime()) || thumbInfo.ModTime().Equal(info.ModTime()) {
			logger.Debugf("Using cached thumbnail for %s", srcPath)
			return thumbPath, nil
		}
	}

	if maxCacheMB > 0 {
		cacheManager := NewCacheManager(cacheDir)
		if _, _, _, err := cacheManager.PruneIfNeeded(maxCacheMB); err != nil {
			logger.Errorf("Failed to prune thumbnail cache: %v", err)
		}
	}

	scaler := draw.ApproxBiLinear
	if err := generateThumbnail(srcPath, thumbPath, maxDim, jpegQuality, scaler); err != nil {
		logger.Errorf("Failed to generate thumbnail for %s: %v", srcPath, err)
		return "", fmt.Errorf("failed to generate thumbnail: %w", err)
	}

	duration := time.Since(start)
	logger.Infof("Thumbnail generated successfully for %s (scaler: %T, took: %v)", srcPath, scaler, duration)
	return thumbPath, nil
}

// generateThumbnail creates a thumbnail using a specific scaler and JPEG quality.
func generateThumbnail(srcPath, thumbPath string, maxDim, jpegQuality int, scaler draw.Scaler) error {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	srcImg, _, err := image.Decode(srcFile)
	if err != nil {
		return fmt.Errorf("failed to decode image: %w", err)
	}

	bounds := srcImg.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid image dimensions: %dx%d", width, height)
	}

	var newWidth, newHeight int
	if width > height {
		newWidth = maxDim
		newHeight = height * maxDim / width
	} else {
		newHeight = maxDim
		newWidth = width * maxDim / height
	}

	if width <= maxDim && height <= maxDim {
		newWidth, newHeight = width, height
	}

	thumbImg := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	scaler.Scale(thumbImg, thumbImg.Bounds(), srcImg, srcImg.Bounds(), draw.Over, nil)

	thumbFile, err := os.Create(thumbPath)
	if err != nil {
		return fmt.Errorf("failed to create thumbnail file: %w", err)
	}
	defer thumbFile.Close()

	if jpegQuality < 1 {
		jpegQuality = 1
	} else if jpegQuality > 100 {
		jpegQuality = 100
	}

	return jpeg.Encode(thumbFile, thumbImg, &jpeg.Options{Quality: jpegQuality})
}

// generateCacheKey implements cache key generation using 4-step algorithm:
// 1. Canonical path via filepath.Abs + EvalSymlinks
// 2. Extract inode/size/ctime (platform-aware via *syscall.Stat_t)
// 3. xxhash of first 64 KiB
// 4. Assemble cacheKey string then SHA-1 hash into final key
func generateCacheKey(imagePath string, maxDim int) (string, error) {
	canonicalPath, err := filepath.Abs(imagePath)
	if err != nil {
		canonicalPath = imagePath // fallback to original path
	} else {
		if resolved, err := filepath.EvalSymlinks(canonicalPath); err == nil {
			canonicalPath = resolved
		}
		// If EvalSymlinks fails, keep the Abs result
	}

	var inode uint64
	var size int64
	var ctime int64

	if stat, err := os.Stat(canonicalPath); err == nil {
		size = stat.Size()

		if sys := stat.Sys(); sys != nil {
			if statT, ok := sys.(*syscall.Stat_t); ok {
				inode = statT.Ino
				ctime = statT.Ctim.Sec
			}
		}
	} else {
		size = 0
	}

	var contentHash uint64
	if file, err := os.Open(canonicalPath); err == nil {
		defer file.Close()

		buffer := make([]byte, 64*1024) // 64 KiB buffer
		n, _ := file.Read(buffer)       // Read up to 64 KiB, ignore EOF error

		hasher := xxhash.New()
		hasher.Write(buffer[:n])
		contentHash = hasher.Sum64()
	}

	keyString := fmt.Sprintf("path:%s|inode:%d|size:%d|ctime:%d|content:%016x|dims:%d",
		canonicalPath, inode, size, ctime, contentHash, maxDim)

	hash := sha1.Sum([]byte(keyString))
	return fmt.Sprintf("%x", hash), nil
}
