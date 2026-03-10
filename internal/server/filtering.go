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

var (
	ignoreCache      = make(map[string]cachedIgnorePatterns)
	ignoreCacheMutex = &sync.RWMutex{}
)

func isIgnored(relPath string, root *security.RootFS, cfg *config.Config) (bool, error) {
	if filepath.Base(relPath) == ignoreFileName {
		return true, nil
	}

	var lastMatch *Pattern

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

func getOrReadIgnoreFile(root *security.RootFS, path string) ([]*Pattern, error) {
	fullPath := filepath.Join(root.Path(), path)

	info, err := root.Stat(path)
	if err != nil {
		return nil, nil
	}
	currentModTime := info.ModTime()

	ignoreCacheMutex.RLock()
	cached, found := ignoreCache[fullPath]
	ignoreCacheMutex.RUnlock()

	if found && cached.modTime.Equal(currentModTime) {
		return cached.patterns, nil
	}

	file, err := root.Open(path)
	if err != nil {
		return nil, nil
	}
	defer file.Close()

	parsedPatterns, err := Parse(file)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ignore file %q: %w", path, err)
	}

	ignoreCacheMutex.Lock()
	ignoreCache[fullPath] = cachedIgnorePatterns{
		patterns: parsedPatterns,
		modTime:  currentModTime,
	}
	ignoreCacheMutex.Unlock()

	return parsedPatterns, nil
}
