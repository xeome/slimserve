package server

import (
	"fmt"
	"html/template"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"slimserve/internal/config"
	"slimserve/internal/files"
	"slimserve/internal/logger"
	"slimserve/internal/security"
	"slimserve/web"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	config       *config.Config
	tmpl         *template.Template
	allowedRoots []string
	roots        []*security.RootFS
}

type FileItem struct {
	Name         string `json:"name"`
	URL          string `json:"url"`
	Size         string `json:"size"`
	ModTime      string `json:"mod_time"`
	Type         string `json:"type"` // folder, image, document, video, audio
	Icon         string `json:"icon"`
	IsImage      bool   `json:"is_image"`
	IsFolder     bool   `json:"is_folder"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
}

type PathSegment struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type ListingData struct {
	Title        string        `json:"title"`
	PathSegments []PathSegment `json:"path_segments"`
	Files        []FileItem    `json:"files"`
	CurrentPath  string        `json:"current_path"`
}

func NewHandler(cfg *config.Config, roots []*security.RootFS) *Handler {
	tmpl := template.Must(template.ParseFS(web.TemplateFS, "templates/base.html", "templates/listing.html"))

	return &Handler{
		config:       cfg,
		tmpl:         tmpl,
		allowedRoots: cfg.Directories,
		roots:        roots,
	}
}

func (h *Handler) ServeFiles(c *gin.Context) {
	requestPath := c.Param("path")
	if requestPath == "" {
		requestPath = "/"
	}

	// If root is requested, serve directory listing of first allowed root directly
	if requestPath == "/" {
		if len(h.roots) > 0 {
			h.serveDirectoryFromRoot(c, h.roots[0], ".", "/")
			return
		}
	}

	// Handle static assets from embedded FS - bypass all other checks
	if strings.HasPrefix(requestPath, "/static/") {
		h.serveStaticFile(c, requestPath)
		return
	}

	// Clean and convert to relative path
	cleanPath := filepath.Clean(requestPath)
	if cleanPath == "." {
		cleanPath = "/"
	}
	// Map absolute URL path to relative filesystem path
	relPath := strings.TrimPrefix(cleanPath, "/")

	// Check for hidden files/directories (components starting with ".") if enabled
	if h.config.DisableDotFiles {
		pathComponents := strings.Split(strings.Trim(cleanPath, "/"), "/")
		for _, component := range pathComponents {
			if component != "" && strings.HasPrefix(component, ".") {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
		}
	}

	// Check for thumbnail request
	if c.Query("thumb") == "1" {
		h.serveThumbnailFromRoot(c, relPath)
		return
	}

	// Try to find the file in one of the RootFS instances
	for _, root := range h.roots {
		// Use RootFS.Stat for traversal-resistant file access
		info, err := root.Stat(relPath)
		if err != nil {
			continue // Try next root
		}

		// If it's a file, serve it
		if !info.IsDir() {
			file, err := root.Open(relPath)
			if err != nil {
				continue
			}
			defer file.Close()

			// Get file info for headers
			fileInfo, err := file.Stat()
			if err != nil {
				continue
			}

			http.ServeContent(c.Writer, c.Request, fileInfo.Name(), fileInfo.ModTime(), file)
			return
		}

		// If it's a directory, show listing
		h.serveDirectoryFromRoot(c, root, relPath, cleanPath)
		return
	}

	// File not found in any allowed root
	c.AbortWithStatus(http.StatusNotFound)
}

// buildListingData creates a directory listing from entries, shared by both listing methods
func (h *Handler) buildListingData(entries []os.DirEntry, requestPath string) ListingData {
	var files []FileItem
	for _, entry := range entries {
		// Skip dot files if configured to do so
		if h.config.DisableDotFiles && strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		fileItem := FileItem{
			Name:     entry.Name(),
			URL:      buildFileURL(requestPath, entry.Name()),
			Size:     formatSize(info.Size()),
			ModTime:  info.ModTime().Format("Jan 2, 2006 15:04"),
			Type:     determineFileType(entry),
			Icon:     getFileIcon(entry),
			IsImage:  isImageFile(entry.Name()),
			IsFolder: entry.IsDir(),
		}

		if fileItem.IsImage {
			fileItem.ThumbnailURL = buildThumbnailURL(requestPath, entry.Name())
		}

		files = append(files, fileItem)
	}

	// Sort files: folders first, then by name
	sort.Slice(files, func(i, j int) bool {
		if files[i].IsFolder != files[j].IsFolder {
			return files[i].IsFolder
		}
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})

	return ListingData{
		Title:        filepath.Base(requestPath),
		PathSegments: buildPathSegments(requestPath),
		Files:        files,
		CurrentPath:  requestPath,
	}
}

func (h *Handler) serveDirectory(c *gin.Context, fullPath, requestPath string) {
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		logger.Log.Error().Err(err).Str("path", fullPath).Msg("Error reading directory")
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	data := h.buildListingData(entries, requestPath)

	c.Header("Content-Type", "text/html")
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		return
	}
	if err := h.tmpl.ExecuteTemplate(c.Writer, "listing.html", data); err != nil {
		logger.Log.Error().Err(err).Str("template", "listing.html").Msg("Error executing template")
		c.AbortWithStatus(http.StatusInternalServerError)
	}
}

func (h *Handler) serveDirectoryFromRoot(c *gin.Context, root *security.RootFS, relPath, requestPath string) {
	// Handle empty or root path cases
	if relPath == "" {
		relPath = "."
	}

	entries, err := root.ReadDir(relPath)
	if err != nil {
		logger.Log.Error().Err(err).Str("path", relPath).Msg("Error reading directory")
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	data := h.buildListingData(entries, requestPath)

	c.Header("Content-Type", "text/html")
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		return
	}
	if err := h.tmpl.ExecuteTemplate(c.Writer, "listing.html", data); err != nil {
		logger.Log.Error().Err(err).Str("template", "listing.html").Msg("Error executing template")
		c.AbortWithStatus(http.StatusInternalServerError)
	}
}

func buildFileURL(basePath, fileName string) string {
	if basePath == "/" {
		return "/" + fileName
	}
	return basePath + "/" + fileName
}

func formatSize(size int64) string {
	units := []struct {
		threshold int64
		unit      string
		divisor   float64
	}{
		{1024 * 1024 * 1024, "GB", 1024 * 1024 * 1024},
		{1024 * 1024, "MB", 1024 * 1024},
		{1024, "KB", 1024},
		{0, "B", 1},
	}

	for _, u := range units {
		if size >= u.threshold {
			if u.unit == "B" {
				return fmt.Sprintf("%d %s", size, u.unit)
			}
			return fmt.Sprintf("%.1f %s", float64(size)/u.divisor, u.unit)
		}
	}
	return fmt.Sprintf("%d B", size)
}

// FileTypeInfo holds both type and icon for a file extension
type FileTypeInfo struct {
	Type string
	Icon string
}

// fileExtMap maps file extensions to their type and icon for special cases
var fileExtMap = map[string]FileTypeInfo{
	// Archives and special files that don't have standard MIME types
	".zip":  {Type: "file", Icon: "archive"},
	".tar":  {Type: "file", Icon: "archive"},
	".gz":   {Type: "file", Icon: "archive"},
	".rar":  {Type: "file", Icon: "archive"},
	".pdf":  {Type: "document", Icon: "file-pdf"},
	".md":   {Type: "document", Icon: "file-text"},
	".doc":  {Type: "document", Icon: "file-text"},
	".docx": {Type: "document", Icon: "file-text"},
	".txt":  {Type: "document", Icon: "file-text"},
}

// getFileTypeFromMime determines file type and icon based on MIME type
func getFileTypeFromMime(mimeType string) (string, string) {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image", "image"
	case strings.HasPrefix(mimeType, "video/"):
		return "video", "video"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio", "audio"
	case strings.HasPrefix(mimeType, "text/") || strings.Contains(mimeType, "document"):
		return "document", "file-text"
	default:
		return "file", "file"
	}
}

func determineFileType(entry os.DirEntry) string {
	if entry.IsDir() {
		return "folder"
	}

	ext := strings.ToLower(filepath.Ext(entry.Name()))
	if info, exists := fileExtMap[ext]; exists {
		return info.Type
	}

	// Use MIME type for common extensions
	mimeType := mime.TypeByExtension(ext)
	if mimeType != "" {
		fileType, _ := getFileTypeFromMime(mimeType)
		return fileType
	}

	return "file"
}

func getFileIcon(entry os.DirEntry) string {
	if entry.IsDir() {
		return "folder"
	}

	ext := strings.ToLower(filepath.Ext(entry.Name()))
	if info, exists := fileExtMap[ext]; exists {
		return info.Icon
	}

	// Use MIME type for common extensions
	mimeType := mime.TypeByExtension(ext)
	if mimeType != "" {
		_, icon := getFileTypeFromMime(mimeType)
		return icon
	}

	return "file"
}

func isImageFile(fileName string) bool {
	ext := strings.ToLower(filepath.Ext(fileName))
	mimeType := mime.TypeByExtension(ext)
	return strings.HasPrefix(mimeType, "image/")
}

func buildThumbnailURL(basePath, fileName string) string {
	fileURL := buildFileURL(basePath, fileName)
	return fileURL + "?thumb=1"
}

func buildPathSegments(requestPath string) []PathSegment {
	var segments []PathSegment

	if requestPath == "/" {
		return segments
	}

	parts := strings.Split(strings.Trim(requestPath, "/"), "/")
	currentPath := ""

	for _, part := range parts {
		if part == "" {
			continue
		}
		currentPath += "/" + part
		segments = append(segments, PathSegment{
			Name: part,
			URL:  currentPath,
		})
	}

	return segments
}

func (h *Handler) serveStaticFile(c *gin.Context, requestPath string) {
	// Remove leading slash and serve from embedded FS
	filePath := strings.TrimPrefix(requestPath, "/")

	fileData, err := web.TemplateFS.ReadFile(filePath)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	// Set appropriate content type based on file extension
	ext := filepath.Ext(filePath)
	switch ext {
	case ".css":
		c.Header("Content-Type", "text/css")
	case ".js":
		c.Header("Content-Type", "application/javascript")
	case ".png":
		c.Header("Content-Type", "image/png")
	case ".jpg", ".jpeg":
		c.Header("Content-Type", "image/jpeg")
	case ".gif":
		c.Header("Content-Type", "image/gif")
	case ".svg":
		c.Header("Content-Type", "image/svg+xml")
	case ".ico":
		c.Header("Content-Type", "image/x-icon")
	default:
		c.Header("Content-Type", "application/octet-stream")
	}

	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		return
	}
	c.Data(http.StatusOK, c.GetHeader("Content-Type"), fileData)
}

// serveFileFromRoot serves a file from RootFS, returns true if successful
func (h *Handler) serveFileFromRoot(c *gin.Context, root *security.RootFS, relPath string) bool {
	file, err := root.Open(relPath)
	if err != nil {
		return false
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return false
	}

	http.ServeContent(c.Writer, c.Request, fileInfo.Name(), fileInfo.ModTime(), file)
	return true
}

// serveThumbnailFromRoot handles thumbnail requests using RootFS
func (h *Handler) serveThumbnailFromRoot(c *gin.Context, relPath string) {
	// Try to find the file in one of the RootFS instances
	for _, root := range h.roots {
		// Check if file exists and is an image
		info, err := root.Stat(relPath)
		if err != nil {
			continue // Try next root
		}

		if info.IsDir() {
			continue // Can't thumbnail a directory
		}

		// Check if it's an image file
		if !isImageFile(filepath.Base(relPath)) {
			// Fallback to serving original file
			if h.serveFileFromRoot(c, root, relPath) {
				return
			}
			continue
		}

		// For thumbnail generation, we still need the full path
		// This is a temporary approach until we refactor the thumbnail subsystem
		fullPath := filepath.Join(root.Path(), relPath)

		// Generate thumbnail with cache size limit
		thumbPath, err := files.GenerateWithCacheLimit(fullPath, 200, h.config.MaxThumbCacheMB, h.config.ThumbJpegQuality)
		if err != nil {
			// If file is too large, return a 413 Payload Too Large status
			if err == files.ErrFileTooLarge {
				c.AbortWithStatus(http.StatusRequestEntityTooLarge)
				return
			}
			// Fallback to serving original file on other errors
			if h.serveFileFromRoot(c, root, relPath) {
				return
			}
			continue
		}

		// Serve the thumbnail
		c.File(thumbPath)
		return
	}

	// File not found in any allowed root
	c.AbortWithStatus(http.StatusNotFound)
}
