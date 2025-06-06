package server

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"slimserve/web"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	allowedRoots []string
	tmpl         *template.Template
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

func NewHandler(allowedRoots []string) *Handler {
	tmpl := template.Must(template.ParseFS(web.TemplateFS, "templates/*.html"))

	return &Handler{
		allowedRoots: allowedRoots,
		tmpl:         tmpl,
	}
}

func (h *Handler) ServeFiles(c *gin.Context) {
	requestPath := c.Param("path")
	if requestPath == "" {
		requestPath = "/"
	}
	// DEBUG: Log requestPath every time ServeFiles is called
	println("[DEBUG] ServeFiles requestPath:", requestPath)
	println("[DEBUG] ServeFiles: Entered ServeFiles handler")

	// Handle static assets from embedded FS - bypass all other checks
	if strings.HasPrefix(requestPath, "/static/") {
		println("[DEBUG] Detected static asset, serving from embed FS:", requestPath)
		h.serveStaticFile(c, requestPath)
		return
	}
	println("[DEBUG] ServeFiles: Not a static asset, continuing with regular file serving")

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

	// Check for hidden files/directories (components starting with ".")
	pathComponents := strings.Split(strings.Trim(cleanPath, "/"), "/")
	for _, component := range pathComponents {
		if component != "" && strings.HasPrefix(component, ".") {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
	}

	// Try to find the file in one of the allowed roots
	for _, root := range h.allowedRoots {
		fullPath := filepath.Join(root, cleanPath)

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
	// For now, return empty - will implement thumbnail generation later
	return ""
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
	println("[DEBUG] serveStaticFile: trying to read file:", filePath)

	fileData, err := web.TemplateFS.ReadFile(filePath)
	if err != nil {
		println("[DEBUG] serveStaticFile: error reading file:", err.Error())
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	println("[DEBUG] serveStaticFile: successfully read file, size:", len(fileData))

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

	c.Data(http.StatusOK, c.GetHeader("Content-Type"), fileData)
}
