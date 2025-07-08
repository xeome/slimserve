package server

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"slimserve/internal/config"
	"slimserve/internal/logger"

	"github.com/gin-gonic/gin"
)

// AdminAuthMiddleware handles authentication specifically for admin endpoints.
// It requires separate admin credentials and provides enhanced security for admin operations.
func AdminAuthMiddleware(cfg *config.Config, store *SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		// If admin is disabled, deny access
		if !cfg.EnableAdmin {
			c.JSON(http.StatusNotFound, gin.H{"error": "admin interface not enabled"})
			c.Abort()
			return
		}

		// Skip authentication for admin login routes
		if c.Request.URL.Path == "/admin/login" {
			c.Next()
			return
		}

		// Skip authentication for admin static assets
		if strings.HasPrefix(c.Request.URL.Path, "/admin/static/") {
			c.Next()
			return
		}

		// Check for admin session cookie
		cookie, err := c.Cookie("slimserve_admin_session")
		if err == nil && store.ValidAdmin(cookie) {
			// Valid admin session found, proceed
			c.Next()
			return
		}

		// Log unauthorized admin access attempt
		logger.Log.Warn().
			Str("ip", c.ClientIP()).
			Str("path", c.Request.URL.Path).
			Str("user_agent", c.GetHeader("User-Agent")).
			Msg("Unauthorized admin access attempt")

		// Determine if this is a browser request
		accept := c.GetHeader("Accept")
		xmlHttpRequest := c.GetHeader("X-Requested-With")
		isBrowser := strings.Contains(accept, "text/html") && xmlHttpRequest != "XMLHttpRequest"

		if isBrowser {
			// Redirect browsers to admin login page with next parameter
			nextURL := url.QueryEscape(c.Request.URL.RequestURI())
			c.Redirect(http.StatusFound, "/admin/login?next="+nextURL)
			c.Abort()
		} else {
			// Return 401 JSON for API requests
			c.JSON(http.StatusUnauthorized, gin.H{"error": "admin authentication required"})
			c.Abort()
		}
	}
}

// AdminRateLimitMiddleware implements rate limiting for admin operations
func AdminRateLimitMiddleware() gin.HandlerFunc {
	// Simple in-memory rate limiter for admin operations
	type rateLimiter struct {
		requests map[string][]time.Time
	}

	limiter := &rateLimiter{
		requests: make(map[string][]time.Time),
	}

	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now()

		// Clean old requests (older than 1 minute)
		if requests, exists := limiter.requests[ip]; exists {
			var validRequests []time.Time
			for _, reqTime := range requests {
				if now.Sub(reqTime) < time.Minute {
					validRequests = append(validRequests, reqTime)
				}
			}
			limiter.requests[ip] = validRequests
		}

		// Check rate limit (max 30 requests per minute for admin operations)
		if len(limiter.requests[ip]) >= 30 {
			logger.Log.Warn().
				Str("ip", ip).
				Msg("Admin rate limit exceeded")
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			c.Abort()
			return
		}

		// Add current request
		limiter.requests[ip] = append(limiter.requests[ip], now)
		c.Next()
	}
}

// CSRFProtectionMiddleware provides CSRF protection for admin operations
func CSRFProtectionMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip CSRF check for GET requests and login
		if c.Request.Method == "GET" || c.Request.URL.Path == "/admin/login" {
			c.Next()
			return
		}

		// Check for CSRF token in header or form
		token := c.GetHeader("X-CSRF-Token")
		if token == "" {
			token = c.PostForm("csrf_token")
		}

		// Get expected token from session
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

// InputValidationMiddleware validates and sanitizes input data
func InputValidationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Validate Content-Length to prevent large payloads
		if c.Request.ContentLength > 100*1024*1024 { // 100MB limit
			logger.Log.Warn().
				Str("ip", c.ClientIP()).
				Int64("content_length", c.Request.ContentLength).
				Msg("Request payload too large")
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "payload too large"})
			c.Abort()
			return
		}

		// Validate Content-Type for POST requests
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
