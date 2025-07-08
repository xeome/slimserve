package server

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"slimserve/internal/logger"

	"github.com/gin-gonic/gin"
)

// UploadManager manages concurrent uploads and tracks progress
type UploadManager struct {
	mu            sync.RWMutex
	activeUploads map[string]*UploadProgress
	maxConcurrent int
}

// UploadProgress tracks the progress of an individual upload
type UploadProgress struct {
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	TotalSize int64     `json:"total_size"`
	Uploaded  int64     `json:"uploaded"`
	Status    string    `json:"status"` // "uploading", "completed", "failed"
	StartTime time.Time `json:"start_time"`
	Error     string    `json:"error,omitempty"`
}

// NewUploadManager creates a new upload manager
func NewUploadManager(maxConcurrent int) *UploadManager {
	return &UploadManager{
		activeUploads: make(map[string]*UploadProgress),
		maxConcurrent: maxConcurrent,
	}
}

// handleFileUpload handles multiple file uploads with validation and progress tracking
func (s *Server) handleFileUpload(c *gin.Context) {
	// Log upload attempt
	logger.Log.Info().
		Str("ip", c.ClientIP()).
		Str("user_agent", c.GetHeader("User-Agent")).
		Msg("File upload attempt")

	// Check concurrent upload limit
	if len(s.uploadManager.activeUploads) >= s.uploadManager.maxConcurrent {
		logger.Log.Warn().
			Str("ip", c.ClientIP()).
			Int("active_uploads", len(s.uploadManager.activeUploads)).
			Int("max_concurrent", s.uploadManager.maxConcurrent).
			Msg("Upload rejected: concurrent limit reached")

		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":          "maximum concurrent uploads reached",
			"max_concurrent": s.uploadManager.maxConcurrent,
		})
		return
	}

	// Parse multipart form with size limit
	maxFormSize := int64(s.config.MaxUploadSizeMB) << 20 // Convert MB to bytes
	if err := c.Request.ParseMultipartForm(maxFormSize); err != nil {
		logger.Log.Error().
			Err(err).
			Str("ip", c.ClientIP()).
			Int("max_size_mb", s.config.MaxUploadSizeMB).
			Msg("Failed to parse multipart form")

		c.JSON(http.StatusBadRequest, gin.H{
			"error":       "failed to parse upload form - file may be too large",
			"max_size_mb": s.config.MaxUploadSizeMB,
		})
		return
	}

	// Extract files from form
	files := c.Request.MultipartForm.File["files"]
	if len(files) == 0 {
		logger.Log.Warn().Str("ip", c.ClientIP()).Msg("Upload request with no files")
		c.JSON(http.StatusBadRequest, gin.H{"error": "no files provided"})
		return
	}

	// Ensure upload directory exists
	uploadDir := s.config.AdminUploadDir
	if err := s.ensureUploadDirectory(uploadDir); err != nil {
		logger.Log.Error().
			Err(err).
			Str("dir", uploadDir).
			Msg("Failed to create upload directory")

		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create upload directory"})
		return
	}

	// Process uploads
	results := s.processUploads(files, uploadDir, c.ClientIP())

	// Determine response status
	status := http.StatusOK
	errorCount := 0
	for _, result := range results {
		if result["status"] == "error" {
			errorCount++
		}
	}

	if errorCount > 0 {
		if errorCount == len(results) {
			status = http.StatusBadRequest // All failed
		} else {
			status = http.StatusPartialContent // Some failed
		}
	}

	logger.Log.Info().
		Str("ip", c.ClientIP()).
		Int("total_files", len(files)).
		Int("successful", len(results)-errorCount).
		Int("failed", errorCount).
		Msg("Upload completed")

	c.JSON(status, gin.H{
		"message": "upload completed",
		"results": results,
		"summary": gin.H{
			"total":      len(files),
			"successful": len(results) - errorCount,
			"failed":     errorCount,
		},
	})
}

// ensureUploadDirectory creates the upload directory if it doesn't exist
func (s *Server) ensureUploadDirectory(uploadDir string) error {
	return os.MkdirAll(uploadDir, 0755)
}

