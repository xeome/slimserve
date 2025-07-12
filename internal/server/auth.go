package server

import (
	"net/http"
	"net/url"
	"strings"

	"slimserve/internal/config"

	"github.com/gin-gonic/gin"
)

// Pre-computed constants to avoid string allocations
const (
	sessionCookieName = "slimserve_session"
	loginPath         = "/login"
	staticPrefix      = "/static/"
	adminPrefix       = "/admin"
	faviconPath       = "/favicon.ico"
	loginQueryPrefix  = "/login?next="
)

var unauthorizedResponse = gin.H{"error": "unauthenticated"}

// SessionAuthMiddleware handles authentication by checking session cookies.
// If unauthenticated, it redirects browsers to /login or returns 401 JSON for API requests.
func SessionAuthMiddleware(cfg *config.Config, store *SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		// If authentication is disabled, proceed without checks
		if !cfg.EnableAuth {
			c.Next()
			return
		}

		path := c.Request.URL.Path

		if path == loginPath {
			c.Next()
			return
		}

		// Skip authentication for static assets
		if strings.HasPrefix(path, staticPrefix) || path == faviconPath {
			c.Next()
			return
		}

		// Skip authentication for admin routes (admin has its own auth)
		if strings.HasPrefix(path, adminPrefix) {
			c.Next()
			return
		}

		// Check for session cookie
		cookie, err := c.Cookie(sessionCookieName)
		if err == nil && store.Valid(cookie) {
			c.Next()
			return
		}

		// Determine if this is a browser request
		accept := c.GetHeader("Accept")
		xmlHttpRequest := c.GetHeader("X-Requested-With")
		isBrowser := strings.Contains(accept, "text/html") && xmlHttpRequest != "XMLHttpRequest"

		if isBrowser {
			nextURL := url.QueryEscape(c.Request.URL.RequestURI())
			var redirectURL strings.Builder
			redirectURL.Grow(len(loginQueryPrefix) + len(nextURL))
			redirectURL.WriteString(loginQueryPrefix)
			redirectURL.WriteString(nextURL)
			c.Redirect(http.StatusFound, redirectURL.String())
			c.Abort()
		} else {
			c.JSON(http.StatusUnauthorized, unauthorizedResponse)
			c.Abort()
		}
	}
}
