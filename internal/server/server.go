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
	"slimserve/internal/version"
	"slimserve/web"

	"github.com/gin-gonic/gin"
)

// Server represents the HTTP server
type Server struct {
	config         *config.Config
	engine         *gin.Engine
	server         *http.Server
	roots          []*security.RootFS
	sessionStore   *SessionStore
	loginTmpl      *template.Template
	adminLoginTmpl *template.Template
	adminTmpl      *template.Template
	uploadManager  *UploadManager
	adminHandler   *AdminHandler
	adminUtils     *AdminUtils
}

// New creates a new server instance with the given configuration
func New(cfg *config.Config) *Server {
	if len(cfg.Directories) == 0 {
		cfg.Directories = []string{"."}
	}

	var roots []*security.RootFS
	for _, dir := range cfg.Directories {
		root, err := security.NewRootFS(dir)
		if err != nil {
			logger.Log.Warn().Err(err).Str("directory", dir).Msg("Failed to create RootFS for directory")
			continue
		}
		roots = append(roots, root)
	}

	gin.SetMode(gin.ReleaseMode)

	engine := gin.New()
	engine.Use(gin.Recovery())

	// Load login template separately to avoid conflicts
	loginTmpl := template.Must(template.ParseFS(web.TemplateFS, "templates/base.html", "templates/login.html"))

	// Load admin templates if admin is enabled
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

	// Handlers will load their own templates
	engine.SetHTMLTemplate(template.New(""))

	srv := &Server{
		config:         cfg,
		engine:         engine,
		roots:          roots,
		sessionStore:   NewSessionStore(),
		loginTmpl:      loginTmpl,
		adminLoginTmpl: adminLoginTmpl,
		adminTmpl:      adminTmpl,
		uploadManager:  NewUploadManager(cfg.MaxConcurrentUploads),
		adminUtils:     NewAdminUtils(),
	}

	// Initialize admin handler if admin is enabled
	if cfg.EnableAdmin {
		srv.adminHandler = NewAdminHandler(srv)
	}

	srv.setupRoutes()
	return srv
}

// handleAdminRoute handles all admin routes in a single method
func (s *Server) handleAdminRoute(c *gin.Context, path, method string) {
	// Admin login routes (no auth required)
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

	// Apply admin authentication middleware for all other admin routes
	adminAuth := AdminAuthMiddleware(s.config, s.sessionStore)
	adminAuth(c)
	if c.IsAborted() {
		return
	}

	// Apply admin security middleware
	rateLimit := AdminRateLimitMiddleware()
	rateLimit(c)
	if c.IsAborted() {
		return
	}

	inputValidation := InputValidationMiddleware()
	inputValidation(c)
	if c.IsAborted() {
		return
	}

	csrfProtection := CSRFProtectionMiddleware()
	csrfProtection(c)
	if c.IsAborted() {
		return
	}

	// Handle protected admin routes
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

// createUnifiedHandler creates a single handler that routes all requests appropriately
func (s *Server) createUnifiedHandler(fileHandler *Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		method := c.Request.Method

		// Handle static files first (no auth needed)
		if strings.HasPrefix(path, "/static/") || path == "/favicon.ico" {
			// Set the path parameter for the file handler
			c.Params = gin.Params{{Key: "path", Value: path}}
			fileHandler.ServeFiles(c)
			return
		}

		// Handle version endpoint (no auth needed)
		if path == "/version" && (method == "GET" || method == "HEAD") {
			s.handleVersion(c)
			return
		}

		// Handle admin routes if admin is enabled
		if s.config.EnableAdmin && strings.HasPrefix(path, "/admin") {
			s.handleAdminRoute(c, path, method)
			return
		}

		// Apply session auth middleware for file serving routes
		sessionAuth := SessionAuthMiddleware(s.config, s.sessionStore)
		sessionAuth(c)
		if c.IsAborted() {
			return
		}

		// Apply access control middleware
		accessControl := s.accessControlMiddleware()
		accessControl(c)
		if c.IsAborted() {
			return
		}

		// Handle authentication routes if auth is enabled
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

		// Handle file serving for all other requests
		// Set the path parameter for the file handler
		c.Params = gin.Params{{Key: "path", Value: path}}
		fileHandler.ServeFiles(c)
	}
}

