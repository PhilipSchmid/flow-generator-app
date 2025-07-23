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
	h.metricsCollector.TCPConnectionsOpenedPerSecond.Inc()

	logging.Logger.Debugf("Accepted TCP connection on %s from %s", conn.LocalAddr().String(), conn.RemoteAddr().String())

	buf := make([]byte, 1024)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			if err != io.EOF {
				logging.Logger.Debugf("TCP connection from %s closed: %v", conn.RemoteAddr().String(), err)
			}
			return
		}
		h.metricsCollector.AddBytesReceived(protocol, portStr, n)

		n, err = conn.Write(buf[:n])
		if err != nil {
			logging.Logger.Debugf("Failed to write to TCP connection from %s: %v", conn.RemoteAddr().String(), err)
			return
		}
		h.metricsCollector.AddBytesSent(protocol, portStr, n)
	}
}