// processUploads processes multiple file uploads
func (s *Server) processUploads(files []*multipart.FileHeader, uploadDir, clientIP string) []gin.H {
	results := make([]gin.H, 0, len(files))

	for _, fileHeader := range files {
		result := s.processFileUpload(fileHeader, uploadDir)
		results = append(results, result)

		// Log individual file result
		if result["status"] == "success" {
			logger.Log.Info().
				Str("ip", clientIP).
				Str("filename", fileHeader.Filename).
				Str("saved_as", result["saved_as"].(string)).
				Int64("size", result["size"].(int64)).
				Msg("File uploaded successfully")

			// Log activity
			if s.adminHandler != nil {
				s.adminHandler.activityStore.AddActivity("upload",
					fmt.Sprintf("File uploaded: %s", fileHeader.Filename),
					clientIP,
					fmt.Sprintf("Size: %d bytes, Saved as: %s", result["size"].(int64), result["saved_as"].(string)))
			}
		} else {
			logger.Log.Warn().
				Str("ip", clientIP).
				Str("filename", fileHeader.Filename).
				Str("error", result["error"].(string)).
				Msg("File upload failed")
		}
	}

	return results
}

// processFileUpload processes a single file upload
func (s *Server) processFileUpload(fileHeader *multipart.FileHeader, uploadDir string) gin.H {
	// Validate file size
	if fileHeader.Size > int64(s.config.MaxUploadSizeMB)<<20 {
		return gin.H{
			"filename": fileHeader.Filename,
			"status":   "error",
			"error":    fmt.Sprintf("file size exceeds maximum of %dMB", s.config.MaxUploadSizeMB),
		}
	}

	// Validate file type
	if !s.isAllowedFileType(fileHeader.Filename) {
		return gin.H{
			"filename": fileHeader.Filename,
			"status":   "error",
			"error":    "file type not allowed",
		}
	}

	// Additional security checks
	if !s.isSecureFilename(fileHeader.Filename) {
		return gin.H{
			"filename": fileHeader.Filename,
			"status":   "error",
			"error":    "filename contains unsafe characters",
		}
	}

	// Sanitize filename
	filename := sanitizeFilename(fileHeader.Filename)
	if filename == "" {
		return gin.H{
			"filename": fileHeader.Filename,
			"status":   "error",
			"error":    "invalid filename",
		}
	}

	// Create unique filename if file already exists
	destPath := filepath.Join(uploadDir, filename)
	destPath = s.getUniqueFilePath(destPath)

	// Open uploaded file
	src, err := fileHeader.Open()
	if err != nil {
		logger.Log.Error().Err(err).Str("filename", filename).Msg("Failed to open uploaded file")
		return gin.H{
			"filename": fileHeader.Filename,
			"status":   "error",
			"error":    "failed to open uploaded file",
		}
	}
	defer src.Close()

	// Create destination file
	dst, err := os.Create(destPath)
	if err != nil {
		logger.Log.Error().Err(err).Str("path", destPath).Msg("Failed to create destination file")
		return gin.H{
			"filename": fileHeader.Filename,
			"status":   "error",
			"error":    "failed to create destination file",
		}
	}
	defer dst.Close()

	// Copy file with progress tracking
	written, err := io.Copy(dst, src)
	if err != nil {
		logger.Log.Error().Err(err).Str("filename", filename).Msg("Failed to copy uploaded file")
		// Clean up partial file
		os.Remove(destPath)
		return gin.H{
			"filename": fileHeader.Filename,
			"status":   "error",
			"error":    "failed to save file",
		}
	}

	// Log successful upload
	logger.Log.Info().
		Str("filename", filename).
		Str("path", destPath).
		Int64("size", written).
		Msg("File uploaded successfully")

	return gin.H{
		"filename":    fileHeader.Filename,
		"saved_as":    filepath.Base(destPath),
		"size":        written,
		"status":      "success",
		"upload_path": destPath,
	}
}

