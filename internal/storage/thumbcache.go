package storage

import (
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"

	"github.com/hashicorp/golang-lru/v2"
)

type ThumbCache struct {
	lru       *lru.Cache[string, thumbValue]
	maxBytes  int64
	currBytes int64
	cacheDir  string
}

type thumbValue struct {
	Size int64
	Ext  string
}

func NewThumbCache(cacheDir string, maxBytes int64) (*ThumbCache, error) {
	maxCount := maxBytes / (128 * 1024)
	if maxCount < 10 {
		maxCount = 10
	}
	c, _ := lru.New[string, thumbValue](int(maxCount))

	tc := &ThumbCache{
		lru:       c,
		maxBytes:  maxBytes,
		currBytes: 0,
		cacheDir:  cacheDir,
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}

	if err := tc.rebuild(); err != nil {
		return nil, err
	}

	return tc, nil
}

func (tc *ThumbCache) rebuild() error {
	entries, err := tc.collectEntries()
	if err != nil {
		return err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ModTime < entries[j].ModTime
	})

	for _, entry := range entries {
		tc.lru.Add(entry.Key, thumbValue{Size: entry.Size, Ext: entry.Ext})
		atomic.AddInt64(&tc.currBytes, entry.Size)
	}

	return nil
}

type thumbEntry struct {
	Key     string
	Size    int64
	Ext     string
	ModTime int64
}

func (tc *ThumbCache) collectEntries() ([]thumbEntry, error) {
	var entries []thumbEntry

	err := filepath.WalkDir(tc.cacheDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !isImageFile(d.Name()) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		name := d.Name()
		ext := filepath.Ext(name)
		key := name[:len(name)-len(ext)]

		entries = append(entries, thumbEntry{
			Key:     key,
			Size:    info.Size(),
			Ext:     ext,
			ModTime: info.ModTime().Unix(),
		})
		return nil
	})

	return entries, err
}

func isImageFile(filename string) bool {
	ext := filepath.Ext(filename)
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return true
	default:
		return false
	}
}

func (tc *ThumbCache) Get(key string) bool {
	_, ok := tc.lru.Get(key)
	return ok
}

func (tc *ThumbCache) Contains(key string) bool {
	return tc.lru.Contains(key)
}

func (tc *ThumbCache) Set(key string, size int64, ext string) {
	if size > tc.maxBytes/2 {
		return
	}

	for atomic.LoadInt64(&tc.currBytes)+size > tc.maxBytes {
		evictedKey, evictedVal, ok := tc.lru.RemoveOldest()
		if !ok {
			atomic.StoreInt64(&tc.currBytes, 0)
			break
		}
		atomic.AddInt64(&tc.currBytes, -evictedVal.Size)
		os.Remove(filepath.Join(tc.cacheDir, evictedKey+evictedVal.Ext))
	}

	tc.lru.Add(key, thumbValue{Size: size, Ext: ext})
	atomic.AddInt64(&tc.currBytes, size)
}

func (tc *ThumbCache) Delete(key string) bool {
	val, ok := tc.lru.Peek(key)
	if ok {
		atomic.AddInt64(&tc.currBytes, -val.Size)
		tc.lru.Remove(key)
		os.Remove(filepath.Join(tc.cacheDir, key+val.Ext))
		return true
	}
	return false
}

func (tc *ThumbCache) SizeMB() int64 {
	return atomic.LoadInt64(&tc.currBytes) / (1024 * 1024)
}

func (tc *ThumbCache) Stats() (int, int64, int64) {
	return tc.lru.Len(), atomic.LoadInt64(&tc.currBytes), tc.maxBytes
}
