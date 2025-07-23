package server

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/handlers"
	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/PhilipSchmid/flow-generator-app/internal/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// Initialize logger for tests
	logging.InitLogger("json", "error")
}

func TestNewTCPServer(t *testing.T) {
	mc := metrics.NewMetricsCollector()
	handler := handlers.NewTCPHandler(mc)
	server := NewTCPServer(8080, handler)

	assert.NotNil(t, server)
	assert.Equal(t, 8080, server.port)
	assert.Equal(t, handler, server.handler)
	assert.NotNil(t, server.ctx)
	assert.NotNil(t, server.cancel)
}

func TestTCPServerStartStop(t *testing.T) {
	mc := metrics.NewMetricsCollector()
	handler := handlers.NewTCPHandler(mc)

	// Find available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	server := NewTCPServer(port, handler)

	// Start server
	err = server.Start()
	require.NoError(t, err)

	// Verify server is listening
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	require.NoError(t, err)
	_ = conn.Close()

	// Stop server
	err = server.Stop()
	assert.NoError(t, err)

	// Verify server is no longer listening
	_, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	assert.Error(t, err)
}

func TestTCPServerPortAndType(t *testing.T) {
	mc := metrics.NewMetricsCollector()
	handler := handlers.NewTCPHandler(mc)
	server := NewTCPServer(8080, handler)

	assert.Equal(t, 8080, server.Port())
	assert.Equal(t, "TCP", server.Type())
}

func TestTCPServerStartError(t *testing.T) {
	mc := metrics.NewMetricsCollector()
	handler := handlers.NewTCPHandler(mc)

	// Use invalid port that will fail
	server := NewTCPServer(-1, handler)

	err := server.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to listen")
}

func TestTCPServerHandleConnections(t *testing.T) {
	mc := metrics.NewMetricsCollector()
	handler := handlers.NewTCPHandler(mc)

	// Find available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	server := NewTCPServer(port, handler)

	// Start server
	err = server.Start()
	require.NoError(t, err)
	defer func() { _ = server.Stop() }()

	// Connect and send data
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	testData := []byte("Hello TCP Server!")
	_, err = conn.Write(testData)
	require.NoError(t, err)

	// Read echo response
	buf := make([]byte, len(testData))
	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	_, err = conn.Read(buf)
	require.NoError(t, err)

	assert.Equal(t, testData, buf)
}

func TestTCPServerConcurrentConnections(t *testing.T) {
	mc := metrics.NewMetricsCollector()
	handler := handlers.NewTCPHandler(mc)

	// Find available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	server := NewTCPServer(port, handler)

	// Start server
	err = server.Start()
	require.NoError(t, err)
	defer func() { _ = server.Stop() }()

	// Create multiple concurrent connections
	numConnections := 10
	done := make(chan bool, numConnections)

	for i := 0; i < numConnections; i++ {
		go func(id int) {
			conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err != nil {
				t.Errorf("Connection %d failed: %v", id, err)
				done <- false
				return
			}
			defer func() { _ = conn.Close() }()

			testData := []byte(fmt.Sprintf("Connection %d", id))
			_, err = conn.Write(testData)
			if err != nil {
				t.Errorf("Write failed for connection %d: %v", id, err)
				done <- false
				return
			}

			buf := make([]byte, len(testData))
			_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			_, err = conn.Read(buf)
			if err != nil {
				t.Errorf("Read failed for connection %d: %v", id, err)
				done <- false
				return
			}

			if string(buf) != string(testData) {
				t.Errorf("Echo mismatch for connection %d", id)
				done <- false
				return
			}

			done <- true
		}(i)
	}

	// Wait for all connections to complete
	successCount := 0
	for i := 0; i < numConnections; i++ {
		if <-done {
			successCount++
		}
	}

	assert.Equal(t, numConnections, successCount)
}

func BenchmarkTCPServerConnection(b *testing.B) {
	mc := metrics.NewMetricsCollector()
	handler := handlers.NewTCPHandler(mc)

	// Find available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(b, err)
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	server := NewTCPServer(port, handler)
	err = server.Start()
	require.NoError(b, err)
	defer func() { _ = server.Stop() }()

	testData := make([]byte, 1024)
	buf := make([]byte, 1024)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			b.Fatal(err)
		}

		_, err = conn.Write(testData)
		if err != nil {
			b.Fatal(err)
		}

		_, err = conn.Read(buf)
		if err != nil {
			b.Fatal(err)
		}

		_ = conn.Close()
	}
}
