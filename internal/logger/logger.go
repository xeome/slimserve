package logger

import (
	"errors"
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
	// Configure time format for compact JSON
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	// Parse log level from config
	level, err := parseLogLevel(cfg.LogLevel)
	if err != nil {
		return err
	}

	// Set global log level
	zerolog.SetGlobalLevel(level)

	// Configure logger output
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr}).With().Caller().Logger()

	// Initialize global logger
	Log = log.Logger

	return nil
}

// parseLogLevel converts string log level to zerolog.Level
func parseLogLevel(levelStr string) (zerolog.Level, error) {
	switch strings.ToLower(levelStr) {
	case "panic":
		return zerolog.PanicLevel, nil
	case "fatal":
		return zerolog.FatalLevel, nil
	case "error":
		return zerolog.ErrorLevel, nil
	case "warn", "warning":
		return zerolog.WarnLevel, nil
	case "info":
		return zerolog.InfoLevel, nil
	case "debug":
		return zerolog.DebugLevel, nil
	case "trace":
		return zerolog.TraceLevel, nil
	case "":
		// Default to info if empty
		return zerolog.InfoLevel, nil
	default:
		return zerolog.InfoLevel, errors.New("unknown log level: " + levelStr)
	}
}

// Middleware returns a gin middleware for HTTP request logging
func Middleware() gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		// Record start time
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method

		// Process request
		c.Next()

		// Calculate duration
		duration := time.Since(start)
		status := c.Writer.Status()
		size := c.Writer.Size()

		// Extract client information
		clientIP := c.ClientIP()
		userAgent := c.Request.UserAgent()

		// Log request
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

// Ergonomic helper functions for common logging patterns

// Infof logs an info message with formatting
func Infof(format string, v ...interface{}) {
	Log.Info().Msgf(format, v...)
}

// Debugf logs a debug message with formatting
func Debugf(format string, v ...interface{}) {
	Log.Debug().Msgf(format, v...)
}

// Errorf logs an error message with formatting
func Errorf(format string, v ...interface{}) {
	Log.Error().Msgf(format, v...)
}

// Warnf logs a warning message with formatting
func Warnf(format string, v ...interface{}) {
	Log.Warn().Msgf(format, v...)
}

// Info logs a simple info message
func Info(msg string) {
	Log.Info().Msg(msg)
}

// Debug logs a simple debug message
func Debug(msg string) {
	Log.Debug().Msg(msg)
}

// Error logs a simple error message
func Error(msg string) {
	Log.Error().Msg(msg)
}

// Warn logs a simple warning message
func Warn(msg string) {
	Log.Warn().Msg(msg)
}
