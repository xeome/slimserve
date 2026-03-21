package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"slimserve/internal/logger"
	"slimserve/internal/server/admin"
	"slimserve/internal/server/auth"
	"slimserve/internal/storage"
	"slimserve/internal/version"

	"github.com/gin-gonic/gin"
)

type AdminHandler struct {
	server        *Server
	activityStore *admin.ActivityStore
}

func NewAdminHandler(server *Server) *AdminHandler {
	return &AdminHandler{
		server:        server,
		activityStore: admin.NewActivityStore(100),
	}
}

func (ah *AdminHandler) getSystemStats(c *gin.Context) {
	stats := gin.H{
		"total_files":   ah.countTotalFiles(),
		"uploads_today": ah.countUploadsToday(),
		"storage_used":  ah.getStorageUsed(),
		"server_uptime": ah.getServerUptime(),
		"memory_usage":  ah.getMemoryUsage(),
	}

	c.JSON(http.StatusOK, stats)
}

func (ah *AdminHandler) getSystemStatus(c *gin.Context) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	storageDir := ah.server.config.GetStorageDir()

	status := gin.H{
		"server": gin.H{
			"status":     "online",
			"uptime":     ah.getServerUptime(),
			"version":    version.Get().Version,
			"go_version": runtime.Version(),
		},
		"memory": gin.H{
			"allocated":   ah.server.adminUtils.FormatBytes(m.Alloc),
			"total_alloc": ah.server.adminUtils.FormatBytes(m.TotalAlloc),
			"sys":         ah.server.adminUtils.FormatBytes(m.Sys),
			"num_gc":      m.NumGC,
		},
		"storage": gin.H{
			"storage_type": storageDir.Type,
			"storage_path": storageDir.Path,
			"total_files":  ah.countTotalFiles(),
			"storage_used": ah.getStorageUsed(),
		},
		"configuration": gin.H{
			"max_upload_size": fmt.Sprintf("%dMB", ah.server.config.MaxUploadSizeMB),
			"max_concurrent":  ah.server.config.MaxConcurrentUploads,
			"allowed_types":   ah.server.config.AllowedUploadTypes,
			"storage_path":    storageDir.Path,
		},
	}

	c.JSON(http.StatusOK, status)
}

func (ah *AdminHandler) getConfiguration(c *gin.Context) {
	storageDir := ah.server.config.GetStorageDir()
	config := gin.H{
		"host":                   ah.server.config.Host,
		"port":                   ah.server.config.Port,
		"storage_type":           storageDir.Type,
		"storage_path":           storageDir.Path,
		"disable_dot_files":      ah.server.config.DisableDotFiles,
		"log_level":              ah.server.config.LogLevel,
		"enable_auth":            ah.server.config.EnableAuth,
		"max_thumb_cache_mb":     ah.server.config.MaxThumbCacheMB,
		"thumb_jpeg_quality":     ah.server.config.ThumbJpegQuality,
		"thumb_max_file_size_mb": ah.server.config.ThumbMaxFileSizeMB,
		"ignore_patterns":        ah.server.config.IgnorePatterns,
		"enable_admin":           ah.server.config.EnableAdmin,
		"max_upload_size_mb":     ah.server.config.MaxUploadSizeMB,
		"allowed_upload_types":   ah.server.config.AllowedUploadTypes,
		"max_concurrent_uploads": ah.server.config.MaxConcurrentUploads,
	}

	c.JSON(http.StatusOK, config)
}

func (ah *AdminHandler) updateConfiguration(c *gin.Context) {
	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid configuration data"})
		return
	}

	updated := false

	if val, ok := updates["max_upload_size_mb"].(float64); ok && val > 0 && val <= 1000 {
		ah.server.config.MaxUploadSizeMB = int(val)
		updated = true
	}

	if val, ok := updates["max_concurrent_uploads"].(float64); ok && val > 0 && val <= 10 {
		ah.server.config.MaxConcurrentUploads = int(val)
		updated = true
	}

	if val, ok := updates["thumb_jpeg_quality"].(float64); ok && val >= 1 && val <= 100 {
		ah.server.config.ThumbJpegQuality = int(val)
		updated = true
	}

	if !updated {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no valid configuration updates provided"})
		return
	}

	logger.Log.Info().
		Str("ip", c.ClientIP()).
		Interface("updates", updates).
		Msg("Admin configuration updated")

	ah.activityStore.AddActivity(admin.ActivityConfig, "Configuration updated", c.ClientIP(), fmt.Sprintf("Updated: %v", updates))

	c.JSON(http.StatusOK, gin.H{"message": "configuration updated successfully"})
}

