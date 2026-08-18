package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	// EnvPrefix is the prefix for all environment variables
	EnvPrefix      = "FLOW_GENERATOR"
	maxPayloadSize = 1 << 20
	maxFlowRate    = 1_000_000_000
)

// NormalizeFlagNames makes kebab-case canonical while keeping legacy
// snake_case command-line flags working as aliases.
func NormalizeFlagNames(flags *pflag.FlagSet) {
	flags.SetNormalizeFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		return pflag.NormalizedName(strings.ReplaceAll(name, "_", "-"))
	})
}

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
	HealthPort     string
}

// Validate validates the common configuration
func (c *CommonConfig) Validate() error {
	validLogLevels := []string{"debug", "info", "warn", "error"}
	if !contains(validLogLevels, c.LogLevel) {
		return fmt.Errorf("invalid log level: %s, must be one of: %v", c.LogLevel, validLogLevels)
	}

	validLogFormats := []string{"human", "json"}
	if !contains(validLogFormats, c.LogFormat) {
		return fmt.Errorf("invalid log format: %s, must be one of: %v", c.LogFormat, validLogFormats)
	}
	if c.MetricsPort != "" {
		if err := validatePort(c.MetricsPort, "metrics_port"); err != nil {
			return err
		}
	}
	if c.TracingEnabled {
		parsed, err := url.ParseRequestURI(c.JaegerEndpoint)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("jaeger_endpoint must be an http(s) OTLP/gRPC URL")
		}
	}

	return nil
}

// Validate validates the client configuration
func (c *ClientConfig) Validate() error {
	if err := c.CommonConfig.Validate(); err != nil {
		return err
	}

	if c.Server == "" {
		return fmt.Errorf("server address cannot be empty")
	}

	if c.Rate <= 0 {
		return fmt.Errorf("rate must be positive")
	}
	if c.Rate > maxFlowRate {
		return fmt.Errorf("rate cannot exceed %d flows per second", maxFlowRate)
	}

	if c.MaxConcurrent <= 0 {
		return fmt.Errorf("max_concurrent must be positive")
	}

	validProtocols := []string{"tcp", "udp", "both"}
	if !contains(validProtocols, c.Protocol) {
		return fmt.Errorf("invalid protocol: %s, must be one of: %v", c.Protocol, validProtocols)
	}

	if c.MinDuration < 0 || c.MaxDuration < 0 {
		return fmt.Errorf("durations cannot be negative")
	}

	if c.MinDuration > c.MaxDuration {
		return fmt.Errorf("min_duration cannot be greater than max_duration")
	}
	if c.MaxDuration == 0 {
		return fmt.Errorf("max_duration must be positive")
	}

	if c.TCPPorts == "" && c.UDPPorts == "" {
		return fmt.Errorf("at least one port (TCP or UDP) must be specified")
	}
	if c.Protocol == "tcp" && c.TCPPorts == "" {
		return fmt.Errorf("tcp_ports must be specified when protocol is tcp")
	}
	if c.Protocol == "udp" && c.UDPPorts == "" {
		return fmt.Errorf("udp_ports must be specified when protocol is udp")
	}
	if err := validatePortList(c.TCPPorts, "tcp_ports"); err != nil {
		return err
	}
	if err := validatePortList(c.UDPPorts, "udp_ports"); err != nil {
		return err
	}

	if c.MTU <= 0 || c.MSS <= 0 {
		return fmt.Errorf("MTU and MSS must be positive")
	}

	if c.MSS >= c.MTU {
		return fmt.Errorf("MSS must be less than MTU")
	}
	if c.PayloadSize < 0 || c.MinPayloadSize < 0 || c.MaxPayloadSize < 0 {
		return fmt.Errorf("payload sizes cannot be negative")
	}
	if c.PayloadSize > maxPayloadSize || c.MinPayloadSize > maxPayloadSize || c.MaxPayloadSize > maxPayloadSize {
		return fmt.Errorf("payload sizes cannot exceed %d bytes", maxPayloadSize)
	}
	if (c.MinPayloadSize == 0) != (c.MaxPayloadSize == 0) {
		return fmt.Errorf("min_payload_size and max_payload_size must be set together")
	}
	if c.MinPayloadSize > c.MaxPayloadSize {
		return fmt.Errorf("min_payload_size cannot be greater than max_payload_size")
	}
	udpPayloadSize := c.PayloadSize
	if udpPayloadSize == 0 {
		udpPayloadSize = c.MaxPayloadSize
	}
	if (c.Protocol == "udp" || (c.Protocol == "both" && c.UDPPorts != "")) && udpPayloadSize > c.MTU {
		return fmt.Errorf("UDP payload size cannot exceed MTU")
	}
	if c.FlowTimeout < 0 {
		return fmt.Errorf("flow_timeout cannot be negative")
	}
	if c.FlowCount < 0 {
		return fmt.Errorf("flow_count cannot be negative")
	}

	return nil
}

