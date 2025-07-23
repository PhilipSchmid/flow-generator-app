package logging

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
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

func BenchmarkLogging(b *testing.B) {
	InitLogger("json", "info")

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
