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
	"slimserve/internal/version"
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
	Version      string        `json:"version,omitempty"`
	VersionInfo  version.Info  `json:"version_info,omitempty"`
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

	if requestPath == "/" && len(h.roots) > 0 {
		h.serveDirectoryFromRoot(c, h.roots[0], ".", "/")
		return
	}

	if strings.HasPrefix(requestPath, "/static/") {
		h.serveStaticFile(c, requestPath)
		return
	}

	cleanPath := filepath.Clean(requestPath)
	if cleanPath == "." {
		cleanPath = "/"
	}
	relPath := strings.TrimPrefix(cleanPath, "/")

	if h.config.DisableDotFiles && h.containsDotFile(cleanPath) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	if c.Query("thumb") == "1" {
		h.serveThumbnailFromRoot(c, relPath)
		return
	}

	if h.tryServeFromRoots(c, relPath, cleanPath) {
		return
	}

	c.AbortWithStatus(http.StatusNotFound)
}

func (h *Handler) containsDotFile(path string) bool {
	pathComponents := strings.Split(strings.Trim(path, "/"), "/")
	for _, component := range pathComponents {
		if component != "" && strings.HasPrefix(component, ".") {
			return true
		}
	}
	return false
}

func (h *Handler) tryServeFromRoots(c *gin.Context, relPath, cleanPath string) bool {
	for _, root := range h.roots {
		if ignored, err := isIgnored(relPath, root, h.config); err != nil {
			logger.Log.Error().Err(err).Str("path", relPath).Msg("Error checking if path is ignored")
			c.AbortWithStatus(http.StatusInternalServerError)
			return true
		} else if ignored {
			c.AbortWithStatus(http.StatusForbidden)
			return true
		}

		info, err := root.Stat(relPath)
		if err != nil {
			continue
		}

		if info.IsDir() {
			h.serveDirectoryFromRoot(c, root, relPath, cleanPath)
		} else {
			h.serveFileFromRoot(c, root, relPath)
		}
		return true
	}
	return false
}

func (h *Handler) buildListingData(root *security.RootFS, entries []os.DirEntry, requestPath string) ListingData {
	estimatedFiles := len(entries)
	if h.config.DisableDotFiles {
		estimatedFiles = int(float64(len(entries)) * 0.9)
	}
	files := make([]FileItem, 0, estimatedFiles)

	for _, entry := range entries {
		if h.config.DisableDotFiles && strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		entryRelPath := filepath.Join(strings.TrimPrefix(requestPath, "/"), entry.Name())

		ignored, err := isIgnored(entryRelPath, root, h.config)
		if err != nil {
			logger.Log.Error().Err(err).Str("path", entryRelPath).Msg("Error checking if path is ignored")
			continue
		}
		if ignored {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		fileName := entry.Name()
		isDir := entry.IsDir()
		isImage := !isDir && isImageFile(fileName)

		fileItem := FileItem{
			Name:     fileName,
			URL:      buildFileURL(requestPath, fileName),
			Size:     formatSize(info.Size()),
			ModTime:  info.ModTime().Format("Jan 2, 2006 15:04"),
			Type:     determineFileType(entry),
			Icon:     getFileIcon(entry),
			IsImage:  isImage,
			IsFolder: isDir,
		}

		if isImage {
			fileItem.ThumbnailURL = buildThumbnailURL(requestPath, fileName)
		}

		files = append(files, fileItem)
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].IsFolder != files[j].IsFolder {
			return files[i].IsFolder
		}
		return files[i].Name < files[j].Name
	})

	return ListingData{
		Title:        filepath.Base(requestPath),
		PathSegments: buildPathSegments(requestPath),
		Files:        files,
		CurrentPath:  requestPath,
		Version:      version.GetShort(),
		VersionInfo:  version.Get(),
	}
}

func (h *Handler) serveDirectoryFromRoot(c *gin.Context, root *security.RootFS, relPath, requestPath string) {
	if relPath == "" {
		relPath = "."
	}

	entries, err := root.ReadDir(relPath)
	if err != nil {
		logger.Log.Error().Err(err).Str("path", relPath).Msg("Error reading directory")
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	data := h.buildListingData(root, entries, requestPath)

	c.Header("Content-Type", "text/html")
	c.Status(http.StatusOK)
	if c.Request.Method == http.MethodHead {
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

// Pre-defined units to avoid slice allocation on each call
var sizeUnits = []struct {
	threshold int64
	unit      string
	divisor   float64
}{
	{1024 * 1024 * 1024, "GB", 1024 * 1024 * 1024},
	{1024 * 1024, "MB", 1024 * 1024},
	{1024, "KB", 1024},
	{0, "B", 1},
}

func formatSize(size int64) string {
	for _, u := range sizeUnits {
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
	if basePath == "/" {
		return "/" + fileName + "?thumb=1"
	}
	return basePath + "/" + fileName + "?thumb=1"
}

func buildPathSegments(requestPath string) []PathSegment {
	if requestPath == "/" {
		return nil
	}

	parts := strings.Split(strings.Trim(requestPath, "/"), "/")
	segments := make([]PathSegment, 0, len(parts))

	var pathBuilder strings.Builder
	pathBuilder.Grow(len(requestPath))

	for _, part := range parts {
		if part == "" {
			continue
		}
		pathBuilder.WriteByte('/')
		pathBuilder.WriteString(part)

		segments = append(segments, PathSegment{
			Name: part,
			URL:  pathBuilder.String(),
		})
	}

	return segments
}

func (h *Handler) serveStaticFile(c *gin.Context, requestPath string) {
	filePath := strings.TrimPrefix(requestPath, "/")

	fileData, err := web.TemplateFS.ReadFile(filePath)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

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

func (h *Handler) serveThumbnailFromRoot(c *gin.Context, relPath string) {
	for _, root := range h.roots {
		info, err := root.Stat(relPath)
		if err != nil {
			continue
		}

		if info.IsDir() {
			continue
		}

		if !isImageFile(filepath.Base(relPath)) {
			if h.serveFileFromRoot(c, root, relPath) {
				return
			}
			continue
		}

		fullPath := filepath.Join(root.Path(), relPath)

		thumbPath, err := files.GenerateWithCacheLimit(fullPath, 250, h.config.MaxThumbCacheMB, h.config.ThumbJpegQuality, h.config.ThumbMaxFileSizeMB)
		if err != nil {
			if err == files.ErrFileTooLarge {
				c.AbortWithStatus(http.StatusRequestEntityTooLarge)
				return
			}
			if h.serveFileFromRoot(c, root, relPath) {
				return
			}
			continue
		}

		c.File(thumbPath)
		return
	}

	c.AbortWithStatus(http.StatusNotFound)
}
