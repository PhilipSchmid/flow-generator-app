package handlers

import (
	"io"
	"net"
	"strconv"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/PhilipSchmid/flow-generator-app/internal/metrics"
)

// TCPHandler handles TCP connections
type TCPHandler struct {
	metricsCollector *metrics.MetricsCollector
}

// NewTCPHandler creates a new TCP handler
func NewTCPHandler(mc *metrics.MetricsCollector) *TCPHandler {
	return &TCPHandler{
		metricsCollector: mc,
	}
}

// Handle processes a TCP connection
func (h *TCPHandler) Handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	h.metricsCollector.ActiveTCPConnections.Inc()
	defer h.metricsCollector.ActiveTCPConnections.Dec()

	port := conn.LocalAddr().(*net.TCPAddr).Port
	portStr := strconv.Itoa(port)
	protocol := "tcp"

	h.metricsCollector.IncRequestsReceived(protocol, portStr)
	h.metricsCollector.IncTCPConnectionsOpened()

	if logging.DebugEnabled() {
		logging.Logger.Debugf("Accepted TCP connection on %s from %s", conn.LocalAddr(), conn.RemoteAddr())
	}

	buf := make([]byte, 1024)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			if err != io.EOF {
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
				if logging.DebugEnabled() {
					logging.Logger.Debugf("Failed to write to TCP connection from %s: %v", conn.RemoteAddr(), writeErr)
				}
				return
			}
		}
		h.metricsCollector.AddBytesSent(protocol, portStr, written)
	}
}
