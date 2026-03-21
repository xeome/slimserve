package server

import (
	"context"
	"html/template"
	"net/http"
	"path/filepath"
	"strings"

	"slimserve/internal/config"
	"slimserve/internal/logger"
	"slimserve/internal/security"
	"slimserve/internal/server/admin"
	"slimserve/internal/server/auth"
	"slimserve/internal/server/filter"
	"slimserve/internal/server/handler"
	"slimserve/internal/storage"
	"slimserve/internal/version"
	"slimserve/web"

	"github.com/gin-gonic/gin"
)

type Server struct {
	config         *config.Config
	engine         *gin.Engine
	server         *http.Server
	backend        storage.Backend
	localRoot      *security.RootFS
	sessionStore   *auth.SessionStore
	loginTmpl      *template.Template
	adminLoginTmpl *template.Template
	adminTmpl      *template.Template
	uploadManager  *admin.UploadManager
	adminHandler   *AdminHandler
	adminUtils     *admin.Utils
}

func New(cfg *config.Config) *Server {
	storageDir := cfg.GetStorageDir()

	var backend storage.Backend
	var localRoot *security.RootFS

	if storageDir.IsS3() {
		cacheBytes := int64(0)
		if cfg.LRUEnabled && cfg.LRUMaxMB > 0 {
			cacheBytes = int64(cfg.LRUMaxMB) * 1024 * 1024
		}
		s3Backend, err := storage.NewS3Backend(&storageDir, cacheBytes, cfg.IgnorePatterns)
		if err != nil {
			logger.Log.Warn().Err(err).Str("bucket", storageDir.Path).Msg("Failed to create S3 backend")
		} else {
			backend = s3Backend
		}
	} else {
		root, err := security.NewRootFS(storageDir.Path)
		if err != nil {
			logger.Log.Warn().Err(err).Str("directory", storageDir.Path).Msg("Failed to create RootFS for directory")
		} else {
			localRoot = root
			backend = storage.NewLocalBackend(root, cfg.IgnorePatterns)
		}
	}

	gin.SetMode(gin.ReleaseMode)

	engine := gin.New()
	engine.Use(gin.Recovery())

	loginTmpl := template.Must(template.ParseFS(web.TemplateFS, "templates/base.html", "templates/login.html"))

	var adminLoginTmpl, adminTmpl *template.Template
	if cfg.EnableAdmin {
		adminLoginTmpl = template.Must(template.ParseFS(web.TemplateFS, "templates/admin_login.html"))
		adminTmpl = template.Must(template.ParseFS(web.TemplateFS,
			"templates/admin_base.html",
			"templates/admin_components.html",
			"templates/admin_dashboard.html",
			"templates/admin_upload.html",
			"templates/admin_files.html",
			"templates/admin_config.html",
			"templates/admin_status.html"))
	}

	srv := &Server{
		config:         cfg,
		engine:         engine,
		backend:        backend,
		localRoot:      localRoot,
		sessionStore:   auth.NewSessionStore(),
		loginTmpl:      loginTmpl,
		adminLoginTmpl: adminLoginTmpl,
		adminTmpl:      adminTmpl,
		uploadManager:  admin.NewUploadManager(cfg.MaxConcurrentUploads),
		adminUtils:     admin.NewUtils(),
	}

	if cfg.EnableAdmin {
		srv.adminHandler = NewAdminHandler(srv)
	}

	srv.setupRoutes()
	return srv
}

func (s *Server) applyAdminMiddleware(c *gin.Context) bool {
	adminAuth := admin.AdminAuthMiddleware(s.config, s.sessionStore)
	adminAuth(c)
	if c.IsAborted() {
		return false
	}

	rateLimit := admin.AdminRateLimitMiddleware()
	rateLimit(c)
	if c.IsAborted() {
		return false
	}

	inputValidation := admin.InputValidationMiddleware()
	inputValidation(c)
	if c.IsAborted() {
		return false
	}

	csrfProtection := admin.CSRFProtectionMiddleware()
	csrfProtection(c)
	if c.IsAborted() {
		return false
	}

	return true
}

