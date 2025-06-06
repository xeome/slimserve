package web

// This file was previously used for responsive template testing,
// but those tests were brittle - they checked for specific CSS classes
// rather than actual functionality.
//
// Template functionality is already covered by:
// - embed_test.go: Tests template parsing and embedding
// - Integration tests in internal/server: Test actual HTTP responses
//
// For visual testing, manual browser testing is more appropriate
// than checking for specific CSS class names.
// TestTailwindCSSContainsRequiredClasses checks that the compiled Tailwind CSS file
// includes key utility classes used in templates (e.g. ".grid"), verifying purge/proper scan.
import (
	"os"
	"strings"
	"testing"
)

func TestTailwindCSSContainsRequiredClasses(t *testing.T) {
	// Try possible locations relative to cwd.
	paths := []string{"web/static/css/tailwind.css", "static/css/tailwind.css"}
	var css []byte
	var err error
	for _, p := range paths {
		css, err = os.ReadFile(p)
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("failed to read tailwind.css (tried %v): %v", paths, err)
	}
	content := string(css)
	if !strings.Contains(content, ".grid") {
		t.Error("expected .grid class not found in compiled Tailwind CSS")
	}
	// Check for dark mode background class from custom theme token (should exist now)
	if !strings.Contains(content, ".dark\\:bg-background") && !strings.Contains(content, ".dark:bg-background") {
		t.Error("expected .dark:bg-background class not found in compiled Tailwind CSS")
	}
}
