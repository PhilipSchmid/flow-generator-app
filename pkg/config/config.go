package config

import (
	"github.com/PhilipSchmid/flow-generator-app/pkg/logging"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// CommonConfig holds configuration fields shared between client and server.
type CommonConfig struct {
	LogLevel       string
	LogFormat      string
	MetricsPort    string
	TracingEnabled bool
	JaegerEndpoint string
}

// ClientConfig holds client-specific configuration, embedding CommonConfig.
type ClientConfig struct {
	CommonConfig
	Server         string
	Rate           float64
	MaxConcurrent  int
	Protocol       string
	MinDuration    float64
	MaxDuration    float64
	ConstantFlows  bool
	TCPPorts       string
	UDPPorts       string
	PayloadSize    int
	MinPayloadSize int
	MaxPayloadSize int
	MTU            int
	MSS            int
	FlowTimeout    float64
	FlowCount      int
}

// ServerConfig holds server-specific configuration, embedding CommonConfig.
type ServerConfig struct {
	CommonConfig
	TCPPortsServer string
	UDPPortsServer string
}

// LoadClientConfig loads and returns the client configuration.
func LoadClientConfig() *ClientConfig {
	// Set defaults for common fields
	viper.SetDefault("log_level", "info")
	viper.SetDefault("log_format", "human")
	viper.SetDefault("metrics_port", "9090")
	viper.SetDefault("tracing_enabled", false)
	viper.SetDefault("jaeger_endpoint", "http://localhost:14268/api/traces")

	// Set defaults for client-specific fields
	viper.SetDefault("server", "localhost")
	viper.SetDefault("rate", 10.0)
	viper.SetDefault("max_concurrent", 100)
	viper.SetDefault("protocol", "both")
	viper.SetDefault("min_duration", 1.0)
	viper.SetDefault("max_duration", 10.0)
	viper.SetDefault("constant_flows", false)
	viper.SetDefault("tcp_ports", "8080")
	viper.SetDefault("udp_ports", "")
	viper.SetDefault("payload_size", 0)
	viper.SetDefault("min_payload_size", 0)
	viper.SetDefault("max_payload_size", 0)
	viper.SetDefault("mtu", 1500)
	viper.SetDefault("mss", 1460)
	viper.SetDefault("flow_timeout", 0.0)
	viper.SetDefault("flow_count", 0)

	// Load environment variables
	viper.AutomaticEnv()

	// Load config file (if present)
	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			logging.Logger.Warnf("Failed to read config file: %v", err)
		}
	}

	// Bind command-line flags
	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		logging.Logger.Fatalf("Failed to bind command-line flags: %v", err)
	}

	// Populate and return ClientConfig
	return &ClientConfig{
		CommonConfig: CommonConfig{
			LogLevel:       viper.GetString("log_level"),
			LogFormat:      viper.GetString("log_format"),
			MetricsPort:    viper.GetString("metrics_port"),
			TracingEnabled: viper.GetBool("tracing_enabled"),
			JaegerEndpoint: viper.GetString("jaeger_endpoint"),
		},
		Server:         viper.GetString("server"),
		Rate:           viper.GetFloat64("rate"),
		MaxConcurrent:  viper.GetInt("max_concurrent"),
		Protocol:       viper.GetString("protocol"),
		MinDuration:    viper.GetFloat64("min_duration"),
		MaxDuration:    viper.GetFloat64("max_duration"),
		ConstantFlows:  viper.GetBool("constant_flows"),
		TCPPorts:       viper.GetString("tcp_ports"),
		UDPPorts:       viper.GetString("udp_ports"),
		PayloadSize:    viper.GetInt("payload_size"),
		MinPayloadSize: viper.GetInt("min_payload_size"),
		MaxPayloadSize: viper.GetInt("max_payload_size"),
		MTU:            viper.GetInt("mtu"),
		MSS:            viper.GetInt("mss"),
		FlowTimeout:    viper.GetFloat64("flow_timeout"),
		FlowCount:      viper.GetInt("flow_count"),
	}
}

// LoadServerConfig loads and returns the server configuration.
func LoadServerConfig() *ServerConfig {
	// Set defaults for common fields
	viper.SetDefault("log_level", "info")
	viper.SetDefault("log_format", "human")
	viper.SetDefault("metrics_port", "9090")
	viper.SetDefault("tracing_enabled", false)
	viper.SetDefault("jaeger_endpoint", "http://localhost:14268/api/traces")

	// Set defaults for server-specific fields
	viper.SetDefault("tcp_ports_server", "8080")
	viper.SetDefault("udp_ports_server", "")

	// Load environment variables
	viper.AutomaticEnv()

	// Load config file (if present)
	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			logging.Logger.Warnf("Failed to read config file: %v", err)
		}
	}

	// Bind command-line flags
	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		logging.Logger.Fatalf("Failed to bind command-line flags: %v", err)
	}

	// Populate and return ServerConfig
	return &ServerConfig{
		CommonConfig: CommonConfig{
			LogLevel:       viper.GetString("log_level"),
			LogFormat:      viper.GetString("log_format"),
			MetricsPort:    viper.GetString("metrics_port"),
			TracingEnabled: viper.GetBool("tracing_enabled"),
			JaegerEndpoint: viper.GetString("jaeger_endpoint"),
		},
		TCPPortsServer: viper.GetString("tcp_ports_server"),
		UDPPortsServer: viper.GetString("udp_ports_server"),
	}
}
