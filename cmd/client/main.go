package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/config"
	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/PhilipSchmid/flow-generator-app/internal/metrics"
	"github.com/PhilipSchmid/flow-generator-app/internal/tracing"
	"github.com/PhilipSchmid/flow-generator-app/internal/version"

	"github.com/spf13/pflag"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// ProtocolPort combines a protocol and its associated port
type ProtocolPort struct {
	Protocol string
	Port     int
}

var payloadCache []byte

const tcpReadBufferSize = 1024

var tcpReadBufferPool = sync.Pool{
	New: func() any { return new([tcpReadBufferSize]byte) },
}

// tcpCloseGracePeriod bounds how long we wait to drain a reply that was
// already in flight when a flow's deadline expired, before closing the
// socket. Keeps closes clean (FIN) instead of racing the peer into an RST.
const tcpCloseGracePeriod = 200 * time.Millisecond

// init initializes the payload cache with random bytes
func init() {
	payloadCache = make([]byte, 1<<20) // 1MB
	for i := range payloadCache {
		// #nosec G404 - math/rand is sufficient for test data generation
		payloadCache[i] = byte(rand.Uint32() & 0xFF) // Random bytes (0-255)
	}
}

// constructAddress formats the server address with port
func constructAddress(server string, port int) string {
	if ip := net.ParseIP(server); ip != nil {
		if ip.To4() == nil { // IPv6 address
			return fmt.Sprintf("[%s]:%d", server, port)
		}
	}
	return fmt.Sprintf("%s:%d", server, port)
}

// getPayloadSize determines the size of the payload to send
func getPayloadSize(cfg *config.ClientConfig) int {
	if size := cfg.PayloadSize; size > 0 {
		return size // Fixed size
	}
	minSize := cfg.MinPayloadSize
	maxSize := cfg.MaxPayloadSize
	if minSize > 0 && maxSize >= minSize {
		// #nosec G404 - math/rand is sufficient for test data generation
		return minSize + rand.IntN(maxSize-minSize+1)
	}
	return 5 // Default to 5 bytes
}

func writeFull(conn net.Conn, payload []byte) (int, error) {
	written := 0
	for written < len(payload) {
		n, err := conn.Write(payload[written:])
		written += n
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

func applyFlowDeadline(ctx context.Context, conn net.Conn) func() {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() {
		_ = conn.SetDeadline(time.Now())
	})
	return func() { _ = stop() }
}

// drainStragglerReply gives conn a short, bounded grace window to receive a
// reply that was already in flight when the caller's read loop broke out
// (deadline hit or read error). Closing a TCP socket while data the peer
// already sent is still arriving makes the kernel answer with RST instead
// of a clean FIN -- indistinguishable from a real failure in Hubble/tcpdump,
// and observed live as synchronized RST bursts across an entire batch of
// otherwise-healthy flows sharing the same fixed flow duration. Half-closes
// the write side first (nothing left to send) so the peer sees FIN promptly
// instead of waiting out its own read timeout.
func drainStragglerReply(conn net.Conn, buf []byte) {
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.CloseWrite()
	}
	if err := conn.SetReadDeadline(time.Now().Add(tcpCloseGracePeriod)); err != nil {
		return
	}
	for {
		if _, err := conn.Read(buf); err != nil {
			return
		}
	}
}

