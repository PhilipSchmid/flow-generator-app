package logging

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestGetLogLevel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected zapcore.Level
	}{
		{"debug level", "debug", zap.DebugLevel},
		{"info level", "info", zap.InfoLevel},
		{"warn level", "warn", zap.WarnLevel},
		{"error level", "error", zap.ErrorLevel},
		{"default level", "unknown", zap.InfoLevel},
		{"empty string", "", zap.InfoLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getLogLevel(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInitLogger(t *testing.T) {
	tests := []struct {
		name      string
		logFormat string
		logLevel  string
	}{
		{"json format with debug", "json", "debug"},
		{"human format with info", "human", "info"},
		{"default format with error", "default", "error"},
		{"empty format with warn", "", "warn"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			InitLogger(tt.logFormat, tt.logLevel)

			assert.NotNil(t, Logger)

			Logger.Info("test message")
			Logger.Debug("debug message")
			Logger.Error("error message")
		})
	}
}

func TestLoggerOutput(t *testing.T) {
	core, recorded := observer.New(zapcore.InfoLevel)

	logger := zap.New(core).Sugar()

	oldLogger := Logger
	Logger = logger
	defer func() { Logger = oldLogger }()

	Logger.Info("info message")
	Logger.Debug("debug message") // Won't be recorded due to level
	Logger.Warn("warn message")
	Logger.Error("error message")

	logs := recorded.All()
	assert.Len(t, logs, 3) // debug should not be recorded

	assert.Equal(t, "info message", logs[0].Message)
	assert.Equal(t, zapcore.InfoLevel, logs[0].Level)

	assert.Equal(t, "warn message", logs[1].Message)
	assert.Equal(t, zapcore.WarnLevel, logs[1].Level)

	assert.Equal(t, "error message", logs[2].Message)
	assert.Equal(t, zapcore.ErrorLevel, logs[2].Level)
}

func TestLoggerWithFields(t *testing.T) {
	InitLogger("json", "info")

	assert.NotPanics(t, func() {
		Logger.Infow("message with fields",
			"key1", "value1",
			"key2", 42,
			"key3", true,
		)

		Logger.Debugw("debug with fields",
			"debug_key", "debug_value",
		)

		Logger.Errorw("error with fields",
			"error_code", 500,
			"error_message", "internal error",
		)
	})
}

func TestLoggerFormats(t *testing.T) {
	t.Run("JSON format", func(t *testing.T) {
		InitLogger("json", "info")
		assert.NotNil(t, Logger)
	})

	t.Run("Human format", func(t *testing.T) {
		InitLogger("human", "info")
		assert.NotNil(t, Logger)
	})
}

func TestHumanLoggerKeepsEventsOnSingleLines(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "human.log")
	cfg := loggerConfig("human", "warn")
	cfg.OutputPaths = []string{logPath}
	cfg.ErrorOutputPaths = []string{logPath}
	logger, err := cfg.Build()
	require.NoError(t, err)

	logger.Warn("peer unavailable")
	logger.Error("listener failed")
	require.NoError(t, logger.Sync())

	contents, err := os.ReadFile(logPath)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	assert.Len(t, lines, 2)
	assert.Contains(t, lines[0], "peer unavailable")
	assert.Contains(t, lines[1], "listener failed")
}

func TestConcurrentLogging(t *testing.T) {
	InitLogger("json", "info")

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			Logger.Infow("concurrent log",
				"goroutine", id,
				"timestamp", "now",
			)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	assert.True(t, true)
}

func TestRateLimiterBoundsBurstAndReportsSuppression(t *testing.T) {
	limiter := NewRateLimiter(time.Second)
	now := time.Unix(100, 0)

	suppressed, allowed := limiter.allowAt(now)
	assert.True(t, allowed)
	assert.Zero(t, suppressed)
	for range 999 {
		_, allowed = limiter.allowAt(now.Add(500 * time.Millisecond))
		assert.False(t, allowed)
	}

	suppressed, allowed = limiter.allowAt(now.Add(time.Second))
	assert.True(t, allowed)
	assert.Equal(t, uint64(999), suppressed)
}

func TestRateLimiterAllowsOneConcurrentEvent(t *testing.T) {
	limiter := NewRateLimiter(time.Second)
	now := time.Unix(100, 0)
	var allowed atomic.Uint64
	var group sync.WaitGroup
	for range 100 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, ok := limiter.allowAt(now); ok {
				allowed.Add(1)
			}
		}()
	}
	group.Wait()
	assert.Equal(t, uint64(1), allowed.Load())

	suppressed, ok := limiter.allowAt(now.Add(time.Second))
	assert.True(t, ok)
	assert.Equal(t, uint64(99), suppressed)
}

