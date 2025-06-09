package config

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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
	// Handle nil vs empty slice for Directories
	if !(len(actual.Directories) == 0 && len(expected.Directories) == 0) {
		if !reflect.DeepEqual(actual.Directories, expected.Directories) {
			t.Errorf("Directories: expected %v, got %v", expected.Directories, actual.Directories)
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
	if actual.ThumbJpegQuality != expected.ThumbJpegQuality {
		t.Errorf("ThumbJpegQuality: expected %d, got %d", expected.ThumbJpegQuality, actual.ThumbJpegQuality)
	}
	if actual.ThumbMaxFileSizeMB != expected.ThumbMaxFileSizeMB {
		t.Errorf("ThumbMaxFileSizeMB: expected %d, got %d", expected.ThumbMaxFileSizeMB, actual.ThumbMaxFileSizeMB)
	}

	// Sort and compare ignore patterns
	sort.Strings(actual.IgnorePatterns)
	sort.Strings(expected.IgnorePatterns)
	// Handle nil vs empty slice for IgnorePatterns
	if !(len(actual.IgnorePatterns) == 0 && len(expected.IgnorePatterns) == 0) {
		if !reflect.DeepEqual(actual.IgnorePatterns, expected.IgnorePatterns) {
			t.Errorf("IgnorePatterns: expected %v, got %v", expected.IgnorePatterns, actual.IgnorePatterns)
		}
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	t.Run("it_loads_default_values_correctly", func(t *testing.T) {
		cleanup := setupTestEnv(t)
		defer cleanup()

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() returned error: %v", err)
		}

		expected := Default()
		compareConfigs(t, *cfg, *expected)
	})
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
				Host:               "127.0.0.1",
				Port:               3000,
				Directories:        []string{}, // Empty slice from JSON unmarshaling
				DisableDotFiles:    false,      // Zero value from JSON unmarshaling
				LogLevel:           "",         // Empty string from JSON unmarshaling
				EnableAuth:         false,      // Zero value from JSON unmarshaling
				Username:           "",         // Empty string from JSON unmarshaling
				Password:           "",         // Empty string from JSON unmarshaling
				MaxThumbCacheMB:    0,          // Zero value from JSON unmarshaling
				ThumbJpegQuality:   0,          // Zero value from JSON unmarshaling
				ThumbMaxFileSizeMB: 0,          // Zero value from JSON unmarshaling
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
				Host:               "env-host",
				Port:               7777,
				Directories:        []string{"/tmp", "/home"},
				DisableDotFiles:    false, // DOT_FILES=true means disable=false
				LogLevel:           "warn",
				EnableAuth:         true,
				Username:           "envuser",
				Password:           "envpass",
				MaxThumbCacheMB:    100, // Default value
				ThumbJpegQuality:   85,  // Default value
				ThumbMaxFileSizeMB: 10,  // Default value
			},
		},
		{
			name: "partial_env_vars",
			envVars: map[string]string{
				"SLIMSERVE_HOST": "partial-host",
				"SLIMSERVE_PORT": "5555",
			},
			expected: Config{
				Host:               "partial-host",
				Port:               5555,
				Directories:        []string{"."}, // Default
				DisableDotFiles:    true,          // Default
				LogLevel:           "info",        // Default
				EnableAuth:         false,         // Default
				Username:           "",            // Default
				Password:           "",            // Default
				MaxThumbCacheMB:    100,           // Default
				ThumbJpegQuality:   85,            // Default
				ThumbMaxFileSizeMB: 10,            // Default
				IgnorePatterns:     []string{},    // Default
			},
		},
		{
			name: "dirs_with_whitespace",
			envVars: map[string]string{
				"SLIMSERVE_DIRS": " /path1 , /path2 , /path3 ",
			},
			expected: Config{
				Host:               "0.0.0.0",                              // Default
				Port:               8080,                                   // Default
				Directories:        []string{"/path1", "/path2", "/path3"}, // Trimmed whitespace
				DisableDotFiles:    true,                                   // Default
				LogLevel:           "info",                                 // Default
				EnableAuth:         false,                                  // Default
				Username:           "",                                     // Default
				Password:           "",                                     // Default
				MaxThumbCacheMB:    100,                                    // Default
				ThumbJpegQuality:   85,                                     // Default
				ThumbMaxFileSizeMB: 10,                                     // Default
				IgnorePatterns:     []string{},                             // Default
			},
		},
		{
			name: "invalid_port_ignored",
			envVars: map[string]string{
				"SLIMSERVE_PORT": "invalid-port",
			},
			expected: Config{
				Host:               "0.0.0.0", // Default
				Port:               8080,      // Default (invalid port ignored)
				Directories:        []string{"."},
				DisableDotFiles:    true,
				LogLevel:           "info",
				EnableAuth:         false,
				Username:           "",
				Password:           "",
				MaxThumbCacheMB:    100,        // Default
				ThumbJpegQuality:   85,         // Default
				ThumbMaxFileSizeMB: 10,         // Default
				IgnorePatterns:     []string{}, // Default
			},
		},
		{
			name: "invalid_bool_ignored",
			envVars: map[string]string{
				"SLIMSERVE_DISABLE_DOTFILES": "invalid-bool",
				"SLIMSERVE_ENABLE_AUTH":      "not-a-bool",
			},
			expected: Config{
				Host:               "0.0.0.0", // Default
				Port:               8080,      // Default
				Directories:        []string{"."},
				DisableDotFiles:    true, // Default (invalid bool ignored)
				LogLevel:           "info",
				EnableAuth:         false, // Default (invalid bool ignored)
				Username:           "",
				Password:           "",
				MaxThumbCacheMB:    100,        // Default
				ThumbJpegQuality:   85,         // Default
				ThumbMaxFileSizeMB: 10,         // Default
				IgnorePatterns:     []string{}, // Default
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

			compareConfigs(t, *cfg, tt.expected)
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
				Host:               "flag-host",
				Port:               6666,
				Directories:        []string{"/flag1", "/flag2"},
				DisableDotFiles:    true,       // disable-dotfiles flag present means disable=true
				LogLevel:           "error",    // Set by -log-level flag
				EnableAuth:         false,      // Default
				Username:           "",         // Default
				Password:           "",         // Default
				MaxThumbCacheMB:    100,        // Default
				ThumbJpegQuality:   85,         // Default
				ThumbMaxFileSizeMB: 10,         // Default
				IgnorePatterns:     []string{}, // Default
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
				Host:               "partial-flag-host",
				Port:               1234,
				Directories:        []string{"."}, // Default
				DisableDotFiles:    true,          // Default
				LogLevel:           "info",        // Default
				EnableAuth:         false,         // Default
				Username:           "",            // Default
				Password:           "",            // Default
				MaxThumbCacheMB:    100,           // Default
				ThumbJpegQuality:   85,            // Default
				ThumbMaxFileSizeMB: 10,            // Default
				IgnorePatterns:     []string{},    // Default
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := setupTestEnv(t)
			defer cleanup()

			// Set command line arguments
			os.Args = tt.args

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() returned error: %v", err)
			}

			compareConfigs(t, *cfg, tt.expected)
		})
	}
}

