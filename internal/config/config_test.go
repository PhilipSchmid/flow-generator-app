package config

import (
	"os"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommonConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  CommonConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: CommonConfig{
				LogLevel:  "info",
				LogFormat: "json",
			},
			wantErr: false,
		},
		{
			name: "invalid log level",
			config: CommonConfig{
				LogLevel:  "invalid",
				LogFormat: "json",
			},
			wantErr: true,
			errMsg:  "invalid log level",
		},
		{
			name: "invalid log format",
			config: CommonConfig{
				LogLevel:  "info",
				LogFormat: "invalid",
			},
			wantErr: true,
			errMsg:  "invalid log format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestClientConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  ClientConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: ClientConfig{
				CommonConfig: CommonConfig{
					LogLevel:  "info",
					LogFormat: "json",
				},
				Server:        "localhost",
				Rate:          10.0,
				MaxConcurrent: 100,
				Protocol:      "tcp",
				MinDuration:   1.0,
				MaxDuration:   10.0,
				TCPPorts:      "8080",
				MTU:           1500,
				MSS:           1460,
			},
			wantErr: false,
		},
		{
			name: "empty server",
			config: ClientConfig{
				CommonConfig: CommonConfig{
					LogLevel:  "info",
					LogFormat: "json",
				},
				Server: "",
			},
			wantErr: true,
			errMsg:  "server address cannot be empty",
		},
		{
			name: "negative rate",
			config: ClientConfig{
				CommonConfig: CommonConfig{
					LogLevel:  "info",
					LogFormat: "json",
				},
				Server: "localhost",
				Rate:   -1.0,
			},
			wantErr: true,
			errMsg:  "rate must be positive",
		},
		{
			name: "invalid protocol",
			config: ClientConfig{
				CommonConfig: CommonConfig{
					LogLevel:  "info",
					LogFormat: "json",
				},
				Server:        "localhost",
				Rate:          10.0,
				MaxConcurrent: 100,
				Protocol:      "invalid",
			},
			wantErr: true,
			errMsg:  "invalid protocol",
		},
		{
			name: "min duration greater than max",
			config: ClientConfig{
				CommonConfig: CommonConfig{
					LogLevel:  "info",
					LogFormat: "json",
				},
				Server:        "localhost",
				Rate:          10.0,
				MaxConcurrent: 100,
				Protocol:      "tcp",
				MinDuration:   10.0,
				MaxDuration:   5.0,
				TCPPorts:      "8080",
				MTU:           1500,
				MSS:           1460,
			},
			wantErr: true,
			errMsg:  "min_duration cannot be greater than max_duration",
		},
		{
			name: "no ports specified",
			config: ClientConfig{
				CommonConfig: CommonConfig{
					LogLevel:  "info",
					LogFormat: "json",
				},
				Server:        "localhost",
				Rate:          10.0,
				MaxConcurrent: 100,
				Protocol:      "tcp",
				MinDuration:   1.0,
				MaxDuration:   10.0,
				TCPPorts:      "",
				UDPPorts:      "",
				MTU:           1500,
				MSS:           1460,
			},
			wantErr: true,
			errMsg:  "at least one port",
		},
		{
			name: "MSS greater than MTU",
			config: ClientConfig{
				CommonConfig: CommonConfig{
					LogLevel:  "info",
					LogFormat: "json",
				},
				Server:        "localhost",
				Rate:          10.0,
				MaxConcurrent: 100,
				Protocol:      "tcp",
				MinDuration:   1.0,
				MaxDuration:   10.0,
				TCPPorts:      "8080",
				MTU:           1000,
				MSS:           1460,
			},
			wantErr: true,
			errMsg:  "MSS must be less than MTU",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestServerConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  ServerConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config with TCP ports",
			config: ServerConfig{
				CommonConfig: CommonConfig{
					LogLevel:  "info",
					LogFormat: "json",
				},
				TCPPortsServer: "8080",
			},
			wantErr: false,
		},
		{
			name: "valid config with UDP ports",
			config: ServerConfig{
				CommonConfig: CommonConfig{
					LogLevel:  "info",
					LogFormat: "json",
				},
				UDPPortsServer: "9000",
			},
			wantErr: false,
		},
		{
			name: "no ports specified",
			config: ServerConfig{
				CommonConfig: CommonConfig{
					LogLevel:  "info",
					LogFormat: "json",
				},
				TCPPortsServer: "",
				UDPPortsServer: "",
			},
			wantErr: true,
			errMsg:  "at least one port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestLoadClientConfig(t *testing.T) {
	// Reset viper and pflags for clean test
	viper.Reset()
	pflag.CommandLine = pflag.NewFlagSet(os.Args[0], pflag.ExitOnError)

	// Set environment variable with prefix
	_ = os.Setenv("FLOW_GENERATOR_LOG_LEVEL", "debug")
	defer func() { _ = os.Unsetenv("FLOW_GENERATOR_LOG_LEVEL") }()

	config, err := LoadClientConfig()
	require.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "debug", config.LogLevel)
	assert.Equal(t, "localhost", config.Server) // default value
}

func TestLoadServerConfig(t *testing.T) {
	// Reset viper and pflags for clean test
	viper.Reset()
	pflag.CommandLine = pflag.NewFlagSet(os.Args[0], pflag.ExitOnError)

	// Set environment variable with prefix
	_ = os.Setenv("FLOW_GENERATOR_METRICS_PORT", "9999")
	defer func() { _ = os.Unsetenv("FLOW_GENERATOR_METRICS_PORT") }()

	config, err := LoadServerConfig()
	require.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "9999", config.MetricsPort)
	assert.Equal(t, "8080", config.TCPPortsServer) // default value
}

func TestContains(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		val   string
		want  bool
	}{
		{
			name:  "value exists",
			slice: []string{"a", "b", "c"},
			val:   "b",
			want:  true,
		},
		{
			name:  "value doesn't exist",
			slice: []string{"a", "b", "c"},
			val:   "d",
			want:  false,
		},
		{
			name:  "empty slice",
			slice: []string{},
			val:   "a",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contains(tt.slice, tt.val)
			assert.Equal(t, tt.want, got)
		})
	}
}
