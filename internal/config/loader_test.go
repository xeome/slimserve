package config

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Test helper functions

// setupTestEnv sets up a clean test environment for config loading
func setupTestEnv(t *testing.T) func() {
	// Save original state
	origArgs := os.Args

	// Reset flags for clean test
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	os.Args = []string{"slimserve"}

	// Clear environment variables
	clearSlimServeEnvVars()

	// Return cleanup function
	return func() {
		os.Args = origArgs
	}
}

// createTempConfigFile creates a temporary JSON config file with the given config
func createTempConfigFile(t *testing.T, cfg Config) string {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test-config.json")

	configData, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	if err := os.WriteFile(configFile, configData, 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	return configFile
}

// setEnvVars sets multiple environment variables and returns cleanup function
func setEnvVars(t *testing.T, envVars map[string]string) func() {
	var cleanupVars []string

	for key, value := range envVars {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("Failed to set env var %s: %v", key, err)
		}
		cleanupVars = append(cleanupVars, key)
	}

	return func() {
		for _, key := range cleanupVars {
			os.Unsetenv(key)
		}
	}
}

// compareConfigs performs detailed field-by-field comparison of two configs
func compareConfigs(t *testing.T, actual, expected Config) {
	t.Helper()

	if actual.Host != expected.Host {
		t.Errorf("Host: expected %q, got %q", expected.Host, actual.Host)
	}
	if actual.Port != expected.Port {
		t.Errorf("Port: expected %d, got %d", expected.Port, actual.Port)
	}
	// Compare directories with nil vs empty slice consideration
	if len(actual.Directories) != len(expected.Directories) {
		t.Errorf("Directories length: expected %d, got %d", len(expected.Directories), len(actual.Directories))
	} else {
		for i, dir := range actual.Directories {
			if i >= len(expected.Directories) || dir != expected.Directories[i] {
				t.Errorf("Directories[%d]: expected %q, got %q", i, expected.Directories[i], dir)
			}
		}
	}
	if actual.DisableDotFiles != expected.DisableDotFiles {
		t.Errorf("DisableDotFiles: expected %t, got %t", expected.DisableDotFiles, actual.DisableDotFiles)
	}
	if actual.LogLevel != expected.LogLevel {
		t.Errorf("LogLevel: expected %q, got %q", expected.LogLevel, actual.LogLevel)
	}
	if actual.EnableAuth != expected.EnableAuth {
		t.Errorf("EnableAuth: expected %t, got %t", expected.EnableAuth, actual.EnableAuth)
	}
	if actual.Username != expected.Username {
		t.Errorf("Username: expected %q, got %q", expected.Username, actual.Username)
	}
	if actual.Password != expected.Password {
		t.Errorf("Password: expected %q, got %q", expected.Password, actual.Password)
	}
	if actual.MaxThumbCacheMB != expected.MaxThumbCacheMB {
		t.Errorf("MaxThumbCacheMB: expected %d, got %d", expected.MaxThumbCacheMB, actual.MaxThumbCacheMB)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	// Check each field individually
	if cfg.Host != "0.0.0.0" {
		t.Errorf("Host: expected %q, got %q", "0.0.0.0", cfg.Host)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port: expected %d, got %d", 8080, cfg.Port)
	}
	if !reflect.DeepEqual(cfg.Directories, []string{"."}) {
		t.Errorf("Directories: expected %v, got %v", []string{"."}, cfg.Directories)
	}
	if cfg.DisableDotFiles != true {
		t.Errorf("DisableDotFiles: expected %t, got %t", true, cfg.DisableDotFiles)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel: expected %q, got %q", "info", cfg.LogLevel)
	}
	if cfg.EnableAuth != false {
		t.Errorf("EnableAuth: expected %t, got %t", false, cfg.EnableAuth)
	}
	if cfg.Username != "" {
		t.Errorf("Username: expected %q, got %q", "", cfg.Username)
	}
	if cfg.Password != "" {
		t.Errorf("Password: expected %q, got %q", "", cfg.Password)
	}
	if cfg.MaxThumbCacheMB != 100 {
		t.Errorf("MaxThumbCacheMB: expected %d, got %d", 100, cfg.MaxThumbCacheMB)
	}
}

func TestLoadConfigJSON(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected Config
	}{
		{
			name: "basic_json_config",
			config: Config{
				Host:            "192.168.1.1",
				Port:            9090,
				Directories:     []string{"/var/www", "/opt/data"},
				DisableDotFiles: false,
				LogLevel:        "debug",
				EnableAuth:      true,
				Username:        "admin",
				Password:        "secret",
			},
			expected: Config{
				Host:            "192.168.1.1",
				Port:            9090,
				Directories:     []string{"/var/www", "/opt/data"},
				DisableDotFiles: false,
				LogLevel:        "debug",
				EnableAuth:      true,
				Username:        "admin",
				Password:        "secret",
			},
		},
		{
			name: "partial_json_config",
			config: Config{
				Host: "127.0.0.1",
				Port: 3000,
				// Don't set other fields - they should come from defaults
			},
			expected: Config{
				Host:            "127.0.0.1",
				Port:            3000,
				Directories:     []string{}, // Empty slice from JSON unmarshaling
				DisableDotFiles: false,      // Zero value from JSON unmarshaling
				LogLevel:        "",         // Empty string from JSON unmarshaling
				EnableAuth:      false,      // Zero value from JSON unmarshaling
				Username:        "",         // Empty string from JSON unmarshaling
				Password:        "",         // Empty string from JSON unmarshaling
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := setupTestEnv(t)
			defer cleanup()

			// Create temporary config file
			configFile := createTempConfigFile(t, tt.config)

			// Set config file via environment variable
			cleanupEnv := setEnvVars(t, map[string]string{
				"SLIMSERVE_CONFIG": configFile,
			})
			defer cleanupEnv()

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() returned error: %v", err)
			}

			compareConfigs(t, *cfg, tt.expected)
		})
	}
}

