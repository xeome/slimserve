package server

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"slimserve/internal/config"
	"slimserve/internal/logger"
	"slimserve/internal/security"
)

const ignoreFileName = ".slimserveignore"

type cachedIgnorePatterns struct {
	patterns []*Pattern
	modTime  time.Time
}

// ignoreCache holds cached, parsed patterns from .slimserveignore files.
var (
	ignoreCache      = make(map[string]cachedIgnorePatterns)
	ignoreCacheMutex = &sync.RWMutex{}
)

// isIgnored checks if a file path should be ignored based on global patterns
// and .slimserveignore files.
func isIgnored(relPath string, root *security.RootFS, cfg *config.Config) (bool, error) {
	if filepath.Base(relPath) == ignoreFileName {
		return true, nil
	}

	var allPatterns []*Pattern

	globalPatternReader := strings.NewReader(strings.Join(cfg.IgnorePatterns, "\n"))
	globalPatterns, err := Parse(globalPatternReader)
	if err != nil {
		return false, fmt.Errorf("failed to parse global ignore patterns: %w", err)
	}
	allPatterns = append(allPatterns, globalPatterns...)

	currentDir := filepath.Dir(relPath)
	for {
		ignoreFilePath := filepath.Join(currentDir, ignoreFileName)
		patterns, err := getOrReadIgnoreFile(root, ignoreFilePath)
		if err != nil {
			logger.Log.Warn().Err(err).Str("path", ignoreFilePath).Msg("Failed to read or parse ignore file")
		}
		allPatterns = append(allPatterns, patterns...)

		if currentDir == "." || currentDir == "/" || currentDir == "" {
			break
		}
		currentDir = filepath.Dir(currentDir)
	}

	ignored := false
	for _, p := range allPatterns {
		if p.Regex.MatchString(relPath) {
			ignored = !p.Negate
		}
	}

	return ignored, nil
}

// getOrReadIgnoreFile retrieves parsed patterns from cache or reads/parses from RootFS.
func getOrReadIgnoreFile(root *security.RootFS, path string) ([]*Pattern, error) {
	fullPath := filepath.Join(root.Path(), path)

	// Stat the file first to get its current modification time.
	info, err := root.Stat(path)
	if err != nil {
		// File doesn't exist, so there are no patterns.
		return nil, nil
	}
	currentModTime := info.ModTime()

	ignoreCacheMutex.RLock()
	cached, found := ignoreCache[fullPath]
	ignoreCacheMutex.RUnlock()

	// If found in cache and the modification time is the same, return the cached patterns.
	if found && cached.modTime.Equal(currentModTime) {
		return cached.patterns, nil
	}

	// Otherwise, either it's not in the cache or it's stale. Read the file.
	file, err := root.Open(path)
	if err != nil {
		return nil, nil // Should have been caught by Stat, but check again.
	}
	defer file.Close()

	parsedPatterns, err := Parse(file)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ignore file %q: %w", path, err)
	}

	// Store the newly parsed patterns and the current mod time in the cache.
	ignoreCacheMutex.Lock()
	ignoreCache[fullPath] = cachedIgnorePatterns{
		patterns: parsedPatterns,
		modTime:  currentModTime,
	}
	ignoreCacheMutex.Unlock()

	return parsedPatterns, nil
}
