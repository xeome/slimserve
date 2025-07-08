package config

import (
	"encoding/json"
	"flag"
	"os"
	"strconv"
	"strings"
)

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

	loadFromEnv(cfg)
	loadFromFlags(cfg)

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

// loadFromEnv loads configuration from environment variables
func loadFromEnv(cfg *Config) {
	if host := os.Getenv("SLIMSERVE_HOST"); host != "" {
		cfg.Host = host
	}

	if port := os.Getenv("SLIMSERVE_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.Port = p
		}
	}

	if dirs := os.Getenv("SLIMSERVE_DIRS"); dirs != "" {
		cfg.Directories = strings.Split(dirs, ",")
		for i, dir := range cfg.Directories {
			cfg.Directories[i] = strings.TrimSpace(dir)
		}
	}

	if dotFiles := os.Getenv("SLIMSERVE_DISABLE_DOTFILES"); dotFiles != "" {
		if val, err := strconv.ParseBool(dotFiles); err == nil {
			cfg.DisableDotFiles = val
		}
	}

	if logLevel := os.Getenv("SLIMSERVE_LOG_LEVEL"); logLevel != "" {
		cfg.LogLevel = logLevel
	}

	if enableAuth := os.Getenv("SLIMSERVE_ENABLE_AUTH"); enableAuth != "" {
		if val, err := strconv.ParseBool(enableAuth); err == nil {
			cfg.EnableAuth = val
		}
	}

	if username := os.Getenv("SLIMSERVE_USERNAME"); username != "" {
		cfg.Username = username
	}

	if password := os.Getenv("SLIMSERVE_PASSWORD"); password != "" {
		cfg.Password = password
	}

	if thumbCache := os.Getenv("SLIMSERVE_THUMB_CACHE_MB"); thumbCache != "" {
		if val, err := strconv.Atoi(thumbCache); err == nil {
			cfg.MaxThumbCacheMB = val
		}
	}

	if jpegQuality := os.Getenv("SLIMSERVE_THUMB_JPEG_QUALITY"); jpegQuality != "" {
		if val, err := strconv.Atoi(jpegQuality); err == nil {
			cfg.ThumbJpegQuality = val
		}
	}

	if ignorePatterns := os.Getenv("SLIMSERVE_IGNORE_PATTERNS"); ignorePatterns != "" {
		cfg.IgnorePatterns = strings.Split(ignorePatterns, ",")
		for i, p := range cfg.IgnorePatterns {
			cfg.IgnorePatterns[i] = strings.TrimSpace(p)
		}
	}

	if thumbMaxSize := os.Getenv("SLIMSERVE_THUMB_MAX_FILE_SIZE_MB"); thumbMaxSize != "" {
		if val, err := strconv.Atoi(thumbMaxSize); err == nil {
			cfg.ThumbMaxFileSizeMB = val
		}
	}

	// Admin configuration from environment
	if enableAdmin := os.Getenv("SLIMSERVE_ENABLE_ADMIN"); enableAdmin != "" {
		if val, err := strconv.ParseBool(enableAdmin); err == nil {
			cfg.EnableAdmin = val
		}
	}

	if adminUsername := os.Getenv("SLIMSERVE_ADMIN_USERNAME"); adminUsername != "" {
		cfg.AdminUsername = adminUsername
	}

	if adminPassword := os.Getenv("SLIMSERVE_ADMIN_PASSWORD"); adminPassword != "" {
		cfg.AdminPassword = adminPassword
	}

	if adminUploadDir := os.Getenv("SLIMSERVE_ADMIN_UPLOAD_DIR"); adminUploadDir != "" {
		cfg.AdminUploadDir = adminUploadDir
	}

	if maxUploadSize := os.Getenv("SLIMSERVE_MAX_UPLOAD_SIZE_MB"); maxUploadSize != "" {
		if val, err := strconv.Atoi(maxUploadSize); err == nil {
			cfg.MaxUploadSizeMB = val
		}
	}

	if allowedTypes := os.Getenv("SLIMSERVE_ALLOWED_UPLOAD_TYPES"); allowedTypes != "" {
		cfg.AllowedUploadTypes = strings.Split(allowedTypes, ",")
		for i, typ := range cfg.AllowedUploadTypes {
			cfg.AllowedUploadTypes[i] = strings.TrimSpace(typ)
		}
	}

	if maxConcurrent := os.Getenv("SLIMSERVE_MAX_CONCURRENT_UPLOADS"); maxConcurrent != "" {
		if val, err := strconv.Atoi(maxConcurrent); err == nil {
			cfg.MaxConcurrentUploads = val
		}
	}
}