func TestRateLimiterIgnoresChangingFields(t *testing.T) {
	core, recorded := observer.New(zapcore.WarnLevel)
	oldLogger := Logger
	Logger = zap.New(core).Sugar()
	t.Cleanup(func() { Logger = oldLogger })
	limiter := NewRateLimiter(time.Hour)

	for i := range 1000 {
		limiter.Warnw("UDP read failed", "source_port", i)
	}

	assert.Len(t, recorded.All(), 1)
}

func BenchmarkLogging(b *testing.B) {
	oldLogger := Logger
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(io.Discard),
		zap.InfoLevel,
	)
	Logger = zap.New(core).Sugar()
	b.Cleanup(func() { Logger = oldLogger })

	b.Run("Simple", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			Logger.Info("benchmark message")
		}
	})

	b.Run("WithFields", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			Logger.Infow("benchmark with fields",
				"iteration", i,
				"key", "value",
			)
		}
	})

	b.Run("Formatted", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			Logger.Infof("benchmark iteration %d", i)
		}
	})
}

func TestDebugEnabled(t *testing.T) {
	oldLogger := Logger
	t.Cleanup(func() { Logger = oldLogger })

	Logger = zap.NewNop().Sugar()
	assert.False(t, DebugEnabled())

	core, _ := observer.New(zapcore.DebugLevel)
	Logger = zap.New(core).Sugar()
	assert.True(t, DebugEnabled())
}

func TestLoggerPanic(t *testing.T) {
	// This would require mocking zap.Config.Build() to return an error
	// For now, we just ensure normal initialization doesn't panic
	assert.NotPanics(t, func() {
		InitLogger("json", "info")
	})
}

func TestSyncLogger(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func()
		setupEnv  map[string]string
		wantErr   bool
	}{
		{
			name: "sync with nil logger",
			setupFunc: func() {
				Logger = nil
			},
			wantErr: false,
		},
		{
			name: "sync in CI environment",
			setupFunc: func() {
				InitLogger("json", "info")
			},
			setupEnv: map[string]string{
				"CI": "true",
			},
			wantErr: false,
		},
		{
			name: "sync in GitHub Actions",
			setupFunc: func() {
				InitLogger("json", "info")
			},
			setupEnv: map[string]string{
				"GITHUB_ACTIONS": "true",
			},
			wantErr: false,
		},
		{
			name: "sync in non-CI environment",
			setupFunc: func() {
				InitLogger("json", "info")
			},
			setupEnv: map[string]string{},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment
			for k, v := range tt.setupEnv {
				t.Setenv(k, v)
			}

			// Run setup
			tt.setupFunc()

			// Test sync
			err := SyncLogger()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsCI(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected bool
	}{
		{
			name:     "no CI environment",
			envVars:  map[string]string{},
			expected: false,
		},
		{
			name: "GitHub Actions",
			envVars: map[string]string{
				"GITHUB_ACTIONS": "true",
			},
			expected: true,
		},
		{
			name: "generic CI",
			envVars: map[string]string{
				"CI": "1",
			},
			expected: true,
		},
		{
			name: "GitLab CI",
			envVars: map[string]string{
				"GITLAB_CI": "true",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all CI env vars first
			ciVars := []string{"CI", "CONTINUOUS_INTEGRATION", "GITHUB_ACTIONS", "GITLAB_CI", "JENKINS_URL", "TRAVIS", "CIRCLECI", "BUILDKITE", "DRONE"}
			oldValues := make(map[string]string)
			for _, v := range ciVars {
				oldValues[v] = os.Getenv(v)
				_ = os.Unsetenv(v)
			}
			defer func() {
				for k, v := range oldValues {
					if v != "" {
						_ = os.Setenv(k, v)
					}
				}
			}()

			// Set test env vars
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			// Test
			result := isCI()
			assert.Equal(t, tt.expected, result)
		})
	}
}
