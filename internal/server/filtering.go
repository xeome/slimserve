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

	var lastMatch *Pattern

	// Global patterns have the lowest precedence, so check them first.
	globalPatternReader := strings.NewReader(strings.Join(cfg.IgnorePatterns, "\n"))
	globalPatterns, err := Parse(globalPatternReader)
	if err != nil {
		return false, fmt.Errorf("failed to parse global ignore patterns: %w", err)
	}
	for _, p := range globalPatterns {
		if p.Regex.MatchString(relPath) {
			lastMatch = p
		}
	}

	// .slimserveignore files override global ignores.
	// We check from the root down to the file's directory, so the last match found has the highest precedence.
	var pathSegments []string
	if relPath != "." && relPath != "/" {
		pathSegments = strings.Split(filepath.Dir(relPath), string(filepath.Separator))
	}

	currentCheckPath := "."
	for i := -1; i < len(pathSegments); i++ {
		if i > -1 {
			currentCheckPath = filepath.Join(currentCheckPath, pathSegments[i])
		}

		ignoreFilePath := filepath.Join(currentCheckPath, ignoreFileName)
		patterns, err := getOrReadIgnoreFile(root, ignoreFilePath)
		if err != nil {
			logger.Log.Warn().Err(err).Str("path", ignoreFilePath).Msg("Failed to read or parse ignore file")
		}

		for _, p := range patterns {
			pathToCheck := relPath
			// If ignore file is not in root, patterns are relative to it.
			if currentCheckPath != "." {
				pathToCheck, _ = filepath.Rel(currentCheckPath, relPath)
			}

			if p.Regex.MatchString(pathToCheck) {
				lastMatch = p
			}
		}
	}

	if lastMatch != nil {
		return !lastMatch.Negate, nil
	}

	return false, nil
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
