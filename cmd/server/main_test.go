package main

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/config"
	"github.com/PhilipSchmid/flow-generator-app/internal/handlers"
	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/PhilipSchmid/flow-generator-app/internal/metrics"
	"github.com/PhilipSchmid/flow-generator-app/internal/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePorts(t *testing.T) {
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
		{"duplicate ports", "8080,8080,8081", []int{8080, 8080, 8081}},
	}

	// Capture log output
	oldLogger := logging.Logger
	logging.InitLogger("json", "error")
	defer func() { logging.Logger = oldLogger }()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parsePorts(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestServerConfiguration(t *testing.T) {
	// Test that we can create a valid server configuration
	cfg := &config.ServerConfig{
		CommonConfig: config.CommonConfig{
			LogLevel:    "info",
			LogFormat:   "json",
			MetricsPort: "9091",
		},
		TCPPortsServer: "8080,8081",
		UDPPortsServer: "9000",
		HealthPort:     "8082",
	}

	// Validate configuration
	err := cfg.Validate()
	assert.NoError(t, err)

	// Test parsing ports from config
	tcpPorts := parsePorts(cfg.TCPPortsServer)
	assert.Equal(t, []int{8080, 8081}, tcpPorts)

	udpPorts := parsePorts(cfg.UDPPortsServer)
	assert.Equal(t, []int{9000}, udpPorts)
}

func TestServerWithTCPPorts(t *testing.T) {
	// Initialize logging
	logging.InitLogger("json", "error")

	// Initialize metrics
	metricsCollector := metrics.NewMetricsCollector()
	tcpHandler := handlers.NewTCPHandler(metricsCollector)

	// Find available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	tcpPort := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	// Create manager and server
	manager := server.NewManager()
	tcpServer := server.NewTCPServer(tcpPort, tcpHandler)
	manager.AddServer(tcpServer)

	// Start server
	err = manager.Start()
	require.NoError(t, err)
	defer func() { _ = manager.Stop() }()

	// Test TCP connection
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tcpPort))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Send test data
	testData := []byte("Hello Server!")
	_, err = conn.Write(testData)
	require.NoError(t, err)

	// Read echo
	buf := make([]byte, len(testData))
	_, err = conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, testData, buf)
}

func TestServerWithUDPPorts(t *testing.T) {
	// Initialize logging
	logging.InitLogger("json", "error")

	// Initialize metrics
	metricsCollector := metrics.NewMetricsCollector()
	udpHandler := handlers.NewUDPHandler(metricsCollector)

	// Find available port
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)
	conn, err := net.ListenUDP("udp", addr)
	require.NoError(t, err)
	udpPort := conn.LocalAddr().(*net.UDPAddr).Port
	_ = conn.Close()

	// Create manager and server
	manager := server.NewManager()
	udpServer := server.NewUDPServer(udpPort, udpHandler)
	manager.AddServer(udpServer)

	// Start server
	err = manager.Start()
	require.NoError(t, err)
	defer func() { _ = manager.Stop() }()

	// Test UDP connection
	clientConn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", udpPort))
	require.NoError(t, err)
	defer func() { _ = clientConn.Close() }()

	// Send test data
	testData := []byte("Hello UDP Server!")
	_, err = clientConn.Write(testData)
	require.NoError(t, err)

	// Read echo
	buf := make([]byte, 1024)
	_ = clientConn.SetReadDeadline(time.Now().Add(1 * time.Second))
	n, err := clientConn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, testData, buf[:n])
}

func TestServerWithBothProtocols(t *testing.T) {
	// Initialize logging
	logging.InitLogger("json", "error")

	// Initialize metrics
	metricsCollector := metrics.NewMetricsCollector()
	tcpHandler := handlers.NewTCPHandler(metricsCollector)
	udpHandler := handlers.NewUDPHandler(metricsCollector)

	// Find available ports
	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	tcpPort := tcpListener.Addr().(*net.TCPAddr).Port
	_ = tcpListener.Close()

	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)
	udpConn, err := net.ListenUDP("udp", udpAddr)
	require.NoError(t, err)
	udpPort := udpConn.LocalAddr().(*net.UDPAddr).Port
	_ = udpConn.Close()

	// Create manager and servers
	manager := server.NewManager()
	tcpServer := server.NewTCPServer(tcpPort, tcpHandler)
	udpServer := server.NewUDPServer(udpPort, udpHandler)
	manager.AddServer(tcpServer)
	manager.AddServer(udpServer)

	// Start servers
	err = manager.Start()
	require.NoError(t, err)
	defer func() { _ = manager.Stop() }()

	// Test both protocols
	t.Run("TCP", func(t *testing.T) {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tcpPort))
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()

		testData := []byte("TCP Test")
		_, err = conn.Write(testData)
		require.NoError(t, err)

		buf := make([]byte, len(testData))
		_, err = conn.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, testData, buf)
	})

	t.Run("UDP", func(t *testing.T) {
		conn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", udpPort))
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()

		testData := []byte("UDP Test")
		_, err = conn.Write(testData)
		require.NoError(t, err)

		buf := make([]byte, 1024)
		_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, err := conn.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, testData, buf[:n])
	})
}

