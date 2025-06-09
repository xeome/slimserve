package server

import (
	"net/http"
	"net/url"
	"strings"

	"slimserve/internal/config"

	"github.com/gin-gonic/gin"
)

// SessionAuthMiddleware handles authentication by checking session cookies.
// If unauthenticated, it redirects browsers to /login or returns 401 JSON for API requests.
func SessionAuthMiddleware(cfg *config.Config, store *SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		// If authentication is disabled, proceed without checks
		if !cfg.EnableAuth {
			c.Next()
			return
		}

		// Skip authentication for login routes
		if c.Request.URL.Path == "/login" {
			c.Next()
			return
		}

		// Skip authentication for static assets
		if strings.HasPrefix(c.Request.URL.Path, "/static/") || c.Request.URL.Path == "/favicon.ico" {
			c.Next()
			return
		}

		// Check for session cookie
		cookie, err := c.Cookie("slimserve_session")
		if err == nil && store.Valid(cookie) {
			// Valid session found, proceed
			c.Next()
			return
		}

		// Determine if this is a browser request
		accept := c.GetHeader("Accept")
		xmlHttpRequest := c.GetHeader("X-Requested-With")
		isBrowser := strings.Contains(accept, "text/html") && xmlHttpRequest != "XMLHttpRequest"

		if isBrowser {
			// Redirect browsers to login page with next parameter
			nextURL := url.QueryEscape(c.Request.URL.RequestURI())
			c.Redirect(http.StatusFound, "/login?next="+nextURL)
			c.Abort()
		} else {
			// Return 401 JSON for API requests
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
			c.Abort()
		}
	}
}