func TestLoadConfigMalformedJSON(t *testing.T) {
	t.Run("it_returns_error_for_malformed_json", func(t *testing.T) {
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
	})
}

func TestLoadConfigMissingFile(t *testing.T) {
	t.Run("it_returns_error_for_missing_config_file", func(t *testing.T) {
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
	})
}

func TestLoadConfigPrecedence(t *testing.T) {
	t.Run("flags_override_env_vars_which_override_config_file", func(t *testing.T) {
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
			Host:               "flag-host", // Flag wins
			Port:               2222,        // Env var wins over file
			Directories:        []string{},  // Empty from JSON (not set in file)
			DisableDotFiles:    false,       // Zero value from JSON
			LogLevel:           "debug",     // File value (not overridden)
			EnableAuth:         false,       // Zero value from JSON
			Username:           "fileuser",  // File value (not overridden)
			Password:           "",          // Empty from JSON
			MaxThumbCacheMB:    0,           // Zero value from JSON
			ThumbJpegQuality:   0,           // Zero value from JSON
			ThumbMaxFileSizeMB: 0,           // Zero value from JSON
		}

		compareConfigs(t, *cfg, expected)
	})
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

func TestLoadConfigIgnorePatternsMerging(t *testing.T) {
	t.Run("it_merges_ignore_patterns_from_flags_and_env", func(t *testing.T) {
		cleanup := setupTestEnv(t)
		defer cleanup()

		// 1. Config file
		fileConfig := Config{
			IgnorePatterns: []string{"file.pattern", "common.pattern"},
		}
		configFile := createTempConfigFile(t, fileConfig)

		// 2. Env vars (should overwrite file config)
		envVars := map[string]string{
			"SLIMSERVE_CONFIG":          configFile,
			"SLIMSERVE_IGNORE_PATTERNS": "env.pattern,common.pattern",
		}
		cleanupEnv := setEnvVars(t, envVars)
		defer cleanupEnv()

		// 3. Flags (should merge with env config)
		os.Args = []string{
			"slimserve",
			"-ignore-patterns", "flag.pattern,env.pattern",
		}

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() returned an unexpected error: %v", err)
		}

		// Env overwrites file, flag merges with env.
		expected := []string{"env.pattern", "common.pattern", "flag.pattern"}
		actual := cfg.IgnorePatterns

		sort.Strings(expected)
		sort.Strings(actual)

		if !reflect.DeepEqual(expected, actual) {
			t.Errorf("Expected IgnorePatterns to be %v, but got %v", expected, actual)
		}
	})
}

func TestLoadConfigBooleanFlagPrecedence(t *testing.T) {
	t.Run("it_correctly_applies_precedence_for_boolean_flags", func(t *testing.T) {
		cleanup := setupTestEnv(t)
		defer cleanup()

		// 1. Config file: sets DisableDotFiles to false
		fileConfig := Config{
			DisableDotFiles: false,
			EnableAuth:      true,
		}
		configFile := createTempConfigFile(t, fileConfig)

		// 2. Env vars: sets DisableDotFiles to true, EnableAuth to false
		envVars := map[string]string{
			"SLIMSERVE_CONFIG":           configFile,
			"SLIMSERVE_DISABLE_DOTFILES": "true",
			"SLIMSERVE_ENABLE_AUTH":      "false",
		}
		cleanupEnv := setEnvVars(t, envVars)
		defer cleanupEnv()

		// 3. Flags: sets DisableDotFiles to false, EnableAuth to true
		os.Args = []string{
			"slimserve",
			"-disable-dotfiles=false",
			"-enable-auth=true",
		}

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() returned an unexpected error: %v", err)
		}

		if cfg.DisableDotFiles != false {
			t.Errorf("Expected DisableDotFiles to be false (from flag), but got %t", cfg.DisableDotFiles)
		}

		if cfg.EnableAuth != true {
			t.Errorf("Expected EnableAuth to be true (from flag), but got %t", cfg.EnableAuth)
		}
	})
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
		"SLIMSERVE_THUMB_JPEG_QUALITY",
		"SLIMSERVE_IGNORE_PATTERNS",
		"SLIMSERVE_THUMB_MAX_FILE_SIZE_MB",
	}

	for _, envVar := range envVars {
		os.Unsetenv(envVar)
	}
}
