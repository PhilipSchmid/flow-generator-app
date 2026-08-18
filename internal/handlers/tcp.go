package handlers

import (
	"context"
	"io"
	"net"
	"strconv"
	"sync"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/PhilipSchmid/flow-generator-app/internal/metrics"
	statusmetrics "github.com/PhilipSchmid/flow-generator-app/internal/status"
	"github.com/PhilipSchmid/flow-generator-app/internal/tracing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const tcpReadBufferSize = 1024

var tcpReadBufferPool = sync.Pool{
	New: func() any { return new([tcpReadBufferSize]byte) },
}

// TCPHandler handles TCP connections
type TCPHandler struct {
	metricsCollector *metrics.MetricsCollector
	statusTracker    *statusmetrics.ServerTracker
}

// NewTCPHandler creates a new TCP handler
func NewTCPHandler(mc *metrics.MetricsCollector) *TCPHandler {
	return &TCPHandler{metricsCollector: mc}
}

// NewTCPHandlerWithStatus creates a TCP handler with dashboard error counters.
func NewTCPHandlerWithStatus(mc *metrics.MetricsCollector, tracker *statusmetrics.ServerTracker) *TCPHandler {
	return &TCPHandler{metricsCollector: mc, statusTracker: tracker}
}

// Handle processes a TCP connection
func (h *TCPHandler) Handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	if h.statusTracker != nil {
		clientKey := h.statusTracker.TCPClientConnected(conn.RemoteAddr())
		defer h.statusTracker.TCPClientDisconnected(clientKey)
	}

	h.metricsCollector.IncActiveTCPConnections()
	defer h.metricsCollector.DecActiveTCPConnections()

	port := conn.LocalAddr().(*net.TCPAddr).Port
	portStr := strconv.Itoa(port)
	const protocol = "tcp"
	if tracing.Enabled() {
		_, span := otel.Tracer("echo-server").Start(context.Background(), "tcp.echo")
		span.SetAttributes(
			attribute.Int("server.port", port),
			attribute.String("network.transport", protocol),
		)
		defer span.End()
	}

	h.metricsCollector.IncRequestsReceived(protocol, portStr)
	h.metricsCollector.IncTCPConnectionsOpened()

	if logging.DebugEnabled() {
		logging.Logger.Debugf("Accepted TCP connection on %s from %s", conn.LocalAddr(), conn.RemoteAddr())
	}

	readBuffer := tcpReadBufferPool.Get().(*[tcpReadBufferSize]byte)
	defer tcpReadBufferPool.Put(readBuffer)
	buf := readBuffer[:]
	for {
		n, err := conn.Read(buf)
		if err != nil {
			if err != io.EOF {
				if h.statusTracker != nil {
					h.statusTracker.RecordReadError()
				}
				if logging.DebugEnabled() {
					logging.Logger.Debugf("TCP connection from %s closed: %v", conn.RemoteAddr(), err)
				}
			}
			return
		}
		h.metricsCollector.AddBytesReceived(protocol, portStr, n)

		written := 0
		for written < n {
			count, writeErr := conn.Write(buf[written:n])
			written += count
			if writeErr != nil {
				if h.statusTracker != nil {
					h.statusTracker.RecordWriteError()
				}
				if logging.DebugEnabled() {
					logging.Logger.Debugf("Failed to write to TCP connection from %s: %v", conn.RemoteAddr(), writeErr)
				}
				return
			}
		}
		h.metricsCollector.AddBytesSent(protocol, portStr, written)
	}
}
