package server

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"slimserve/internal/logger"
	"slimserve/internal/storage"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleFileUpload(c *gin.Context) {
	// Log upload attempt
	logger.Log.Info().
		Str("ip", c.ClientIP()).
		Str("user_agent", c.GetHeader("User-Agent")).
		Msg("File upload attempt")

	// Check concurrent upload limit
	if s.uploadManager.ActiveUploadsCount() >= s.uploadManager.GetMaxConcurrent() {
		logger.Log.Warn().
			Str("ip", c.ClientIP()).
			Int("active_uploads", s.uploadManager.ActiveUploadsCount()).
			Int("max_concurrent", s.uploadManager.GetMaxConcurrent()).
			Msg("Upload rejected: concurrent limit reached")

		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":          "maximum concurrent uploads reached",
			"max_concurrent": s.uploadManager.GetMaxConcurrent(),
		})
		return
	}

	maxFormSize := int64(s.config.MaxUploadSizeMB) * 1024 * 1024
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

	storageDir := s.config.GetStorageDir()
	var results []gin.H

	if storageDir.IsS3() {
		uploader, ok := s.backend.(storage.Uploader)
		if !ok {
			logger.Log.Error().Msg("Backend does not support uploads")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "upload backend does not support uploads"})
			return
		}
		results = s.processUploadsWithUploader(c.Request.Context(), files, uploader, c.ClientIP())
	} else {
		if err := s.ensureUploadDirectory(storageDir.Path); err != nil {
			logger.Log.Error().
				Err(err).
				Str("dir", storageDir.Path).
				Msg("Failed to create upload directory")

			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create upload directory"})
			return
		}
		results = s.processUploads(files, storageDir.Path, c.ClientIP())
	}

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

func (s *Server) processUploadsWithUploader(ctx context.Context, files []*multipart.FileHeader, uploader storage.Uploader, clientIP string) []gin.H {
	results := make([]gin.H, 0, len(files))

	for _, fileHeader := range files {
		result := s.processFileUploadWithUploader(ctx, fileHeader, uploader)
		results = append(results, result)

		if result["status"] == "success" {
			logger.Log.Info().
				Str("ip", clientIP).
				Str("filename", fileHeader.Filename).
				Str("key", result["key"].(string)).
				Int64("size", result["size"].(int64)).
				Msg("File uploaded to backend successfully")

			if s.adminHandler != nil {
				s.adminHandler.activityStore.AddActivity("upload",
					fmt.Sprintf("File uploaded: %s", fileHeader.Filename),
					clientIP,
					fmt.Sprintf("Size: %d bytes, Key: %s", result["size"].(int64), result["key"].(string)))
			}
		} else {
			logger.Log.Warn().
				Str("ip", clientIP).
				Str("filename", fileHeader.Filename).
				Str("error", result["error"].(string)).
				Msg("Upload failed")
		}
	}

	return results
}

func (s *Server) processFileUploadWithUploader(ctx context.Context, fileHeader *multipart.FileHeader, uploader storage.Uploader) gin.H {
	// Apply timeout for upload operations
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	if fileHeader.Size > int64(s.config.MaxUploadSizeMB)*1024*1024 {
		return gin.H{
			"filename": fileHeader.Filename,
			"status":   "error",
			"error":    fmt.Sprintf("file size exceeds maximum of %dMB", s.config.MaxUploadSizeMB),
		}
	}

	if !s.isAllowedFileType(fileHeader.Filename) {
		return gin.H{
			"filename": fileHeader.Filename,
			"status":   "error",
			"error":    fmt.Sprintf("file type not allowed: %s", fileHeader.Filename),
		}
	}

	filename := filepath.Base(fileHeader.Filename)
	if filename == "" || filename == "." {
		return gin.H{
			"filename": fileHeader.Filename,
			"status":   "error",
			"error":    fmt.Sprintf("invalid filename: %s", fileHeader.Filename),
		}
	}

	src, err := fileHeader.Open()
	if err != nil {
		logger.Log.Error().Err(err).Str("filename", filename).Msg("Failed to open uploaded file")
		return gin.H{
			"filename": fileHeader.Filename,
			"status":   "error",
			"error":    fmt.Sprintf("failed to open file %s: %v", fileHeader.Filename, err),
		}
	}
	defer src.Close() //nolint:errcheck

	data, err := io.ReadAll(src)
	if err != nil {
		logger.Log.Error().Err(err).Str("filename", filename).Msg("Failed to read uploaded file")
		return gin.H{
			"filename": fileHeader.Filename,
			"status":   "error",
			"error":    fmt.Sprintf("failed to read file %s: %v", fileHeader.Filename, err),
		}
	}

	// Upload to backend
	key := filename
	if err := uploader.Put(ctx, key, data); err != nil {
		logger.Log.Error().Err(err).Str("key", key).Msg("Failed to upload to backend")
		return gin.H{
			"filename": fileHeader.Filename,
			"status":   "error",
			"error":    "failed to upload to backend",
		}
	}

	logger.Log.Info().
		Str("key", key).
		Int64("size", int64(len(data))).
		Msg("File uploaded to backend successfully")

	return gin.H{
		"filename": fileHeader.Filename,
		"key":      key,
		"size":     int64(len(data)),
		"status":   "success",
	}
}

