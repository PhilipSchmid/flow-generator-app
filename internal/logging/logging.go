package logging

import (
	"os"
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger is always safe to use. InitLogger replaces the no-op logger during
// application startup.
var Logger = zap.NewNop().Sugar()

// RateLimiter bounds repeated log events without dropping their metrics. Each
// instance is intended for one stable operation such as a TCP dial or UDP read.
type RateLimiter struct {
	intervalNanos int64
	nextLog       atomic.Int64
	suppressed    atomic.Uint64
}

// NewRateLimiter creates a limiter that emits at most one event per interval.
func NewRateLimiter(interval time.Duration) *RateLimiter {
	if interval <= 0 {
		interval = time.Second
	}
	return &RateLimiter{intervalNanos: int64(interval)}
}

// Warnw logs a rate-limited warning with structured context.
func (l *RateLimiter) Warnw(message string, fields ...any) {
	l.logw(zap.WarnLevel, message, fields...)
}

// Errorw logs a rate-limited error with structured context.
func (l *RateLimiter) Errorw(message string, fields ...any) {
	l.logw(zap.ErrorLevel, message, fields...)
}

// Debugw logs a rate-limited debug event with structured context.
func (l *RateLimiter) Debugw(message string, fields ...any) {
	l.logw(zap.DebugLevel, message, fields...)
}

func (l *RateLimiter) logw(level zapcore.Level, message string, fields ...any) {
	if l == nil || Logger == nil || !Logger.Desugar().Core().Enabled(level) {
		return
	}
	suppressed, allowed := l.allowAt(time.Now())
	if !allowed {
		return
	}
	if suppressed > 0 {
		fields = append(fields, "suppressed", suppressed)
	}
	switch level {
	case zap.DebugLevel:
		Logger.Debugw(message, fields...)
	case zap.WarnLevel:
		Logger.Warnw(message, fields...)
	case zap.ErrorLevel:
		Logger.Errorw(message, fields...)
	}
}

func (l *RateLimiter) allowAt(now time.Time) (uint64, bool) {
	nowNanos := now.UnixNano()
	for {
		next := l.nextLog.Load()
		if nowNanos < next {
			l.suppressed.Add(1)
			return 0, false
		}
		if l.nextLog.CompareAndSwap(next, nowNanos+l.intervalNanos) {
			return l.suppressed.Swap(0), true
		}
	}
}

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

func loggerConfig(logFormat string, logLevel string) zap.Config {
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
	// Zap's development preset expands every warning into a stack trace. Peer
	// loss is an expected operational condition, so keep human logs one event
	// per line. Panics still include the runtime stack.
	cfg.DisableStacktrace = true
	// Repeated network failures can otherwise make logging the bottleneck. Keep
	// the first messages for diagnosis, then sample identical log sites.
	cfg.Sampling = &zap.SamplingConfig{Initial: 100, Thereafter: 100}
	return cfg
}

// InitLogger initializes the logger based on logformat and loglevel
func InitLogger(logFormat string, logLevel string) {
	cfg := loggerConfig(logFormat, logLevel)

	// Build the logger
	logger, err := cfg.Build()
	if err != nil {
		panic("Failed to initialize logger: " + err.Error())
	}

	// Assign the sugared logger
	Logger = logger.Sugar()
}

// DebugEnabled lets hot paths avoid constructing debug-only values.
func DebugEnabled() bool {
	return Logger != nil && Logger.Desugar().Core().Enabled(zap.DebugLevel)
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