func (ah *AdminHandler) getAuthConfig(c *gin.Context) {
	config := gin.H{
		"enable_auth":        ah.server.config.EnableAuth,
		"username":           ah.server.config.Username,
		"password_set":       ah.server.config.PasswordHash != "" || ah.server.config.Password != "",
		"enable_admin":       ah.server.config.EnableAdmin,
		"admin_username":     ah.server.config.AdminUsername,
		"admin_password_set": ah.server.config.AdminPasswordHash != "" || ah.server.config.AdminPassword != "",
	}

	c.JSON(http.StatusOK, config)
}

func (ah *AdminHandler) updateAuthConfig(c *gin.Context) {
	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid configuration data"})
		return
	}

	updated := false

	if val, ok := updates["enable_auth"].(bool); ok {
		ah.server.config.EnableAuth = val
		updated = true
	}

	if val, ok := updates["username"].(string); ok {
		ah.server.config.Username = val
		updated = true
	}

	if val, ok := updates["password"].(string); ok && val != "" {
		hash, err := auth.HashPassword(val)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
			return
		}
		ah.server.config.PasswordHash = hash
		ah.server.config.Password = ""
		updated = true
	}

	if val, ok := updates["enable_admin"].(bool); ok {
		ah.server.config.EnableAdmin = val
		updated = true
	}

	if val, ok := updates["admin_username"].(string); ok {
		ah.server.config.AdminUsername = val
		updated = true
	}

	if val, ok := updates["admin_password"].(string); ok && val != "" {
		hash, err := auth.HashPassword(val)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash admin password"})
			return
		}
		ah.server.config.AdminPasswordHash = hash
		ah.server.config.AdminPassword = ""
		updated = true
	}

	if !updated {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no valid authentication updates provided"})
		return
	}

	logger.Log.Info().
		Str("ip", c.ClientIP()).
		Msg("Admin authentication configuration updated")

	ah.activityStore.AddActivity(admin.ActivityConfig, "Authentication settings updated", c.ClientIP(), "Auth configuration changed")

	c.JSON(http.StatusOK, gin.H{"message": "authentication updated successfully"})
}

func (ah *AdminHandler) listFiles(c *gin.Context) {
	path := c.DefaultQuery("path", "/")

	relPath := strings.TrimPrefix(path, "/")
	if relPath == "" {
		relPath = "."
	}

	entries, err := ah.server.backend.ReadDir(c.Request.Context(), relPath)
	if err != nil {
		logger.Log.Error().Err(err).Str("path", path).Msg("Failed to read directory")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read directory"})
		return
	}

	var files []gin.H
	for _, entry := range entries {
		if ah.server.config.DisableDotFiles && strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		info, _ := entry.Info()
		var size int64
		var modTime time.Time
		if info != nil {
			size = info.Size()
			modTime = info.ModTime()
		}

		files = append(files, gin.H{
			"name":     entry.Name(),
			"size":     size,
			"is_dir":   entry.IsDir(),
			"mod_time": modTime,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"path":  path,
		"files": files,
	})
}

func (ah *AdminHandler) deleteFile(c *gin.Context) {
	var req struct {
		Path     string `json:"path" binding:"required"`
		Filename string `json:"filename" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	fullPath := filepath.Join(req.Path, req.Filename)
	if !ah.isPathAllowed(fullPath) {
		c.JSON(http.StatusForbidden, gin.H{"error": "path not allowed"})
		return
	}

	err := os.RemoveAll(fullPath)
	if err != nil {
		logger.Log.Error().Err(err).Str("path", fullPath).Msg("Failed to delete file")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete file"})
		return
	}

	logger.Log.Info().
		Str("ip", c.ClientIP()).
		Str("path", fullPath).
		Msg("File deleted via admin interface")

	ah.activityStore.AddActivity(admin.ActivityDelete, fmt.Sprintf("Deleted: %s", req.Filename), c.ClientIP(), fullPath)

	c.JSON(http.StatusOK, gin.H{"message": "file deleted successfully"})
}

func (ah *AdminHandler) moveFile(c *gin.Context) {
	var req struct {
		Source      string `json:"source" binding:"required"`
		Destination string `json:"destination" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if !ah.isPathAllowed(req.Source) {
		c.JSON(http.StatusForbidden, gin.H{"error": "source path not allowed"})
		return
	}

	if !ah.isPathAllowed(req.Destination) {
		c.JSON(http.StatusForbidden, gin.H{"error": "destination path not allowed"})
		return
	}

	uploader, ok := ah.server.backend.(storage.Uploader)
	if !ok {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "backend does not support move operations"})
		return
	}

	relSrc := strings.TrimPrefix(req.Source, "/")
	relDest := strings.TrimPrefix(req.Destination, "/")

	err := uploader.Move(c.Request.Context(), relSrc, relDest)
	if err != nil {
		logger.Log.Error().Err(err).
			Str("source", req.Source).
			Str("destination", req.Destination).
			Msg("Failed to move file")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to move file"})
		return
	}

	logger.Log.Info().
		Str("ip", c.ClientIP()).
		Str("source", req.Source).
		Str("destination", req.Destination).
		Msg("File moved via admin interface")

	ah.activityStore.AddActivity(admin.ActivityMove, fmt.Sprintf("Moved: %s -> %s", req.Source, req.Destination), c.ClientIP(), "")

	c.JSON(http.StatusOK, gin.H{"message": "file moved successfully"})
}

