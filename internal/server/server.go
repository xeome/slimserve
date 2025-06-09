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
	"slimserve/web"

	"github.com/gin-gonic/gin"
)

// Server represents the HTTP server
type Server struct {
	config       *config.Config
	engine       *gin.Engine
	server       *http.Server
	roots        []*security.RootFS
	sessionStore *SessionStore
	loginTmpl    *template.Template
}

// New creates a new server instance with the given configuration
func New(cfg *config.Config) *Server {
	if len(cfg.Directories) == 0 {
		// Default to current directory when none provided
		cfg.Directories = []string{"."}
	}

	// Build RootFS instances from configured directories
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

	// Handlers will load their own templates
	engine.SetHTMLTemplate(template.New(""))

	srv := &Server{
		config:       cfg,
		engine:       engine,
		roots:        roots,
		sessionStore: NewSessionStore(),
		loginTmpl:    loginTmpl,
	}

	srv.setupRoutes()
	return srv
}

// setupRoutes configures the server routes
func (s *Server) setupRoutes() {
	handler := NewHandler(s.config, s.roots)

	// Add logging middleware
	s.engine.Use(logger.Middleware())

	// Add session authentication middleware
	s.engine.Use(SessionAuthMiddleware(s.config, s.sessionStore))

	// Add access control middleware for file serving (but skip for static assets)
	s.engine.Use(s.accessControlMiddleware())

	// Create a wrapper handler that checks for auth routes first
	authHandler := s.createAuthAwareHandler(handler)

	// Single wildcard route that handles both auth and file serving
	s.engine.GET("/*path", authHandler)
	s.engine.POST("/*path", authHandler)
	s.engine.HEAD("/*path", authHandler)
}

// createAuthAwareHandler creates a handler that checks for auth routes before file serving
func (s *Server) createAuthAwareHandler(fileHandler *Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// Handle authentication routes if auth is enabled
		if s.config.EnableAuth {
			switch {
			case path == "/login" && c.Request.Method == "GET":
				s.showLogin(c)
				return
			case path == "/login" && c.Request.Method == "POST":
				s.doLogin(c)
				return
			}
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

		// Skip access control for static assets and login routes
		if strings.HasPrefix(requestedPath, "/static/") || requestedPath == "/login" {
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

		// Path not allowed in any root
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

	// Close all RootFS instances
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
