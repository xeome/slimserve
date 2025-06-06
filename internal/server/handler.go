package server

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"slimserve/internal/files"
	"slimserve/web"

	"slimserve/internal/config"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	config       *config.Config
	tmpl         *template.Template
	allowedRoots []string
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

func NewHandler(cfg *config.Config) *Handler {
	tmpl := template.Must(template.ParseFS(web.TemplateFS, "templates/*.html"))

	return &Handler{
		config:       cfg,
		tmpl:         tmpl,
		allowedRoots: cfg.Directories,
	}
}

func (h *Handler) ServeFiles(c *gin.Context) {
	requestPath := c.Param("path")
	if requestPath == "" {
		requestPath = "/"
	}
	// If root is requested, serve directory listing of first allowed root directly
	if requestPath == "/" {
		if len(h.allowedRoots) > 0 {
			h.serveDirectory(c, h.allowedRoots[0], "/")
			return
		}
	}
	// Handle static assets from embedded FS - bypass all other checks
	if strings.HasPrefix(requestPath, "/static/") {
		h.serveStaticFile(c, requestPath)
		return
	}

	// Basic path traversal protection - deny any path containing ".."
	if strings.Contains(requestPath, "..") {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	// Clean and join with root directory
	cleanPath := filepath.Clean(requestPath)
	if cleanPath == "." {
		cleanPath = "/"
	}
	// Map absolute URL path to relative filesystem path
	relPath := strings.TrimPrefix(cleanPath, "/")

	// Check for hidden files/directories (components starting with ".")
	pathComponents := strings.Split(strings.Trim(cleanPath, "/"), "/")
	for _, component := range pathComponents {
		if component != "" && strings.HasPrefix(component, ".") {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
	}

	// Check for thumbnail request
	if c.Query("thumb") == "1" {
		h.serveThumbnail(c, cleanPath)
		return
	}

	// Try to find the file in one of the allowed roots
	for _, root := range h.allowedRoots {
		fullPath := filepath.Join(root, relPath)

		// Additional security check - ensure resolved path is within allowed root
		absPath, err := filepath.Abs(fullPath)
		if err != nil {
			continue
		}

		rootPath := filepath.Clean(root)
		if !strings.HasSuffix(rootPath, string(filepath.Separator)) {
			rootPath += string(filepath.Separator)
		}

		if !strings.HasPrefix(absPath+string(filepath.Separator), rootPath) && absPath != filepath.Clean(root) {
			continue // Path is outside allowed root
		}

		// Check if file/directory exists
		info, err := os.Stat(fullPath)
		if err != nil {
			continue // Try next root
		}

		// If it's a file, serve it
		if !info.IsDir() {
			c.File(fullPath)
			return
		}

		// If it's a directory, show listing
		h.serveDirectory(c, fullPath, cleanPath)
		return
	}

	// File not found in any allowed root
	c.AbortWithStatus(http.StatusNotFound)
}

func (h *Handler) serveDirectory(c *gin.Context, fullPath, requestPath string) {
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	var files []FileItem
	for _, entry := range entries {
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

	data := ListingData{
		Title:        filepath.Base(requestPath),
		PathSegments: buildPathSegments(requestPath),
		Files:        files,
		CurrentPath:  requestPath,
	}

	c.Header("Content-Type", "text/html")
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		return
	}
	if err := h.tmpl.ExecuteTemplate(c.Writer, "listing.html", data); err != nil {
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
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	} else if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	} else if size < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	}
	return fmt.Sprintf("%.1f GB", float64(size)/(1024*1024*1024))
}

func determineFileType(entry os.DirEntry) string {
	if entry.IsDir() {
		return "folder"
	}

	ext := strings.ToLower(filepath.Ext(entry.Name()))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg":
		return "image"
	case ".mp4", ".avi", ".mkv", ".mov", ".webm":
		return "video"
	case ".mp3", ".wav", ".flac", ".ogg":
		return "audio"
	case ".pdf", ".doc", ".docx", ".txt", ".md":
		return "document"
	default:
		return "file"
	}
}

func getFileIcon(entry os.DirEntry) string {
	if entry.IsDir() {
		return "folder"
	}

	ext := strings.ToLower(filepath.Ext(entry.Name()))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg":
		return "image"
	case ".mp4", ".avi", ".mkv", ".mov", ".webm":
		return "video"
	case ".mp3", ".wav", ".flac", ".ogg":
		return "audio"
	case ".pdf":
		return "file-pdf"
	case ".doc", ".docx":
		return "file-text"
	case ".txt", ".md":
		return "file-text"
	case ".zip", ".tar", ".gz", ".rar":
		return "archive"
	default:
		return "file"
	}
}

func isImageFile(fileName string) bool {
	ext := strings.ToLower(filepath.Ext(fileName))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg":
		return true
	default:
		return false
	}
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

// serveThumbnail handles thumbnail requests
func (h *Handler) serveThumbnail(c *gin.Context, requestPath string) {
	// Try to find the file in one of the allowed roots
	for _, root := range h.allowedRoots {
		fullPath := filepath.Join(root, requestPath)

		// Additional security check - ensure resolved path is within allowed root
		absPath, err := filepath.Abs(fullPath)
		if err != nil {
			continue
		}

		rootPath := filepath.Clean(root)
		if !strings.HasSuffix(rootPath, string(filepath.Separator)) {
			rootPath += string(filepath.Separator)
		}

		if !strings.HasPrefix(absPath+string(filepath.Separator), rootPath) && absPath != filepath.Clean(root) {
			continue // Path is outside allowed root
		}

		// Check if file exists and is an image
		info, err := os.Stat(fullPath)
		if err != nil {
			continue // Try next root
		}

		if info.IsDir() {
			continue // Can't thumbnail a directory
		}

		// Check if it's an image file
		if !isImageFile(filepath.Base(fullPath)) {
			// Fallback to serving original file
			c.File(fullPath)
			return
		}

		// Generate thumbnail
		thumbPath, err := files.Generate(fullPath, 200)
		if err != nil {
			// Fallback to serving original file on error
			c.File(fullPath)
			return
		}

		// Serve the thumbnail
		c.File(thumbPath)
		return
	}

	// File not found in any allowed root
	c.AbortWithStatus(http.StatusNotFound)
}
