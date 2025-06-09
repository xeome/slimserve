package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"slimserve/internal/config"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// Helper function to extract cookie value from Set-Cookie header
func extractCookie(recorder *httptest.ResponseRecorder, cookieName string) string {
	setCookieHeader := recorder.Header().Get("Set-Cookie")
	if setCookieHeader == "" {
		return ""
	}

	parts := strings.Split(setCookieHeader, ";")
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if strings.HasPrefix(trimmed, cookieName+"=") {
			return strings.TrimPrefix(trimmed, cookieName+"=")
		}
	}
	return ""
}

func TestSessionAuthMiddleware(t *testing.T) {
	// Setup Gin in test mode
	gin.SetMode(gin.TestMode)

	// Test helper to create a simple handler that returns 200 OK
	testHandler := func(c *gin.Context) {
		c.String(http.StatusOK, "success")
	}

	t.Run("auth disabled - public access returns 200", func(t *testing.T) {
		cfg := &config.Config{
			EnableAuth: false,
			Username:   "admin",
			Password:   "secret",
		}
		store := NewSessionStore()

		engine := gin.New()
		engine.Use(SessionAuthMiddleware(cfg, store))
		engine.GET("/test", testHandler)

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "success", w.Body.String())
	})

	t.Run("auth enabled - no cookie browser request redirects to login", func(t *testing.T) {
		cfg := &config.Config{
			EnableAuth: true,
			Username:   "admin",
			Password:   "secret",
		}
		store := NewSessionStore()

		engine := gin.New()
		engine.Use(SessionAuthMiddleware(cfg, store))
		engine.GET("/test", testHandler)

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusFound, w.Code)
		location := w.Header().Get("Location")
		assert.Contains(t, location, "/login")
		assert.Contains(t, location, "next=")
	})

	t.Run("auth enabled - no cookie API request returns 401 JSON", func(t *testing.T) {
		cfg := &config.Config{
			EnableAuth: true,
			Username:   "admin",
			Password:   "secret",
		}
		store := NewSessionStore()

		engine := gin.New()
		engine.Use(SessionAuthMiddleware(cfg, store))
		engine.GET("/api/test", testHandler)

		req := httptest.NewRequest("GET", "/api/test", nil)
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "unauthenticated", response["error"])
	})

	t.Run("auth enabled - valid session cookie returns 200", func(t *testing.T) {
		cfg := &config.Config{
			EnableAuth: true,
			Username:   "admin",
			Password:   "secret",
		}
		store := NewSessionStore()

		// Create a valid session token
		token := store.NewToken()
		store.Add(token)

		engine := gin.New()
		engine.Use(SessionAuthMiddleware(cfg, store))
		engine.GET("/test", testHandler)

		req := httptest.NewRequest("GET", "/test", nil)
		req.AddCookie(&http.Cookie{Name: "slimserve_session", Value: token})
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "success", w.Body.String())
	})

	t.Run("auth enabled - invalid session cookie redirects browser to login", func(t *testing.T) {
		cfg := &config.Config{
			EnableAuth: true,
			Username:   "admin",
			Password:   "secret",
		}
		store := NewSessionStore()

		engine := gin.New()
		engine.Use(SessionAuthMiddleware(cfg, store))
		engine.GET("/test", testHandler)

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.AddCookie(&http.Cookie{Name: "slimserve_session", Value: "invalid-token"})
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusFound, w.Code)
		location := w.Header().Get("Location")
		assert.Contains(t, location, "/login")
	})

	t.Run("auth enabled - server restart invalidates sessions", func(t *testing.T) {
		cfg := &config.Config{
			EnableAuth: true,
			Username:   "admin",
			Password:   "secret",
		}

		// Create first session store and token
		store1 := NewSessionStore()
		token := store1.NewToken()
		store1.Add(token)

		// Simulate server restart by creating new session store
		store2 := NewSessionStore()

		engine := gin.New()
		engine.Use(SessionAuthMiddleware(cfg, store2))
		engine.GET("/test", testHandler)

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.AddCookie(&http.Cookie{Name: "slimserve_session", Value: token})
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		// Should redirect to login since session store is new/empty
		assert.Equal(t, http.StatusFound, w.Code)
		location := w.Header().Get("Location")
		assert.Contains(t, location, "/login")
	})
}

func TestLoginFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		EnableAuth: true,
		Username:   "testuser",
		Password:   "testpass",
	}

	t.Run("HTML form login success", func(t *testing.T) {
		server := New(cfg)
		engine := server.GetEngine()

		// POST login with form data
		formData := url.Values{}
		formData.Set("username", "testuser")
		formData.Set("password", "testpass")
		formData.Set("next", "/dashboard")

		req := httptest.NewRequest("POST", "/login", strings.NewReader(formData.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		// Should redirect to next page
		assert.Equal(t, http.StatusFound, w.Code)
		assert.Equal(t, "/dashboard", w.Header().Get("Location"))

		// Should set session cookie
		sessionToken := extractCookie(w, "slimserve_session")
		assert.NotEmpty(t, sessionToken)
		assert.Contains(t, w.Header().Get("Set-Cookie"), "HttpOnly")
		assert.Contains(t, w.Header().Get("Set-Cookie"), "Path=/")

		// Verify token is valid in session store
		assert.True(t, server.sessionStore.Valid(sessionToken))
	})

	t.Run("HTML form login failure", func(t *testing.T) {
		server := New(cfg)
		engine := server.GetEngine()

		formData := url.Values{}
		formData.Set("username", "testuser")
		formData.Set("password", "wrongpass")
		formData.Set("next", "/dashboard")

		req := httptest.NewRequest("POST", "/login", strings.NewReader(formData.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		// Should render login page with error (200 status)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid username or password")

		// Should not set cookie
		sessionToken := extractCookie(w, "slimserve_session")
		assert.Empty(t, sessionToken)
	})

	t.Run("JSON login success", func(t *testing.T) {
		server := New(cfg)
		engine := server.GetEngine()

		loginData := map[string]string{
			"username": "testuser",
			"password": "testpass",
			"next":     "/api/data",
		}
		jsonData, _ := json.Marshal(loginData)

		req := httptest.NewRequest("POST", "/login", bytes.NewReader(jsonData))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, true, response["success"])
		assert.Equal(t, "/api/data", response["redirect"])

		// Should set session cookie
		sessionToken := extractCookie(w, "slimserve_session")
		assert.NotEmpty(t, sessionToken)
		assert.True(t, server.sessionStore.Valid(sessionToken))
	})

	t.Run("JSON login failure", func(t *testing.T) {
		server := New(cfg)
		engine := server.GetEngine()

		loginData := map[string]string{
			"username": "testuser",
			"password": "wrongpass",
		}
		jsonData, _ := json.Marshal(loginData)

		req := httptest.NewRequest("POST", "/login", bytes.NewReader(jsonData))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "invalid credentials", response["error"])

		// Should not set cookie
		sessionToken := extractCookie(w, "slimserve_session")
		assert.Empty(t, sessionToken)
	})
}

func TestSessionAuthMiddleware_Integration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		EnableAuth: true,
		Username:   "testuser",
		Password:   "testpass",
	}

	store := NewSessionStore()

	engine := gin.New()
	engine.Use(SessionAuthMiddleware(cfg, store))

	// Add test routes
	engine.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "home")
	})
	engine.GET("/api/data", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": "protected"})
	})
	engine.POST("/api/upload", func(c *gin.Context) {
		c.String(http.StatusOK, "uploaded")
	})

	testCases := []struct {
		name           string
		method         string
		path           string
		accept         string
		expectedStatus int
		expectedAction string // "redirect" or "json_error"
	}{
		{"GET root unauthenticated browser", "GET", "/", "text/html", http.StatusFound, "redirect"},
		{"GET API unauthenticated", "GET", "/api/data", "application/json", http.StatusUnauthorized, "json_error"},
		{"POST API unauthenticated", "POST", "/api/upload", "application/json", http.StatusUnauthorized, "json_error"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Accept", tc.accept)
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, req)

			assert.Equal(t, tc.expectedStatus, w.Code)

			if tc.expectedAction == "redirect" {
				location := w.Header().Get("Location")
				assert.Contains(t, location, "/login")
			} else if tc.expectedAction == "json_error" {
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)
				assert.Equal(t, "unauthenticated", response["error"])
			}
		})
	}

	// Test authenticated requests using session token
	t.Run("authenticated requests work", func(t *testing.T) {
		// Create valid session token manually for this test
		token := store.NewToken()
		store.Add(token)

		// Now test authenticated API request
		apiReq := httptest.NewRequest("GET", "/api/data", nil)
		apiReq.AddCookie(&http.Cookie{Name: "slimserve_session", Value: token})
		apiW := httptest.NewRecorder()
		engine.ServeHTTP(apiW, apiReq)

		assert.Equal(t, http.StatusOK, apiW.Code)

		var response map[string]interface{}
		err := json.Unmarshal(apiW.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "protected", response["data"])
	})
}
