package server

import (
	"bytes"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"slimserve/internal/config"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		EnableAdmin:   true,
		AdminUsername: "admin",
		AdminPassword: "password123",
	}

	server := &Server{
		config:       cfg,
		sessionStore: NewSessionStore(),
		adminUtils:   NewAdminUtils(),
	}

	engine := gin.New()
	engine.Use(AdminAuthMiddleware(cfg, server.sessionStore))
	engine.GET("/admin/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "authenticated"})
	})

	t.Run("Unauthenticated request should be rejected", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/admin/test", nil)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("Valid admin session should be accepted", func(t *testing.T) {
		// Create valid admin session
		token := server.sessionStore.NewToken()
		server.sessionStore.AddAdmin(token)

		req := httptest.NewRequest("GET", "/admin/test", nil)
		req.AddCookie(&http.Cookie{
			Name:  "slimserve_admin_session",
			Value: token,
		})
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestAdminLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		EnableAdmin:   true,
		AdminUsername: "admin",
		AdminPassword: "password123",
	}

	server := &Server{
		config:       cfg,
		sessionStore: NewSessionStore(),
	}

	t.Run("Valid credentials should create session", func(t *testing.T) {
		valid := server.validateAdminCredentials("admin", "password123")
		assert.True(t, valid)
	})

	t.Run("Invalid credentials should be rejected", func(t *testing.T) {
		valid := server.validateAdminCredentials("admin", "wrongpassword")
		assert.False(t, valid)

		valid = server.validateAdminCredentials("wronguser", "password123")
		assert.False(t, valid)
	})

	t.Run("Empty credentials should be rejected", func(t *testing.T) {
		valid := server.validateAdminCredentials("", "")
		assert.False(t, valid)
	})
}

func TestFileUploadSecurity(t *testing.T) {
	cfg := &config.Config{
		EnableAdmin:        true,
		MaxUploadSizeMB:    10,
		AllowedUploadTypes: []string{"txt", "jpg", "png"},
	}

	server := &Server{
		config: cfg,
	}

	t.Run("Allowed file types should pass validation", func(t *testing.T) {
		assert.True(t, server.isAllowedFileType("test.txt"))
		assert.True(t, server.isAllowedFileType("image.jpg"))
		assert.True(t, server.isAllowedFileType("photo.PNG"))
	})

	t.Run("Disallowed file types should fail validation", func(t *testing.T) {
		assert.False(t, server.isAllowedFileType("script.exe"))
		assert.False(t, server.isAllowedFileType("malware.bat"))
		assert.False(t, server.isAllowedFileType("document.pdf"))
	})

	t.Run("Secure filenames should pass validation", func(t *testing.T) {
		assert.True(t, server.isSecureFilename("normal_file.txt"))
		assert.True(t, server.isSecureFilename("image-2023.jpg"))
		assert.True(t, server.isSecureFilename("document_v1.2.pdf"))
	})

	t.Run("Unsafe filenames should fail validation", func(t *testing.T) {
		assert.False(t, server.isSecureFilename("../../../etc/passwd"))
		assert.False(t, server.isSecureFilename("file<script>.txt"))
		assert.False(t, server.isSecureFilename("file|command.txt"))
		assert.False(t, server.isSecureFilename("file\x00.txt"))
		assert.False(t, server.isSecureFilename("malware.exe"))
	})
}

func TestFileUploadHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create temporary upload directory
	tmpDir, err := os.MkdirTemp("", "slimserve_test_upload")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		EnableAdmin:        true,
		AdminUploadDir:     tmpDir,
		MaxUploadSizeMB:    10,
		AllowedUploadTypes: []string{"txt"},
	}

	server := &Server{
		config:        cfg,
		uploadManager: NewUploadManager(3),
	}

	engine := gin.New()
	engine.POST("/admin/api/upload", server.handleFileUpload)

	t.Run("Valid file upload should succeed", func(t *testing.T) {
		// Create multipart form with test file
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		part, err := writer.CreateFormFile("files", "test.txt")
		require.NoError(t, err)

		_, err = part.Write([]byte("test content"))
		require.NoError(t, err)

		err = writer.Close()
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/admin/api/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()

		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "upload completed", response["message"])
		assert.Contains(t, response, "results")
	})

	t.Run("Upload with disallowed file type should fail", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		part, err := writer.CreateFormFile("files", "malware.exe")
		require.NoError(t, err)

		_, err = part.Write([]byte("malicious content"))
		require.NoError(t, err)

		err = writer.Close()
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/admin/api/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()

		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		results := response["results"].([]interface{})
		result := results[0].(map[string]interface{})
		assert.Equal(t, "error", result["status"])
		assert.Contains(t, result["error"], "file type not allowed")
	})
}