// isAllowedFileType checks if the file type is allowed for upload
func (s *Server) isAllowedFileType(filename string) bool {
	if len(s.config.AllowedUploadTypes) == 0 {
		return true // No restrictions if list is empty
	}

	// Check for wildcard (allow all types)
	for _, allowedType := range s.config.AllowedUploadTypes {
		if strings.TrimSpace(allowedType) == "*" {
			return true
		}
	}

	ext := strings.ToLower(filepath.Ext(filename))
	if ext != "" && ext[0] == '.' {
		ext = ext[1:] // Remove the dot
	}

	// Check against allowed types
	for _, allowedType := range s.config.AllowedUploadTypes {
		if strings.ToLower(allowedType) == ext {
			return true
		}
	}

	return false
}

// isSecureFilename performs additional security checks on filenames
func (s *Server) isSecureFilename(filename string) bool {
	// Check for dangerous extensions
	dangerousExts := []string{
		"exe", "bat", "cmd", "com", "pif", "scr", "vbs", "js", "jar",
		"sh", "py", "pl", "php", "asp", "aspx", "jsp", "cgi",
	}

	ext := strings.ToLower(filepath.Ext(filename))
	if ext != "" && ext[0] == '.' {
		ext = ext[1:]
	}

	for _, dangerous := range dangerousExts {
		if ext == dangerous {
			return false
		}
	}

	// Check for suspicious patterns
	suspicious := []string{
		"../", "..\\", "..", "~", "$", "`", "|", "&", ";",
		"<", ">", "?", "*", ":", "\"", "'",
	}

	for _, pattern := range suspicious {
		if strings.Contains(filename, pattern) {
			return false
		}
	}

	// Check filename length
	if len(filename) > 255 {
		return false
	}

	// Check for null bytes
	if strings.Contains(filename, "\x00") {
		return false
	}

	return true
}

// sanitizeFilename removes dangerous characters from filename
func sanitizeFilename(filename string) string {
	// Remove path separators and other dangerous characters
	filename = filepath.Base(filename)
	filename = strings.ReplaceAll(filename, "..", "")
	filename = strings.ReplaceAll(filename, "/", "")
	filename = strings.ReplaceAll(filename, "\\", "")
	filename = strings.ReplaceAll(filename, ":", "")
	filename = strings.ReplaceAll(filename, "*", "")
	filename = strings.ReplaceAll(filename, "?", "")
	filename = strings.ReplaceAll(filename, "\"", "")
	filename = strings.ReplaceAll(filename, "<", "")
	filename = strings.ReplaceAll(filename, ">", "")
	filename = strings.ReplaceAll(filename, "|", "")

	// Trim whitespace
	filename = strings.TrimSpace(filename)

	// Ensure filename is not empty and not a hidden file
	if filename == "" || strings.HasPrefix(filename, ".") {
		return ""
	}

	return filename
}

// getUniqueFilePath generates a unique file path if the file already exists
func (s *Server) getUniqueFilePath(originalPath string) string {
	if _, err := os.Stat(originalPath); os.IsNotExist(err) {
		return originalPath
	}

	dir := filepath.Dir(originalPath)
	filename := filepath.Base(originalPath)
	ext := filepath.Ext(filename)
	nameWithoutExt := strings.TrimSuffix(filename, ext)

	for i := 1; i < 1000; i++ { // Limit attempts to prevent infinite loop
		newFilename := fmt.Sprintf("%s_%d%s", nameWithoutExt, i, ext)
		newPath := filepath.Join(dir, newFilename)

		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			return newPath
		}
	}

	// If we can't find a unique name, append timestamp
	timestamp := time.Now().Unix()
	newFilename := fmt.Sprintf("%s_%d%s", nameWithoutExt, timestamp, ext)
	return filepath.Join(dir, newFilename)
}

// getUploadProgress returns the progress of active uploads
func (s *Server) getUploadProgress(c *gin.Context) {
	s.uploadManager.mu.RLock()
	defer s.uploadManager.mu.RUnlock()

	var uploads []*UploadProgress
	for _, upload := range s.uploadManager.activeUploads {
		uploads = append(uploads, upload)
	}

	c.JSON(http.StatusOK, gin.H{
		"active_uploads": uploads,
		"max_concurrent": s.uploadManager.maxConcurrent,
	})
}
