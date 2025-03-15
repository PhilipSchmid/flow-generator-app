package config

import (
	"github.com/PhilipSchmid/flow-generator-app/pkg/logging"
	"github.com/spf13/viper"
)

func InitConfig() {
	viper.SetDefault("loglevel", "info")
	viper.SetDefault("metrics_port", "9090")
	viper.SetDefault("tracing_enabled", false)
	viper.SetDefault("jaeger_endpoint", "http://localhost:14268/api/traces")

	// Client-specific defaults
	viper.SetDefault("server", "localhost")
	viper.SetDefault("rate", 10.0)
	viper.SetDefault("max_concurrent", 100)
	viper.SetDefault("protocol", "both")
	viper.SetDefault("min_duration", 1.0)
	viper.SetDefault("max_duration", 10.0)

	viper.AutomaticEnv()
	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			logging.Logger.Warnf("Failed to read config file: %v", err)
		}
	}
}