func TestLoadConfigEnvVars(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected Config
	}{
		{
			name: "all_env_vars",
			envVars: map[string]string{
				"SLIMSERVE_HOST":             "env-host",
				"SLIMSERVE_PORT":             "7777",
				"SLIMSERVE_DIRS":             "/tmp,/home",
				"SLIMSERVE_DISABLE_DOTFILES": "false",
				"SLIMSERVE_LOG_LEVEL":        "warn",
				"SLIMSERVE_ENABLE_AUTH":      "true",
				"SLIMSERVE_USERNAME":         "envuser",
				"SLIMSERVE_PASSWORD":         "envpass",
			},
			expected: Config{
				Host:            "env-host",
				Port:            7777,
				Directories:     []string{"/tmp", "/home"},
				DisableDotFiles: false, // DOT_FILES=true means disable=false
				LogLevel:        "warn",
				EnableAuth:      true,
				Username:        "envuser",
				Password:        "envpass",
				MaxThumbCacheMB: 100, // Default value
			},
		},
		{
			name: "partial_env_vars",
			envVars: map[string]string{
				"SLIMSERVE_HOST": "partial-host",
				"SLIMSERVE_PORT": "5555",
			},
			expected: Config{
				Host:            "partial-host",
				Port:            5555,
				Directories:     []string{"."}, // Default
				DisableDotFiles: true,          // Default
				LogLevel:        "info",        // Default
				EnableAuth:      false,         // Default
				Username:        "",            // Default
				Password:        "",            // Default
				MaxThumbCacheMB: 100,           // Default
			},
		},
		{
			name: "dirs_with_whitespace",
			envVars: map[string]string{
				"SLIMSERVE_DIRS": " /path1 , /path2 , /path3 ",
			},
			expected: Config{
				Host:            "0.0.0.0",                              // Default
				Port:            8080,                                   // Default
				Directories:     []string{"/path1", "/path2", "/path3"}, // Trimmed whitespace
				DisableDotFiles: true,                                   // Default
				LogLevel:        "info",                                 // Default
				EnableAuth:      false,                                  // Default
				Username:        "",                                     // Default
				Password:        "",                                     // Default
				MaxThumbCacheMB: 100,                                    // Default
			},
		},
		{
			name: "invalid_port_ignored",
			envVars: map[string]string{
				"SLIMSERVE_PORT": "invalid-port",
			},
			expected: Config{
				Host:            "0.0.0.0", // Default
				Port:            8080,      // Default (invalid port ignored)
				Directories:     []string{"."},
				DisableDotFiles: true,
				LogLevel:        "info",
				EnableAuth:      false,
				Username:        "",
				Password:        "",
				MaxThumbCacheMB: 100, // Default
			},
		},
		{
			name: "invalid_bool_ignored",
			envVars: map[string]string{
				"SLIMSERVE_DISABLE_DOTFILES": "invalid-bool",
				"SLIMSERVE_ENABLE_AUTH":      "not-a-bool",
			},
			expected: Config{
				Host:            "0.0.0.0", // Default
				Port:            8080,      // Default
				Directories:     []string{"."},
				DisableDotFiles: true, // Default (invalid bool ignored)
				LogLevel:        "info",
				EnableAuth:      false, // Default (invalid bool ignored)
				Username:        "",
				Password:        "",
				MaxThumbCacheMB: 100, // Default
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := setupTestEnv(t)
			defer cleanup()

			// Set test environment variables
			cleanupEnv := setEnvVars(t, tt.envVars)
			defer cleanupEnv()

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() returned error: %v", err)
			}

			// Compare fields individually
			if cfg.Host != tt.expected.Host {
				t.Errorf("Host: expected %q, got %q", tt.expected.Host, cfg.Host)
			}
			if cfg.Port != tt.expected.Port {
				t.Errorf("Port: expected %d, got %d", tt.expected.Port, cfg.Port)
			}
			if !reflect.DeepEqual(cfg.Directories, tt.expected.Directories) {
				t.Errorf("Directories: expected %v, got %v", tt.expected.Directories, cfg.Directories)
			}
			if cfg.DisableDotFiles != tt.expected.DisableDotFiles {
				t.Errorf("DisableDotFiles: expected %t, got %t", tt.expected.DisableDotFiles, cfg.DisableDotFiles)
			}
			if cfg.LogLevel != tt.expected.LogLevel {
				t.Errorf("LogLevel: expected %q, got %q", tt.expected.LogLevel, cfg.LogLevel)
			}
			if cfg.EnableAuth != tt.expected.EnableAuth {
				t.Errorf("EnableAuth: expected %t, got %t", tt.expected.EnableAuth, cfg.EnableAuth)
			}
			if cfg.Username != tt.expected.Username {
				t.Errorf("Username: expected %q, got %q", tt.expected.Username, cfg.Username)
			}
			if cfg.Password != tt.expected.Password {
				t.Errorf("Password: expected %q, got %q", tt.expected.Password, cfg.Password)
			}
			if cfg.MaxThumbCacheMB != tt.expected.MaxThumbCacheMB {
				t.Errorf("MaxThumbCacheMB: expected %d, got %d", tt.expected.MaxThumbCacheMB, cfg.MaxThumbCacheMB)
			}
		})
	}
}

func TestLoadConfigFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected Config
	}{
		{
			name: "all_flags",
			args: []string{
				"slimserve",
				"-host", "flag-host",
				"-port", "6666",
				"-dirs", "/flag1,/flag2",
				"-disable-dotfiles=true",
				"-log-level", "error",
			},
			expected: Config{
				Host:            "flag-host",
				Port:            6666,
				Directories:     []string{"/flag1", "/flag2"},
				DisableDotFiles: true,    // disable-dotfiles flag present means disable=true
				LogLevel:        "error", // Set by -log-level flag
				EnableAuth:      false,   // Default
				Username:        "",      // Default
				Password:        "",      // Default
			},
		},
		{
			name: "partial_flags",
			args: []string{
				"slimserve",
				"-host", "partial-flag-host",
				"-port", "1234",
			},
			expected: Config{
				Host:            "partial-flag-host",
				Port:            1234,
				Directories:     []string{"."}, // Default
				DisableDotFiles: true,          // Default
				LogLevel:        "info",        // Default
				EnableAuth:      false,         // Default
				Username:        "",            // Default
				Password:        "",            // Default
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original state
			origArgs := os.Args
			defer func() { os.Args = origArgs }()

			// Reset flags for clean test
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

			// Clear environment variables
			clearSlimServeEnvVars()

			// Set command line arguments
			os.Args = tt.args

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() returned error: %v", err)
			}

			// Compare individual fields for more precise error reporting
			if cfg.Host != tt.expected.Host {
				t.Errorf("Host: expected %q, got %q", tt.expected.Host, cfg.Host)
			}
			if cfg.Port != tt.expected.Port {
				t.Errorf("Port: expected %d, got %d", tt.expected.Port, cfg.Port)
			}
			// Compare directories with nil vs empty slice consideration
			if len(cfg.Directories) != len(tt.expected.Directories) {
				t.Errorf("Directories length: expected %d, got %d", len(tt.expected.Directories), len(cfg.Directories))
			} else {
				for i, dir := range cfg.Directories {
					if i >= len(tt.expected.Directories) || dir != tt.expected.Directories[i] {
						t.Errorf("Directories[%d]: expected %q, got %q", i, tt.expected.Directories[i], dir)
					}
				}
			}
			if cfg.DisableDotFiles != tt.expected.DisableDotFiles {
				t.Errorf("DisableDotFiles: expected %t, got %t", tt.expected.DisableDotFiles, cfg.DisableDotFiles)
			}
			if cfg.LogLevel != tt.expected.LogLevel {
				t.Errorf("LogLevel: expected %q, got %q", tt.expected.LogLevel, cfg.LogLevel)
			}
			if cfg.EnableAuth != tt.expected.EnableAuth {
				t.Errorf("EnableAuth: expected %t, got %t", tt.expected.EnableAuth, cfg.EnableAuth)
			}
			if cfg.Username != tt.expected.Username {
				t.Errorf("Username: expected %q, got %q", tt.expected.Username, cfg.Username)
			}
			if cfg.Password != tt.expected.Password {
				t.Errorf("Password: expected %q, got %q", tt.expected.Password, cfg.Password)
			}
		})
	}
}

