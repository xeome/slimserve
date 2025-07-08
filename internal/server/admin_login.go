package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"slimserve/internal/logger"

	"github.com/gin-gonic/gin"
)

// showAdminLogin renders the admin login template
func (s *Server) showAdminLogin(c *gin.Context) {
	// Get the next parameter from query string, default to "/admin"
	next := c.DefaultQuery("next", "/admin")
	next = validateAdminRedirectURL(next)

	// Get error message from query string if present
	errorMsg := c.Query("error")

	// Generate CSRF token
	csrfToken := generateCSRFToken()

	// Set CSRF token cookie
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		"slimserve_csrf_token",
		csrfToken,
		0, // session cookie
		"/admin",
		"",
		c.Request.TLS != nil, // secure for HTTPS
		true,                 // httpOnly
	)

	// Prepare template data
	data := gin.H{
		"next":       next,
		"csrf_token": csrfToken,
	}

	// Add error message if present
	if errorMsg != "" {
		data["error"] = errorMsg
	}

	// Check if admin login template is loaded
	if s.adminLoginTmpl == nil {
		logger.Log.Error().Msg("Admin login template not loaded")
		http.Error(c.Writer, "admin login template not loaded", http.StatusInternalServerError)
		return
	}

	// Render the admin login template
	c.Status(http.StatusOK)
	if err := s.adminLoginTmpl.ExecuteTemplate(c.Writer, "admin_base", data); err != nil {
		logger.Log.Error().Err(err).Msg("Failed to render admin login page")
		http.Error(c.Writer, "failed to render admin login page", http.StatusInternalServerError)
	}
}

// doAdminLogin handles admin login form submission
func (s *Server) doAdminLogin(c *gin.Context) {
	var username, password, next string

	// Parse credentials based on content type
	contentType := c.GetHeader("Content-Type")
	if strings.Contains(contentType, "application/json") {
		// Handle JSON request
		var jsonData struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Next     string `json:"next"`
		}

		if err := c.ShouldBindJSON(&jsonData); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request format"})
			return
		}

		username = jsonData.Username
		password = jsonData.Password
		next = jsonData.Next
	} else {
		// Handle form-encoded request
		username = c.PostForm("username")
		password = c.PostForm("password")
		next = c.PostForm("next")
	}

	// Validate and sanitize next URL
	next = validateAdminRedirectURL(next)

	// Validate admin credentials
	if !s.validateAdminCredentials(username, password) {
		// Log failed login attempt
		logger.Log.Warn().
			Str("ip", c.ClientIP()).
			Str("username", username).
			Str("user_agent", c.GetHeader("User-Agent")).
			Msg("Failed admin login attempt")

		// Handle failure based on Accept header
		acceptHeader := c.GetHeader("Accept")
		if strings.Contains(acceptHeader, "application/json") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid admin credentials"})
			return
		} else {
			// Re-render login page with error
			data := gin.H{
				"error":      "Invalid admin username or password",
				"next":       next,
				"csrf_token": generateCSRFToken(),
			}
			c.Status(http.StatusUnauthorized)
			if err := s.adminLoginTmpl.ExecuteTemplate(c.Writer, "admin_base", data); err != nil {
				http.Error(c.Writer, "failed to render admin login page", http.StatusInternalServerError)
			}
			return
		}
	}

	// Generate admin session token
	token := s.sessionStore.NewToken()
	s.sessionStore.AddAdmin(token)

	// Log successful admin login
	logger.Log.Info().
		Str("ip", c.ClientIP()).
		Str("username", username).
		Msg("Successful admin login")

	// Log activity
	if s.adminHandler != nil {
		s.adminHandler.activityStore.AddActivity("login", fmt.Sprintf("Admin login: %s", username), c.ClientIP(), "")
	}

	// Set admin session cookie with enhanced security
	secure := c.Request.TLS != nil
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		"slimserve_admin_session",
		token,
		0,        // session cookie
		"/admin", // restrict to admin paths
		"",
		secure, // secure for HTTPS
		true,   // httpOnly
	)

	// Handle success based on content type
	if strings.Contains(contentType, "application/json") {
		c.JSON(http.StatusOK, gin.H{"success": true, "redirect": next})
	} else {
		// Redirect to next page
		c.Redirect(http.StatusFound, next)
	}
}

// validateAdminCredentials performs constant-time admin credential comparison
func (s *Server) validateAdminCredentials(username, password string) bool {
	// Check if admin is enabled and credentials are configured
	if !s.config.EnableAdmin || s.config.AdminUsername == "" || s.config.AdminPassword == "" {
		return false
	}

	// Constant-time comparison to prevent timing attacks
	usernameMatch := constantTimeEqual(username, s.config.AdminUsername)
	passwordMatch := constantTimeEqual(password, s.config.AdminPassword)

	return usernameMatch && passwordMatch
}