func TestServerManager(t *testing.T) {
	// Initialize components
	logging.InitLogger("json", "error")

	metricsCollector := metrics.NewMetricsCollector()
	tcpHandler := handlers.NewTCPHandler(metricsCollector)
	udpHandler := handlers.NewUDPHandler(metricsCollector)

	// Create manager
	manager := server.NewManager()

	// Add servers with port 0 for auto-assignment
	tcpServer := server.NewTCPServer(0, tcpHandler)
	udpServer := server.NewUDPServer(0, udpHandler)

	manager.AddServer(tcpServer)
	manager.AddServer(udpServer)

	// Verify server count
	assert.Equal(t, 2, manager.ServerCount())

	// Test start/stop
	err := manager.Start()
	assert.NoError(t, err)
	assert.True(t, manager.Running())

	err = manager.Stop()
	assert.NoError(t, err)
	assert.False(t, manager.Running())
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.ServerConfig
		wantErr bool
	}{
		{
			name: "valid config with TCP",
			cfg: config.ServerConfig{
				CommonConfig: config.CommonConfig{
					LogLevel:  "info",
					LogFormat: "json",
				},
				TCPPortsServer: "8080",
			},
			wantErr: false,
		},
		{
			name: "valid config with UDP",
			cfg: config.ServerConfig{
				CommonConfig: config.CommonConfig{
					LogLevel:  "info",
					LogFormat: "json",
				},
				UDPPortsServer: "9000",
			},
			wantErr: false,
		},
		{
			name: "invalid - no ports",
			cfg: config.ServerConfig{
				CommonConfig: config.CommonConfig{
					LogLevel:  "info",
					LogFormat: "json",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid log level",
			cfg: config.ServerConfig{
				CommonConfig: config.CommonConfig{
					LogLevel:  "invalid",
					LogFormat: "json",
				},
				TCPPortsServer: "8080",
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

func TestMetricsCollectorUsage(t *testing.T) {
	// Initialize
	logging.InitLogger("json", "error")
	mc := metrics.NewMetricsCollector()

	// Test that metrics methods work
	assert.NotPanics(t, func() {
		mc.IncRequestsReceived("tcp", "8080")
		mc.IncRequestsSent("tcp", "8080")
		mc.AddBytesReceived("tcp", "8080", 1024)
		mc.AddBytesSent("tcp", "8080", 1024)
		mc.IncTCPConnectionsOpened()
		mc.IncUDPPacketsReceived()
		mc.SetActiveTCPConnections(5)
		// Don't flush metrics during test to avoid verbose output
	})
}

func BenchmarkTCPServer(b *testing.B) {
	// Initialize
	logging.InitLogger("json", "error")
	metricsCollector := metrics.NewMetricsCollector()
	tcpHandler := handlers.NewTCPHandler(metricsCollector)

	// Find port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(b, err)
	tcpPort := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	// Create and start server
	manager := server.NewManager()
	tcpServer := server.NewTCPServer(tcpPort, tcpHandler)
	manager.AddServer(tcpServer)

	err = manager.Start()
	require.NoError(b, err)
	defer func() { _ = manager.Stop() }()

	// Connect
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tcpPort))
	require.NoError(b, err)
	defer func() { _ = conn.Close() }()

	testData := make([]byte, 1024)
	buf := make([]byte, 1024)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err = conn.Write(testData)
		if err != nil {
			b.Fatal(err)
		}
		_, err = conn.Read(buf)
		if err != nil {
			b.Fatal(err)
		}
	}
}
