package server

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"slimserve/internal/config"
	"slimserve/internal/logger"
	"slimserve/internal/security"

	"github.com/gin-gonic/gin"
)

// Server represents the HTTP server
type Server struct {
	config *config.Config
	engine *gin.Engine
	server *http.Server
	roots  []*security.RootFS
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

	srv := &Server{
		config: cfg,
		engine: engine,
		roots:  roots,
	}

	srv.setupRoutes()
	return srv
}

// setupRoutes configures the server routes
func (s *Server) setupRoutes() {
	handler := NewHandler(s.config, s.roots)

	// Add logging middleware
	s.engine.Use(logger.Middleware())

	// Add access control middleware for file serving (but skip for static assets)
	s.engine.Use(s.accessControlMiddleware())

	// Only one wildcard route: use handler logic to serve static vs dynamic
	s.engine.GET("/*path", handler.ServeFiles)
	s.engine.HEAD("/*path", handler.ServeFiles)
}

// accessControlMiddleware validates that requested paths are within allowed roots
// and denies access to hidden files/directories
func (s *Server) accessControlMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestedPath := c.Request.URL.Path

		// Skip access control for static assets
		if strings.HasPrefix(requestedPath, "/static/") {
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
		pathAllowed := false
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
				pathAllowed = true
				break
			}
		}

		if !pathAllowed {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		c.Next()
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

// Start starts the HTTP server using config host and port (for backward compatibility)
func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	return s.Run(addr)
}

// Stop gracefully stops the HTTP server
func (s *Server) Stop(ctx context.Context) error {
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
