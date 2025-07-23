package handlers

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/PhilipSchmid/flow-generator-app/internal/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	logging.InitLogger("json", "error")
}

func TestNewUDPHandler(t *testing.T) {
	mc := metrics.NewMetricsCollector()
	handler := NewUDPHandler(mc)

	assert.NotNil(t, handler)
	assert.Equal(t, mc, handler.metricsCollector)
}

func TestUDPHandlerHandle(t *testing.T) {
	mc := metrics.NewMetricsCollector()
	handler := NewUDPHandler(mc)

	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)

	conn, err := net.ListenUDP("udp", addr)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	serverAddr := conn.LocalAddr().(*net.UDPAddr)

	done := make(chan bool)
	go func() {
		go handler.Handle(conn)
		time.Sleep(500 * time.Millisecond)
		_ = conn.Close()
		done <- true
	}()

	clientConn, err := net.Dial("udp", serverAddr.String())
	require.NoError(t, err)
	defer func() { _ = clientConn.Close() }()

	testData := []byte("Hello UDP!")
	_, err = clientConn.Write(testData)
	require.NoError(t, err)

	buf := make([]byte, 1024)
	_ = clientConn.SetReadDeadline(time.Now().Add(1 * time.Second))
	n, err := clientConn.Read(buf)
	require.NoError(t, err)

	assert.Equal(t, testData, buf[:n])

	<-done
}

func TestUDPHandlerMetrics(t *testing.T) {
	mc := metrics.NewMetricsCollector()
	handler := NewUDPHandler(mc)

	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)

	conn, err := net.ListenUDP("udp", addr)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	serverAddr := conn.LocalAddr().(*net.UDPAddr)

	done := make(chan bool)
	go func() {
		go handler.Handle(conn)
		time.Sleep(500 * time.Millisecond)
		_ = conn.Close()
		done <- true
	}()

	clientConn, err := net.Dial("udp", serverAddr.String())
	require.NoError(t, err)
	defer func() { _ = clientConn.Close() }()

	packetsSent := 0
	packetsReceived := 0

	for i := 0; i < 5; i++ {
		testData := []byte(fmt.Sprintf("test packet %d", i))
		_, err = clientConn.Write(testData)
		require.NoError(t, err)
		packetsSent++

		buf := make([]byte, 1024)
		_ = clientConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, err := clientConn.Read(buf)
		if err == nil {
			packetsReceived++
			assert.Equal(t, testData, buf[:n])
		}
	}

	<-done

	assert.Greater(t, packetsReceived, 0)
	assert.LessOrEqual(t, packetsReceived, packetsSent)
}

func BenchmarkUDPHandler(b *testing.B) {
	mc := metrics.NewMetricsCollector()
	handler := NewUDPHandler(mc)

	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(b, err)

	conn, err := net.ListenUDP("udp", addr)
	require.NoError(b, err)
	defer func() { _ = conn.Close() }()

	serverAddr := conn.LocalAddr().(*net.UDPAddr)

	go handler.Handle(conn)

	clientConn, err := net.Dial("udp", serverAddr.String())
	require.NoError(b, err)
	defer func() { _ = clientConn.Close() }()

	data := make([]byte, 1024)
	buf := make([]byte, 1024)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err = clientConn.Write(data)
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