func (s *Server) handleAdminRoute(c *gin.Context, path, method string) {
	switch {
	case path == "/admin/login" && (method == "GET" || method == "HEAD"):
		s.showAdminLogin(c)
		return
	case path == "/admin/login" && method == "POST":
		s.doAdminLogin(c)
		return
	case path == "/admin/logout" && method == "POST":
		s.doAdminLogout(c)
		return
	}

	if !s.applyAdminMiddleware(c) {
		return
	}

	switch {
	case path == "/admin" && (method == "GET" || method == "HEAD"):
		s.showAdminDashboard(c)
	case path == "/admin/" && (method == "GET" || method == "HEAD"):
		s.showAdminDashboard(c)
	case path == "/admin/upload" && (method == "GET" || method == "HEAD"):
		s.showAdminUpload(c)
	case path == "/admin/files" && (method == "GET" || method == "HEAD"):
		s.showAdminFiles(c)
	case path == "/admin/config" && (method == "GET" || method == "HEAD"):
		s.showAdminConfig(c)
	case path == "/admin/status" && (method == "GET" || method == "HEAD"):
		s.showAdminStatus(c)
	case path == "/admin/api/stats" && (method == "GET" || method == "HEAD"):
		s.adminHandler.getSystemStats(c)
	case path == "/admin/api/status" && (method == "GET" || method == "HEAD"):
		s.adminHandler.getSystemStatus(c)
	case path == "/admin/api/activity" && (method == "GET" || method == "HEAD"):
		s.adminHandler.getRecentActivity(c)
	case path == "/admin/api/config" && (method == "GET" || method == "HEAD"):
		s.adminHandler.getConfiguration(c)
	case path == "/admin/api/config" && method == "POST":
		s.adminHandler.updateConfiguration(c)
	case path == "/admin/api/auth" && (method == "GET" || method == "HEAD"):
		s.adminHandler.getAuthConfig(c)
	case path == "/admin/api/auth" && method == "POST":
		s.adminHandler.updateAuthConfig(c)
	case path == "/admin/api/files" && (method == "GET" || method == "HEAD"):
		s.adminHandler.listFiles(c)
	case path == "/admin/api/files/delete" && method == "POST":
		s.adminHandler.deleteFile(c)
	case path == "/admin/api/files/mkdir" && method == "POST":
		s.adminHandler.createDirectory(c)
	case path == "/admin/api/upload" && method == "POST":
		s.handleFileUpload(c)
	case path == "/admin/api/upload/progress" && (method == "GET" || method == "HEAD"):
		s.getUploadProgress(c)
	default:
		c.AbortWithStatus(http.StatusNotFound)
	}
}

func (s *Server) createUnifiedHandler(fileHandler *handler.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		method := c.Request.Method

		if strings.HasPrefix(path, "/static/") || path == "/favicon.ico" {
			c.Params = gin.Params{{Key: "path", Value: path}}
			fileHandler.ServeFiles(c)
			return
		}

		if path == "/version" && (method == "GET" || method == "HEAD") {
			s.handleVersion(c)
			return
		}

		if s.config.EnableAdmin && strings.HasPrefix(path, "/admin") {
			s.handleAdminRoute(c, path, method)
			return
		}

		sessionAuth := auth.SessionAuthMiddleware(s.config, s.sessionStore)
		sessionAuth(c)
		if c.IsAborted() {
			return
		}

		accessControl := s.accessControlMiddleware()
		accessControl(c)
		if c.IsAborted() {
			return
		}

		if s.config.EnableAuth {
			switch {
			case path == "/login" && (method == "GET" || method == "HEAD"):
				s.showLogin(c)
				return
			case path == "/login" && method == "POST":
				s.doLogin(c)
				return
			}
		}

		c.Params = gin.Params{{Key: "path", Value: path}}
		fileHandler.ServeFiles(c)
	}
}

func (s *Server) setupRoutes() {
	fileHandler := handler.NewHandler(s.config, s.backend, s.localRoot)

	s.engine.Use(logger.Middleware())

	unifiedHandler := s.createUnifiedHandler(fileHandler)

	s.engine.NoRoute(unifiedHandler)
}

func (s *Server) accessControlMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestedPath := c.Request.URL.Path

		if strings.HasPrefix(requestedPath, "/static/") ||
			requestedPath == "/login" ||
			strings.HasPrefix(requestedPath, "/admin") {
			c.Next()
			return
		}

		if strings.Contains(requestedPath, "..") {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		cleanPath := filepath.Clean(requestedPath)
		relPath := strings.TrimPrefix(cleanPath, "/")

		if s.config.DisableDotFiles {
			pathComponents := strings.Split(strings.Trim(cleanPath, "/"), "/")
			for _, component := range pathComponents {
				if component != "" && strings.HasPrefix(component, ".") {
					c.AbortWithStatus(http.StatusForbidden)
					return
				}
			}
		}

		storageDir := s.config.GetStorageDir()
		if storageDir.IsS3() {
			c.Next()
			return
		}

		candidatePath := filepath.Join(storageDir.Path, relPath)
		absPath, err := filepath.Abs(candidatePath)
		if err != nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		absRoot, err := filepath.Abs(storageDir.Path)
		if err != nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		rootPath := filepath.Join(absRoot, "")

		if strings.HasPrefix(absPath+string(filepath.Separator), rootPath) || absPath == filepath.Clean(absRoot) {
			c.Next()
			return
		}

		c.AbortWithStatus(http.StatusForbidden)
	}
}

func (s *Server) Run(addr string) error {
	s.server = &http.Server{
		Addr:    addr,
		Handler: s.engine,
	}
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}

	if s.localRoot != nil {
		if err := s.localRoot.Close(); err != nil {
			logger.Log.Warn().Err(err).Msg("Failed to close RootFS")
		}
	}

	return s.server.Shutdown(ctx)
}

func (s *Server) GetEngine() *gin.Engine {
	return s.engine
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.engine.ServeHTTP(w, r)
}

func (s *Server) handleVersion(c *gin.Context) {
	versionInfo := version.Get()
	c.JSON(http.StatusOK, versionInfo)
}

func (s *Server) addVersionToTemplateData(data gin.H) gin.H {
	if data == nil {
		data = gin.H{}
	}
	data["Version"] = version.GetShort()
	data["VersionInfo"] = version.Get()
	return data
}

func isIgnored(relPath string, root *security.RootFS, cfg *config.Config) (bool, error) {
	return filter.IsIgnored(relPath, root, cfg)
}
