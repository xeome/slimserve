package files

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"mime"
	"os"
	"path/filepath"
	"slimserve/internal/logger"
	"strings"
	"syscall"
	"time"

	"github.com/cespare/xxhash/v2"
	"golang.org/x/image/draw"
)

var (
	// ErrCacheSizeExceeded is deprecated and no longer returned, kept for API compatibility
	ErrCacheSizeExceeded = errors.New("thumbnail cache size limit exceeded")
)

// Generate creates a thumbnail for the given source file path with the specified maximum dimension.
// The thumbnail is cached in the system temp directory under slimserve/thumbcache.
// Cache directory can be overridden via SLIMSERVE_CACHE_DIR environment variable.
//
// Returns the path to the generated thumbnail file, or an error if generation fails.
// Cached filename scheme: <sha1 of filePath + mtime + maxDim>.<ext>
//
// Supported formats: JPEG, PNG, GIF (detected via MIME type)
// Files larger than 10MB are skipped to prevent memory issues.
func Generate(srcPath string, maxDim int) (string, error) {
	return GenerateWithCacheLimit(srcPath, maxDim, 0) // 0 = no limit for backward compatibility
}

// GenerateWithCacheLimit creates a thumbnail with cache size checking.
// If maxCacheMB > 0, checks if creating the thumbnail would exceed the cache size limit.
// Returns ErrCacheSizeExceeded if the cache size limit would be exceeded.
func GenerateWithCacheLimit(srcPath string, maxDim int, maxCacheMB int) (string, error) {
	start := time.Now()
	logger.Debugf("Starting thumbnail generation for %s (max dimension: %d)", srcPath, maxDim)

	// Check file size first - skip if > 10MB
	info, err := os.Stat(srcPath)
	if err != nil {
		logger.Errorf("Failed to stat source file %s: %v", srcPath, err)
		return "", fmt.Errorf("failed to stat source file: %w", err)
	}

	const maxFileSize = 10 * 1024 * 1024 // 10MB
	if info.Size() > maxFileSize {
		logger.Errorf("File too large for thumbnail generation: %s (%d bytes)", srcPath, info.Size())
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

	// Generate sophisticated cache key using 4-step algorithm
	cacheKey, err := generateCacheKey(srcPath, maxDim)
	if err != nil {
		return "", fmt.Errorf("failed to generate cache key: %w", err)
	}

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

	thumbPath := filepath.Join(cacheDir, fmt.Sprintf("%s%s", cacheKey, outputExt))

	// Check if cached thumbnail exists and is newer than source
	if thumbInfo, err := os.Stat(thumbPath); err == nil {
		if thumbInfo.ModTime().After(info.ModTime()) || thumbInfo.ModTime().Equal(info.ModTime()) {
			logger.Debugf("Using cached thumbnail for %s", srcPath)
			return thumbPath, nil
		}
	}

	// Check cache size limit and prune if necessary before creating new thumbnail
	if maxCacheMB > 0 {
		cacheManager := NewCacheManager(cacheDir)
		_, _, _, err := cacheManager.PruneIfNeeded(maxCacheMB)
		if err != nil {
			logger.Errorf("Failed to prune thumbnail cache: %v", err)
			// Continue with generation anyway - pruning failure shouldn't prevent thumbnail generation
		}
	}

	// Generate thumbnail
	if err := generateThumbnail(srcPath, thumbPath, maxDim, outputExt); err != nil {
		logger.Errorf("Failed to generate thumbnail for %s: %v", srcPath, err)
		return "", fmt.Errorf("failed to generate thumbnail: %w", err)
	}

	duration := time.Since(start)
	logger.Infof("Thumbnail generated successfully for %s (took %v)", srcPath, duration)
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

	// Scale using CatmullRom for highest quality
	draw.CatmullRom.Scale(thumbImg, thumbImg.Bounds(), srcImg, srcImg.Bounds(), draw.Over, nil)

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
