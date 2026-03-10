package web

import (
	"html/template"
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
