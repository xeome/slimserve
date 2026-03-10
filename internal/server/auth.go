package server

import (
	"net/http"
	"net/url"
	"strings"

	"slimserve/internal/config"

	"github.com/gin-gonic/gin"
)

const (
	sessionCookieName = "slimserve_session"
	loginPath         = "/login"
	staticPrefix      = "/static/"
	adminPrefix       = "/admin"
	faviconPath       = "/favicon.ico"
	loginQueryPrefix  = "/login?next="
)

var unauthorizedResponse = gin.H{"error": "unauthenticated"}

func SessionAuthMiddleware(cfg *config.Config, store *SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cfg.EnableAuth {
			c.Next()
			return
		}

		path := c.Request.URL.Path

		if path == loginPath {
			c.Next()
			return
		}

		if strings.HasPrefix(path, staticPrefix) || path == faviconPath {
			c.Next()
			return
		}

		if strings.HasPrefix(path, adminPrefix) {
			c.Next()
			return
		}

		cookie, err := c.Cookie(sessionCookieName)
		if err == nil && store.Valid(cookie) {
			c.Next()
			return
		}

		accept := c.GetHeader("Accept")
		xmlHttpRequest := c.GetHeader("X-Requested-With")
		isBrowser := strings.Contains(accept, "text/html") && xmlHttpRequest != "XMLHttpRequest"

		if isBrowser {
			nextURL := url.QueryEscape(c.Request.URL.RequestURI())
			c.Redirect(http.StatusFound, loginQueryPrefix+nextURL)
			c.Abort()
		} else {
			c.JSON(http.StatusUnauthorized, unauthorizedResponse)
			c.Abort()
		}
	}
}