// generateFlow generates network traffic to the server and reads the echoed response
func generateFlow(mainCtx context.Context, cfg *config.ClientConfig, mc *metrics.MetricsCollector, pp ProtocolPort, duration float64) {
	payloadSize := getPayloadSize(cfg)
	payload := payloadCache[:payloadSize]

	if logging.DebugEnabled() {
		logging.Logger.Debugf("Starting %s flow for %.3f seconds to %s on port %d with payload size %d bytes", pp.Protocol, duration, cfg.Server, pp.Port, payloadSize)
	}

	// Create a context for this flow with its own timeout
	flowCtx, flowCancel := context.WithTimeout(mainCtx, time.Duration(duration*float64(time.Second)))
	defer flowCancel()
	if tracing.Enabled() {
		_, span := otel.Tracer("flow-generator").Start(flowCtx, "network.flow")
		span.SetAttributes(
			attribute.String("network.transport", pp.Protocol),
			attribute.String("server.address", cfg.Server),
			attribute.Int("server.port", pp.Port),
			attribute.Int("network.io.bytes", payloadSize),
		)
		defer span.End()
	}

	addr := constructAddress(cfg.Server, pp.Port)
	portStr := strconv.Itoa(pp.Port)
	dialer := net.Dialer{}
	if pp.Protocol == "tcp" {
		conn, err := dialer.DialContext(flowCtx, "tcp", addr)
		if err != nil {
			if flowCtx.Err() == nil {
				logging.Logger.Warnf("Failed to connect to %s:%d (TCP): %v", cfg.Server, pp.Port, err)
			}
			return
		}
		defer func() { _ = conn.Close() }()
		defer applyFlowDeadline(flowCtx, conn)()

		if len(payload) > cfg.MSS && logging.DebugEnabled() {
			logging.Logger.Debugf("TCP payload size %d exceeds MSS %d; the network stack will segment it", len(payload), cfg.MSS)
		}

		nSent, err := writeFull(conn, payload)
		if err != nil {
			if flowCtx.Err() == nil {
				logging.Logger.Warnf("Failed to write to TCP connection: %v", err)
			}
			return
		}
		mc.IncRequestsSent("tcp", portStr)
		mc.AddBytesSent("tcp", portStr, nSent)
		mc.IncTCPConnectionsOpened()

		totalReceived := 0
		readBuffer := tcpReadBufferPool.Get().(*[tcpReadBufferSize]byte)
		buf := readBuffer[:]
		for totalReceived < payloadSize {
			n, err := conn.Read(buf)
			if err != nil {
				if flowCtx.Err() == nil {
					logging.Logger.Warnf("Failed to read full TCP response: %v", err)
				}
				break
			}
			totalReceived += n
			mc.AddBytesReceived("tcp", portStr, n)
		}
		if totalReceived != payloadSize {
			if flowCtx.Err() == nil {
				logging.Logger.Warnf("TCP byte mismatch: sent %d bytes, received %d bytes", payloadSize, totalReceived)
			}

			// The read above broke early (deadline hit or connection error),
			// so a reply the server already sent may still be in flight.
			// Drain it before Close() -- see drainStragglerReply.
			drainStragglerReply(conn, buf)
		}
		tcpReadBufferPool.Put(readBuffer)

		// Wait for the flow's context to be done (timeout or mainCtx cancellation)
		<-flowCtx.Done()
		if logging.DebugEnabled() {
			logging.Logger.Debugf("TCP flow to %s:%d ended after %.3f seconds", cfg.Server, pp.Port, duration)
		}
	} else { // udp
		conn, err := dialer.DialContext(flowCtx, "udp", addr)
		if err != nil {
			if flowCtx.Err() == nil {
				logging.Logger.Warnf("Failed to connect to %s:%d (UDP): %v", cfg.Server, pp.Port, err)
			}
			return
		}
		defer func() { _ = conn.Close() }()
		defer applyFlowDeadline(flowCtx, conn)()

		if len(payload) > cfg.MTU {
			// Payload size is fixed for the lifetime of this flow, so if it
			// exceeds the MTU once it exceeds it on every iteration -- retrying
			// here would busy-loop for the full flow duration instead of
			// backing off. Fail the whole flow instead.
			logging.Logger.Warnf("UDP payload size %d exceeds MTU %d, aborting flow", len(payload), cfg.MTU)
			return
		}

		buf := make([]byte, payloadSize)
		cadence := time.NewTimer(100 * time.Millisecond)
		defer cadence.Stop()
		for flowCtx.Err() == nil {
			nSent, err := conn.Write(payload)
			if err == nil && nSent != len(payload) {
				err = io.ErrShortWrite
			}
			if err != nil {
				if flowCtx.Err() != nil {
					return
				}
				logging.Logger.Warnf("Failed to write to UDP connection: %v", err)
			} else {
				mc.IncRequestsSent("udp", portStr)
				mc.AddBytesSent("udp", portStr, nSent)

				deadline := time.Now().Add(time.Second)
				if flowDeadline, ok := flowCtx.Deadline(); ok && flowDeadline.Before(deadline) {
					deadline = flowDeadline
				}
				_ = conn.SetReadDeadline(deadline)
				nReceived, readErr := conn.Read(buf)
				if readErr != nil {
					var netErr net.Error
					if errors.As(readErr, &netErr) && netErr.Timeout() {
						if flowCtx.Err() == nil && logging.DebugEnabled() {
							logging.Logger.Debugf("Timeout waiting for UDP response from %s:%d", cfg.Server, pp.Port)
						}
					} else if flowCtx.Err() == nil {
						logging.Logger.Warnf("Failed to read from UDP connection: %v", readErr)
					}
				} else {
					mc.AddBytesReceived("udp", portStr, nReceived)
					if nReceived != payloadSize {
						logging.Logger.Warnf("UDP byte mismatch: sent %d bytes, received %d bytes", payloadSize, nReceived)
					}
				}
			}

			select {
			case <-cadence.C:
				cadence.Reset(100 * time.Millisecond)
			case <-flowCtx.Done():
				return
			}
		}
	}
}

