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

func TestNewUDPServer(t *testing.T) {
	mc := metrics.NewMetricsCollector()
	handler := handlers.NewUDPHandler(mc)
	server := NewUDPServer(9000, handler)

	assert.NotNil(t, server)
	assert.Equal(t, 9000, server.port)
	assert.Equal(t, handler, server.handler)
	assert.NotNil(t, server.ctx)
	assert.NotNil(t, server.cancel)
}

func TestUDPServerStartStop(t *testing.T) {
	mc := metrics.NewMetricsCollector()
	handler := handlers.NewUDPHandler(mc)

	// Find available port
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)
	conn, err := net.ListenUDP("udp", addr)
	require.NoError(t, err)
	port := conn.LocalAddr().(*net.UDPAddr).Port
	_ = conn.Close()

	server := NewUDPServer(port, handler)

	// Start server
	err = server.Start()
	require.NoError(t, err)

	// Verify server is listening by sending a packet
	clientConn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", port))
	require.NoError(t, err)
	_, err = clientConn.Write([]byte("test"))
	require.NoError(t, err)
	_ = clientConn.Close()

	// Stop server
	err = server.Stop()
	assert.NoError(t, err)
}

func TestUDPServerPortAndType(t *testing.T) {
	mc := metrics.NewMetricsCollector()
	handler := handlers.NewUDPHandler(mc)
	server := NewUDPServer(9000, handler)

	assert.Equal(t, 9000, server.Port())
	assert.Equal(t, "UDP", server.Type())
}

func TestUDPServerStartError(t *testing.T) {
	mc := metrics.NewMetricsCollector()
	handler := handlers.NewUDPHandler(mc)

	// Use invalid port that will fail
	server := NewUDPServer(-1, handler)

	err := server.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to")
}

func TestUDPServerHandlePackets(t *testing.T) {
	mc := metrics.NewMetricsCollector()
	handler := handlers.NewUDPHandler(mc)

	// Find available port
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)
	conn, err := net.ListenUDP("udp", addr)
	require.NoError(t, err)
	port := conn.LocalAddr().(*net.UDPAddr).Port
	_ = conn.Close()

	server := NewUDPServer(port, handler)

	// Start server
	err = server.Start()
	require.NoError(t, err)
	defer func() { _ = server.Stop() }()

	// Connect and send data
	clientConn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", port))
	require.NoError(t, err)
	defer func() { _ = clientConn.Close() }()

	testData := []byte("Hello UDP Server!")
	_, err = clientConn.Write(testData)
	require.NoError(t, err)

	// Read echo response
	buf := make([]byte, 1024)
	_ = clientConn.SetReadDeadline(time.Now().Add(1 * time.Second))
	n, err := clientConn.Read(buf)
	require.NoError(t, err)

	assert.Equal(t, testData, buf[:n])
}

func TestUDPServerConcurrentPackets(t *testing.T) {
	mc := metrics.NewMetricsCollector()
	handler := handlers.NewUDPHandler(mc)

	// Find available port
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)
	conn, err := net.ListenUDP("udp", addr)
	require.NoError(t, err)
	port := conn.LocalAddr().(*net.UDPAddr).Port
	_ = conn.Close()

	server := NewUDPServer(port, handler)

	// Start server
	err = server.Start()
	require.NoError(t, err)
	defer func() { _ = server.Stop() }()

	// Send multiple packets concurrently
	numPackets := 10
	done := make(chan bool, numPackets)

	for i := 0; i < numPackets; i++ {
		go func(id int) {
			clientConn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", port))
			if err != nil {
				t.Errorf("Connection %d failed: %v", id, err)
				done <- false
				return
			}
			defer func() { _ = clientConn.Close() }()

			testData := []byte(fmt.Sprintf("Packet %d", id))
			_, err = clientConn.Write(testData)
			if err != nil {
				t.Errorf("Write failed for packet %d: %v", id, err)
				done <- false
				return
			}

			buf := make([]byte, 1024)
			_ = clientConn.SetReadDeadline(time.Now().Add(1 * time.Second))
			n, err := clientConn.Read(buf)
			if err != nil {
				t.Errorf("Read failed for packet %d: %v", id, err)
				done <- false
				return
			}

			if string(buf[:n]) != string(testData) {
				t.Errorf("Echo mismatch for packet %d", id)
				done <- false
				return
			}

			done <- true
		}(i)
	}

	// Wait for all packets to complete
	successCount := 0
	for i := 0; i < numPackets; i++ {
		if <-done {
			successCount++
		}
	}

	assert.Equal(t, numPackets, successCount)
}

func BenchmarkUDPServerPacket(b *testing.B) {
	mc := metrics.NewMetricsCollector()
	handler := handlers.NewUDPHandler(mc)

	// Find available port
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(b, err)
	conn, err := net.ListenUDP("udp", addr)
	require.NoError(b, err)
	port := conn.LocalAddr().(*net.UDPAddr).Port
	_ = conn.Close()

	server := NewUDPServer(port, handler)
	err = server.Start()
	require.NoError(b, err)
	defer func() { _ = server.Stop() }()

	clientConn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", port))
	require.NoError(b, err)
	defer func() { _ = clientConn.Close() }()

	testData := make([]byte, 1024)
	buf := make([]byte, 1024)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err = clientConn.Write(testData)
		if err != nil {
			b.Fatal(err)
		}

		_ = clientConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		_, err = clientConn.Read(buf)
		if err != nil {
			b.Fatal(err)
		}
	}
}
