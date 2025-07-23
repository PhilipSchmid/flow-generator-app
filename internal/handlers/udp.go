package handlers

import (
	"net"
	"strconv"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/PhilipSchmid/flow-generator-app/internal/metrics"
)

// UDPHandler handles UDP packets
type UDPHandler struct {
	metricsCollector *metrics.MetricsCollector
}

// NewUDPHandler creates a new UDP handler
func NewUDPHandler(mc *metrics.MetricsCollector) *UDPHandler {
	return &UDPHandler{
		metricsCollector: mc,
	}
}

// Handle processes UDP packets on the given connection
func (h *UDPHandler) Handle(conn *net.UDPConn) {
	buf := make([]byte, 1024)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			logging.Logger.Infof("UDP connection closed: %v", err)
			return
		}

		port := conn.LocalAddr().(*net.UDPAddr).Port
		portStr := strconv.Itoa(port)
		protocol := "udp"

		h.metricsCollector.IncRequestsReceived(protocol, portStr)
		h.metricsCollector.UDPPacketsReceived.Inc()
		h.metricsCollector.AddBytesReceived(protocol, portStr, n)

		logging.Logger.Debugf("Received UDP packet from %s", addr.String())

		n, err = conn.WriteToUDP(buf[:n], addr)
		if err != nil {
			logging.Logger.Debugf("Failed to write UDP packet to %s: %v", addr.String(), err)
			continue
		}
		h.metricsCollector.AddBytesSent(protocol, portStr, n)
	}
}