func TestLoadConfigMalformedJSON(t *testing.T) {
	// Save original state
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	// Reset flags for clean test
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	// Clear environment variables
	clearSlimServeEnvVars()

	// Create temporary config file with invalid JSON
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "invalid-config.json")
	invalidJSON := `{"host": "test", "port": "invalid-json"`

	if err := os.WriteFile(configFile, []byte(invalidJSON), 0644); err != nil {
		t.Fatalf("Failed to write invalid config file: %v", err)
	}

	// Set config file via environment variable
	if err := os.Setenv("SLIMSERVE_CONFIG", configFile); err != nil {
		t.Fatalf("Failed to set config env var: %v", err)
	}
	defer os.Unsetenv("SLIMSERVE_CONFIG")

	_, err := Load()
	if err == nil {
		t.Fatal("Expected error for malformed JSON, got nil")
	}

	// Check for various JSON parsing error messages
	errMsg := err.Error()
	if !strings.Contains(errMsg, "invalid character") && !strings.Contains(errMsg, "unexpected end of JSON input") {
		t.Errorf("Expected JSON parsing error, got: %v", err)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	// Save original state
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	// Reset flags for clean test
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	// Clear environment variables
	clearSlimServeEnvVars()

	// Reference non-existent config file via environment variable
	nonExistentFile := "/path/that/does/not/exist/config.json"
	if err := os.Setenv("SLIMSERVE_CONFIG", nonExistentFile); err != nil {
		t.Fatalf("Failed to set config env var: %v", err)
	}
	defer os.Unsetenv("SLIMSERVE_CONFIG")

	_, err := Load()
	if err == nil {
		t.Fatal("Expected error for missing config file, got nil")
	}

	if !strings.Contains(err.Error(), "no such file or directory") && !strings.Contains(err.Error(), "cannot find the file") {
		t.Errorf("Expected file not found error, got: %v", err)
	}
}

