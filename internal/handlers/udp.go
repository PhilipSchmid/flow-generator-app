package handlers

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/PhilipSchmid/flow-generator-app/internal/metrics"
	statusmetrics "github.com/PhilipSchmid/flow-generator-app/internal/status"
	"github.com/PhilipSchmid/flow-generator-app/internal/tracing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var udpFailureLogs = struct {
	read  *logging.RateLimiter
	write *logging.RateLimiter
}{
	read:  logging.NewRateLimiter(time.Second),
	write: logging.NewRateLimiter(time.Second),
}

// UDPHandler handles UDP packets
type UDPHandler struct {
	metricsCollector *metrics.MetricsCollector
	statusTracker    *statusmetrics.ServerTracker
}

// NewUDPHandler creates a new UDP handler
func NewUDPHandler(mc *metrics.MetricsCollector) *UDPHandler {
	return &UDPHandler{metricsCollector: mc}
}

// NewUDPHandlerWithStatus creates a UDP handler with dashboard error counters.
func NewUDPHandlerWithStatus(mc *metrics.MetricsCollector, tracker *statusmetrics.ServerTracker) *UDPHandler {
	return &UDPHandler{metricsCollector: mc, statusTracker: tracker}
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
				if h.statusTracker != nil {
					h.statusTracker.RecordReadError()
				}
				udpFailureLogs.read.Warnw("UDP listener read failed", "port", portStr, "error", err)
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
			if h.statusTracker != nil {
				h.statusTracker.RecordWriteError()
			}
			if packetSpan != nil {
				packetSpan.RecordError(err)
				packetSpan.End()
			}
			if logging.DebugEnabled() {
				udpFailureLogs.write.Debugw("UDP response write failed", "remote", addr, "error", err)
			}
			continue
		}
		h.metricsCollector.AddBytesSent(protocol, portStr, n)
		if packetSpan != nil {
			packetSpan.End()
		}
	}
}
