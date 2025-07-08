package config

// Config holds the application configuration
type Config struct {
	Host               string   `json:"host"`
	Port               int      `json:"port"`
	Directories        []string `json:"directories"`
	DisableDotFiles    bool     `json:"disable_dot_files"`
	LogLevel           string   `json:"log_level"`
	EnableAuth         bool     `json:"enable_auth"`
	Username           string   `json:"username"`
	Password           string   `json:"password"`
	MaxThumbCacheMB    int      `json:"thumb_cache_mb"`
	ThumbJpegQuality   int      `json:"thumb_jpeg_quality"`
	ThumbMaxFileSizeMB int      `json:"thumb_max_file_size_mb"`
	IgnorePatterns     []string `json:"ignore_patterns"`

	// Admin configuration
	EnableAdmin          bool     `json:"enable_admin"`
	AdminUsername        string   `json:"admin_username"`
	AdminPassword        string   `json:"admin_password"`
	AdminUploadDir       string   `json:"admin_upload_dir"`
	MaxUploadSizeMB      int      `json:"max_upload_size_mb"`
	AllowedUploadTypes   []string `json:"allowed_upload_types"`
	MaxConcurrentUploads int      `json:"max_concurrent_uploads"`
}

// Default returns a Config with default values
func Default() *Config {
	return &Config{
		Host:               "0.0.0.0",
		Port:               8080,
		Directories:        []string{"."},
		DisableDotFiles:    true,
		LogLevel:           "info",
		EnableAuth:         false,
		Username:           "",
		Password:           "",
		MaxThumbCacheMB:    100,
		ThumbJpegQuality:   85,
		ThumbMaxFileSizeMB: 10,
		IgnorePatterns:     []string{},

		// Admin defaults
		EnableAdmin:          false,
		AdminUsername:        "",
		AdminPassword:        "",
		AdminUploadDir:       "./uploads",
		MaxUploadSizeMB:      100,
		AllowedUploadTypes:   []string{"*"}, // Allow all file types by default
		MaxConcurrentUploads: 3,
	}
}
