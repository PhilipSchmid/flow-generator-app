package logging

import (
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Logger *zap.SugaredLogger

// getLogLevel converts a string level to a zapcore.Level
func getLogLevel(level string) zapcore.Level {
	switch level {
	case "debug":
		return zap.DebugLevel
	case "info":
		return zap.InfoLevel
	case "warn":
		return zap.WarnLevel
	case "error":
		return zap.ErrorLevel
	default:
		return zap.InfoLevel
	}
}

// InitLogger initializes the logger based on logformat and loglevel
func InitLogger(logFormat string, logLevel string) {
	var cfg zap.Config

	switch logFormat {
	case "human":
		cfg = zap.NewDevelopmentConfig()
	case "json":
		cfg = zap.NewProductionConfig()
	default:
		// Default to human-readable if an invalid format is provided
		cfg = zap.NewDevelopmentConfig()
	}

	cfg.Level = zap.NewAtomicLevelAt(getLogLevel(logLevel))

	// Build the logger
	logger, err := cfg.Build()
	if err != nil {
		panic("Failed to initialize logger: " + err.Error())
	}

	// Assign the sugared logger
	Logger = logger.Sugar()
}

// SyncLogger safely syncs the logger, handling CI environment issues
func SyncLogger() error {
	if Logger == nil {
		return nil
	}

	// Get the underlying zap logger
	baseLogger := Logger.Desugar()

	// In CI environments (GitHub Actions, etc.), syncing stderr often fails
	// Check if we're in a CI environment
	if isCI() {
		// In CI, we can skip sync entirely as logs are captured by the CI system
		return nil
	}

	// In non-CI environments, attempt sync but ignore stderr-related errors
	if err := baseLogger.Sync(); err != nil {
		// Ignore errors related to syncing stderr/stdout as these are common and harmless
		errStr := err.Error()
		if strings.Contains(errStr, "/dev/stderr") ||
			strings.Contains(errStr, "/dev/stdout") ||
			strings.Contains(errStr, "inappropriate ioctl for device") ||
			strings.Contains(errStr, "invalid argument") ||
			strings.Contains(errStr, "bad file descriptor") {
			return nil
		}
		return err
	}

	return nil
}

// isCI detects if we're running in a CI environment
func isCI() bool {
	// Check common CI environment variables
	ciVars := []string{
		"CI",
		"CONTINUOUS_INTEGRATION",
		"GITHUB_ACTIONS",
		"GITLAB_CI",
		"JENKINS_URL",
		"TRAVIS",
		"CIRCLECI",
		"BUILDKITE",
		"DRONE",
	}

	for _, envVar := range ciVars {
		if os.Getenv(envVar) != "" {
			return true
		}
	}

	return false
}