func TestCookieSecurity(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		EnableAdmin:   true,
		AdminUsername: "admin",
		AdminPassword: "secret123",
	}

	t.Run("HTTP cookies should have correct security attributes", func(t *testing.T) {
		server := &Server{
			config:       cfg,
			sessionStore: NewSessionStore(),
		}

		engine := gin.New()
		engine.POST("/admin/login", server.doAdminLogin)

		formData := url.Values{}
		formData.Set("username", "admin")
		formData.Set("password", "secret123")

		req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(formData.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusFound, w.Code)

		// Check admin session cookie attributes
		setCookieHeaders := w.Header().Values("Set-Cookie")
		var adminSessionCookie string
		for _, header := range setCookieHeaders {
			if strings.Contains(header, "slimserve_admin_session=") {
				adminSessionCookie = header
				break
			}
		}

		assert.NotEmpty(t, adminSessionCookie)
		assert.Contains(t, adminSessionCookie, "HttpOnly")
		assert.Contains(t, adminSessionCookie, "Path=/admin")
		assert.Contains(t, adminSessionCookie, "SameSite=Lax")
		assert.NotContains(t, adminSessionCookie, "Secure")   // Should not be secure over HTTP
		assert.NotContains(t, adminSessionCookie, "Max-Age=") // Session cookie
	})

	t.Run("HTTPS cookies should have Secure flag", func(t *testing.T) {
		server := &Server{
			config:       cfg,
			sessionStore: NewSessionStore(),
		}

		engine := gin.New()
		engine.POST("/admin/login", server.doAdminLogin)

		formData := url.Values{}
		formData.Set("username", "admin")
		formData.Set("password", "secret123")

		req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(formData.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.TLS = &tls.ConnectionState{} // Simulate HTTPS
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusFound, w.Code)

		// Check admin session cookie has Secure flag
		setCookieHeaders := w.Header().Values("Set-Cookie")
		var adminSessionCookie string
		for _, header := range setCookieHeaders {
			if strings.Contains(header, "slimserve_admin_session=") {
				adminSessionCookie = header
				break
			}
		}

		assert.NotEmpty(t, adminSessionCookie)
		assert.Contains(t, adminSessionCookie, "Secure")
		assert.Contains(t, adminSessionCookie, "HttpOnly")
		assert.Contains(t, adminSessionCookie, "Path=/admin")
	})
}

// Helper function to extract cookie value from Set-Cookie header
func extractAdminCookie(recorder *httptest.ResponseRecorder, cookieName string) string {
	setCookieHeaders := recorder.Header().Values("Set-Cookie")
	for _, header := range setCookieHeaders {
		parts := strings.Split(header, ";")
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if strings.HasPrefix(trimmed, cookieName+"=") {
				return strings.TrimPrefix(trimmed, cookieName+"=")
			}
		}
	}
	return ""
}

func TestAdminLoginFlow(t *testing.T) {
	t.Run("CSRF token generation and redirect validation", func(t *testing.T) {
		// Test CSRF token is generated correctly
		csrfToken := generateCSRFToken()
		assert.NotEmpty(t, csrfToken)
		assert.Len(t, csrfToken, 64) // 32 bytes hex encoded = 64 chars

		// Test valid redirect URL
		next := validateAdminRedirectURL("/admin/dashboard")
		assert.Equal(t, "/admin/dashboard", next)

		// Test invalid redirect URLs default to /admin
		next = validateAdminRedirectURL("http://evil.com")
		assert.Equal(t, "/admin", next)

		next = validateAdminRedirectURL("//evil.com")
		assert.Equal(t, "/admin", next)
	})
}

