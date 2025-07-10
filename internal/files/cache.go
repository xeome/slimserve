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

type CachedFile struct {
	Path    string
	ModTime time.Time
	Size    int64 // size in bytes
}

var imageExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".webp": true,
}

func IsImageFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return imageExtensions[ext]
}

type CacheManager struct {
	cacheDir string
}

func NewCacheManager(cacheDir string) *CacheManager {
	return &CacheManager{
		cacheDir: cacheDir,
	}
}

func (cm *CacheManager) GetCacheDir() string {
	return cm.cacheDir
}

func (cm *CacheManager) EnsureCacheDir() error {
	return os.MkdirAll(cm.cacheDir, 0755)
}

// SizeMB calculates the total size of image files in the cache directory in MB
func (cm *CacheManager) SizeMB() (int64, error) {
	var totalSize int64

	err := filepath.WalkDir(cm.cacheDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			return nil
		}

		// Only count image files (thumbnails)
		if !IsImageFile(d.Name()) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		totalSize += info.Size()
		return nil
	})

	if err != nil {
		return 0, err
	}

	return totalSize / (1024 * 1024), nil
}

// collectCacheFiles gathers all image files in the cache directory
func (cm *CacheManager) collectCacheFiles() ([]CachedFile, error) {
	// Start with a smaller initial capacity and let it grow naturally
	// This balances memory usage for small caches vs allocation efficiency for large ones
	files := make([]CachedFile, 0, 64)

	err := filepath.WalkDir(cm.cacheDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			return nil
		}

		if !IsImageFile(d.Name()) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
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
	targetBytes := targetMB * 1024 * 1024

	var files []CachedFile
	var totalBytes int64

	err := filepath.WalkDir(cm.cacheDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !IsImageFile(d.Name()) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		fileSize := info.Size()
		totalBytes += fileSize

		files = append(files, CachedFile{
			Path:    path,
			ModTime: info.ModTime(),
			Size:    fileSize,
		})
		return nil
	})

	if err != nil {
		return 0, 0, fmt.Errorf("failed to walk cache directory: %w", err)
	}

	// If we're already under the target, no pruning needed
	if totalBytes <= targetBytes {
		return 0, 0, nil
	}

	// Sort by modification time (oldest first) for efficient pruning
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime.Before(files[j].ModTime)
	})

	needToFreeBytes := totalBytes - targetBytes
	var removed int
	var freedBytes int64

	// Remove oldest files until we reach the target
	for i := range files {
		if freedBytes >= needToFreeBytes {
			break
		}

		if err := os.Remove(files[i].Path); err != nil {
			logger.Warnf("Failed to remove cache file during pruning: %s: %v", files[i].Path, err)
			continue
		}

		removed++
		freedBytes += files[i].Size
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
