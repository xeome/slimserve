package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"slimserve/internal/logger"

	"github.com/gin-gonic/gin"
)

type ActivityEntry struct {
	ID          int       `json:"id"`
	Type        string    `json:"type"` // "login", "upload", "config", "delete", "mkdir"
	Description string    `json:"description"`
	Timestamp   time.Time `json:"timestamp"`
	IP          string    `json:"ip"`
	Details     string    `json:"details,omitempty"`
}

type ActivityStore struct {
	mu         sync.RWMutex
	activities []ActivityEntry
	nextID     int
	maxEntries int
}

func NewActivityStore(maxEntries int) *ActivityStore {
	return &ActivityStore{
		activities: make([]ActivityEntry, 0, maxEntries),
		nextID:     1,
		maxEntries: maxEntries,
	}
}

func (as *ActivityStore) AddActivity(activityType, description, ip, details string) {
	as.mu.Lock()
	defer as.mu.Unlock()

	entry := ActivityEntry{
		ID:          as.nextID,
		Type:        activityType,
		Description: description,
		Timestamp:   time.Now(),
		IP:          ip,
		Details:     details,
	}

	as.activities = append(as.activities, entry)
	as.nextID++

	if len(as.activities) > as.maxEntries {
		as.activities = as.activities[len(as.activities)-as.maxEntries:]
	}
}

func (as *ActivityStore) GetRecentActivities(limit int) []ActivityEntry {
	as.mu.RLock()
	defer as.mu.RUnlock()

	if limit <= 0 || limit > len(as.activities) {
		limit = len(as.activities)
	}

	result := make([]ActivityEntry, limit)
	for i := 0; i < limit; i++ {
		result[i] = as.activities[len(as.activities)-1-i]
	}

	return result
}

type AdminHandler struct {
	server        *Server
	activityStore *ActivityStore
}