type schedulerStats struct {
	started      uint64
	skipped      uint64
	limitReached bool
}

func runFlowScheduler(ctx context.Context, rate float64, maxConcurrent, flowCount int, launch func(context.Context)) schedulerStats {
	interval := time.Duration(float64(time.Second) / rate)
	if interval < time.Nanosecond {
		interval = time.Nanosecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	stats := schedulerStats{}
	wait := func() {
		wg.Wait()
	}

	for {
		select {
		case <-ctx.Done():
			wait()
			return stats
		case <-ticker.C:
			select {
			case sem <- struct{}{}:
				stats.started++
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer func() { <-sem }()
					launch(ctx)
				}()
				if flowCount > 0 && stats.started >= uint64(flowCount) {
					stats.limitReached = true
					wait()
					return stats
				}
			default:
				stats.skipped++
			}
		}
	}
}

// parsePorts parses a comma-separated string of ports into a slice of integers
func parsePorts(portsStr string) []int {
	if portsStr == "" {
		return []int{}
	}
	var ports []int
	for _, p := range strings.Split(portsStr, ",") {
		p = strings.TrimSpace(p)
		port, err := strconv.Atoi(p)
		if err == nil && port > 0 && port <= 65535 {
			ports = append(ports, port)
		} else {
			logging.Logger.Warnf("Invalid port '%s' ignored", p)
		}
	}
	return ports
}

