package auth

import (
	"net/http"
	"net/url"
	"strings"

	"slimserve/internal/config"

	"github.com/gin-gonic/gin"
)

const (
	SessionCookieName = "slimserve_session"
	LoginPath         = "/login"
	StaticPrefix      = "/static/"
	AdminPrefix       = "/admin"
	FaviconPath       = "/favicon.ico"
	LoginQueryPrefix  = "/login?next="
)

var unauthorizedResponse = gin.H{"error": "unauthenticated"}

func SessionAuthMiddleware(cfg *config.Config, store *SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cfg.EnableAuth {
			c.Next()
			return
		}

		path := c.Request.URL.Path

		if path == LoginPath {
			c.Next()
			return
		}

		if strings.HasPrefix(path, StaticPrefix) || path == FaviconPath {
			c.Next()
			return
		}

		if strings.HasPrefix(path, AdminPrefix) {
			c.Next()
			return
		}

		cookie, err := c.Cookie(SessionCookieName)
		if err == nil && store.Valid(cookie) {
			c.Next()
			return
		}

		accept := c.GetHeader("Accept")
		xmlHttpRequest := c.GetHeader("X-Requested-With")
		isBrowser := strings.Contains(accept, "text/html") && xmlHttpRequest != "XMLHttpRequest"

		if isBrowser {
			nextURL := url.QueryEscape(c.Request.URL.RequestURI())
			c.Redirect(http.StatusFound, LoginQueryPrefix+nextURL)
			c.Abort()
		} else {
			c.JSON(http.StatusUnauthorized, unauthorizedResponse)
			c.Abort()
		}
	}
}