func (ah *AdminHandler) createDirectory(c *gin.Context) {
	var req struct {
		Path string `json:"path" binding:"required"`
		Name string `json:"name" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	req.Name = filepath.Base(req.Name)
	if req.Name == "" || req.Name == "." {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid directory name"})
		return
	}

	fullPath := filepath.Join(req.Path, req.Name)
	if !ah.isPathAllowed(fullPath) {
		c.JSON(http.StatusForbidden, gin.H{"error": "path not allowed"})
		return
	}

	err := os.MkdirAll(fullPath, 0755)
	if err != nil {
		logger.Log.Error().Err(err).Str("path", fullPath).Msg("Failed to create directory")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create directory"})
		return
	}

	logger.Log.Info().
		Str("ip", c.ClientIP()).
		Str("path", fullPath).
		Msg("Directory created via admin interface")

	ah.activityStore.AddActivity(admin.ActivityMkdir, fmt.Sprintf("Created directory: %s", req.Name), c.ClientIP(), fullPath)

	c.JSON(http.StatusOK, gin.H{"message": "directory created successfully"})
}

func (ah *AdminHandler) getRecentActivity(c *gin.Context) {
	activities := ah.activityStore.GetRecentActivities(20)

	result := make([]gin.H, len(activities))
	for i, activity := range activities {
		result[i] = gin.H{
			"id":          activity.ID,
			"type":        activity.Type,
			"description": activity.Description,
			"timestamp":   activity.Timestamp.Format(time.RFC3339),
			"ip":          activity.IP,
		}
		if activity.Details != "" {
			result[i]["details"] = activity.Details
		}
	}

	c.JSON(http.StatusOK, result)
}

func (ah *AdminHandler) countTotalFiles() int {
	storageDir := ah.server.config.GetStorageDir()
	if storageDir.IsS3() {
		return 0
	}
	count := 0
	filepath.Walk(storageDir.Path, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			count++
		}
		return nil
	})
	return count
}

func (ah *AdminHandler) countUploadsToday() int {
	return ah.activityStore.CountUploadsToday()
}

func (ah *AdminHandler) getStorageUsed() string {
	storageDir := ah.server.config.GetStorageDir()
	if storageDir.IsS3() {
		return "N/A"
	}
	var totalSize int64
	filepath.Walk(storageDir.Path, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})
	return ah.server.adminUtils.FormatBytes(uint64(totalSize))
}

func (ah *AdminHandler) getServerUptime() string {
	return ah.server.adminUtils.GetUptime()
}

func (ah *AdminHandler) getMemoryUsage() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return ah.server.adminUtils.FormatBytes(uint64(m.Alloc))
}

func (ah *AdminHandler) isPathAllowed(path string) bool {
	storageDir := ah.server.config.GetStorageDir()
	if storageDir.IsS3() {
		return true
	}

	fullPath := filepath.Join(storageDir.Path, path)
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return false
	}
	absAllowed, err := filepath.Abs(storageDir.Path)
	if err != nil {
		return false
	}

	return strings.HasPrefix(absPath, absAllowed)
}
