package config

// Config holds the application configuration
type Config struct {
	Host             string   `json:"host"`
	Port             int      `json:"port"`
	Directories      []string `json:"directories"`
	DisableDotFiles  bool     `json:"disable_dot_files"`
	LogLevel         string   `json:"log_level"`
	EnableAuth       bool     `json:"enable_auth"`
	Username         string   `json:"username"`
	Password         string   `json:"password"`
	MaxThumbCacheMB  int      `json:"thumb_cache_mb"`
	ThumbJpegQuality int      `json:"thumb_jpeg_quality"`
	IgnorePatterns   []string `json:"ignore_patterns"`
}

// Default returns a Config with default values
func Default() *Config {
	return &Config{
		Host:             "0.0.0.0",
		Port:             8080,
		Directories:      []string{"."},
		DisableDotFiles:  true,
		LogLevel:         "info",
		EnableAuth:       false,
		Username:         "",
		Password:         "",
		MaxThumbCacheMB:  100,
		ThumbJpegQuality: 85,
		IgnorePatterns:   []string{},
	}
}
