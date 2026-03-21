package config

const (
	BackendLocal = "local"
	BackendS3    = "s3"
)

type DirectoryConfig struct {
	Path string `json:"path"`
	Type string `json:"type"`

	// S3-specific options
	Region    string `json:"region,omitempty"`
	Endpoint  string `json:"endpoint,omitempty"`
	AccessKey string `json:"access_key,omitempty"`
	SecretKey string `json:"secret_key,omitempty"`
	Prefix    string `json:"prefix,omitempty"`
}

func (d *DirectoryConfig) IsS3() bool {
	return d.Type == BackendS3
}

func (d *DirectoryConfig) IsLocal() bool {
	return d.Type == BackendLocal || d.Type == ""
}

type Config struct {
	Host               string   `json:"host"`
	Port               int      `json:"port"`
	DisableDotFiles    bool     `json:"disable_dot_files"`
	LogLevel           string   `json:"log_level"`
	EnableAuth         bool     `json:"enable_auth"`
	Username           string   `json:"username"`
	Password           string   `json:"password"`
	PasswordHash       string   `json:"-"` // Hash for runtime verification, not serialized
	MaxThumbCacheMB    int      `json:"thumb_cache_mb"`
	ThumbJpegQuality   int      `json:"thumb_jpeg_quality"`
	ThumbMaxFileSizeMB int      `json:"thumb_max_file_size_mb"`
	IgnorePatterns     []string `json:"ignore_patterns"`

	// Storage configuration (single backend: local or S3)
	StoragePath string `json:"storage_path"`  // Path for local or bucket name for S3
	StorageType string `json:"storage_type"`  // "local" or "s3"
	S3Region    string `json:"s3_region"`     // S3 region
	S3Endpoint  string `json:"s3_endpoint"`   // S3 endpoint (for MinIO, etc.)
	S3AccessKey string `json:"s3_access_key"` // S3 access key
	S3SecretKey string `json:"s3_secret_key"` // S3 secret key
	S3Prefix    string `json:"s3_prefix"`     // S3 key prefix
	LRUEnabled  bool   `json:"lru_enabled"`
	LRUMaxMB    int    `json:"lru_max_mb"`

	// Admin configuration
	EnableAdmin          bool     `json:"enable_admin"`
	AdminUsername        string   `json:"admin_username"`
	AdminPassword        string   `json:"admin_password"`
	AdminPasswordHash    string   `json:"-"` // Hash for runtime verification, not serialized
	MaxUploadSizeMB      int      `json:"max_upload_size_mb"`
	AllowedUploadTypes   []string `json:"allowed_upload_types"`
	MaxConcurrentUploads int      `json:"max_concurrent_uploads"`
}

// GetStorageDir returns the storage directory configuration
func (c *Config) GetStorageDir() DirectoryConfig {
	if c.StorageType == BackendS3 {
		return DirectoryConfig{
			Path:      c.StoragePath,
			Type:      BackendS3,
			Region:    c.S3Region,
			Endpoint:  c.S3Endpoint,
			AccessKey: c.S3AccessKey,
			SecretKey: c.S3SecretKey,
			Prefix:    c.S3Prefix,
		}
	}
	return DirectoryConfig{
		Path: c.StoragePath,
		Type: BackendLocal,
	}
}

// Default returns a Config with default values
func Default() *Config {
	return &Config{
		Host:               "0.0.0.0",
		Port:               8080,
		DisableDotFiles:    true,
		LogLevel:           "info",
		EnableAuth:         false,
		Username:           "",
		Password:           "",
		MaxThumbCacheMB:    100,
		ThumbJpegQuality:   85,
		ThumbMaxFileSizeMB: 10,
		IgnorePatterns:     []string{},

		StoragePath: ".",
		StorageType: BackendLocal,
		LRUEnabled:  true,
		LRUMaxMB:    0,

		EnableAdmin:          false,
		AdminUsername:        "",
		AdminPassword:        "",
		MaxUploadSizeMB:      100,
		AllowedUploadTypes:   []string{"*"},
		MaxConcurrentUploads: 3,
	}
}
