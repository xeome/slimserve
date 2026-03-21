package files

import (
	"fmt"
	"os"
	"path/filepath"
	"slimserve/internal/logger"
	"slimserve/internal/storage"
	"strings"
)

type CacheManager struct {
	cacheDir string
	thumb    *storage.ThumbCache
}

func NewCacheManager(cacheDir string, maxCacheMB int) (*CacheManager, error) {
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	maxBytes := int64(maxCacheMB) * 1024 * 1024
	thumb, err := storage.NewThumbCache(cacheDir, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to create thumbnail cache: %w", err)
	}

	return &CacheManager{
		cacheDir: cacheDir,
		thumb:    thumb,
	}, nil
}

func (cm *CacheManager) GetCacheDir() string {
	return cm.cacheDir
}

func (cm *CacheManager) EnsureCacheDir() error {
	return os.MkdirAll(cm.cacheDir, 0755)
}

func (cm *CacheManager) SizeMB() int64 {
	return cm.thumb.SizeMB()
}

func (cm *CacheManager) Contains(key string) bool {
	return cm.thumb.Contains(key)
}

func (cm *CacheManager) Get(key string) bool {
	return cm.thumb.Get(key)
}

func (cm *CacheManager) Set(key string, size int64, ext string) {
	cm.thumb.Set(key, size, ext)
}

func (cm *CacheManager) Delete(key string) bool {
	return cm.thumb.Delete(key)
}

func (cm *CacheManager) Stats() (int, int64, int64) {
	return cm.thumb.Stats()
}

func IsImageFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return true
	default:
		return false
	}
}

func (cm *CacheManager) PruneIfNeeded(maxCacheMB int) (bool, int, int64, error) {
	if maxCacheMB <= 0 {
		return false, 0, 0, nil
	}

	currentSize := cm.SizeMB()
	estimatedThumbSizeMB := int64(1)
	if currentSize+estimatedThumbSizeMB <= int64(maxCacheMB) {
		return false, 0, 0, nil
	}

	_, usedBytes, maxBytes := cm.Stats()
	if usedBytes <= maxBytes {
		return false, 0, 0, nil
	}

	newSizeMB := cm.SizeMB()
	if newSizeMB > 0 {
		logger.Log.Info().Msgf("Cache pruned: event=cache_prune, cache_size_mb=%d, limit_mb=%d",
			newSizeMB, maxCacheMB)
	}

	return true, 0, 0, nil
}
