package handlers

import (
	"context"
	"errors"
	"net"
	"strconv"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/PhilipSchmid/flow-generator-app/internal/metrics"
	"github.com/PhilipSchmid/flow-generator-app/internal/tracing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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
	// A UDP datagram can be larger than the client's default MTU. Allocate the
	// receive buffer once so valid datagrams are never silently truncated.
	buf := make([]byte, 64*1024)
	portStr := strconv.Itoa(conn.LocalAddr().(*net.UDPAddr).Port)
	const protocol = "udp"
	for {
		n, addr, err := conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				logging.Logger.Warnf("UDP read failed on port %s: %v", portStr, err)
			}
			return
		}

		h.metricsCollector.IncRequestsReceived(protocol, portStr)
		h.metricsCollector.IncUDPPacketsReceived()
		h.metricsCollector.AddBytesReceived(protocol, portStr, n)
		var packetSpan trace.Span
		if tracing.Enabled() {
			_, packetSpan = otel.Tracer("echo-server").Start(context.Background(), "udp.echo")
			packetSpan.SetAttributes(
				attribute.String("server.port", portStr),
				attribute.String("network.transport", protocol),
				attribute.Int("network.io.bytes", n),
			)
		}

		if logging.DebugEnabled() {
			logging.Logger.Debugf("Received UDP packet from %s", addr)
		}

		n, err = conn.WriteToUDPAddrPort(buf[:n], addr)
		if err != nil {
			if packetSpan != nil {
				packetSpan.RecordError(err)
				packetSpan.End()
			}
			if logging.DebugEnabled() {
				logging.Logger.Debugf("Failed to write UDP packet to %s: %v", addr, err)
			}
			continue
		}
		h.metricsCollector.AddBytesSent(protocol, portStr, n)
		if packetSpan != nil {
			packetSpan.End()
		}
	}
}
