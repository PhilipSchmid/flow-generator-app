package handlers

import (
	"bytes"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/PhilipSchmid/flow-generator-app/internal/metrics"
	"github.com/stretchr/testify/assert"
)

func init() {
	logging.InitLogger("json", "error")
}

// mockConn implements net.Conn for testing
type mockConn struct {
	readBuf    *bytes.Buffer
	writeBuf   *bytes.Buffer
	closed     bool
	localAddr  net.Addr
	remoteAddr net.Addr
	mu         sync.Mutex
}

func newMockConn() *mockConn {
	return &mockConn{
		readBuf:    bytes.NewBuffer([]byte{}),
		writeBuf:   bytes.NewBuffer([]byte{}),
		localAddr:  &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080},
		remoteAddr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345},
	}
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.readBuf.Read(b)
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeBuf.Write(b)
}

func (m *mockConn) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

//nolint:unused // These methods are used in tests but linter doesn't detect it
func (m *mockConn) getWrittenData() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeBuf.Bytes()
}

//nolint:unused // These methods are used in tests but linter doesn't detect it
func (m *mockConn) resetReadBuf() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readBuf.Reset()
}

//nolint:unused // These methods are used in tests but linter doesn't detect it
func (m *mockConn) writeToReadBuf(data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readBuf.Write(data)
}

//nolint:unused // These methods are used in tests but linter doesn't detect it
func (m *mockConn) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

func (m *mockConn) LocalAddr() net.Addr {
	return m.localAddr
}

func (m *mockConn) RemoteAddr() net.Addr {
	return m.remoteAddr
}

func (m *mockConn) SetDeadline(t time.Time) error {
	return nil
}

func (m *mockConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (m *mockConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func TestNewTCPHandler(t *testing.T) {
	mc := metrics.NewMetricsCollector()
	handler := NewTCPHandler(mc)

	assert.NotNil(t, handler)
	assert.Equal(t, mc, handler.metricsCollector)
}

func TestTCPHandlerHandle(t *testing.T) {
	mc := metrics.NewMetricsCollector()
	handler := NewTCPHandler(mc)

	conn := newMockConn()
	testData := []byte("Hello, World!")
	conn.writeToReadBuf(testData)

	done := make(chan bool)
	go func() {
		handler.Handle(conn)
		done <- true
	}()

	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, testData, conn.getWrittenData())

	conn.resetReadBuf()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Handler did not finish in time")
	}

	assert.True(t, conn.isClosed())
}

func TestTCPHandlerMetrics(t *testing.T) {
	mc := metrics.NewMetricsCollector()
	handler := NewTCPHandler(mc)

	conn := newMockConn()
	testData := []byte("test data")
	conn.writeToReadBuf(testData)

	done := make(chan bool)
	go func() {
		handler.Handle(conn)
		done <- true
	}()

	time.Sleep(50 * time.Millisecond)

	writtenData := conn.getWrittenData()
	assert.True(t, len(writtenData) > 0)

	conn.resetReadBuf()

	<-done

	assert.True(t, conn.isClosed())
}

func BenchmarkTCPHandler(b *testing.B) {
	mc := metrics.NewMetricsCollector()
	handler := NewTCPHandler(mc)

	data := bytes.Repeat([]byte("x"), 1024)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		conn := newMockConn()
		conn.writeToReadBuf(data)

		handler.Handle(conn)
	}
}
