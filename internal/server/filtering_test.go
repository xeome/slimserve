package server

import (
	"os"
	"path/filepath"
	"slimserve/internal/config"
	"slimserve/internal/security"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsIgnored(t *testing.T) {
	// Setup a temporary directory structure
	tmpDir, err := os.MkdirTemp("", "slimserve-filter-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create files and .slimserveignore
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("..."), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file.log"), []byte("..."), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("..."), 0644))
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "node_modules"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "node_modules", "some-lib.js"), []byte("..."), 0644))

	// Root level .slimserveignore
	ignoreContentRoot := `
# Comments should be ignored
*.log
/node_modules
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".slimserveignore"), []byte(ignoreContentRoot), 0644))

	// Nested directory with its own .slimserveignore
	nestedDir := filepath.Join(tmpDir, "nested")
	require.NoError(t, os.Mkdir(nestedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(nestedDir, "secret.dat"), []byte("..."), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(nestedDir, "public.txt"), []byte("..."), 0644))

	ignoreContentNested := `
secret.*
`
	require.NoError(t, os.WriteFile(filepath.Join(nestedDir, ".slimserveignore"), []byte(ignoreContentNested), 0644))

	// --- Negation Test Setup ---
	negationParentDir := filepath.Join(tmpDir, "negation_parent")
	require.NoError(t, os.Mkdir(negationParentDir, 0755))
	negationChildDir := filepath.Join(negationParentDir, "negation_child")
	require.NoError(t, os.Mkdir(negationChildDir, 0755))

	require.NoError(t, os.WriteFile(filepath.Join(negationParentDir, "general.txt"), []byte("..."), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(negationChildDir, "important.txt"), []byte("..."), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(negationChildDir, "ordinary.txt"), []byte("..."), 0644))

	// Parent ignore file: ignore all .txt files
	ignoreContentParentNegation := `
*.txt
`
	require.NoError(t, os.WriteFile(filepath.Join(negationParentDir, ".slimserveignore"), []byte(ignoreContentParentNegation), 0644))

	// Child ignore file: negate for important.txt
	ignoreContentChildNegation := `
!important.txt
`
	require.NoError(t, os.WriteFile(filepath.Join(negationChildDir, ".slimserveignore"), []byte(ignoreContentChildNegation), 0644))

	// --- Test Cases ---
	cfg := &config.Config{
		IgnorePatterns: []string{"*.bak", ".env"},
	}
	root, err := security.NewRootFS(tmpDir)
	require.NoError(t, err)
	defer root.Close()
	// Clear cache before running tests
	ignoreCache = make(map[string]cachedIgnorePatterns)

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"regular file", "file.txt", false},
		{"global pattern from CLI (.env)", ".env", true},
		{".slimserveignore file itself", ".slimserveignore", true},
		{"nested .slimserveignore file", "nested/.slimserveignore", true},
		{"file ignored by root .slimserveignore (*.log)", "file.log", true},
		{"directory ignored by root .slimserveignore", "node_modules", true},
		{"file within ignored directory", "node_modules/some-lib.js", true},
		{"public file in nested dir", "nested/public.txt", false},
		{"secret file in nested dir", "nested/secret.dat", true},
		// Negation tests
		{"parent ignored txt", "negation_parent/general.txt", true},
		{"child negated txt (important)", "negation_parent/negation_child/important.txt", false},
		{"child still ignored txt (ordinary)", "negation_parent/negation_child/ordinary.txt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ignored, err := isIgnored(tt.path, root, cfg)
			require.NoError(t, err)
			require.Equal(t, tt.expected, ignored)
		})
	}
}
