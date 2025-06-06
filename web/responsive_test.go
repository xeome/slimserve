package web

import (
	"bytes"
	"html/template"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Mock file info for testing
type MockFileInfo struct {
	name    string
	size    int64
	isDir   bool
	modTime time.Time
}

// Methods/properties for template compatibility
func (m MockFileInfo) Type() string {
	if m.isDir {
		return "folder"
	} else {
		return "file"
	}
}
func (m MockFileInfo) Icon() string {
	if m.isDir {
		return "folder"
	} else {
		return "file"
	}
}
func (m MockFileInfo) URL() string      { return "/" + m.name }
func (m MockFileInfo) Name_() string    { return m.name }
func (m MockFileInfo) Size_() string    { return "1 KB" }
func (m MockFileInfo) ModTime_() string { return m.modTime.Format("2006-01-02 15:04") }
func (m MockFileInfo) IsFolder() bool   { return m.isDir }

func (m MockFileInfo) Name() string       { return m.name }
func (m MockFileInfo) Size() int64        { return m.size }
func (m MockFileInfo) IsDir() bool        { return m.isDir }
func (m MockFileInfo) ModTime() time.Time { return m.modTime }

// Template data structure for testing
type TemplateData struct {
	Title        string
	Path         string
	Files        []MockFileInfo
	PathSegments []BreadcrumbItem
}

type BreadcrumbItem struct {
	Name string
	URL  string
}

func TestResponsiveTemplateRendering(t *testing.T) {
	// Parse the template
	tmpl, err := template.ParseFS(TemplateFS, "templates/base.html", "templates/listing.html")
	if err != nil {
		t.Fatalf("Failed to parse templates: %v", err)
	}

	// Test data
	testData := TemplateData{
		Title: "Test Directory",
		Path:  "/test/directory",
		Files: []MockFileInfo{
			{name: "document.txt", size: 1024, isDir: false, modTime: time.Now()},
			{name: "images", size: 0, isDir: true, modTime: time.Now()},
			{name: "config.json", size: 512, isDir: false, modTime: time.Now()},
		},
		PathSegments: []BreadcrumbItem{
			{Name: "Home", URL: "/"},
			{Name: "test", URL: "/test/"},
			{Name: "directory", URL: ""},
		},
	}

	// Render template
	var buf bytes.Buffer
	err = tmpl.ExecuteTemplate(&buf, "base", testData)
	if err != nil {
		t.Fatalf("Failed to execute template: %v", err)
	}

	html := buf.String()

	// Test 1: Verify core Franken UI / UIkit classes are present in template output
	expectedUKClasses := []string{
		"uk-container",
		"uk-list",
		"uk-card",
		"uk-btn",
		"uk-breadcrumb",
	}

	for _, class := range expectedUKClasses {
		if !strings.Contains(html, class) {
			t.Errorf("Expected UIkit/Franken UI class '%s' not found in template output", class)
		}
	}
}

// Test helper to simulate HTTP requests with different viewports
func simulateViewportRequest(t *testing.T, viewport string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/?viewport="+viewport, nil)

	// Add viewport-specific headers
	switch viewport {
	case "mobile":
		req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 14_0 like Mac OS X)")
	case "tablet":
		req.Header.Set("User-Agent", "Mozilla/5.0 (iPad; CPU OS 14_0 like Mac OS X)")
	case "desktop":
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")
	}

	rr := httptest.NewRecorder()
	return rr
}
