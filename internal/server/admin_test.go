package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestAdminUtils(t *testing.T) {
	utils := NewAdminUtils()

	t.Run("Format bytes should work correctly", func(t *testing.T) {
		assert.Equal(t, "0 B", utils.formatBytes(0))
		assert.Equal(t, "1.0 KB", utils.formatBytes(1024))
		assert.Equal(t, "1.0 MB", utils.formatBytes(1024*1024))
		assert.Equal(t, "1.5 KB", utils.formatBytes(1536))
	})

	t.Run("Uptime should be formatted correctly", func(t *testing.T) {
		uptime := utils.GetUptime()
		assert.Contains(t, uptime, "m") // Should contain minutes
	})
}

func TestSessionStore(t *testing.T) {
	store := NewSessionStore()

	t.Run("Admin session management", func(t *testing.T) {
		// Test adding admin token
		token := store.NewToken()
		assert.NotEmpty(t, token)

		store.AddAdmin(token)
		assert.True(t, store.ValidAdmin(token))
		assert.Equal(t, 1, store.CountAdmin())

		// Test removing admin token
		store.RemoveAdmin(token)
		assert.False(t, store.ValidAdmin(token))
		assert.Equal(t, 0, store.CountAdmin())
	})

	t.Run("Regular and admin sessions should be separate", func(t *testing.T) {
		token := store.NewToken()

		store.Add(token)
		assert.True(t, store.Valid(token))
		assert.False(t, store.ValidAdmin(token))

		store.AddAdmin(token)
		assert.True(t, store.ValidAdmin(token))
		assert.True(t, store.Valid(token))
	})
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal_file.txt", "normal_file.txt"},
		{"file with spaces.txt", "file with spaces.txt"},
		{"../../../etc/passwd", "passwd"},
		{"file/with/path.txt", "path.txt"},
		{"file\\with\\backslash.txt", "filewithbackslash.txt"},
		{"file:with:colons.txt", "filewithcolons.txt"},
		{"file*with*stars.txt", "filewithstars.txt"},
		{"file?with?questions.txt", "filewithquestions.txt"},
		{"file\"with\"quotes.txt", "filewithquotes.txt"},
		{"file<with>brackets.txt", "filewithbrackets.txt"},
		{"file|with|pipes.txt", "filewithpipes.txt"},
		{".hidden_file.txt", ""},
		{"", ""},
		{"   spaced   ", "spaced"},
	}

	for _, test := range tests {
		t.Run("Sanitize: "+test.input, func(t *testing.T) {
			result := sanitizeFilename(test.input)
			assert.Equal(t, test.expected, result)
		})
	}
}
