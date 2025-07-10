package config

import (
	"encoding/json"
	"flag"
	"os"
	"reflect"
	"strconv"
	"strings"
)

// fieldMapping defines how a config field maps to environment variables and flags
type fieldMapping struct {
	fieldName    string // Name of the field in the Config struct
	envVar       string // Environment variable name
	flagName     string // CLI flag name
	flagDesc     string // Flag description
	fieldType    string // Type: "string", "int", "bool", "stringSlice"
	defaultValue any    // Default value for the flag
}

// configMappings defines all field mappings for the Config struct
var configMappings = []fieldMapping{
	{"Host", "SLIMSERVE_HOST", "host", "Host to bind to", "string", ""},
	{"Port", "SLIMSERVE_PORT", "port", "Port to serve on", "int", 0},
	{"Directories", "SLIMSERVE_DIRS", "dirs", "Comma-separated list of directories to serve", "stringSlice", ""},
	{"DisableDotFiles", "SLIMSERVE_DISABLE_DOTFILES", "disable-dotfiles", "Block access to dot files", "bool", false},
	{"LogLevel", "SLIMSERVE_LOG_LEVEL", "log-level", "Log level (debug, info, warn, error)", "string", ""},
	{"EnableAuth", "SLIMSERVE_ENABLE_AUTH", "enable-auth", "Enable basic authentication", "bool", false},
	{"Username", "SLIMSERVE_USERNAME", "username", "Username for basic auth", "string", ""},
	{"Password", "SLIMSERVE_PASSWORD", "password", "Password for basic auth", "string", ""},
	{"MaxThumbCacheMB", "SLIMSERVE_THUMB_CACHE_MB", "thumb-cache-mb", "Maximum thumbnail cache size in MB", "int", 0},
	{"ThumbJpegQuality", "SLIMSERVE_THUMB_JPEG_QUALITY", "thumb-jpeg-quality", "Thumbnail JPEG quality (1-100)", "int", 0},
	{"ThumbMaxFileSizeMB", "SLIMSERVE_THUMB_MAX_FILE_SIZE_MB", "thumb-max-file-size-mb", "Maximum file size in MB for thumbnail generation", "int", 0},
	{"IgnorePatterns", "SLIMSERVE_IGNORE_PATTERNS", "ignore-patterns", "Comma-separated list of glob patterns to ignore", "stringSlice", ""},
	{"EnableAdmin", "SLIMSERVE_ENABLE_ADMIN", "enable-admin", "Enable admin interface", "bool", false},
	{"AdminUsername", "SLIMSERVE_ADMIN_USERNAME", "admin-username", "Admin username", "string", ""},
	{"AdminPassword", "SLIMSERVE_ADMIN_PASSWORD", "admin-password", "Admin password", "string", ""},
	{"AdminUploadDir", "SLIMSERVE_ADMIN_UPLOAD_DIR", "admin-upload-dir", "Directory for admin uploads", "string", ""},
	{"MaxUploadSizeMB", "SLIMSERVE_MAX_UPLOAD_SIZE_MB", "max-upload-size-mb", "Maximum upload size in MB", "int", 0},
	{"AllowedUploadTypes", "SLIMSERVE_ALLOWED_UPLOAD_TYPES", "allowed-upload-types", "Comma-separated list of allowed upload file types", "stringSlice", ""},
	{"MaxConcurrentUploads", "SLIMSERVE_MAX_CONCURRENT_UPLOADS", "max-concurrent-uploads", "Maximum concurrent uploads", "int", 0},
}

// Load loads configuration from multiple sources with precedence:
// 1. CLI flags (highest)
// 2. Environment variables
// 3. Configuration file
// 4. Default values (lowest)
func Load() (*Config, error) {
	cfg := Default()

	configFile := getConfigFile()
	if configFile != "" {
		if err := loadFromFile(cfg, configFile); err != nil {
			return nil, err
		}
	}

	loadFromEnvGeneric(cfg)
	registerFlags()
	loadFromFlagsGeneric(cfg)

	return cfg, nil
}

// getConfigFile returns the configuration file path from flags or environment
func getConfigFile() string {
	configFlag := flag.Lookup("config")
	if configFlag != nil && configFlag.Value.String() != "" {
		return configFlag.Value.String()
	}

	if envConfig := os.Getenv("SLIMSERVE_CONFIG"); envConfig != "" {
		return envConfig
	}

	if _, err := os.Stat("slimserve.json"); err == nil {
		return "slimserve.json"
	}

	return ""
}

// loadFromFile loads configuration from a JSON file
func loadFromFile(cfg *Config, filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, cfg)
}

// Type conversion utilities

