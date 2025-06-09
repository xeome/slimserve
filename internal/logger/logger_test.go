package logger

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"slimserve/internal/config"
)

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		name         string
		levelStr     string
		expectedLvl  zerolog.Level
		expectErr    bool
		errSubstring string
	}{
		{"Empty String", "", zerolog.InfoLevel, false, ""},
		{"Valid Level Lowercase", "debug", zerolog.DebugLevel, false, ""},
		{"Valid Level Uppercase", "WARN", zerolog.WarnLevel, false, ""},
		{"Valid Level Mixed Case", "Error", zerolog.ErrorLevel, false, ""},
		{"Warning Alias", "warning", zerolog.WarnLevel, false, ""},
		{"Invalid Level", "invalid", zerolog.InfoLevel, false, ""},
		{"Trace Level", "trace", zerolog.TraceLevel, false, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lvl, err := parseLogLevel(tc.levelStr)
			assert.Equal(t, tc.expectedLvl, lvl)
			if tc.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errSubstring)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

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

// safeBuffer is a goroutine-safe bytes.Buffer
type safeBuffer struct {
	b  bytes.Buffer
	mu sync.Mutex
}

func (s *safeBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestInfof(t *testing.T) {
	originalLog := Log
	defer func() { Log = originalLog }()
	var buf safeBuffer
	Log = zerolog.New(&buf)

	Infof("hello %s", "world")

	logOutput := buf.String()
	assert.Contains(t, logOutput, `"level":"info"`)
	assert.Contains(t, logOutput, `"message":"hello world"`)
}

func TestDebugf(t *testing.T) {
	originalLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	defer zerolog.SetGlobalLevel(originalLevel)

	originalLog := Log
	defer func() { Log = originalLog }()
	var buf safeBuffer
	Log = zerolog.New(&buf)

	Debugf("hello %s", "world")

	logOutput := buf.String()
	assert.Contains(t, logOutput, `"level":"debug"`)
	assert.Contains(t, logOutput, `"message":"hello world"`)
}

func TestWarnf(t *testing.T) {
	originalLog := Log
	defer func() { Log = originalLog }()
	var buf safeBuffer
	Log = zerolog.New(&buf)

	Warnf("hello %s", "world")

	logOutput := buf.String()
	assert.Contains(t, logOutput, `"level":"warn"`)
	assert.Contains(t, logOutput, `"message":"hello world"`)
}

func TestErrorf(t *testing.T) {
	originalLog := Log
	defer func() { Log = originalLog }()
	var buf safeBuffer
	Log = zerolog.New(&buf)

	Errorf("hello %s", "world")

	logOutput := buf.String()
	assert.Contains(t, logOutput, `"level":"error"`)
	assert.Contains(t, logOutput, `"message":"hello world"`)
}

// New sets up a temporary logger for testing purposes, redirecting its output.
// It returns the logger and a function to read the output.
func New(w io.Writer) (*zerolog.Logger, func() string) {
	if w == nil {
		w = io.Discard
	}
	log := zerolog.New(w)
	return &log, func() string {
		if buf, ok := w.(*bytes.Buffer); ok {
			return buf.String()
		}
		return ""
	}
}