func TestAdminLoginPost(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		EnableAdmin:   true,
		AdminUsername: "admin",
		AdminPassword: "secret123",
	}

	t.Run("Valid admin login with form data", func(t *testing.T) {
		server := &Server{
			config:       cfg,
			sessionStore: NewSessionStore(),
		}

		engine := gin.New()
		engine.POST("/admin/login", server.doAdminLogin)

		// Create form data
		formData := url.Values{}
		formData.Set("username", "admin")
		formData.Set("password", "secret123")
		formData.Set("next", "/admin/dashboard")

		req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(formData.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusFound, w.Code)
		assert.Equal(t, "/admin/dashboard", w.Header().Get("Location"))

		// Check that admin session cookie is set
		sessionToken := extractAdminCookie(w, "slimserve_admin_session")
		assert.NotEmpty(t, sessionToken)
		assert.Len(t, sessionToken, 64) // 32 bytes hex encoded = 64 chars

		// Check cookie security attributes
		setCookieHeaders := w.Header().Values("Set-Cookie")
		var sessionCookie string
		for _, header := range setCookieHeaders {
			if strings.Contains(header, "slimserve_admin_session=") {
				sessionCookie = header
				break
			}
		}
		assert.Contains(t, sessionCookie, "HttpOnly")
		assert.Contains(t, sessionCookie, "Path=/admin")
		assert.Contains(t, sessionCookie, "SameSite=Lax")

		// Verify token is valid in session store
		assert.True(t, server.sessionStore.ValidAdmin(sessionToken))
	})

	t.Run("Valid admin login with JSON data", func(t *testing.T) {
		server := &Server{
			config:       cfg,
			sessionStore: NewSessionStore(),
		}

		engine := gin.New()
		engine.POST("/admin/login", server.doAdminLogin)

		loginData := map[string]string{
			"username": "admin",
			"password": "secret123",
			"next":     "/admin/api",
		}
		jsonData, _ := json.Marshal(loginData)

		req := httptest.NewRequest("POST", "/admin/login", bytes.NewReader(jsonData))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, true, response["success"])
		assert.Equal(t, "/admin/api", response["redirect"])

		// Check that admin session cookie is set
		sessionToken := extractAdminCookie(w, "slimserve_admin_session")
		assert.NotEmpty(t, sessionToken)
		assert.True(t, server.sessionStore.ValidAdmin(sessionToken))
	})

	t.Run("Invalid admin credentials with form data", func(t *testing.T) {
		server := &Server{
			config:       cfg,
			sessionStore: NewSessionStore(),
		}

		engine := gin.New()
		engine.POST("/admin/login", server.doAdminLogin)

		formData := url.Values{}
		formData.Set("username", "admin")
		formData.Set("password", "wrongpassword")
		formData.Set("next", "/admin/dashboard")

		req := httptest.NewRequest("POST", "/admin/login", strings.NewReader(formData.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json") // Force JSON response to avoid template rendering
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "invalid admin credentials", response["error"])

		// Should not set admin session cookie
		sessionToken := extractAdminCookie(w, "slimserve_admin_session")
		assert.Empty(t, sessionToken)
	})

	t.Run("Invalid admin credentials with JSON data", func(t *testing.T) {
		server := &Server{
			config:       cfg,
			sessionStore: NewSessionStore(),
		}

		engine := gin.New()
		engine.POST("/admin/login", server.doAdminLogin)

		loginData := map[string]string{
			"username": "admin",
			"password": "wrongpassword",
		}
		jsonData, _ := json.Marshal(loginData)

		req := httptest.NewRequest("POST", "/admin/login", bytes.NewReader(jsonData))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "invalid admin credentials", response["error"])

		// Should not set admin session cookie
		sessionToken := extractAdminCookie(w, "slimserve_admin_session")
		assert.Empty(t, sessionToken)
	})
}

func TestCSRFProtectionMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Test handler that returns success if CSRF check passes
	testHandler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	}

	t.Run("GET requests should bypass CSRF check", func(t *testing.T) {
		engine := gin.New()
		engine.Use(CSRFProtectionMiddleware())
		engine.GET("/admin/test", testHandler)

		req := httptest.NewRequest("GET", "/admin/test", nil)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Admin login should bypass CSRF check", func(t *testing.T) {
		engine := gin.New()
		engine.Use(CSRFProtectionMiddleware())
		engine.POST("/admin/login", testHandler)

		req := httptest.NewRequest("POST", "/admin/login", nil)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("POST request with valid CSRF token in header should pass", func(t *testing.T) {
		engine := gin.New()
		engine.Use(CSRFProtectionMiddleware())
		engine.POST("/admin/test", testHandler)

		// Generate a test CSRF token
		csrfToken := "test-csrf-token-123"

		req := httptest.NewRequest("POST", "/admin/test", nil)
		req.Header.Set("X-CSRF-Token", csrfToken)
		req.AddCookie(&http.Cookie{
			Name:  "slimserve_csrf_token",
			Value: csrfToken,
		})
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("POST request with valid CSRF token in form should pass", func(t *testing.T) {
		engine := gin.New()
		engine.Use(CSRFProtectionMiddleware())
		engine.POST("/admin/test", testHandler)

		csrfToken := "test-csrf-token-456"

		formData := url.Values{}
		formData.Set("csrf_token", csrfToken)

		req := httptest.NewRequest("POST", "/admin/test", strings.NewReader(formData.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{
			Name:  "slimserve_csrf_token",
			Value: csrfToken,
		})
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("POST request with missing CSRF token should fail", func(t *testing.T) {
		engine := gin.New()
		engine.Use(CSRFProtectionMiddleware())
		engine.POST("/admin/test", testHandler)

		req := httptest.NewRequest("POST", "/admin/test", nil)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "invalid CSRF token", response["error"])
	})

	t.Run("POST request with mismatched CSRF token should fail", func(t *testing.T) {
		engine := gin.New()
		engine.Use(CSRFProtectionMiddleware())
		engine.POST("/admin/test", testHandler)

		req := httptest.NewRequest("POST", "/admin/test", nil)
		req.Header.Set("X-CSRF-Token", "wrong-token")
		req.AddCookie(&http.Cookie{
			Name:  "slimserve_csrf_token",
			Value: "correct-token",
		})
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "invalid CSRF token", response["error"])
	})

	t.Run("POST request with missing CSRF cookie should fail", func(t *testing.T) {
		engine := gin.New()
		engine.Use(CSRFProtectionMiddleware())
		engine.POST("/admin/test", testHandler)

		req := httptest.NewRequest("POST", "/admin/test", nil)
		req.Header.Set("X-CSRF-Token", "some-token")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("PUT request should also be protected by CSRF", func(t *testing.T) {
		engine := gin.New()
		engine.Use(CSRFProtectionMiddleware())
		engine.PUT("/admin/test", testHandler)

		csrfToken := "test-csrf-token-put"

		req := httptest.NewRequest("PUT", "/admin/test", nil)
		req.Header.Set("X-CSRF-Token", csrfToken)
		req.AddCookie(&http.Cookie{
			Name:  "slimserve_csrf_token",
			Value: csrfToken,
		})
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("DELETE request should also be protected by CSRF", func(t *testing.T) {
		engine := gin.New()
		engine.Use(CSRFProtectionMiddleware())
		engine.DELETE("/admin/test", testHandler)

		csrfToken := "test-csrf-token-delete"

		req := httptest.NewRequest("DELETE", "/admin/test", nil)
		req.Header.Set("X-CSRF-Token", csrfToken)
		req.AddCookie(&http.Cookie{
			Name:  "slimserve_csrf_token",
			Value: csrfToken,
		})
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestCSRFTokenGeneration(t *testing.T) {
	t.Run("generateCSRFToken should return valid hex string", func(t *testing.T) {
		token := generateCSRFToken()
		assert.NotEmpty(t, token)
		assert.Len(t, token, 64) // 32 bytes hex encoded = 64 chars

		// Should be valid hex
		_, err := hex.DecodeString(token)
		assert.NoError(t, err)
	})

	t.Run("generateCSRFToken should return different tokens", func(t *testing.T) {
		token1 := generateCSRFToken()
		token2 := generateCSRFToken()
		assert.NotEqual(t, token1, token2)
	})

	t.Run("getOrSetCSRFToken should generate new token when none exists", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		server := &Server{}

		engine := gin.New()
		engine.GET("/test", func(c *gin.Context) {
			token := server.getOrSetCSRFToken(c)
			c.String(http.StatusOK, token)
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		token := w.Body.String()
		assert.NotEmpty(t, token)
		assert.Len(t, token, 64)

		// Check that CSRF token cookie is set
		csrfToken := extractAdminCookie(w, "slimserve_csrf_token")
		assert.Equal(t, token, csrfToken)
	})

	t.Run("getOrSetCSRFToken should return existing token from cookie", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		server := &Server{}
		existingToken := "existing-csrf-token-123456789012345678901234567890123456789012"

		engine := gin.New()
		engine.GET("/test", func(c *gin.Context) {
			token := server.getOrSetCSRFToken(c)
			c.String(http.StatusOK, token)
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.AddCookie(&http.Cookie{
			Name:  "slimserve_csrf_token",
			Value: existingToken,
		})
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		token := w.Body.String()
		assert.Equal(t, existingToken, token)

		// Should not set a new cookie since one already exists
		setCookieHeaders := w.Header().Values("Set-Cookie")
		assert.Empty(t, setCookieHeaders)
	})
}

func TestConstantTimeEqual(t *testing.T) {
	t.Run("Equal strings should return true", func(t *testing.T) {
		assert.True(t, constantTimeEqual("hello", "hello"))
		assert.True(t, constantTimeEqual("", ""))
		assert.True(t, constantTimeEqual("test123", "test123"))
	})

	t.Run("Different strings should return false", func(t *testing.T) {
		assert.False(t, constantTimeEqual("hello", "world"))
		assert.False(t, constantTimeEqual("test", ""))
		assert.False(t, constantTimeEqual("", "test"))
		assert.False(t, constantTimeEqual("abc", "abcd"))
		assert.False(t, constantTimeEqual("abcd", "abc"))
	})

	t.Run("Different length strings should return false", func(t *testing.T) {
		assert.False(t, constantTimeEqual("short", "longer"))
		assert.False(t, constantTimeEqual("longer", "short"))
	})
}

func TestAdminAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Test handler that returns success if auth passes
	testHandler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "authenticated"})
	}

	t.Run("Admin disabled should return 404", func(t *testing.T) {
		cfg := &config.Config{
			EnableAdmin: false,
		}
		store := NewSessionStore()

		engine := gin.New()
		engine.Use(AdminAuthMiddleware(cfg, store))
		engine.GET("/admin/test", testHandler)

		req := httptest.NewRequest("GET", "/admin/test", nil)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "admin interface not enabled", response["error"])
	})

	t.Run("Admin login route should bypass authentication", func(t *testing.T) {
		cfg := &config.Config{
			EnableAdmin: true,
		}
		store := NewSessionStore()

		engine := gin.New()
		engine.Use(AdminAuthMiddleware(cfg, store))
		engine.GET("/admin/login", testHandler)

		req := httptest.NewRequest("GET", "/admin/login", nil)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Admin static assets should bypass authentication", func(t *testing.T) {
		cfg := &config.Config{
			EnableAdmin: true,
		}
		store := NewSessionStore()

		engine := gin.New()
		engine.Use(AdminAuthMiddleware(cfg, store))
		engine.GET("/admin/static/css/style.css", testHandler)

		req := httptest.NewRequest("GET", "/admin/static/css/style.css", nil)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Valid admin session should pass authentication", func(t *testing.T) {
		cfg := &config.Config{
			EnableAdmin: true,
		}
		store := NewSessionStore()

		// Create valid admin session
		token := store.NewToken()
		store.AddAdmin(token)

		engine := gin.New()
		engine.Use(AdminAuthMiddleware(cfg, store))
		engine.GET("/admin/dashboard", testHandler)

		req := httptest.NewRequest("GET", "/admin/dashboard", nil)
		req.AddCookie(&http.Cookie{
			Name:  "slimserve_admin_session",
			Value: token,
		})
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Invalid admin session should redirect browser to login", func(t *testing.T) {
		cfg := &config.Config{
			EnableAdmin: true,
		}
		store := NewSessionStore()

		engine := gin.New()
		engine.Use(AdminAuthMiddleware(cfg, store))
		engine.GET("/admin/dashboard", testHandler)

		req := httptest.NewRequest("GET", "/admin/dashboard", nil)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.AddCookie(&http.Cookie{
			Name:  "slimserve_admin_session",
			Value: "invalid-token",
		})
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusFound, w.Code)
		location := w.Header().Get("Location")
		assert.Contains(t, location, "/admin/login")
		assert.Contains(t, location, "next=")
		assert.Contains(t, location, url.QueryEscape("/admin/dashboard"))
	})

	t.Run("Missing admin session should redirect browser to login", func(t *testing.T) {
		cfg := &config.Config{
			EnableAdmin: true,
		}
		store := NewSessionStore()

		engine := gin.New()
		engine.Use(AdminAuthMiddleware(cfg, store))
		engine.GET("/admin/dashboard", testHandler)

		req := httptest.NewRequest("GET", "/admin/dashboard", nil)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusFound, w.Code)
		location := w.Header().Get("Location")
		assert.Contains(t, location, "/admin/login")
		assert.Contains(t, location, "next=")
	})

	t.Run("Invalid admin session should return 401 for API requests", func(t *testing.T) {
		cfg := &config.Config{
			EnableAdmin: true,
		}
		store := NewSessionStore()

		engine := gin.New()
		engine.Use(AdminAuthMiddleware(cfg, store))
		engine.GET("/admin/api/data", testHandler)

		req := httptest.NewRequest("GET", "/admin/api/data", nil)
		req.Header.Set("Accept", "application/json")
		req.AddCookie(&http.Cookie{
			Name:  "slimserve_admin_session",
			Value: "invalid-token",
		})
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "admin authentication required", response["error"])
	})

	t.Run("Missing admin session should return 401 for API requests", func(t *testing.T) {
		cfg := &config.Config{
			EnableAdmin: true,
		}
		store := NewSessionStore()

		engine := gin.New()
		engine.Use(AdminAuthMiddleware(cfg, store))
		engine.POST("/admin/api/upload", testHandler)

		req := httptest.NewRequest("POST", "/admin/api/upload", nil)
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "admin authentication required", response["error"])
	})

	t.Run("XMLHttpRequest should be treated as API request", func(t *testing.T) {
		cfg := &config.Config{
			EnableAdmin: true,
		}
		store := NewSessionStore()

		engine := gin.New()
		engine.Use(AdminAuthMiddleware(cfg, store))
		engine.GET("/admin/dashboard", testHandler)

		req := httptest.NewRequest("GET", "/admin/dashboard", nil)
		req.Header.Set("Accept", "text/html")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "admin authentication required", response["error"])
	})
}

func TestAdminLogout(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		EnableAdmin:   true,
		AdminUsername: "admin",
		AdminPassword: "secret123",
	}

	t.Run("Admin logout should clear session and cookies", func(t *testing.T) {
		server := &Server{
			config:       cfg,
			sessionStore: NewSessionStore(),
		}

		// Create valid admin session
		token := server.sessionStore.NewToken()
		server.sessionStore.AddAdmin(token)

		// Verify session is valid before logout
		assert.True(t, server.sessionStore.ValidAdmin(token))

		engine := gin.New()
		engine.POST("/admin/logout", server.doAdminLogout)

		req := httptest.NewRequest("POST", "/admin/logout", nil)
		req.AddCookie(&http.Cookie{
			Name:  "slimserve_admin_session",
			Value: token,
		})
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusFound, w.Code)
		assert.Equal(t, "/admin/login", w.Header().Get("Location"))

		// Verify session is removed from store
		assert.False(t, server.sessionStore.ValidAdmin(token))

		// Check that admin session cookie is cleared
		setCookieHeaders := w.Header().Values("Set-Cookie")
		assert.NotEmpty(t, setCookieHeaders)

		// Find admin session cookie clear directive
		var adminSessionCleared bool
		var csrfTokenCleared bool
		for _, header := range setCookieHeaders {
			if strings.Contains(header, "slimserve_admin_session=") && strings.Contains(header, "Max-Age=") {
				adminSessionCleared = true
				assert.Contains(t, header, "Path=/admin")
				assert.Contains(t, header, "HttpOnly")
			}
			if strings.Contains(header, "slimserve_csrf_token=") && strings.Contains(header, "Max-Age=") {
				csrfTokenCleared = true
				assert.Contains(t, header, "Path=/admin")
				assert.Contains(t, header, "HttpOnly")
			}
		}
		assert.True(t, adminSessionCleared, "Admin session cookie should be cleared")
		assert.True(t, csrfTokenCleared, "CSRF token cookie should be cleared")
	})

	t.Run("Admin logout without session should still redirect", func(t *testing.T) {
		server := &Server{
			config:       cfg,
			sessionStore: NewSessionStore(),
		}

		engine := gin.New()
		engine.POST("/admin/logout", server.doAdminLogout)

		req := httptest.NewRequest("POST", "/admin/logout", nil)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusFound, w.Code)
		assert.Equal(t, "/admin/login", w.Header().Get("Location"))

		// Should still clear cookies even if no session exists
		setCookieHeaders := w.Header().Values("Set-Cookie")
		assert.NotEmpty(t, setCookieHeaders)
	})

	t.Run("Admin logout with invalid session should clear cookies", func(t *testing.T) {
		server := &Server{
			config:       cfg,
			sessionStore: NewSessionStore(),
		}

		engine := gin.New()
		engine.POST("/admin/logout", server.doAdminLogout)

		req := httptest.NewRequest("POST", "/admin/logout", nil)
		req.AddCookie(&http.Cookie{
			Name:  "slimserve_admin_session",
			Value: "invalid-token",
		})
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusFound, w.Code)
		assert.Equal(t, "/admin/login", w.Header().Get("Location"))

		// Should clear cookies even with invalid session
		setCookieHeaders := w.Header().Values("Set-Cookie")
		assert.NotEmpty(t, setCookieHeaders)

		var adminSessionCleared bool
		for _, header := range setCookieHeaders {
			if strings.Contains(header, "slimserve_admin_session=;") {
				adminSessionCleared = true
				break
			}
		}
		assert.True(t, adminSessionCleared)
	})

	t.Run("Admin logout should work with HTTPS", func(t *testing.T) {
		server := &Server{
			config:       cfg,
			sessionStore: NewSessionStore(),
		}

		// Create valid admin session
		token := server.sessionStore.NewToken()
		server.sessionStore.AddAdmin(token)

		engine := gin.New()
		engine.POST("/admin/logout", server.doAdminLogout)

		req := httptest.NewRequest("POST", "/admin/logout", nil)
		req.TLS = &tls.ConnectionState{} // Simulate HTTPS
		req.AddCookie(&http.Cookie{
			Name:  "slimserve_admin_session",
			Value: token,
		})
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusFound, w.Code)

		// Check that cookies are cleared with Secure flag for HTTPS
		setCookieHeaders := w.Header().Values("Set-Cookie")
		assert.NotEmpty(t, setCookieHeaders)

		for _, header := range setCookieHeaders {
			if strings.Contains(header, "slimserve_admin_session=;") {
				assert.Contains(t, header, "Secure")
			}
			if strings.Contains(header, "slimserve_csrf_token=;") {
				assert.Contains(t, header, "Secure")
			}
		}
	})
}