// parseStringSlice parses a comma-separated string into a slice of trimmed strings
func parseStringSlice(value string) []string {
	if value == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}
	return parts
}

// parseInt safely converts a string to int, returning 0 on error
func parseInt(value string) int {
	if val, err := strconv.Atoi(value); err == nil {
		return val
	}
	return 0
}

// loadFromEnvGeneric loads configuration from environment variables using field mappings
func loadFromEnvGeneric(cfg *Config) {
	cfgValue := reflect.ValueOf(cfg).Elem()

	for _, mapping := range configMappings {
		envValue := os.Getenv(mapping.envVar)
		if envValue == "" {
			continue
		}

		field := cfgValue.FieldByName(mapping.fieldName)
		if !field.IsValid() || !field.CanSet() {
			continue
		}

		switch mapping.fieldType {
		case "string":
			field.SetString(envValue)
		case "int":
			if val := parseInt(envValue); val != 0 || envValue == "0" {
				field.SetInt(int64(val))
			}
		case "bool":
			if val, err := strconv.ParseBool(envValue); err == nil {
				field.SetBool(val)
			}
		case "stringSlice":
			slice := parseStringSlice(envValue)
			field.Set(reflect.ValueOf(slice))
		}
	}
}

// registerFlags registers CLI flags using field mappings
func registerFlags() {
	for _, mapping := range configMappings {
		if flag.Lookup(mapping.flagName) != nil {
			continue // Flag already registered
		}

		switch mapping.fieldType {
		case "string":
			defaultVal := ""
			if mapping.defaultValue != nil {
				defaultVal = mapping.defaultValue.(string)
			}
			flag.String(mapping.flagName, defaultVal, mapping.flagDesc)
		case "int":
			defaultVal := 0
			if mapping.defaultValue != nil {
				defaultVal = mapping.defaultValue.(int)
			}
			flag.Int(mapping.flagName, defaultVal, mapping.flagDesc)
		case "bool":
			defaultVal := false
			if mapping.defaultValue != nil {
				defaultVal = mapping.defaultValue.(bool)
			}
			flag.Bool(mapping.flagName, defaultVal, mapping.flagDesc)
		case "stringSlice":
			// String slices are handled as comma-separated strings in flags
			flag.String(mapping.flagName, "", mapping.flagDesc)
		}
	}

	// Register the config flag separately since it's not part of the Config struct
	if flag.Lookup("config") == nil {
		flag.String("config", "", "Path to configuration file")
	}
}

// loadFromFlagsGeneric loads configuration from CLI flags using field mappings
func loadFromFlagsGeneric(cfg *Config) {
	// Parse flags if not already parsed
	if !flag.Parsed() {
		flag.Parse()
	}

	cfgValue := reflect.ValueOf(cfg).Elem()

	// Track which flags were actually set by checking command line args
	setFlags := make(map[string]bool)
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-") {
			flagName := strings.TrimLeft(arg, "-")
			if idx := strings.Index(flagName, "="); idx != -1 {
				flagName = flagName[:idx]
			}
			setFlags[flagName] = true
		}
	}

	for _, mapping := range configMappings {
		flagObj := flag.Lookup(mapping.flagName)
		if flagObj == nil {
			continue
		}

		// Check if this flag was actually set on command line
		if !setFlags[mapping.flagName] {
			continue
		}

		flagValue := flagObj.Value.String()

		field := cfgValue.FieldByName(mapping.fieldName)
		if !field.IsValid() || !field.CanSet() {
			continue
		}

		switch mapping.fieldType {
		case "string":
			field.SetString(flagValue)
		case "int":
			if val := parseInt(flagValue); val != 0 || flagValue == "0" {
				field.SetInt(int64(val))
			}
		case "bool":
			if val, err := strconv.ParseBool(flagValue); err == nil {
				field.SetBool(val)
			}
		case "stringSlice":
			if flagValue != "" {
				slice := parseStringSlice(flagValue)
				// For ignore patterns, merge with existing to avoid duplicates
				if mapping.fieldName == "IgnorePatterns" {
					existing := field.Interface().([]string)
					merged := mergeStringSlices(existing, slice)
					field.Set(reflect.ValueOf(merged))
				} else {
					field.Set(reflect.ValueOf(slice))
				}
			}
		}
	}
}

// mergeStringSlices merges two string slices, avoiding duplicates
func mergeStringSlices(existing, new []string) []string {
	existingMap := make(map[string]struct{})
	for _, item := range existing {
		existingMap[item] = struct{}{}
	}

	result := make([]string, len(existing))
	copy(result, existing)

	for _, item := range new {
		if _, exists := existingMap[item]; !exists {
			result = append(result, item)
		}
	}

	return result
}
