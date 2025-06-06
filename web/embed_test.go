package web

import (
	"html/template"
	"math"
	"strconv"
	"testing"
)

func TestEmbeddedAssets(t *testing.T) {
	// Test that CSS files are embedded
	cssFile, err := TemplateFS.ReadFile("static/css/theme.css")
	if err != nil {
		t.Fatalf("Failed to read embedded theme CSS file: %v", err)
	}

	if len(cssFile) == 0 {
		t.Fatal("CSS file is empty")
	}

	// Test CSS file size (should be under 25KB gzipped, but we'll check raw size is reasonable)
	if len(cssFile) > 50*1024 { // 50KB raw size limit
		t.Errorf("CSS file too large: %d bytes (raw)", len(cssFile))
	}

	// Test that templates can be parsed
	tmpl, err := template.ParseFS(TemplateFS, "templates/*.html")
	if err != nil {
		t.Fatalf("Failed to parse embedded templates: %v", err)
	}

	// Verify specific templates exist
	expectedTemplates := []string{"base.html", "listing.html"}
	templateMap := tmpl.Templates()

	for _, expectedName := range expectedTemplates {
		found := false
		for _, tmpl := range templateMap {
			if tmpl.Name() == expectedName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected template %s not found", expectedName)
		}
	}
}

// TestWCAGColorContrast verifies WCAG AA compliance for color palette
func TestWCAGColorContrast(t *testing.T) {
	// WCAG AA requires contrast ratio of at least 4.5:1 for normal text
	minContrastRatio := 4.5

	testCases := []struct {
		name       string
		foreground string
		background string
	}{
		{"text on background", "#111111", "#ffffff"},
		{"accent on background", "#0066cc", "#ffffff"},
		{"muted on background", "#666666", "#ffffff"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ratio := calculateContrastRatio(tc.foreground, tc.background)
			if ratio < minContrastRatio {
				t.Errorf("Contrast ratio %.2f is below WCAG AA requirement of %.1f for %s", ratio, minContrastRatio, tc.name)
			}
		})
	}
}

// calculateContrastRatio calculates the WCAG contrast ratio between two hex colors
func calculateContrastRatio(color1, color2 string) float64 {
	l1 := relativeLuminance(color1)
	l2 := relativeLuminance(color2)

	// Ensure l1 is the lighter color
	if l1 < l2 {
		l1, l2 = l2, l1
	}

	return (l1 + 0.05) / (l2 + 0.05)
}

// relativeLuminance calculates the relative luminance of a hex color
func relativeLuminance(hexColor string) float64 {
	// Remove # prefix if present
	if hexColor[0] == '#' {
		hexColor = hexColor[1:]
	}

	// Parse RGB values
	r, _ := strconv.ParseInt(hexColor[0:2], 16, 0)
	g, _ := strconv.ParseInt(hexColor[2:4], 16, 0)
	b, _ := strconv.ParseInt(hexColor[4:6], 16, 0)

	// Convert to 0-1 range
	rNorm := float64(r) / 255.0
	gNorm := float64(g) / 255.0
	bNorm := float64(b) / 255.0

	// Apply gamma correction
	rLin := gammaCorrect(rNorm)
	gLin := gammaCorrect(gNorm)
	bLin := gammaCorrect(bNorm)

	// Calculate relative luminance using ITU-R BT.709 coefficients
	return 0.2126*rLin + 0.7152*gLin + 0.0722*bLin
}

// gammaCorrect applies gamma correction for luminance calculation
func gammaCorrect(val float64) float64 {
	if val <= 0.03928 {
		return val / 12.92
	}
	return math.Pow((val+0.055)/1.055, 2.4)
}
