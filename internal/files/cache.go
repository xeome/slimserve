package files

import (
	"fmt"
	"os"
	"path/filepath"
	"slimserve/internal/logger"
	"sort"
	"strings"
	"time"
)

// CachedFile represents a cached file with metadata
type CachedFile struct {
	Path    string
	ModTime time.Time
	Size    int64 // size in bytes
}

// IsImageFile checks if a file is a valid image based on its extension
func IsImageFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return true
	default:
		return false
	}
}

// CacheManager handles thumbnail cache operations
type CacheManager struct {
	cacheDir string
}

// NewCacheManager creates a new cache manager instance
func NewCacheManager(cacheDir string) *CacheManager {
	return &CacheManager{
		cacheDir: cacheDir,
	}
}

// GetCacheDir returns the cache directory path
func (cm *CacheManager) GetCacheDir() string {
	return cm.cacheDir
}

// EnsureCacheDir creates the cache directory if it doesn't exist
func (cm *CacheManager) EnsureCacheDir() error {
	return os.MkdirAll(cm.cacheDir, 0755)
}

// SizeMB calculates the total size of image files in the cache directory in MB
func (cm *CacheManager) SizeMB() (int64, error) {
	var totalSize int64

	err := filepath.WalkDir(cm.cacheDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// If we can't access a file, skip it but continue
			return nil
		}

		if d.IsDir() {
			return nil // Skip directories
		}

		// Only count image files (thumbnails)
		if !IsImageFile(d.Name()) {
			return nil // Skip non-image files
		}

		info, err := d.Info()
		if err != nil {
			return nil // Skip files we can't get info for
		}

		totalSize += info.Size()
		return nil
	})

	if err != nil {
		return 0, err
	}

	// Convert bytes to MB
	return totalSize / (1024 * 1024), nil
}

// collectCacheFiles gathers all image files in the cache directory
func (cm *CacheManager) collectCacheFiles() ([]CachedFile, error) {
	var files []CachedFile

	err := filepath.WalkDir(cm.cacheDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip files we can't access but continue walking
		}

		if d.IsDir() {
			return nil // Skip directories
		}

		// Only consider image files for cache operations
		if !IsImageFile(d.Name()) {
			return nil // Skip non-image files
		}

		info, err := d.Info()
		if err != nil {
			return nil // Skip files we can't get info for
		}

		files = append(files, CachedFile{
			Path:    path,
			ModTime: info.ModTime(),
			Size:    info.Size(),
		})

		return nil
	})

	return files, err
}

// Prune removes old image files until the cache size is <= targetMB.
// Only removes image files (thumbnails), leaving other files intact.
// Returns the number of files removed, MB freed, and any error.
func (cm *CacheManager) Prune(targetMB int64) (int, int64, error) {
	files, err := cm.collectCacheFiles()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to collect cache files: %w", err)
	}

	// Calculate total size in bytes
	var totalBytes int64
	for _, file := range files {
		totalBytes += file.Size
	}

	// Convert to MB for comparison
	totalMB := totalBytes / (1024 * 1024)

	// If we're already under the target, no pruning needed
	if totalMB <= targetMB {
		return 0, 0, nil
	}

	// Sort files by modification time (oldest first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime.Before(files[j].ModTime)
	})

	var removed int
	var freedBytes int64

	// Remove files until we're under the target size
	for _, file := range files {
		currentMB := (totalBytes - freedBytes) / (1024 * 1024)
		if currentMB <= targetMB {
			break
		}

		if err := os.Remove(file.Path); err != nil {
			logger.Warnf("Failed to remove cache file during pruning: %s: %v", file.Path, err)
			continue
		}

		removed++
		freedBytes += file.Size
	}

	freedMB := freedBytes / (1024 * 1024)
	return removed, freedMB, nil
}

// PruneIfNeeded checks cache size and prunes if it exceeds maxCacheMB
// Returns true if pruning was performed, along with pruning statistics
func (cm *CacheManager) PruneIfNeeded(maxCacheMB int) (bool, int, int64, error) {
	if maxCacheMB <= 0 {
		return false, 0, 0, nil // No limit set
	}

	currentSize, err := cm.SizeMB()
	if err != nil {
		return false, 0, 0, fmt.Errorf("failed to get cache size: %w", err)
	}

	estimatedThumbSizeMB := int64(1) // Conservative estimate: 1MB per thumbnail
	if currentSize+estimatedThumbSizeMB <= int64(maxCacheMB) {
		return false, 0, 0, nil // No pruning needed
	}

	// Prune to 50% of limit to avoid frequent pruning
	targetSizeMB := int64(maxCacheMB) / 2
	removed, freedMB, err := cm.Prune(targetSizeMB)
	if err != nil {
		return false, 0, 0, fmt.Errorf("failed to prune cache: %w", err)
	}

	if removed > 0 {
		newCacheSizeMB, _ := cm.SizeMB()
		logger.Infof("Cache pruned: event=cache_prune, removed_files=%d, freed_mb=%d, new_cache_size_mb=%d, limit_mb=%d",
			removed, freedMB, newCacheSizeMB, maxCacheMB)
	}

	return true, removed, freedMB, nil
}
