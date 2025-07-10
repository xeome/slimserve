package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slimserve/internal/config"
	"testing"

	"github.com/gin-gonic/gin"
)

// setupBenchmarkServer creates a server instance for benchmarking
func setupBenchmarkServer(b *testing.B) *Server {
	testDir := b.TempDir()

	// Create test files
	for i := 0; i < 10; i++ {
		filePath := filepath.Join(testDir, fmt.Sprintf("file_%d.txt", i))
		content := fmt.Sprintf("Test file content %d", i)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			b.Fatalf("Failed to create test file: %v", err)
		}
	}

	cfg := &config.Config{
		Directories:     []string{testDir},
		DisableDotFiles: true,
		EnableAuth:      false, // Disable auth to avoid template issues in benchmarks
		EnableAdmin:     false, // Disable admin to avoid template issues in benchmarks
	}

	// Use the proper server constructor to avoid template issues
	server := New(cfg)
	gin.SetMode(gin.TestMode)

	return server
}

// BenchmarkAccessControlMiddleware benchmarks the access control middleware
func BenchmarkAccessControlMiddleware(b *testing.B) {
	server := setupBenchmarkServer(b)
	middleware := server.accessControlMiddleware()

	testPaths := []string{
		"/file_0.txt",
		"/static/style.css",
		"/admin/dashboard",
		"/login",
		"/nonexistent/file.txt",
		"/path/with/../traversal",
		"/very/deep/nested/path/file.txt",
	}

	for _, path := range testPaths {
		b.Run(fmt.Sprintf("path_%s", filepath.Base(path)), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest("GET", path, nil)

				middleware(c)
			}
		})
	}
}

// BenchmarkSessionAuthMiddleware benchmarks the session authentication middleware
func BenchmarkSessionAuthMiddleware(b *testing.B) {
	server := setupBenchmarkServer(b)
	middleware := SessionAuthMiddleware(server.config, server.sessionStore)

	// Test scenarios: with and without valid session
	scenarios := []struct {
		name        string
		hasSession  bool
		sessionData map[string]interface{}
	}{
		{"no_session", false, nil},
		{"valid_session", true, map[string]interface{}{"authenticated": true}},
		{"invalid_session", true, map[string]interface{}{"authenticated": false}},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest("GET", "/file_0.txt", nil)

				if scenario.hasSession {
					// Create a session token for testing
					token := server.sessionStore.NewToken()
					if authenticated, ok := scenario.sessionData["authenticated"].(bool); ok && authenticated {
						server.sessionStore.Add(token)
						// Add session cookie to request
						c.Request.AddCookie(&http.Cookie{
							Name:  "slimserve_session",
							Value: token,
						})
					}
				}

				middleware(c)
			}
		})
	}
}

// BenchmarkCreateUnifiedHandler benchmarks the unified request handler
func BenchmarkCreateUnifiedHandler(b *testing.B) {
	server := setupBenchmarkServer(b)
	handler := NewHandler(server.config, server.roots)
	unifiedHandler := server.createUnifiedHandler(handler)

	testRequests := []struct {
		method string
		path   string
	}{
		{"GET", "/file_0.txt"},
		{"GET", "/static/style.css"},
		{"GET", "/admin/dashboard"},
		{"GET", "/login"},
		{"POST", "/login"},
		{"GET", "/version"},
		{"HEAD", "/file_0.txt"},
		{"GET", "/nonexistent.txt"},
	}

	for _, req := range testRequests {
		b.Run(fmt.Sprintf("%s_%s", req.method, filepath.Base(req.path)), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(req.method, req.path, nil)

				unifiedHandler(c)
			}
		})
	}
}

// BenchmarkPathValidation benchmarks path validation and cleaning operations
func BenchmarkPathValidation(b *testing.B) {
	testPaths := []string{
		"/",
		"/simple/path",
		"/path/with/many/segments/file.txt",
		"/path/../with/../traversal",
		"/path/./with/./current/./dir",
		"//double//slashes//path",
		"/path/with spaces/file.txt",
		"/very/long/path/with/many/segments/and/deep/nesting/structure/file.txt",
	}

	for _, path := range testPaths {
		b.Run(fmt.Sprintf("path_%d_chars", len(path)), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Simulate the path cleaning operations done in ServeFiles
				cleanPath := filepath.Clean(path)
				if cleanPath == "." {
					cleanPath = "/"
				}
				_ = cleanPath
			}
		})
	}
}

// BenchmarkRouteMatching benchmarks the route matching logic
func BenchmarkRouteMatching(b *testing.B) {
	testPaths := []string{
		"/static/css/style.css",
		"/static/js/script.js",
		"/static/images/logo.png",
		"/admin/dashboard",
		"/admin/users",
		"/admin/settings",
		"/login",
		"/version",
		"/file.txt",
		"/dir/subdir/file.txt",
	}

	for _, path := range testPaths {
		b.Run(fmt.Sprintf("route_%s", filepath.Base(path)), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Simulate the route matching logic from createUnifiedHandler
				isStatic := path == "/favicon.ico" || (len(path) >= 8 && path[:8] == "/static/")
				isVersion := path == "/version"
				isAdmin := len(path) >= 6 && path[:6] == "/admin"
				isLogin := path == "/login"

				_ = isStatic
				_ = isVersion
				_ = isAdmin
				_ = isLogin
			}
		})
	}
}

// BenchmarkConcurrentRequests benchmarks handling multiple concurrent requests
func BenchmarkConcurrentRequests(b *testing.B) {
	server := setupBenchmarkServer(b)
	handler := NewHandler(server.config, server.roots)
	unifiedHandler := server.createUnifiedHandler(handler)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/file_0.txt", nil)

			unifiedHandler(c)

			if w.Code != http.StatusOK {
				b.Fatalf("Expected status 200, got %d", w.Code)
			}
		}
	})
}

// BenchmarkMiddlewareChain benchmarks the complete middleware chain
func BenchmarkMiddlewareChain(b *testing.B) {
	server := setupBenchmarkServer(b)

	// Create middleware chain similar to what's used in production
	middlewares := []gin.HandlerFunc{
		server.accessControlMiddleware(),
		server.conditionalAuthMiddleware(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/file_0.txt", nil)

		// Execute middleware chain
		for _, middleware := range middlewares {
			middleware(c)
			if c.IsAborted() {
				break
			}
		}
	}
}
