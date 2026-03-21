package logger

import (
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"slimserve/internal/config"
)

// Log is the global logger instance
var Log zerolog.Logger

// Init configures global zerolog defaults based on Config.LogLevel.
// Accepts "panic","fatal","error","warn","info","debug","trace" (case-insensitive).
func Init(cfg *config.Config) error {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	level, err := parseLogLevel(cfg.LogLevel)
	if err != nil {
		return err
	}

	zerolog.SetGlobalLevel(level)
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr}).With().Caller().Logger()
	Log = log.Logger

	return nil
}

// parseLogLevel converts string log level to zerolog.Level
func parseLogLevel(levelStr string) (zerolog.Level, error) {
	if levelStr == "" {
		return zerolog.InfoLevel, nil // Default level
	}
	// Handle "warning" alias for "warn"
	if strings.ToLower(levelStr) == "warning" {
		levelStr = "warn"
	}
	level, err := zerolog.ParseLevel(levelStr)
	if err != nil {
		Log.Warn().Str("level", levelStr).Msg("Invalid log level, using info")
		return zerolog.InfoLevel, nil
	}
	return level, nil
}

// Middleware returns a gin middleware for HTTP request logging
func Middleware() gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method

		c.Next()

		duration := time.Since(start)
		status := c.Writer.Status()
		size := c.Writer.Size()
		clientIP := c.ClientIP()
		userAgent := c.Request.UserAgent()
		Log.Info().
			Str("method", method).
			Str("path", path).
			Int("status", status).
			Int("size", size).
			Dur("duration", duration).
			Str("remote_ip", clientIP).
			Str("user_agent", userAgent).
			Msg("HTTP request")
	})
}