// Validate validates the server configuration
func (c *ServerConfig) Validate() error {
	if err := c.CommonConfig.Validate(); err != nil {
		return err
	}

	if c.TCPPortsServer == "" && c.UDPPortsServer == "" {
		return fmt.Errorf("at least one port (TCP or UDP) must be specified")
	}
	if err := validatePortList(c.TCPPortsServer, "tcp_ports_server"); err != nil {
		return err
	}
	if err := validatePortList(c.UDPPortsServer, "udp_ports_server"); err != nil {
		return err
	}
	if c.HealthPort != "" {
		if err := validatePort(c.HealthPort, "health_port"); err != nil {
			return err
		}
	}
	if c.MetricsPort != "" && c.MetricsPort == c.HealthPort {
		return fmt.Errorf("metrics_port and health_port must be different")
	}
	if portListContains(c.TCPPortsServer, c.MetricsPort) {
		return fmt.Errorf("metrics_port conflicts with tcp_ports_server")
	}
	if portListContains(c.TCPPortsServer, c.HealthPort) {
		return fmt.Errorf("health_port conflicts with tcp_ports_server")
	}

	return nil
}

// LoadClientConfig loads and returns the client configuration.
func LoadClientConfig() (*ClientConfig, error) {
	initViper()
	setClientDefaults()

	// Bind kebab-case flags to the existing snake_case configuration keys.
	if err := bindFlags(pflag.CommandLine); err != nil {
		return nil, fmt.Errorf("failed to bind command-line flags: %w", err)
	}

	// Populate ClientConfig
	config := &ClientConfig{
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

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return config, nil
}

// LoadServerConfig loads and returns the server configuration.
func LoadServerConfig() (*ServerConfig, error) {
	initViper()
	setServerDefaults()

	// Bind kebab-case flags to the existing snake_case configuration keys.
	if err := bindFlags(pflag.CommandLine); err != nil {
		return nil, fmt.Errorf("failed to bind command-line flags: %w", err)
	}

	// Populate ServerConfig
	config := &ServerConfig{
		CommonConfig: CommonConfig{
			LogLevel:       viper.GetString("log_level"),
			LogFormat:      viper.GetString("log_format"),
			MetricsPort:    viper.GetString("metrics_port"),
			TracingEnabled: viper.GetBool("tracing_enabled"),
			JaegerEndpoint: viper.GetString("jaeger_endpoint"),
		},
		TCPPortsServer: viper.GetString("tcp_ports_server"),
		UDPPortsServer: viper.GetString("udp_ports_server"),
		HealthPort:     viper.GetString("health_port"),
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return config, nil
}

func bindFlags(flags *pflag.FlagSet) error {
	var bindErr error
	flags.VisitAll(func(flag *pflag.Flag) {
		if bindErr != nil {
			return
		}
		key := strings.ReplaceAll(flag.Name, "-", "_")
		if err := viper.BindPFlag(key, flag); err != nil {
			bindErr = fmt.Errorf("failed to bind --%s: %w", flag.Name, err)
		}
	})
	return bindErr
}

// initViper initializes viper with common settings
func initViper() {
	viper.SetEnvPrefix(EnvPrefix)
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// Load config file (if present)
	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	viper.AddConfigPath("/etc/flow-generator")
	if home, err := os.UserHomeDir(); err == nil {
		viper.AddConfigPath(home + "/.flow-generator")
	}

	// Ignore if config file is not found
	_ = viper.ReadInConfig()
}

// setCommonDefaults sets default values for common configuration
func setCommonDefaults() {
	viper.SetDefault("log_level", "info")
	viper.SetDefault("log_format", "human")
	viper.SetDefault("metrics_port", "9090")
	viper.SetDefault("tracing_enabled", false)
	viper.SetDefault("jaeger_endpoint", "http://localhost:4317")
}

// setClientDefaults sets default values for client configuration
func setClientDefaults() {
	setCommonDefaults()
	viper.SetDefault("metrics_port", "9091")

	// Client-specific defaults
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
}

func validatePort(value, name string) error {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("%s must be a port between 1 and 65535", name)
	}
	return nil
}

func validatePortList(value, name string) error {
	if value == "" {
		return nil
	}
	seen := make(map[int]struct{})
	for _, rawPort := range strings.Split(value, ",") {
		portText := strings.TrimSpace(rawPort)
		port, err := strconv.Atoi(portText)
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("%s contains invalid port %q", name, portText)
		}
		if _, exists := seen[port]; exists {
			return fmt.Errorf("%s contains duplicate port %d", name, port)
		}
		seen[port] = struct{}{}
	}
	return nil
}

func portListContains(value, target string) bool {
	if value == "" || target == "" {
		return false
	}
	for _, rawPort := range strings.Split(value, ",") {
		if strings.TrimSpace(rawPort) == target {
			return true
		}
	}
	return false
}

// setServerDefaults sets default values for server configuration
func setServerDefaults() {
	setCommonDefaults()

	// Server-specific defaults
	viper.SetDefault("tcp_ports_server", "8080")
	viper.SetDefault("udp_ports_server", "")
	viper.SetDefault("health_port", "8082")
}

// contains checks if a string slice contains a specific value
func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}
