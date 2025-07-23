package main

import (
	"context"
	"math/rand/v2"
	"net"
	"sync"
	"testing"

	"github.com/PhilipSchmid/flow-generator-app/internal/config"
	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/PhilipSchmid/flow-generator-app/internal/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConstructAddress(t *testing.T) {
	tests := []struct {
		name     string
		server   string
		port     int
		expected string
	}{
		{"IPv4 address", "192.168.1.1", 8080, "192.168.1.1:8080"},
		{"IPv6 address", "2001:db8::1", 8080, "[2001:db8::1]:8080"},
		{"hostname", "example.com", 8080, "example.com:8080"},
		{"localhost", "localhost", 8080, "localhost:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := constructAddress(tt.server, tt.port)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetPayloadSize(t *testing.T) {
	tests := []struct {
		name     string
		cfg      config.ClientConfig
		expected int
	}{
		{
			name: "fixed payload size",
			cfg: config.ClientConfig{
				PayloadSize: 1024,
			},
			expected: 1024,
		},
		{
			name:     "default payload size",
			cfg:      config.ClientConfig{},
			expected: 5,
		},
		{
			name: "random payload size",
			cfg: config.ClientConfig{
				MinPayloadSize: 100,
				MaxPayloadSize: 200,
			},
			expected: -1, // Will check range
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldCfg := cfg
			cfg = &tt.cfg
			defer func() { cfg = oldCfg }()

			src := rand.New(rand.NewPCG(0, 0))
			size := getPayloadSize(src)

			if tt.expected == -1 {
				assert.GreaterOrEqual(t, size, tt.cfg.MinPayloadSize)
				assert.LessOrEqual(t, size, tt.cfg.MaxPayloadSize)
			} else {
				assert.Equal(t, tt.expected, size)
			}
		})
	}
}

func TestClientConfiguration(t *testing.T) {
	testCfg := &config.ClientConfig{
		CommonConfig: config.CommonConfig{
			LogLevel:    "info",
			LogFormat:   "json",
			MetricsPort: "9091",
		},
		Server:         "localhost",
		Rate:           10.0,
		MaxConcurrent:  100,
		Protocol:       "both",
		MinDuration:    1.0,
		MaxDuration:    5.0,
		TCPPorts:       "8080,8081",
		UDPPorts:       "9000",
		PayloadSize:    100,
		MinPayloadSize: 50,
		MaxPayloadSize: 200,
		MTU:            1500,
		MSS:            1460,
	}

	err := testCfg.Validate()
	assert.NoError(t, err)

	invalidCfg := *testCfg
	invalidCfg.Server = ""
	err = invalidCfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "server address cannot be empty")

	invalidCfg = *testCfg
	invalidCfg.Rate = -1
	err = invalidCfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate must be positive")
}

func TestProtocolPort(t *testing.T) {
	pp := ProtocolPort{
		Protocol: "tcp",
		Port:     8080,
	}

	assert.Equal(t, "tcp", pp.Protocol)
	assert.Equal(t, 8080, pp.Port)
}

func TestParsePorts(t *testing.T) {
	logging.InitLogger("json", "error")

	tests := []struct {
		name     string
		input    string
		expected []int
	}{
		{"single port", "8080", []int{8080}},
		{"multiple ports", "8080,8081,8082", []int{8080, 8081, 8082}},
		{"with spaces", " 8080 , 8081 , 8082 ", []int{8080, 8081, 8082}},
		{"empty string", "", []int{}},
		{"invalid port", "8080,invalid,8081", []int{8080, 8081}},
		{"out of range port", "8080,70000,8081", []int{8080, 8081}},
		{"negative port", "8080,-1,8081", []int{8080, 8081}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parsePorts(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateFlow(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				buf := make([]byte, 1024)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					_, _ = c.Write(buf[:n])
				}
			}(conn)
		}
	}()

	serverAddr := listener.Addr().(*net.TCPAddr)

	testCfg := &config.ClientConfig{
		PayloadSize: 100,
	}
	oldCfg := cfg
	cfg = testCfg
	defer func() { cfg = oldCfg }()

	oldMc := mc
	mc = metrics.NewMetricsCollector()
	defer func() { mc = oldMc }()

	ctx := context.Background()
	pp := ProtocolPort{Protocol: "tcp", Port: serverAddr.Port}
	src := rand.New(rand.NewPCG(0, 0))
	var wg sync.WaitGroup

	wg.Add(1)
	generateFlow(ctx, "127.0.0.1", pp, 0.1, src, 1500, 1460, &wg)
	wg.Wait()

	assert.True(t, true)
}

func TestMetricsCollectorInterface(t *testing.T) {
	mc := metrics.NewMetricsCollector()

	assert.NotPanics(t, func() {
		mc.IncRequestsReceived("tcp", "8080")
		mc.IncRequestsSent("tcp", "8080")
		mc.IncRequestsReceived("udp", "9000")
		mc.IncRequestsSent("udp", "9000")
		mc.AddBytesReceived("tcp", "8080", 1024)
		mc.AddBytesSent("tcp", "8080", 1024)
		mc.AddBytesReceived("udp", "9000", 512)
		mc.AddBytesSent("udp", "9000", 512)
	})
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.ClientConfig
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: config.ClientConfig{
				CommonConfig: config.CommonConfig{
					LogLevel:  "info",
					LogFormat: "json",
				},
				Server:        "127.0.0.1",
				TCPPorts:      "8080",
				Protocol:      "tcp",
				Rate:          10,
				MaxConcurrent: 5,
				MinDuration:   0.1,
				MaxDuration:   1.0,
				MTU:           1500,
				MSS:           1460,
			},
			wantErr: false,
		},
		{
			name: "invalid - no server",
			cfg: config.ClientConfig{
				TCPPorts: "8080",
			},
			wantErr: true,
		},
		{
			name: "invalid - no ports",
			cfg: config.ClientConfig{
				Server: "127.0.0.1",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func BenchmarkGenerateFlow(b *testing.B) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(b, err)
	defer func() { _ = listener.Close() }()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					_, _ = c.Write(buf[:n])
				}
			}(conn)
		}
	}()

	serverAddr := listener.Addr().(*net.TCPAddr)

	testCfg := &config.ClientConfig{
		PayloadSize: 1024,
	}
	oldCfg := cfg
	cfg = testCfg
	defer func() { cfg = oldCfg }()

	oldMc := mc
	mc = metrics.NewMetricsCollector()
	defer func() { mc = oldMc }()

	ctx := context.Background()
	pp := ProtocolPort{Protocol: "tcp", Port: serverAddr.Port}
	src := rand.New(rand.NewPCG(0, 0))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(1)
		generateFlow(ctx, "127.0.0.1", pp, 0.01, src, 1500, 1460, &wg)
		wg.Wait()
	}
}
