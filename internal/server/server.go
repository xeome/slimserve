package server

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

type Server struct {
	engine       *gin.Engine
	allowedRoots []string
}

func New(allowedRoots []string) *Server {
	gin.SetMode(gin.ReleaseMode)

	engine := gin.New()
	engine.Use(gin.Recovery())

	// Convert to absolute paths for security validation
	absoluteRoots := make([]string, len(allowedRoots))
	for i, root := range allowedRoots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			// If we can't get absolute path, use original
			absRoot = root
		}
		absoluteRoots[i] = absRoot
	}

	srv := &Server{
		engine:       engine,
		allowedRoots: absoluteRoots,
	}

	srv.setupRoutes()
	return srv
}

func (s *Server) setupRoutes() {
	handler := NewHandler(s.allowedRoots)

	// Add debug logging for all requests at engine level
	s.engine.Use(func(c *gin.Context) {
		println("[DEBUG] Router received:", c.Request.URL.Path)
		c.Next()
	})

	// Add access control middleware for file serving (but skip for static assets)
	s.engine.Use(s.accessControlMiddleware())

	// Only one wildcard route: use handler logic to serve static vs dynamic
	s.engine.GET("/*path", handler.ServeFiles)
}

// accessControlMiddleware validates that requested paths are within allowed roots
// and denies access to hidden files/directories
func (s *Server) accessControlMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestedPath := c.Request.URL.Path
		println("[DEBUG] Middleware path:", requestedPath)

		// Skip access control for static assets
		if strings.HasPrefix(requestedPath, "/static/") {
			println("[DEBUG] Middleware: static asset, skipping access control:", requestedPath)
			c.Next()
			return
		}

		// Basic path traversal protection - deny any path containing ".."
		if strings.Contains(requestedPath, "..") {
			println("[DEBUG] Middleware: '..' found, denying.")
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		// Clean the path to resolve any .. or . components
		cleanPath := filepath.Clean(requestedPath)

		// Check for hidden files/directories (components starting with ".")
		pathComponents := strings.Split(strings.Trim(cleanPath, "/"), "/")
		for _, component := range pathComponents {
			if component != "" && strings.HasPrefix(component, ".") {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
		}

		// Check if path is within allowed roots
		pathAllowed := false
		for _, root := range s.allowedRoots {
			// Try to resolve the absolute path for the requested file
			candidatePath := filepath.Join(root, cleanPath)
			absPath, err := filepath.Abs(candidatePath)
			if err != nil {
				continue
			}

			// Ensure root path ends with separator for proper prefix checking
			rootPath := filepath.Clean(root)
			if !strings.HasSuffix(rootPath, string(filepath.Separator)) {
				rootPath += string(filepath.Separator)
			}

			// Check if the absolute path is within the allowed root
			if strings.HasPrefix(absPath+string(filepath.Separator), rootPath) || absPath == filepath.Clean(root) {
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

func (s *Server) Run(addr string) error {
	return s.engine.Run(addr)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.engine.ServeHTTP(w, r)
}
