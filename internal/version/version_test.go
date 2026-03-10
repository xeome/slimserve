package version

import (
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	// Test with short commit hash
	info := Info{
		Version:    "v1.0.0",
		CommitHash: "abcdef1234567890",
	}

	result := info.String()
	expected := "v1.0.0 (abcdef1)"
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	// Test with unknown commit hash
	info.CommitHash = "unknown"
	result = info.String()
	expected = "v1.0.0"
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	// Test with short commit hash
	info.CommitHash = "abc"
	result = info.String()
	expected = "v1.0.0"
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestJSON(t *testing.T) {
	info := Info{
		Version:    "v1.0.0",
		CommitHash: "abcdef1234567890",
		BuildDate:  "2023-01-01T00:00:00Z",
		BuildUser:  "test@example.com",
		GoVersion:  "go1.20",
		Platform:   "linux",
		Arch:       "amd64",
	}

	jsonData, err := info.JSON()
	if err != nil {
		t.Errorf("JSON() returned error: %v", err)
	}

	jsonStr := string(jsonData)
	if !strings.Contains(jsonStr, "v1.0.0") {
		t.Error("JSON should contain version")
	}
	if !strings.Contains(jsonStr, "abcdef1234567890") {
		t.Error("JSON should contain commit hash")
	}
}