// conditionalAuthMiddleware applies session auth middleware only to non-admin and non-static routes
func (s *Server) conditionalAuthMiddleware() gin.HandlerFunc {
	sessionAuth := SessionAuthMiddleware(s.config, s.sessionStore)
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		// Skip session auth for admin routes (admin has its own auth)
		if strings.HasPrefix(path, "/admin") {
			c.Next()
			return
		}
		// Skip session auth for static routes
		if strings.HasPrefix(path, "/static/") || path == "/favicon.ico" {
			c.Next()
			return
		}
		// Apply session auth for all other routes
		sessionAuth(c)
	}
}

// setupRoutes configures the server routes
func (s *Server) setupRoutes() {
	handler := NewHandler(s.config, s.roots)

	s.engine.Use(logger.Middleware())

	// Create a unified handler that routes all requests appropriately
	unifiedHandler := s.createUnifiedHandler(handler)

	// Use NoRoute to handle all requests
	s.engine.NoRoute(unifiedHandler)
}

// createAuthAwareHandler creates a handler that checks for auth routes before file serving
func (s *Server) createAuthAwareHandler(fileHandler *Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// Handle authentication routes if auth is enabled
		if s.config.EnableAuth {
			switch {
			case path == "/login" && (c.Request.Method == "GET" || c.Request.Method == "HEAD"):
				s.showLogin(c)
				return
			case path == "/login" && c.Request.Method == "POST":
				s.doLogin(c)
				return
			}
		}

		// Admin routes should not reach here since they're handled by specific routes
		// If an admin route reaches here, it means it's not found, so return 404
		if strings.HasPrefix(path, "/admin") {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		// Handle file serving for all other requests
		fileHandler.ServeFiles(c)
	}
}

// accessControlMiddleware validates that requested paths are within allowed roots
// and denies access to hidden files/directories
func (s *Server) accessControlMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestedPath := c.Request.URL.Path

		// Skip access control for static assets, login routes, and admin routes
		if strings.HasPrefix(requestedPath, "/static/") ||
			requestedPath == "/login" ||
			strings.HasPrefix(requestedPath, "/admin") {
			c.Next()
			return
		}

		// Basic path traversal protection - deny any path containing ".."
		if strings.Contains(requestedPath, "..") {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		// Clean the path to resolve any .. or . components
		cleanPath := filepath.Clean(requestedPath)
		// Convert absolute URL path to relative filesystem path
		relPath := strings.TrimPrefix(cleanPath, "/")

		// Check for hidden files/directories (components starting with ".") - configurable
		if s.config.DisableDotFiles {
			pathComponents := strings.Split(strings.Trim(cleanPath, "/"), "/")
			for _, component := range pathComponents {
				if component != "" && strings.HasPrefix(component, ".") {
					c.AbortWithStatus(http.StatusForbidden)
					return
				}
			}
		}

		// Check if path is within allowed roots
		for _, root := range s.config.Directories {
			// Try to resolve the absolute path for the requested file
			candidatePath := filepath.Join(root, relPath)
			absPath, err := filepath.Abs(candidatePath)
			if err != nil {
				continue
			}

			// Get absolute root path
			absRoot, err := filepath.Abs(root)
			if err != nil {
				continue
			}

			// Ensure root path ends with separator for proper prefix checking
			rootPath := filepath.Clean(absRoot)
			if !strings.HasSuffix(rootPath, string(filepath.Separator)) {
				rootPath += string(filepath.Separator)
			}

			// Check if the absolute path is within the allowed root
			if strings.HasPrefix(absPath+string(filepath.Separator), rootPath) || absPath == filepath.Clean(absRoot) {
				c.Next()
				return
			}
		}

		c.AbortWithStatus(http.StatusForbidden)
	}
}

// Run starts the HTTP server on the specified address
func (s *Server) Run(addr string) error {
	s.server = &http.Server{
		Addr:    addr,
		Handler: s.engine,
	}
	return s.server.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}

	for _, root := range s.roots {
		if err := root.Close(); err != nil {
			logger.Log.Warn().Err(err).Msg("Failed to close RootFS")
		}
	}

	return s.server.Shutdown(ctx)
}

// GetEngine returns the Gin engine (for testing)
func (s *Server) GetEngine() *gin.Engine {
	return s.engine
}

// ServeHTTP implements http.Handler interface
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.engine.ServeHTTP(w, r)
}

// handleVersion returns version information as JSON
func (s *Server) handleVersion(c *gin.Context) {
	versionInfo := version.Get()
	c.JSON(http.StatusOK, versionInfo)
}

// addVersionToTemplateData adds version information to template data
func (s *Server) addVersionToTemplateData(data gin.H) gin.H {
	if data == nil {
		data = gin.H{}
	}
	data["Version"] = version.GetShort()
	data["VersionInfo"] = version.Get()
	return data
}
