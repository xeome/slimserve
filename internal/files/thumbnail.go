package files

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // import for side effects
	"io"
	"os"
	"path/filepath"
	"slimserve/internal/logger"
	"syscall"
	"time"

	"github.com/cespare/xxhash/v2"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // import for side effects
)

var (
	// ErrFileTooLarge is returned when a source image exceeds the size limit for thumbnailing.
	ErrFileTooLarge = errors.New("file too large for thumbnail generation")
)

// Generate creates a thumbnail for the given source file path with the specified maximum dimension.
// It is kept for API compatibility - external code may still call this function.
func Generate(srcPath string, maxDim int) (string, error) {
	return GenerateWithCacheLimit(srcPath, maxDim, 0, 85, 10)
}

// GenerateWithCacheLimit creates a thumbnail with cache size checking and configurable generation options.
// It now supports forcing JPEG output, configurable JPEG quality, and a conditional scaling algorithm.
func GenerateWithCacheLimit(srcPath string, maxDim, maxCacheMB, jpegQuality, maxFileMB int) (string, error) {
	start := time.Now()
	logger.Log.Debug().Msgf("Starting thumbnail generation for %s (max dimension: %d)", srcPath, maxDim)

	info, err := os.Stat(srcPath)
	if err != nil {
		logger.Log.Error().Msgf("Failed to stat source file %s: %v", srcPath, err)
		return "", fmt.Errorf("failed to stat source file: %w", err)
	}

	maxFileSizeBytes := int64(maxFileMB) * 1024 * 1024
	if info.Size() > maxFileSizeBytes {
		logger.Log.Error().Msgf("File too large for thumbnail generation: %s (%d bytes > %d MB)", srcPath, info.Size(), maxFileMB)
		return "", ErrFileTooLarge
	}

	cacheDir := os.Getenv("SLIMSERVE_CACHE_DIR")
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "slimserve", "thumbcache")
	}

	cacheKey, err := generateCacheKey(srcPath, maxDim)
	if err != nil {
		return "", fmt.Errorf("failed to generate cache key: %w", err)
	}

	outputExt := ".jpg"
	thumbPath := filepath.Join(cacheDir, fmt.Sprintf("%s%s", cacheKey, outputExt))

	var cacheManager *CacheManager
	if maxCacheMB > 0 {
		cacheManager, err = NewCacheManager(cacheDir, maxCacheMB)
		if err != nil {
			logger.Log.Warn().Msgf("Failed to create cache manager: %v, proceeding without cache", err)
		} else if cacheManager.Contains(cacheKey) {
			logger.Log.Debug().Msgf("Using cached thumbnail for %s", srcPath)
			return thumbPath, nil
		}
	} else {
		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create cache directory: %w", err)
		}
	}

	scaler := draw.ApproxBiLinear
	if err := generateThumbnail(srcPath, thumbPath, maxDim, jpegQuality, scaler); err != nil {
		logger.Log.Error().Msgf("Failed to generate thumbnail for %s: %v", srcPath, err)
		return "", fmt.Errorf("failed to generate thumbnail: %w", err)
	}

	if cacheManager != nil {
		if thumbInfo, err := os.Stat(thumbPath); err == nil {
			cacheManager.Set(cacheKey, thumbInfo.Size(), ".jpg")
		}
	}

	duration := time.Since(start)
	logger.Log.Info().Msgf("Thumbnail generated successfully for %s (scaler: %T, took: %v)", srcPath, scaler, duration)
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
	}

	var inode uint64
	var size int64
	var ctime int64

	stat, err := os.Stat(canonicalPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat file for cache key: %w", err)
	}
	size = stat.Size()

	if sys := stat.Sys(); sys != nil {
		if statT, ok := sys.(*syscall.Stat_t); ok {
			inode = statT.Ino
			ctime = statT.Ctim.Sec
		}
	}

	var contentHash uint64
	if file, err := os.Open(canonicalPath); err == nil {
		defer file.Close()

		buffer := make([]byte, 64*1024)
		n, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			logger.Log.Debug().Msgf("Failed to read file for content hash: %v", err)
		} else {
			hasher := xxhash.New()
			hasher.Write(buffer[:n])
			contentHash = hasher.Sum64()
		}
	}

	keyString := fmt.Sprintf("path:%s|inode:%d|size:%d|ctime:%d|content:%016x|dims:%d",
		canonicalPath, inode, size, ctime, contentHash, maxDim)

	hash := sha1.Sum([]byte(keyString))
	return fmt.Sprintf("%x", hash), nil
}