// loadFromFlags loads configuration from CLI flags
func loadFromFlags(cfg *Config) {
	// Define flags if not already defined
	if flag.Lookup("host") == nil {
		flag.String("host", cfg.Host, "Host to bind to")
	}
	if flag.Lookup("port") == nil {
		flag.Int("port", cfg.Port, "Port to serve on")
	}
	if flag.Lookup("dirs") == nil {
		flag.String("dirs", "", "Comma-separated list of directories to serve")
	}
	if flag.Lookup("disable-dotfiles") == nil {
		flag.Bool("disable-dotfiles", cfg.DisableDotFiles, "Block access to dot files")
	}
	if flag.Lookup("log-level") == nil {
		flag.String("log-level", cfg.LogLevel, "Log level (debug, info, warn, error)")
	}
	if flag.Lookup("enable-auth") == nil {
		flag.Bool("enable-auth", cfg.EnableAuth, "Enable basic authentication")
	}
	if flag.Lookup("username") == nil {
		flag.String("username", cfg.Username, "Username for basic auth")
	}
	if flag.Lookup("password") == nil {
		flag.String("password", cfg.Password, "Password for basic auth")
	}
	if flag.Lookup("config") == nil {
		flag.String("config", "", "Path to configuration file")
	}
	if flag.Lookup("thumb-cache-mb") == nil {
		flag.Int("thumb-cache-mb", cfg.MaxThumbCacheMB, "Maximum thumbnail cache size in MB")
	}
	if flag.Lookup("thumb-jpeg-quality") == nil {
		flag.Int("thumb-jpeg-quality", cfg.ThumbJpegQuality, "Thumbnail JPEG quality (1-100)")
	}
	if flag.Lookup("ignore-patterns") == nil {
		flag.String("ignore-patterns", "", "Comma-separated list of glob patterns to ignore")
	}
	if flag.Lookup("thumb-max-file-size-mb") == nil {
		flag.Int("thumb-max-file-size-mb", cfg.ThumbMaxFileSizeMB, "Maximum file size in MB for thumbnail generation")
	}

	// Admin flags
	if flag.Lookup("enable-admin") == nil {
		flag.Bool("enable-admin", cfg.EnableAdmin, "Enable admin interface")
	}
	if flag.Lookup("admin-username") == nil {
		flag.String("admin-username", cfg.AdminUsername, "Admin username")
	}
	if flag.Lookup("admin-password") == nil {
		flag.String("admin-password", cfg.AdminPassword, "Admin password")
	}
	if flag.Lookup("admin-upload-dir") == nil {
		flag.String("admin-upload-dir", cfg.AdminUploadDir, "Directory for admin uploads")
	}
	if flag.Lookup("max-upload-size-mb") == nil {
		flag.Int("max-upload-size-mb", cfg.MaxUploadSizeMB, "Maximum upload size in MB")
	}
	if flag.Lookup("allowed-upload-types") == nil {
		flag.String("allowed-upload-types", "", "Comma-separated list of allowed upload file types")
	}
	if flag.Lookup("max-concurrent-uploads") == nil {
		flag.Int("max-concurrent-uploads", cfg.MaxConcurrentUploads, "Maximum concurrent uploads")
	}

	// Parse flags if not already parsed
	if !flag.Parsed() {
		flag.Parse()
	}

	// Apply flag values
	if hostFlag := flag.Lookup("host"); hostFlag != nil && hostFlag.Value.String() != hostFlag.DefValue {
		cfg.Host = hostFlag.Value.String()
	}

	if portFlag := flag.Lookup("port"); portFlag != nil && portFlag.Value.String() != portFlag.DefValue {
		if p, err := strconv.Atoi(portFlag.Value.String()); err == nil {
			cfg.Port = p
		}
	}

	if dirsFlag := flag.Lookup("dirs"); dirsFlag != nil && dirsFlag.Value.String() != "" {
		dirs := strings.Split(dirsFlag.Value.String(), ",")
		for i, dir := range dirs {
			dirs[i] = strings.TrimSpace(dir)
		}
		cfg.Directories = dirs
	}

	if dotFilesFlag := flag.Lookup("disable-dotfiles"); dotFilesFlag != nil && dotFilesFlag.Value.String() != dotFilesFlag.DefValue {
		if val, err := strconv.ParseBool(dotFilesFlag.Value.String()); err == nil {
			cfg.DisableDotFiles = val // Flag is "disable dot files", config is "disable dot files"
		}
	}

	if logLevelFlag := flag.Lookup("log-level"); logLevelFlag != nil && logLevelFlag.Value.String() != logLevelFlag.DefValue {
		cfg.LogLevel = logLevelFlag.Value.String()
	}

	if enableAuthFlag := flag.Lookup("enable-auth"); enableAuthFlag != nil && enableAuthFlag.Value.String() != enableAuthFlag.DefValue {
		if val, err := strconv.ParseBool(enableAuthFlag.Value.String()); err == nil {
			cfg.EnableAuth = val
		}
	}

	if usernameFlag := flag.Lookup("username"); usernameFlag != nil && usernameFlag.Value.String() != usernameFlag.DefValue {
		cfg.Username = usernameFlag.Value.String()
	}

	if passwordFlag := flag.Lookup("password"); passwordFlag != nil && passwordFlag.Value.String() != passwordFlag.DefValue {
		cfg.Password = passwordFlag.Value.String()
	}

	if thumbCacheFlag := flag.Lookup("thumb-cache-mb"); thumbCacheFlag != nil && thumbCacheFlag.Value.String() != thumbCacheFlag.DefValue {
		if val, err := strconv.Atoi(thumbCacheFlag.Value.String()); err == nil {
			cfg.MaxThumbCacheMB = val
		}
	}

	if jpegQualityFlag := flag.Lookup("thumb-jpeg-quality"); jpegQualityFlag != nil && jpegQualityFlag.Value.String() != jpegQualityFlag.DefValue {
		if val, err := strconv.Atoi(jpegQualityFlag.Value.String()); err == nil {
			cfg.ThumbJpegQuality = val
		}
	}

	if ignorePatternsFlag := flag.Lookup("ignore-patterns"); ignorePatternsFlag != nil && ignorePatternsFlag.Value.String() != "" {
		patterns := strings.Split(ignorePatternsFlag.Value.String(), ",")
		for i, p := range patterns {
			patterns[i] = strings.TrimSpace(p)
		}

		// Merge with existing patterns from env/file, preventing duplicates
		existingPatterns := make(map[string]struct{})
		for _, p := range cfg.IgnorePatterns {
			existingPatterns[p] = struct{}{}
		}
		for _, p := range patterns {
			if _, exists := existingPatterns[p]; !exists {
				cfg.IgnorePatterns = append(cfg.IgnorePatterns, p)
			}
		}
	}

	if thumbMaxSizeFlag := flag.Lookup("thumb-max-file-size-mb"); thumbMaxSizeFlag != nil && thumbMaxSizeFlag.Value.String() != thumbMaxSizeFlag.DefValue {
		if val, err := strconv.Atoi(thumbMaxSizeFlag.Value.String()); err == nil {
			cfg.ThumbMaxFileSizeMB = val
		}
	}

	// Apply admin flag values
	if enableAdminFlag := flag.Lookup("enable-admin"); enableAdminFlag != nil && enableAdminFlag.Value.String() != enableAdminFlag.DefValue {
		if val, err := strconv.ParseBool(enableAdminFlag.Value.String()); err == nil {
			cfg.EnableAdmin = val
		}
	}

	if adminUsernameFlag := flag.Lookup("admin-username"); adminUsernameFlag != nil && adminUsernameFlag.Value.String() != adminUsernameFlag.DefValue {
		cfg.AdminUsername = adminUsernameFlag.Value.String()
	}

	if adminPasswordFlag := flag.Lookup("admin-password"); adminPasswordFlag != nil && adminPasswordFlag.Value.String() != adminPasswordFlag.DefValue {
		cfg.AdminPassword = adminPasswordFlag.Value.String()
	}

	if adminUploadDirFlag := flag.Lookup("admin-upload-dir"); adminUploadDirFlag != nil && adminUploadDirFlag.Value.String() != adminUploadDirFlag.DefValue {
		cfg.AdminUploadDir = adminUploadDirFlag.Value.String()
	}

	if maxUploadSizeFlag := flag.Lookup("max-upload-size-mb"); maxUploadSizeFlag != nil && maxUploadSizeFlag.Value.String() != maxUploadSizeFlag.DefValue {
		if val, err := strconv.Atoi(maxUploadSizeFlag.Value.String()); err == nil {
			cfg.MaxUploadSizeMB = val
		}
	}

	if allowedTypesFlag := flag.Lookup("allowed-upload-types"); allowedTypesFlag != nil && allowedTypesFlag.Value.String() != "" {
		types := strings.Split(allowedTypesFlag.Value.String(), ",")
		for i, typ := range types {
			types[i] = strings.TrimSpace(typ)
		}
		cfg.AllowedUploadTypes = types
	}

	if maxConcurrentFlag := flag.Lookup("max-concurrent-uploads"); maxConcurrentFlag != nil && maxConcurrentFlag.Value.String() != maxConcurrentFlag.DefValue {
		if val, err := strconv.Atoi(maxConcurrentFlag.Value.String()); err == nil {
			cfg.MaxConcurrentUploads = val
		}
	}
}
