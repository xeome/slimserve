package version

import (
	"strings"
	"testing"
	"time"
)

func TestGet(t *testing.T) {
	info := Get()
	
	// Test that all fields are populated
	if info.Version == "" {
		t.Error("Version should not be empty")
	}
	if info.CommitHash == "" {
		t.Error("CommitHash should not be empty")
	}
	if info.BuildDate == "" {
		t.Error("BuildDate should not be empty")
	}
	if info.BuildUser == "" {
		t.Error("BuildUser should not be empty")
	}
	if info.GoVersion == "" {
		t.Error("GoVersion should not be empty")
	}
	if info.Platform == "" {
		t.Error("Platform should not be empty")
	}
	if info.Arch == "" {
		t.Error("Arch should not be empty")
	}
}

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

func TestGetShort(t *testing.T) {
	result := GetShort()
	if result == "" {
		t.Error("GetShort should not return empty string")
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

func TestGetBuildTime(t *testing.T) {
	// Test with valid RFC3339 date
	originalBuildDate := BuildDate
	defer func() { BuildDate = originalBuildDate }()
	
	BuildDate = "2023-01-01T12:00:00Z"
	buildTime, err := GetBuildTime()
	if err != nil {
		t.Errorf("GetBuildTime() returned error for valid date: %v", err)
	}
	
	expected := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	if !buildTime.Equal(expected) {
		t.Errorf("Expected %v, got %v", expected, buildTime)
	}
	
	// Test with unknown build date
	BuildDate = "unknown"
	_, err = GetBuildTime()
	if err == nil {
		t.Error("GetBuildTime() should return error for unknown date")
	}
	
	// Test with invalid date format
	BuildDate = "invalid-date"
	_, err = GetBuildTime()
	if err == nil {
		t.Error("GetBuildTime() should return error for invalid date")
	}
}
