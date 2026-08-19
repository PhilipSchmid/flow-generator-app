package server

import (
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/handlers"
	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/PhilipSchmid/flow-generator-app/internal/metrics"
	statusmetrics "github.com/PhilipSchmid/flow-generator-app/internal/status"
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

	server := NewTCPServer(0, handler)

	// Start server
	err := server.Start()
	require.NoError(t, err)
	port := server.Port()
	require.NotZero(t, port)

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

	server := NewTCPServer(0, handler)

	// Start server
	err := server.Start()
	require.NoError(t, err)
	defer func() { _ = server.Stop() }()
	port := server.Port()

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

	server := NewTCPServer(0, handler)

	// Start server
	err := server.Start()
	require.NoError(t, err)
	defer func() { _ = server.Stop() }()
	port := server.Port()

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

func TestTCPServerStopClosesIdleConnections(t *testing.T) {
	mc := metrics.NewMetricsCollector()
	tracker := &statusmetrics.ServerTracker{}
	server := NewTCPServerWithStatus(0, handlers.NewTCPHandlerWithStatus(mc, tracker), tracker)
	require.NoError(t, server.Start())

	port := server.Port()
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	require.Eventually(t, func() bool {
		server.connsMu.Lock()
		defer server.connsMu.Unlock()
		return len(server.conns) == 1
	}, time.Second, 10*time.Millisecond)

	stopped := make(chan error, 1)
	go func() { stopped <- server.Stop() }()
	select {
	case err := <-stopped:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("server stop blocked on an idle client connection")
	}
	assert.Zero(t, tracker.Errors().Read)
	assert.Zero(t, tracker.Errors().Write)
}

type temporaryAcceptError struct{}

func (temporaryAcceptError) Error() string   { return "temporary accept failure" }
func (temporaryAcceptError) Timeout() bool   { return false }
func (temporaryAcceptError) Temporary() bool { return true }

type failingTCPListener struct{ calls atomic.Uint64 }

func (l *failingTCPListener) Accept() (net.Conn, error) {
	l.calls.Add(1)
	return nil, temporaryAcceptError{}
}
func (*failingTCPListener) Close() error   { return nil }
func (*failingTCPListener) Addr() net.Addr { return &net.TCPAddr{} }

type permanentAcceptError struct{}

func (permanentAcceptError) Error() string { return "permanent accept failure" }

type permanentFailingTCPListener struct{}

func (*permanentFailingTCPListener) Accept() (net.Conn, error) { return nil, permanentAcceptError{} }
func (*permanentFailingTCPListener) Close() error              { return nil }
func (*permanentFailingTCPListener) Addr() net.Addr            { return &net.TCPAddr{} }

func TestTCPServerBacksOffTemporaryAcceptFailures(t *testing.T) {
	listener := &failingTCPListener{}
	server := NewTCPServer(0, nil)
	server.listener = listener
	server.wg.Add(1)
	go server.acceptConnections()

	time.Sleep(30 * time.Millisecond)
	server.cancel()
	server.wg.Wait()
	assert.GreaterOrEqual(t, listener.calls.Load(), uint64(2))
	assert.Less(t, listener.calls.Load(), uint64(10), "temporary accept errors must not spin")
}

func TestTCPServerReportsPermanentAcceptFailure(t *testing.T) {
	failure := make(chan error, 1)
	server := NewTCPServer(0, nil)
	server.listener = &permanentFailingTCPListener{}
	server.SetFailureHandler(func(err error) { failure <- err })
	server.wg.Add(1)
	go server.acceptConnections()

	select {
	case err := <-failure:
		assert.ErrorIs(t, err, permanentAcceptError{})
	case <-time.After(time.Second):
		t.Fatal("permanent listener failure was not reported")
	}
	server.wg.Wait()
}

func BenchmarkTCPServerRoundTrip(b *testing.B) {
	mc := metrics.NewMetricsCollector()
	handler := handlers.NewTCPHandler(mc)

	server := NewTCPServer(0, handler)
	err := server.Start()
	require.NoError(b, err)
	defer func() { _ = server.Stop() }()
	port := server.Port()

	testData := make([]byte, 1024)
	buf := make([]byte, 1024)
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	require.NoError(b, err)
	defer func() { _ = conn.Close() }()

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
