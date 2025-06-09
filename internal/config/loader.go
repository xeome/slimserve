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

	// Load from configuration file first (if specified)
	configFile := getConfigFile()
	if configFile != "" {
		if err := loadFromFile(cfg, configFile); err != nil {
			return nil, err
		}
	}

	// Load from environment variables (overrides file config)
	loadFromEnv(cfg)

	// Load from CLI flags (overrides everything else)
	loadFromFlags(cfg)

	return cfg, nil
}

// getConfigFile returns the configuration file path from flags or environment
func getConfigFile() string {
	// Check CLI flag first
	configFlag := flag.Lookup("config")
	if configFlag != nil && configFlag.Value.String() != "" {
		return configFlag.Value.String()
	}

	// Check environment variable
	if envConfig := os.Getenv("SLIMSERVE_CONFIG"); envConfig != "" {
		return envConfig
	}

	// Check for default config file
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
		// Trim whitespace from each directory
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
}