func (s *Server) ensureUploadDirectory(uploadDir string) error {
	return os.MkdirAll(uploadDir, 0755)
}

func (s *Server) processUploads(files []*multipart.FileHeader, uploadDir, clientIP string) []gin.H {
	uploader, ok := s.backend.(storage.Uploader)
	if !ok {
		logger.Log.Error().Msg("Backend does not support uploads")
		results := make([]gin.H, 0, len(files))
		for _, fileHeader := range files {
			results = append(results, gin.H{
				"filename": fileHeader.Filename,
				"status":   "error",
				"error":    "upload backend does not support uploads",
			})
		}
		return results
	}

	ctx := context.Background()
	results := make([]gin.H, 0, len(files))

	for _, fileHeader := range files {
		result := s.processFileUpload(ctx, fileHeader, uploader)
		results = append(results, result)

		if result["status"] == "success" {
			logger.Log.Info().
				Str("ip", clientIP).
				Str("filename", fileHeader.Filename).
				Str("saved_as", result["saved_as"].(string)).
				Int64("size", result["size"].(int64)).
				Msg("File uploaded successfully")

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

func (s *Server) processFileUpload(ctx context.Context, fileHeader *multipart.FileHeader, uploader storage.Uploader) gin.H {
	if fileHeader.Size > int64(s.config.MaxUploadSizeMB)*1024*1024 {
		return gin.H{
			"filename": fileHeader.Filename,
			"status":   "error",
			"error":    fmt.Sprintf("file size exceeds maximum of %dMB", s.config.MaxUploadSizeMB),
		}
	}

	if !s.isAllowedFileType(fileHeader.Filename) {
		return gin.H{
			"filename": fileHeader.Filename,
			"status":   "error",
			"error":    fmt.Sprintf("file type not allowed: %s", fileHeader.Filename),
		}
	}

	filename := filepath.Base(fileHeader.Filename)
	if filename == "" || filename == "." {
		return gin.H{
			"filename": fileHeader.Filename,
			"status":   "error",
			"error":    fmt.Sprintf("invalid filename: %s", fileHeader.Filename),
		}
	}

	src, err := fileHeader.Open()
	if err != nil {
		logger.Log.Error().Err(err).Str("filename", filename).Msg("Failed to open uploaded file")
		return gin.H{
			"filename": fileHeader.Filename,
			"status":   "error",
			"error":    fmt.Sprintf("failed to open file %s: %v", fileHeader.Filename, err),
		}
	}
	defer src.Close() //nolint:errcheck

	data, err := io.ReadAll(src)
	if err != nil {
		logger.Log.Error().Err(err).Str("filename", filename).Msg("Failed to read uploaded file")
		return gin.H{
			"filename": fileHeader.Filename,
			"status":   "error",
			"error":    fmt.Sprintf("failed to read file %s: %v", fileHeader.Filename, err),
		}
	}

	if err := uploader.Put(ctx, filename, data); err != nil {
		logger.Log.Error().Err(err).Str("filename", filename).Msg("Failed to upload file")
		return gin.H{
			"filename": fileHeader.Filename,
			"status":   "error",
			"error":    fmt.Sprintf("failed to save file %s: %v", fileHeader.Filename, err),
		}
	}

	logger.Log.Info().
		Str("filename", filename).
		Int64("size", int64(len(data))).
		Msg("File uploaded successfully")

	return gin.H{
		"filename": fileHeader.Filename,
		"saved_as": filename,
		"size":     int64(len(data)),
		"status":   "success",
	}
}

func (s *Server) isAllowedFileType(filename string) bool {
	if len(s.config.AllowedUploadTypes) == 0 {
		return true // No restrictions if list is empty
	}

	ext := strings.ToLower(filepath.Ext(filename))
	if ext != "" && ext[0] == '.' {
		ext = ext[1:] // Remove the dot
	}

	for _, allowedType := range s.config.AllowedUploadTypes {
		lowerAllowed := strings.ToLower(strings.TrimSpace(allowedType))
		if lowerAllowed == "*" {
			return true
		}
		if lowerAllowed == ext {
			return true
		}
	}

	return false
}

func (s *Server) getUploadProgress(c *gin.Context) {
	uploads := s.uploadManager.GetActiveUploads()

	c.JSON(http.StatusOK, gin.H{
		"active_uploads": uploads,
		"max_concurrent": s.uploadManager.GetMaxConcurrent(),
	})
}
