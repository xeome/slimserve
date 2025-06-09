package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// showLogin renders the login template with optional error message and next path from query param
func (s *Server) showLogin(c *gin.Context) {
	// Get the next parameter from query string, default to "/"
	next := c.DefaultQuery("next", "/")
	next = validateRedirectURL(next)

	// Get error message from query string if present
	errorMsg := c.Query("error")

	// Prepare template data
	data := gin.H{
		"next": next,
	}

	// Add error message if present
	if errorMsg != "" {
		data["error"] = errorMsg
	}

	// Render the login template
	if err := s.loginTmpl.ExecuteTemplate(c.Writer, "base", data); err != nil {
		http.Error(c.Writer, "failed to render login page", http.StatusInternalServerError)
	}
}

// doLogin handles login form submission (both form-encoded and JSON)
func (s *Server) doLogin(c *gin.Context) {
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

	// Validate and sanitize next URL to prevent open redirect attacks
	next = validateRedirectURL(next)

	// Validate credentials against config
	if !s.validateCredentials(username, password) {
		// Handle failure based on Accept header
		acceptHeader := c.GetHeader("Accept")
		if strings.Contains(acceptHeader, "application/json") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		} else {
			// Re-render login page with error
			data := gin.H{
				"error": "Invalid username or password",
				"next":  next,
			}
			if err := s.loginTmpl.ExecuteTemplate(c.Writer, "base", data); err != nil {
				http.Error(c.Writer, "failed to render login page", http.StatusInternalServerError)
			}
			return
		}
	}

	// Generate session token
	token := s.sessionStore.NewToken()
	s.sessionStore.Add(token)

	// Set session cookie with enhanced security attributes
	secure := c.Request.TLS != nil // Set Secure flag only for HTTPS
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		"slimserve_session", // name (matches auth.go line 24)
		token,               // value
		0,                   // maxAge (session cookie)
		"/",                 // path
		"",                  // domain
		secure,              // secure (true for HTTPS)
		true,                // httpOnly
	)

	// Handle success based on content type
	if strings.Contains(contentType, "application/json") {
		c.JSON(http.StatusOK, gin.H{"success": true, "redirect": next})
	} else {
		// Redirect to next page
		c.Redirect(http.StatusFound, next)
	}
}

// validateCredentials performs constant-time credential comparison
func (s *Server) validateCredentials(username, password string) bool {
	// Check if authentication is enabled and credentials are configured
	if !s.config.EnableAuth || s.config.Username == "" || s.config.Password == "" {
		return false
	}

	// Constant-time comparison to prevent timing attacks
	usernameMatch := constantTimeEqual(username, s.config.Username)
	passwordMatch := constantTimeEqual(password, s.config.Password)

	return usernameMatch && passwordMatch
}

// constantTimeEqual performs constant-time string comparison
func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}

	result := byte(0)
	for i := 0; i < len(a); i++ {
		result |= a[i] ^ b[i]
	}

	return result == 0
}

// validateRedirectURL validates the next URL to prevent open redirect attacks
func validateRedirectURL(next string) string {
	// Default to root if empty
	if next == "" {
		return "/"
	}

	// Only allow relative paths that start with "/" and don't contain "://" or start with "//"
	if !strings.HasPrefix(next, "/") || strings.Contains(next, "://") || strings.HasPrefix(next, "//") {
		return "/"
	}

	return next
}