func main() {
	// Define command-line flags
	versionFlag := pflag.Bool("version", false, "Print version information and exit")
	pflag.String("log_level", "", "Log level: debug, info, warn, error")
	pflag.String("log_format", "", "Log format: human or json")
	pflag.String("metrics_port", "", "Port for the metrics server")
	pflag.Bool("tracing_enabled", false, "Enable tracing")
	pflag.String("jaeger_endpoint", "", "Jaeger endpoint")
	pflag.String("server", "", "Server address or hostname")
	pflag.Float64("rate", 0, "Flow generation rate in flows per second")
	pflag.Int("max_concurrent", 0, "Maximum number of concurrent flows")
	pflag.String("protocol", "", "Protocol to use (tcp, udp, both)")
	pflag.Float64("min_duration", 0, "Minimum flow duration in seconds")
	pflag.Float64("max_duration", 0, "Maximum flow duration in seconds")
	pflag.Bool("constant_flows", false, "Enable constant flow mode")
	pflag.String("tcp_ports", "", "Comma-separated list of TCP ports")
	pflag.String("udp_ports", "", "Comma-separated list of UDP ports")
	pflag.Int("payload_size", 0, "Fixed payload size in bytes")
	pflag.Int("min_payload_size", 0, "Minimum payload size in bytes")
	pflag.Int("max_payload_size", 0, "Maximum payload size in bytes")
	pflag.Int("mtu", 0, "Maximum Transmission Unit in bytes")
	pflag.Int("mss", 0, "Maximum Segment Size in bytes")
	pflag.Float64("flow_timeout", 0.0, "Timeout in seconds for flow generation (0 for no timeout)")
	pflag.Int("flow_count", 0, "Maximum number of flows to generate (0 for no limit)")

	// Parse flags
	pflag.Parse()

	// Handle version flag
	if *versionFlag {
		fmt.Println("Flow Generator Client")
		fmt.Println(version.Info())
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.LoadClientConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	logging.InitLogger(cfg.LogFormat, cfg.LogLevel)
	defer func() {
		if err := logging.SyncLogger(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to sync logger: %v\n", err)
		}
	}()

	mc := metrics.NewMetricsCollector()
	metricsServer, err := metrics.StartMetricsServer(cfg.MetricsPort)
	if err != nil {
		logging.Logger.Fatalf("Failed to start metrics server: %v", err)
	}
	defer func() {
		ctx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := metricsServer.Stop(ctx); err != nil {
			logging.Logger.Errorf("Failed to stop metrics server: %v", err)
		}
	}()

	if cfg.TracingEnabled {
		tracerProvider, traceErr := tracing.InitTracer(context.Background(), "flow-generator", cfg.JaegerEndpoint)
		if traceErr != nil {
			logging.Logger.Fatalf("Failed to initialize tracing: %v", traceErr)
		}
		defer func() {
			ctx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if err := tracing.Shutdown(ctx, tracerProvider); err != nil {
				logging.Logger.Errorf("Failed to flush tracing: %v", err)
			}
		}()
	}

	tcpPorts := parsePorts(cfg.TCPPorts)
	udpPorts := parsePorts(cfg.UDPPorts)

	// Build list of available ports
	var availablePorts []ProtocolPort
	if cfg.Protocol == "tcp" || cfg.Protocol == "both" {
		for _, p := range tcpPorts {
			availablePorts = append(availablePorts, ProtocolPort{"tcp", p})
		}
	}
	if cfg.Protocol == "udp" || cfg.Protocol == "both" {
		for _, p := range udpPorts {
			availablePorts = append(availablePorts, ProtocolPort{"udp", p})
		}
	}

	if len(availablePorts) == 0 {
		logging.Logger.Error("No valid ports available for the selected protocol")
		os.Exit(1)
	}

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	mainCtx := signalCtx

	// Apply flow timeout if set
	if cfg.FlowTimeout > 0 {
		var timeoutCancel context.CancelFunc
		mainCtx, timeoutCancel = context.WithTimeout(mainCtx, time.Duration(cfg.FlowTimeout*float64(time.Second)))
		defer timeoutCancel()
	}

	constantDuration := float64(cfg.MaxConcurrent) / cfg.Rate
	if cfg.ConstantFlows && constantDuration < cfg.MinDuration {
		logging.Logger.Warnf("Constant flow duration %.3fs is below min_duration %.3fs; increase max_concurrent or lower rate", constantDuration, cfg.MinDuration)
	}
	logging.Logger.Infow("Flow generation started",
		"rate", cfg.Rate,
		"max_concurrent", cfg.MaxConcurrent,
		"protocol", cfg.Protocol,
		"constant_flows", cfg.ConstantFlows,
	)

	stats := runFlowScheduler(mainCtx, cfg.Rate, cfg.MaxConcurrent, cfg.FlowCount, func(ctx context.Context) {
		// #nosec G404 - math/rand is sufficient for flow scheduling randomization
		pp := availablePorts[rand.IntN(len(availablePorts))]
		duration := constantDuration
		if !cfg.ConstantFlows {
			// #nosec G404 - math/rand is sufficient for flow scheduling randomization
			duration = cfg.MinDuration + rand.Float64()*(cfg.MaxDuration-cfg.MinDuration)
		}
		generateFlow(ctx, cfg, mc, pp, duration)
	})

	if stats.limitReached {
		logging.Logger.Info("Flow count limit reached; final flows drained")
	} else {
		logging.Logger.Info("Flow generation canceled; active flows stopped")
	}
	logging.Logger.Infow("Flow generation completed",
		"flows_started", stats.started,
		"starts_skipped_at_capacity", stats.skipped,
	)
	mc.LogMetrics(cfg.LogFormat)
}