func NewAdminHandler(server *Server) *AdminHandler {
	return &AdminHandler{
		server:        server,
		activityStore: NewActivityStore(100), // Keep last 100 activities
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

	status := gin.H{
		"server": gin.H{
			"status":     "online",
			"uptime":     ah.getServerUptime(),
			"version":    "1.0.0", // TODO: Get from build info
			"go_version": runtime.Version(),
		},
		"memory": gin.H{
			"allocated":   ah.server.adminUtils.formatBytes(m.Alloc),
			"total_alloc": ah.server.adminUtils.formatBytes(m.TotalAlloc),
			"sys":         ah.server.adminUtils.formatBytes(m.Sys),
			"num_gc":      m.NumGC,
		},
		"storage": gin.H{
			"upload_dir":   ah.server.config.AdminUploadDir,
			"total_files":  ah.countTotalFiles(),
			"storage_used": ah.getStorageUsed(),
		},
		"configuration": gin.H{
			"max_upload_size":    fmt.Sprintf("%dMB", ah.server.config.MaxUploadSizeMB),
			"max_concurrent":     ah.server.config.MaxConcurrentUploads,
			"allowed_types":      ah.server.config.AllowedUploadTypes,
			"directories_served": ah.server.config.Directories,
		},
	}

	c.JSON(http.StatusOK, status)
}

func (ah *AdminHandler) getConfiguration(c *gin.Context) {
	config := gin.H{
		"host":                   ah.server.config.Host,
		"port":                   ah.server.config.Port,
		"directories":            ah.server.config.Directories,
		"disable_dot_files":      ah.server.config.DisableDotFiles,
		"log_level":              ah.server.config.LogLevel,
		"enable_auth":            ah.server.config.EnableAuth,
		"max_thumb_cache_mb":     ah.server.config.MaxThumbCacheMB,
		"thumb_jpeg_quality":     ah.server.config.ThumbJpegQuality,
		"thumb_max_file_size_mb": ah.server.config.ThumbMaxFileSizeMB,
		"ignore_patterns":        ah.server.config.IgnorePatterns,
		"enable_admin":           ah.server.config.EnableAdmin,
		"admin_upload_dir":       ah.server.config.AdminUploadDir,
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

	ah.activityStore.AddActivity("config", "Configuration updated", c.ClientIP(), fmt.Sprintf("Updated: %v", updates))

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
		hash, err := HashPassword(val)
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
		hash, err := HashPassword(val)
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

	ah.activityStore.AddActivity("config", "Authentication settings updated", c.ClientIP(), "Auth configuration changed")

	c.JSON(http.StatusOK, gin.H{"message": "authentication updated successfully"})
}

func (ah *AdminHandler) listFiles(c *gin.Context) {
	path := c.DefaultQuery("path", "/")

	var targetDir string
	if path == "/" && len(ah.server.config.Directories) > 0 {
		targetDir = ah.server.config.Directories[0]
	} else {
		found := false
		for _, dir := range ah.server.config.Directories {
			absDir, err := filepath.Abs(dir)
			if err != nil {
				continue
			}

			var candidatePath string
			if path == "/" {
				candidatePath = absDir
			} else {
				relativePath := strings.TrimPrefix(path, "/")
				candidatePath = filepath.Join(absDir, relativePath)
			}

			if strings.HasPrefix(candidatePath, absDir) {
				targetDir = candidatePath
				found = true
				break
			}
		}
		if !found {
			c.JSON(http.StatusForbidden, gin.H{"error": "path not allowed"})
			return
		}
	}

	entries, err := os.ReadDir(targetDir)
	if err != nil {
		logger.Log.Error().Err(err).Str("path", targetDir).Msg("Failed to read directory")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read directory"})
		return
	}

	var files []gin.H
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		if ah.server.config.DisableDotFiles && strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		files = append(files, gin.H{
			"name":     entry.Name(),
			"size":     info.Size(),
			"is_dir":   entry.IsDir(),
			"mod_time": info.ModTime(),
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

	ah.activityStore.AddActivity("delete", fmt.Sprintf("Deleted: %s", req.Filename), c.ClientIP(), fullPath)

	c.JSON(http.StatusOK, gin.H{"message": "file deleted successfully"})
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

	req.Name = sanitizeFilename(req.Name)
	if req.Name == "" {
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

	ah.activityStore.AddActivity("mkdir", fmt.Sprintf("Created directory: %s", req.Name), c.ClientIP(), fullPath)

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
	count := 0
	for _, dir := range ah.server.config.Directories {
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				count++
			}
			return nil
		})
	}
	return count
}

func (ah *AdminHandler) countUploadsToday() int {
	today := time.Now().Truncate(24 * time.Hour)
	count := 0

	ah.activityStore.mu.RLock()
	defer ah.activityStore.mu.RUnlock()

	for _, activity := range ah.activityStore.activities {
		if activity.Type == "upload" && activity.Timestamp.After(today) {
			count++
		}
	}

	return count
}

func (ah *AdminHandler) getStorageUsed() string {
	var totalSize int64
	for _, dir := range ah.server.config.Directories {
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				totalSize += info.Size()
			}
			return nil
		})
	}
	return ah.server.adminUtils.formatBytes(uint64(totalSize))
}

func (ah *AdminHandler) getServerUptime() string {
	return ah.server.adminUtils.GetUptime()
}

func (ah *AdminHandler) getMemoryUsage() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return ah.server.adminUtils.formatBytes(uint64(m.Alloc))
}

func (ah *AdminHandler) isPathAllowed(path string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	for _, allowedDir := range ah.server.config.Directories {
		absAllowed, err := filepath.Abs(allowedDir)
		if err != nil {
			continue
		}

		if strings.HasPrefix(absPath, absAllowed) {
			return true
		}
	}

	if ah.server.config.AdminUploadDir != "" {
		absUploadDir, err := filepath.Abs(ah.server.config.AdminUploadDir)
		if err == nil && strings.HasPrefix(absPath, absUploadDir) {
			return true
		}
	}

	return false
}
