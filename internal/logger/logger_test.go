package logger

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"slimserve/internal/config"
)

func TestInit(t *testing.T) {
	// Keep original logger
	originalLogger := log.Logger
	// Keep original stderr
	originalStderr := os.Stderr
	// Restore at the end of the test
	defer func() {
		log.Logger = originalLogger
		os.Stderr = originalStderr
	}()

	_, w, _ := os.Pipe()
	os.Stderr = w

	cfg := &config.Config{LogLevel: "debug"}
	err := Init(cfg)
	require.NoError(t, err)
	assert.Equal(t, zerolog.DebugLevel, zerolog.GlobalLevel())

	// Test invalid level
	cfg.LogLevel = "invalid"
	err = Init(cfg)
	assert.NoError(t, err)                                    // Should default to Info, not error
	assert.Equal(t, zerolog.InfoLevel, zerolog.GlobalLevel()) // Fallback
}

func TestMiddleware(t *testing.T) {
	// 1. Setup: redirect logger output to a buffer
	var logBuf bytes.Buffer
	Log = zerolog.New(&logBuf).With().Timestamp().Logger()

	// 2. Setup Gin server
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Middleware())
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// 3. Make request
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("User-Agent", "test-agent")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 4. Assertions
	assert.Equal(t, http.StatusOK, w.Code)

	// 5. Verify log output
	var logOutput map[string]interface{}
	err := json.Unmarshal(logBuf.Bytes(), &logOutput)
	require.NoError(t, err, "Failed to unmarshal log output")

	assert.Equal(t, "HTTP request", logOutput["message"])
	assert.Equal(t, http.MethodGet, logOutput["method"])
	assert.Equal(t, "/test", logOutput["path"])
	assert.Equal(t, float64(http.StatusOK), logOutput["status"])
	assert.Contains(t, logOutput, "duration")
	assert.Equal(t, "test-agent", logOutput["user_agent"])
}
