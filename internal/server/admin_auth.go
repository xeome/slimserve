package server

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"slimserve/internal/config"
	"slimserve/internal/logger"

	"github.com/gin-gonic/gin"
)

func AdminAuthMiddleware(cfg *config.Config, store *SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cfg.EnableAdmin {
			c.JSON(http.StatusNotFound, gin.H{"error": "admin interface not enabled"})
			c.Abort()
			return
		}

		if c.Request.URL.Path == "/admin/login" {
			c.Next()
			return
		}

		if strings.HasPrefix(c.Request.URL.Path, "/admin/static/") {
			c.Next()
			return
		}

		cookie, err := c.Cookie("slimserve_admin_session")
		if err == nil && store.ValidAdmin(cookie) {
			c.Next()
			return
		}

		logger.Log.Warn().
			Str("ip", c.ClientIP()).
			Str("path", c.Request.URL.Path).
			Str("user_agent", c.GetHeader("User-Agent")).
			Msg("Unauthorized admin access attempt")

		accept := c.GetHeader("Accept")
		xmlHttpRequest := c.GetHeader("X-Requested-With")
		isBrowser := strings.Contains(accept, "text/html") && xmlHttpRequest != "XMLHttpRequest"

		if isBrowser {
			nextURL := url.QueryEscape(c.Request.URL.RequestURI())
			c.Redirect(http.StatusFound, "/admin/login?next="+nextURL)
			c.Abort()
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "admin authentication required"})
			c.Abort()
		}
	}
}

func AdminRateLimitMiddleware() gin.HandlerFunc {
	type rateLimiter struct {
		mu       sync.Mutex
		requests map[string][]time.Time
		stopCh   chan struct{}
	}

	limiter := &rateLimiter{
		requests: make(map[string][]time.Time),
		stopCh:   make(chan struct{}),
	}

	go func() {
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				limiter.mu.Lock()
				now := time.Now()
				for ip, requests := range limiter.requests {
					var validRequests []time.Time
					for _, reqTime := range requests {
						if now.Sub(reqTime) < time.Minute {
							validRequests = append(validRequests, reqTime)
						}
					}
					if len(validRequests) == 0 {
						delete(limiter.requests, ip)
					} else {
						limiter.requests[ip] = validRequests
					}
				}
				limiter.mu.Unlock()
			case <-limiter.stopCh:
				return
			}
		}
	}()

	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now()

		limiter.mu.Lock()
		if requests, exists := limiter.requests[ip]; exists {
			var validRequests []time.Time
			for _, reqTime := range requests {
				if now.Sub(reqTime) < time.Minute {
					validRequests = append(validRequests, reqTime)
				}
			}
			limiter.requests[ip] = validRequests
		}

		if len(limiter.requests[ip]) >= 30 {
			limiter.mu.Unlock()
			logger.Log.Warn().
				Str("ip", ip).
				Msg("Admin rate limit exceeded")
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			c.Abort()
			return
		}

		limiter.requests[ip] = append(limiter.requests[ip], now)
		limiter.mu.Unlock()
		c.Next()
	}
}

func CSRFProtectionMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == "GET" || c.Request.URL.Path == "/admin/login" {
			c.Next()
			return
		}

		token := c.GetHeader("X-CSRF-Token")
		if token == "" {
			token = c.PostForm("csrf_token")
		}

		expectedToken, err := c.Cookie("slimserve_csrf_token")
		if err != nil || token == "" || !constantTimeEqual(token, expectedToken) {
			logger.Log.Warn().
				Str("ip", c.ClientIP()).
				Str("path", c.Request.URL.Path).
				Str("user_agent", c.GetHeader("User-Agent")).
				Bool("token_present", token != "").
				Bool("cookie_present", err == nil).
				Msg("CSRF token validation failed")
			c.JSON(http.StatusForbidden, gin.H{"error": "invalid CSRF token"})
			c.Abort()
			return
		}

		c.Next()
	}
}

func InputValidationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > 100*1024*1024 {
			logger.Log.Warn().
				Str("ip", c.ClientIP()).
				Int64("content_length", c.Request.ContentLength).
				Msg("Request payload too large")
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "payload too large"})
			c.Abort()
			return
		}

		if c.Request.Method == "POST" {
			contentType := c.GetHeader("Content-Type")
			if contentType == "" {
				logger.Log.Warn().
					Str("ip", c.ClientIP()).
					Str("path", c.Request.URL.Path).
					Msg("Missing Content-Type header")
				c.JSON(http.StatusBadRequest, gin.H{"error": "missing content type"})
				c.Abort()
				return
			}
		}

		c.Next()
	}
}
