package handler

import (
	"context"
	"fmt"
	"html/template"
	"io/fs"
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
	"slimserve/internal/server/filter"
	"slimserve/internal/storage"
	"slimserve/internal/version"
	"slimserve/web"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	config    *config.Config
	tmpl      *template.Template
	backend   storage.Backend
	localRoot *security.RootFS
}

type FileItem struct {
	Name         string `json:"name"`
	URL          string `json:"url"`
	Size         string `json:"size"`
	ModTime      string `json:"mod_time"`
	Type         string `json:"type"`
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

func NewHandler(cfg *config.Config, backend storage.Backend, localRoot *security.RootFS) *Handler {
	tmpl := template.Must(template.ParseFS(web.TemplateFS, "templates/base.html", "templates/listing.html"))

	return &Handler{
		config:    cfg,
		tmpl:      tmpl,
		backend:   backend,
		localRoot: localRoot,
	}
}

func (h *Handler) ServeFiles(c *gin.Context) {
	requestPath := c.Param("path")
	if requestPath == "" {
		requestPath = "/"
	}

	if requestPath == "/" && h.backend != nil {
		h.serveDirectoryFromBackend(c, h.backend, ".", "/")
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
		h.serveThumbnail(c, relPath)
		return
	}

	if h.tryServeFromBackend(c, relPath, cleanPath) {
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

func (h *Handler) tryServeFromBackend(c *gin.Context, relPath, cleanPath string) bool {
	if h.backend == nil {
		return false
	}
	ctx := c.Request.Context()

	if ignored, err := h.backend.IsIgnored(ctx, relPath); err != nil {
		logger.Log.Error().Err(err).Str("path", relPath).Msg("Error checking if path is ignored")
		c.AbortWithStatus(http.StatusInternalServerError)
		return true
	} else if ignored {
		c.AbortWithStatus(http.StatusForbidden)
		return true
	}

	info, err := h.backend.Stat(ctx, relPath)
	if err != nil {
		return false
	}

	if info.IsDir() {
		h.serveDirectoryFromBackend(c, h.backend, relPath, cleanPath)
	} else {
		h.serveFileFromBackend(c, h.backend, relPath)
	}
	return true
}

type entryInterface interface {
	Name() string
	IsDir() bool
	Info() (fs.FileInfo, error)
}

func buildListingData[E entryInterface](
	ctx context.Context,
	entries []E,
	requestPath string,
	isIgnoredFunc func(context.Context, string) (bool, error),
	typeFunc func(E) string,
	iconFunc func(E) string,
) ListingData {
	estimatedFiles := len(entries)
	files := make([]FileItem, 0, estimatedFiles)

	for _, entry := range entries {
		entryRelPath := filepath.Join(strings.TrimPrefix(requestPath, "/"), entry.Name())
		ignored, err := isIgnoredFunc(ctx, entryRelPath)
		if err != nil {
			logger.Log.Debug().Err(err).Str("path", entryRelPath).Msg("Error checking ignore patterns")
		}
		if ignored {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			logger.Log.Debug().Err(err).Str("path", entryRelPath).Msg("Failed to get file info")
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
			Type:     typeFunc(entry),
			Icon:     iconFunc(entry),
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

func determineFileTypeFromEntry(entry *storage.DirEntry) string {
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

func getFileIconFromEntry(entry *storage.DirEntry) string {
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

func (h *Handler) serveDirectoryFromBackend(c *gin.Context, backend storage.Backend, relPath, requestPath string) {
	ctx := c.Request.Context()
	if relPath == "" {
		relPath = "."
	}

	entries, err := backend.ReadDir(ctx, relPath)
	if err != nil {
		logger.Log.Error().Err(err).Str("path", relPath).Msg("Error reading directory")
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	isIgnoredFunc := func(ctx context.Context, entryRelPath string) (bool, error) {
		fullRelPath := filepath.Join(strings.TrimPrefix(requestPath, "/"), entryRelPath)
		if _, ok := backend.(*storage.LocalBackend); ok {
			return filter.IsIgnored(fullRelPath, h.localRoot, h.config)
		}
		return backend.IsIgnored(ctx, fullRelPath)
	}

	data := buildListingData(ctx, entries, requestPath,
		isIgnoredFunc,
		func(e *storage.DirEntry) string { return determineFileTypeFromEntry(e) },
		func(e *storage.DirEntry) string { return getFileIconFromEntry(e) },
	)

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

func (h *Handler) serveFileFromBackend(c *gin.Context, backend storage.Backend, relPath string) bool {
	ctx := c.Request.Context()
	file, err := backend.Open(ctx, relPath)
	if err != nil {
		return false
	}
	defer file.Close()

	info, err := backend.Stat(ctx, relPath)
	if err != nil {
		return false
	}

	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), file)
	return true
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

	data := buildListingData(c.Request.Context(), entries, requestPath,
		func(ctx context.Context, path string) (bool, error) { return filter.IsIgnored(path, root, h.config) },
		determineFileType,
		getFileIcon,
	)

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

type FileTypeInfo struct {
	Type string
	Icon string
}

var fileExtMap = map[string]FileTypeInfo{
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

func (h *Handler) serveThumbnail(c *gin.Context, relPath string) {
	if h.localRoot == nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	info, err := h.localRoot.Stat(relPath)
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	if info.IsDir() {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	if !isImageFile(filepath.Base(relPath)) {
		if h.serveFileFromRoot(c, h.localRoot, relPath) {
			return
		}
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	thumbPath, err := files.GenerateWithCacheLimit(filepath.Join(h.localRoot.Path(), relPath), 250, h.config.MaxThumbCacheMB, h.config.ThumbJpegQuality, h.config.ThumbMaxFileSizeMB)
	if err != nil {
		if err == files.ErrFileTooLarge {
			c.AbortWithStatus(http.StatusRequestEntityTooLarge)
			return
		}
		if h.serveFileFromRoot(c, h.localRoot, relPath) {
			return
		}
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	c.File(thumbPath)
}
