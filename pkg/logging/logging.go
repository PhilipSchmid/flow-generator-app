package logging

import (
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
	if logFormat == "human" {
		cfg = zap.NewDevelopmentConfig()
	} else if logFormat == "json" {
		cfg = zap.NewProductionConfig()
	} else {
		// Default to human-readable if an invalid format is provided
		cfg = zap.NewDevelopmentConfig()
	}

	// Set the log level dynamically
	cfg.Level = zap.NewAtomicLevelAt(getLogLevel(logLevel))

	// Build the logger
	logger, err := cfg.Build()
	if err != nil {
		panic("Failed to initialize logger: " + err.Error())
	}

	// Assign the sugared logger
	Logger = logger.Sugar()
}