// validateAdminRedirectURL validates and sanitizes admin redirect URLs
func validateAdminRedirectURL(next string) string {
	if next == "" {
		return "/admin"
	}

	// Only allow relative URLs starting with /admin
	if !strings.HasPrefix(next, "/admin") {
		return "/admin"
	}

	// Prevent open redirect attacks by ensuring it's a relative URL
	if strings.Contains(next, "://") || strings.HasPrefix(next, "//") {
		return "/admin"
	}

	return next
}

// generateCSRFToken generates a cryptographically secure CSRF token
func generateCSRFToken() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to a simple token if crypto/rand fails
		logger.Log.Error().Err(err).Msg("Failed to generate CSRF token")
		return "fallback-token"
	}
	return hex.EncodeToString(bytes)
}

// doAdminLogout handles admin logout
func (s *Server) doAdminLogout(c *gin.Context) {
	// Get admin session token
	cookie, err := c.Cookie("slimserve_admin_session")
	if err == nil {
		// Remove token from session store
		s.sessionStore.RemoveAdmin(cookie)
	}

	// Clear admin session cookie
	c.SetCookie(
		"slimserve_admin_session",
		"",
		-1, // expire immediately
		"/admin",
		"",
		c.Request.TLS != nil,
		true,
	)

	// Clear CSRF token cookie
	c.SetCookie(
		"slimserve_csrf_token",
		"",
		-1, // expire immediately
		"/admin",
		"",
		c.Request.TLS != nil,
		true,
	)

	// Log admin logout
	logger.Log.Info().
		Str("ip", c.ClientIP()).
		Msg("Admin logout")

	// Redirect to admin login
	c.Redirect(http.StatusFound, "/admin/login")
}

// showAdminDashboard renders the admin dashboard
func (s *Server) showAdminDashboard(c *gin.Context) {
	data := gin.H{
		"Title":      "Dashboard",
		"csrf_token": generateCSRFToken(),
	}

	// Check if admin template is loaded
	if s.adminTmpl == nil {
		logger.Log.Error().Msg("Admin template not loaded")
		http.Error(c.Writer, "admin template not loaded", http.StatusInternalServerError)
		return
	}

	c.Status(http.StatusOK)
	if err := s.adminTmpl.ExecuteTemplate(c.Writer, "admin_dashboard.html", data); err != nil {
		logger.Log.Error().Err(err).Msg("Failed to render admin dashboard")
		http.Error(c.Writer, "failed to render admin dashboard", http.StatusInternalServerError)
	}
}

// showAdminUpload renders the admin upload page
func (s *Server) showAdminUpload(c *gin.Context) {
	data := gin.H{
		"Title":           "Upload Files",
		"csrf_token":      generateCSRFToken(),
		"max_upload_size": s.config.MaxUploadSizeMB,
		"allowed_types":   strings.Join(s.config.AllowedUploadTypes, ", "),
	}

	c.Status(http.StatusOK)
	if err := s.adminTmpl.ExecuteTemplate(c.Writer, "admin_upload.html", data); err != nil {
		logger.Log.Error().Err(err).Msg("Failed to render admin upload page")
		http.Error(c.Writer, "failed to render admin upload page", http.StatusInternalServerError)
	}
}

// showAdminFiles renders the admin file management page
func (s *Server) showAdminFiles(c *gin.Context) {
	data := gin.H{
		"Title":      "File Management",
		"csrf_token": generateCSRFToken(),
	}

	c.Status(http.StatusOK)
	if err := s.adminTmpl.ExecuteTemplate(c.Writer, "admin_files.html", data); err != nil {
		logger.Log.Error().Err(err).Msg("Failed to render admin files page")
		http.Error(c.Writer, "failed to render admin files page", http.StatusInternalServerError)
	}
}

// showAdminConfig renders the admin configuration page
func (s *Server) showAdminConfig(c *gin.Context) {
	data := gin.H{
		"Title":      "Configuration",
		"csrf_token": generateCSRFToken(),
	}

	c.Status(http.StatusOK)
	if err := s.adminTmpl.ExecuteTemplate(c.Writer, "admin_config.html", data); err != nil {
		logger.Log.Error().Err(err).Msg("Failed to render admin config page")
		http.Error(c.Writer, "failed to render admin config page", http.StatusInternalServerError)
	}
}

// showAdminStatus renders the admin system status page
func (s *Server) showAdminStatus(c *gin.Context) {
	data := gin.H{
		"Title":      "System Status",
		"csrf_token": generateCSRFToken(),
	}

	c.Status(http.StatusOK)
	if err := s.adminTmpl.ExecuteTemplate(c.Writer, "admin_status.html", data); err != nil {
		logger.Log.Error().Err(err).Msg("Failed to render admin status page")
		http.Error(c.Writer, "failed to render admin status page", http.StatusInternalServerError)
	}
}