func TestLoadConfigPrecedence(t *testing.T) {
	// Test that flags override environment variables which override config file

	// Save original state
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	// Reset flags for clean test
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	// Clear environment variables
	clearSlimServeEnvVars()

	// Create config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "precedence-config.json")
	fileConfig := Config{
		Host:     "file-host",
		Port:     1111,
		LogLevel: "debug",
		Username: "fileuser",
	}

	configData, err := json.Marshal(fileConfig)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	if err := os.WriteFile(configFile, configData, 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Set environment variables (should override file)
	if err := os.Setenv("SLIMSERVE_HOST", "env-host"); err != nil {
		t.Fatalf("Failed to set env var: %v", err)
	}
	defer os.Unsetenv("SLIMSERVE_HOST")

	if err := os.Setenv("SLIMSERVE_PORT", "2222"); err != nil {
		t.Fatalf("Failed to set env var: %v", err)
	}
	defer os.Unsetenv("SLIMSERVE_PORT")

	// Set config file via environment variable
	if err := os.Setenv("SLIMSERVE_CONFIG", configFile); err != nil {
		t.Fatalf("Failed to set config env var: %v", err)
	}
	defer os.Unsetenv("SLIMSERVE_CONFIG")

	// Set flags (should override everything)
	os.Args = []string{
		"slimserve",
		"-host", "flag-host",
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	// Expected: flag values override env vars, env vars override file, file overrides defaults
	expected := Config{
		Host:            "flag-host", // Flag wins
		Port:            2222,        // Env var wins over file
		Directories:     []string{},  // Empty from JSON (not set in file)
		DisableDotFiles: false,       // Zero value from JSON
		LogLevel:        "debug",     // File value (not overridden)
		EnableAuth:      false,       // Zero value from JSON
		Username:        "fileuser",  // File value (not overridden)
		Password:        "",          // Empty from JSON
	}

	// Compare individual fields for more precise error reporting
	if cfg.Host != expected.Host {
		t.Errorf("Host: expected %q, got %q", expected.Host, cfg.Host)
	}
	if cfg.Port != expected.Port {
		t.Errorf("Port: expected %d, got %d", expected.Port, cfg.Port)
	}
	// Compare directories with nil vs empty slice consideration
	if len(cfg.Directories) != len(expected.Directories) {
		t.Errorf("Directories length: expected %d, got %d", len(expected.Directories), len(cfg.Directories))
	} else {
		for i, dir := range cfg.Directories {
			if i >= len(expected.Directories) || dir != expected.Directories[i] {
				t.Errorf("Directories[%d]: expected %q, got %q", i, expected.Directories[i], dir)
			}
		}
	}
	if cfg.DisableDotFiles != expected.DisableDotFiles {
		t.Errorf("DisableDotFiles: expected %t, got %t", expected.DisableDotFiles, cfg.DisableDotFiles)
	}
	if cfg.LogLevel != expected.LogLevel {
		t.Errorf("LogLevel: expected %q, got %q", expected.LogLevel, cfg.LogLevel)
	}
	if cfg.EnableAuth != expected.EnableAuth {
		t.Errorf("EnableAuth: expected %t, got %t", expected.EnableAuth, cfg.EnableAuth)
	}
	if cfg.Username != expected.Username {
		t.Errorf("Username: expected %q, got %q", expected.Username, cfg.Username)
	}
	if cfg.Password != expected.Password {
		t.Errorf("Password: expected %q, got %q", expected.Password, cfg.Password)
	}
}

func TestGetConfigFile(t *testing.T) {
	tests := []struct {
		name         string
		flagValue    string
		envValue     string
		defaultFile  bool
		expectedFile string
	}{
		{
			name:         "flag_takes_precedence",
			flagValue:    "/flag/config.json",
			envValue:     "/env/config.json",
			expectedFile: "/flag/config.json",
		},
		{
			name:         "env_when_no_flag",
			envValue:     "/env/config.json",
			expectedFile: "/env/config.json",
		},
		{
			name:         "default_file_exists",
			defaultFile:  true,
			expectedFile: "slimserve.json",
		},
		{
			name:         "no_config_file",
			expectedFile: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original state
			origArgs := os.Args
			defer func() { os.Args = origArgs }()

			// Reset flags for clean test
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

			// Clear environment variables
			clearSlimServeEnvVars()

			// Set up flag if needed
			if tt.flagValue != "" {
				// Define the config flag first
				flag.String("config", "", "Path to configuration file")
				os.Args = []string{"slimserve", "-config", tt.flagValue}
				flag.Parse()
			} else {
				os.Args = []string{"slimserve"}
			}

			// Set up environment variable if needed
			if tt.envValue != "" {
				if err := os.Setenv("SLIMSERVE_CONFIG", tt.envValue); err != nil {
					t.Fatalf("Failed to set env var: %v", err)
				}
				defer os.Unsetenv("SLIMSERVE_CONFIG")
			}

			// Create default file if needed
			var defaultFilePath string
			if tt.defaultFile {
				tmpDir := t.TempDir()
				origDir, err := os.Getwd()
				if err != nil {
					t.Fatalf("Failed to get working directory: %v", err)
				}
				defer os.Chdir(origDir)

				if err := os.Chdir(tmpDir); err != nil {
					t.Fatalf("Failed to change directory: %v", err)
				}

				defaultFilePath = filepath.Join(tmpDir, "slimserve.json")
				if err := os.WriteFile(defaultFilePath, []byte("{}"), 0644); err != nil {
					t.Fatalf("Failed to create default config file: %v", err)
				}
			}

			result := getConfigFile()
			if tt.defaultFile && result != "" {
				// For default file test, just check the filename
				if filepath.Base(result) != "slimserve.json" {
					t.Errorf("Expected filename 'slimserve.json', got '%s'", filepath.Base(result))
				}
			} else if result != tt.expectedFile {
				t.Errorf("Expected config file '%s', got '%s'", tt.expectedFile, result)
			}
		})
	}
}

// Helper function to clear all SlimServe environment variables
func clearSlimServeEnvVars() {
	envVars := []string{
		"SLIMSERVE_HOST",
		"SLIMSERVE_PORT",
		"SLIMSERVE_DIRS",
		"SLIMSERVE_DISABLE_DOTFILES",
		"SLIMSERVE_LOG_LEVEL",
		"SLIMSERVE_ENABLE_AUTH",
		"SLIMSERVE_USERNAME",
		"SLIMSERVE_PASSWORD",
		"SLIMSERVE_CONFIG",
		"SLIMSERVE_THUMB_CACHE_MB",
	}

	for _, envVar := range envVars {
		os.Unsetenv(envVar)
	}
}
