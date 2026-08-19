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
	statusapi "github.com/PhilipSchmid/flow-generator-app/internal/status"
	"github.com/PhilipSchmid/flow-generator-app/internal/tracing"
	"github.com/PhilipSchmid/flow-generator-app/internal/version"

	"github.com/spf13/pflag"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// ProtocolPort combines a protocol and its associated port
type ProtocolPort struct {
	Protocol    string
	Port        int
	address     string
	portStr     string
	statusIndex int
}

func newProtocolPort(server, protocol string, port int) ProtocolPort {
	return ProtocolPort{
		Protocol: protocol,
		Port:     port,
		address:  constructAddress(server, port),
		portStr:  strconv.Itoa(port),
	}
}

var payloadCache []byte

const tcpReadBufferSize = 1024

const (
	progressLogInterval       = 30 * time.Second
	networkFailureLogInterval = 30 * time.Second
)

var tcpReadBufferPool = sync.Pool{
	New: func() any { return new([tcpReadBufferSize]byte) },
}

var flowLogs = struct {
	tcpDial, tcpWrite, tcpRead, tcpMismatch *logging.RateLimiter
	udpDial, udpWrite, udpRead, udpTimeout  *logging.RateLimiter
	udpMismatch, udpMTU                     *logging.RateLimiter
}{
	tcpDial: logging.NewRateLimiter(networkFailureLogInterval), tcpWrite: logging.NewRateLimiter(networkFailureLogInterval),
	tcpRead: logging.NewRateLimiter(networkFailureLogInterval), tcpMismatch: logging.NewRateLimiter(networkFailureLogInterval),
	udpDial: logging.NewRateLimiter(networkFailureLogInterval), udpWrite: logging.NewRateLimiter(networkFailureLogInterval),
	udpRead: logging.NewRateLimiter(networkFailureLogInterval), udpTimeout: logging.NewRateLimiter(networkFailureLogInterval),
	udpMismatch: logging.NewRateLimiter(networkFailureLogInterval), udpMTU: logging.NewRateLimiter(networkFailureLogInterval),
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

func expectedFlowEnd(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
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
type flowOutcome uint8

const (
	flowCompleted flowOutcome = iota
	flowCanceled
	flowFailed
)

type flowObserver struct {
	tracker       *statusapi.ClientTracker
	sampleLatency bool
}

func (o flowObserver) dialError() {
	if o.tracker != nil {
		o.tracker.RecordDialError()
	}
}
func (o flowObserver) readError() {
	if o.tracker != nil {
		o.tracker.RecordReadError()
	}
}
func (o flowObserver) writeError() {
	if o.tracker != nil {
		o.tracker.RecordWriteError()
	}
}
func (o flowObserver) mismatch() {
	if o.tracker != nil {
		o.tracker.RecordMismatch()
	}
}
func (o flowObserver) mtuError() {
	if o.tracker != nil {
		o.tracker.RecordMTUError()
	}
}
func (o flowObserver) latency(protocol string, started time.Time) {
	if o.tracker != nil && o.sampleLatency {
		o.tracker.ObserveLatency(protocol, time.Since(started))
	}
}

func finishFlow(mainCtx context.Context, successful bool) flowOutcome {
	if mainCtx.Err() != nil {
		return flowCanceled
	}
	if successful {
		return flowCompleted
	}
	return flowFailed
}

// generateFlow generates network traffic to the server and reads the echoed response.
func generateFlow(mainCtx context.Context, cfg *config.ClientConfig, mc *metrics.MetricsCollector, pp ProtocolPort, duration float64) flowOutcome {
	return generateFlowObserved(mainCtx, cfg, mc, pp, duration, flowObserver{})
}

func generateFlowObserved(mainCtx context.Context, cfg *config.ClientConfig, mc *metrics.MetricsCollector, pp ProtocolPort, duration float64, observer flowObserver) flowOutcome {
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

	dialer := net.Dialer{}
	if pp.Protocol == "tcp" {
		conn, err := dialer.DialContext(flowCtx, "tcp", pp.address)
		if err != nil {
			if !expectedFlowEnd(flowCtx, err) {
				observer.dialError()
				flowLogs.tcpDial.Warnw("TCP connection failed", "server", cfg.Server, "port", pp.Port, "error", err)
			}
			return finishFlow(mainCtx, false)
		}
		defer func() { _ = conn.Close() }()
		defer applyFlowDeadline(flowCtx, conn)()

		if len(payload) > cfg.MSS && logging.DebugEnabled() {
			logging.Logger.Debugf("TCP payload size %d exceeds MSS %d; the network stack will segment it", len(payload), cfg.MSS)
		}

		echoStarted := time.Now()
		nSent, err := writeFull(conn, payload)
		if err != nil {
			if !expectedFlowEnd(flowCtx, err) {
				observer.writeError()
				flowLogs.tcpWrite.Warnw("TCP write failed", "server", cfg.Server, "port", pp.Port, "error", err)
			}
			return finishFlow(mainCtx, false)
		}
		mc.IncRequestsSent("tcp", pp.portStr)
		mc.AddBytesSent("tcp", pp.portStr, nSent)
		mc.IncTCPConnectionsOpened()

		totalReceived := 0
		readBuffer := tcpReadBufferPool.Get().(*[tcpReadBufferSize]byte)
		buf := readBuffer[:]
		for totalReceived < payloadSize {
			n, err := conn.Read(buf)
			if err != nil {
				expected := expectedFlowEnd(flowCtx, err)
				if !expected {
					observer.readError()
					flowLogs.tcpRead.Warnw("TCP read failed", "server", cfg.Server, "port", pp.Port, "error", err)
				}
				break
			}
			totalReceived += n
			mc.AddBytesReceived("tcp", pp.portStr, n)
		}
		if totalReceived != payloadSize {
			if flowCtx.Err() == nil {
				observer.mismatch()
				flowLogs.tcpMismatch.Warnw("TCP byte mismatch", "server", cfg.Server, "port", pp.Port, "sent", payloadSize, "received", totalReceived)
			}

			// The read above broke early (deadline hit or connection error),
			// so a reply the server already sent may still be in flight.
			// Drain it before Close() -- see drainStragglerReply.
			drainStragglerReply(conn, buf)
		} else {
			observer.latency("tcp", echoStarted)
		}
		tcpReadBufferPool.Put(readBuffer)

		// Wait for the flow's context to be done (timeout or mainCtx cancellation)
		<-flowCtx.Done()
		if logging.DebugEnabled() {
			logging.Logger.Debugf("TCP flow to %s:%d ended after %.3f seconds", cfg.Server, pp.Port, duration)
		}
		return finishFlow(mainCtx, totalReceived == payloadSize)
	} else { // udp
		conn, err := dialer.DialContext(flowCtx, "udp", pp.address)
		if err != nil {
			if !expectedFlowEnd(flowCtx, err) {
				observer.dialError()
				flowLogs.udpDial.Warnw("UDP connection failed", "server", cfg.Server, "port", pp.Port, "error", err)
			}
			return finishFlow(mainCtx, false)
		}
		defer func() { _ = conn.Close() }()
		defer applyFlowDeadline(flowCtx, conn)()

		if len(payload) > cfg.MTU {
			// Payload size is fixed for the lifetime of this flow, so if it
			// exceeds the MTU once it exceeds it on every iteration -- retrying
			// here would busy-loop for the full flow duration instead of
			// backing off. Fail the whole flow instead.
			flowLogs.udpMTU.Warnw("UDP payload exceeds MTU", "payload_bytes", len(payload), "mtu_bytes", cfg.MTU)
			observer.mtuError()
			return flowFailed
		}

		buf := make([]byte, payloadSize)
		successful := false
		latencyPending := observer.sampleLatency
		cadence := time.NewTimer(100 * time.Millisecond)
		defer cadence.Stop()
		for flowCtx.Err() == nil {
			echoStarted := time.Now()
			nSent, err := conn.Write(payload)
			if err == nil && nSent != len(payload) {
				err = io.ErrShortWrite
			}
			if err != nil {
				if expectedFlowEnd(flowCtx, err) {
					return finishFlow(mainCtx, successful)
				}
				observer.writeError()
				flowLogs.udpWrite.Warnw("UDP write failed", "server", cfg.Server, "port", pp.Port, "error", err)
			} else {
				mc.IncRequestsSent("udp", pp.portStr)
				mc.AddBytesSent("udp", pp.portStr, nSent)

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
							flowLogs.udpTimeout.Debugw("UDP response timed out", "server", cfg.Server, "port", pp.Port)
						}
					} else if flowCtx.Err() == nil {
						observer.readError()
						flowLogs.udpRead.Warnw("UDP read failed", "server", cfg.Server, "port", pp.Port, "error", readErr)
					}
				} else {
					mc.AddBytesReceived("udp", pp.portStr, nReceived)
					if nReceived != payloadSize {
						observer.mismatch()
						flowLogs.udpMismatch.Warnw("UDP byte mismatch", "server", cfg.Server, "port", pp.Port, "sent", payloadSize, "received", nReceived)
					} else {
						successful = true
						if latencyPending {
							observer.latency("udp", echoStarted)
							latencyPending = false
						}
					}
				}
			}

			select {
			case <-cadence.C:
				cadence.Reset(100 * time.Millisecond)
			case <-flowCtx.Done():
				return finishFlow(mainCtx, successful)
			}
		}
		return finishFlow(mainCtx, successful)
	}
}

type schedulerStats struct {
	started      uint64
	skipped      uint64
	limitReached bool
}

func runFlowScheduler(ctx context.Context, rate float64, maxConcurrent, flowCount int, launch func(context.Context)) schedulerStats {
	return runFlowSchedulerTracked(ctx, rate, maxConcurrent, flowCount, launch, nil)
}

func runFlowSchedulerTracked(ctx context.Context, rate float64, maxConcurrent, flowCount int, launch func(context.Context), tracker *statusapi.ClientTracker) schedulerStats {
	interval := time.Duration(float64(time.Second) / rate)
	if interval < time.Nanosecond {
		interval = time.Nanosecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	progressTicker := time.NewTicker(progressLogInterval)
	defer progressTicker.Stop()

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
					if tracker != nil {
						tracker.SetLimitReached()
					}
					wait()
					return stats
				}
			default:
				stats.skipped++
				if tracker != nil {
					tracker.StartSkipped()
				}
			}
		case <-progressTicker.C:
			logging.Logger.Infow("Flow generation progress",
				"flows_started", stats.started,
				"active_flows", len(sem),
				"starts_skipped_at_capacity", stats.skipped,
			)
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
	startedAt := time.Now().UTC()
	config.NormalizeFlagNames(pflag.CommandLine)

	// Define command-line flags
	versionFlag := pflag.Bool("version", false, "Print version information and exit")
	pflag.String("log-level", "", "Log level: debug, info, warn, error")
	pflag.String("log-format", "", "Log format: human or json")
	pflag.String("metrics-port", "", "Port for the metrics server")
	pflag.String("status-port", "", "Loopback status port for the dashboard (0 to disable)")
	pflag.Bool("tracing-enabled", false, "Enable tracing")
	pflag.String("jaeger-endpoint", "", "Jaeger endpoint")
	pflag.String("server", "", "Server address or hostname")
	pflag.Float64("rate", 0, "Flow generation rate in flows per second")
	pflag.Int("max-concurrent", 0, "Maximum number of concurrent flows")
	pflag.String("protocol", "", "Protocol to use (tcp, udp, both)")
	pflag.Float64("min-duration", 0, "Minimum flow duration in seconds")
	pflag.Float64("max-duration", 0, "Maximum flow duration in seconds")
	pflag.Bool("constant-flows", false, "Enable constant flow mode")
	pflag.String("tcp-ports", "", "Comma-separated list of TCP ports")
	pflag.String("udp-ports", "", "Comma-separated list of UDP ports")
	pflag.Int("payload-size", 0, "Fixed payload size in bytes")
	pflag.Int("min-payload-size", 0, "Minimum payload size in bytes")
	pflag.Int("max-payload-size", 0, "Maximum payload size in bytes")
	pflag.Int("mtu", 0, "Maximum Transmission Unit in bytes")
	pflag.Int("mss", 0, "Maximum Segment Size in bytes")
	pflag.Float64("flow-timeout", 0.0, "Timeout in seconds for flow generation (0 for no timeout)")
	pflag.Int("flow-count", 0, "Maximum number of flows to generate (0 for no limit)")

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
			pp := newProtocolPort(cfg.Server, "tcp", p)
			pp.statusIndex = len(availablePorts)
			availablePorts = append(availablePorts, pp)
		}
	}
	if cfg.Protocol == "udp" || cfg.Protocol == "both" {
		for _, p := range udpPorts {
			pp := newProtocolPort(cfg.Server, "udp", p)
			pp.statusIndex = len(availablePorts)
			availablePorts = append(availablePorts, pp)
		}
	}

	if len(availablePorts) == 0 {
		logging.Logger.Error("No valid ports available for the selected protocol")
		os.Exit(1)
	}
	statusPorts := make([]statusapi.PortFlowSnapshot, len(availablePorts))
	for i, pp := range availablePorts {
		statusPorts[i] = statusapi.PortFlowSnapshot{Protocol: pp.Protocol, Port: pp.Port}
	}
	var statusTracker *statusapi.ClientTracker
	var statusServer *statusapi.Server
	if cfg.StatusPort != "0" {
		statusTracker = statusapi.NewClientTracker(statusPorts)
		statusServer, err = statusapi.Start(cfg.StatusPort, func() statusapi.Snapshot {
			client := statusTracker.Snapshot()
			state := "running"
			if client.LimitReached {
				state = "draining"
			}
			return statusapi.Snapshot{
				SchemaVersion: statusapi.SchemaVersion,
				Role:          "client",
				Version:       version.Short(),
				SampledAt:     time.Now().UTC(),
				StartedAt:     startedAt,
				State:         state,
				Configuration: statusapi.Configuration{
					Target: cfg.Server, Protocol: cfg.Protocol,
					TCPPorts: tcpPorts, UDPPorts: udpPorts,
					Rate: cfg.Rate, MaxConcurrent: cfg.MaxConcurrent,
					MinDuration: cfg.MinDuration, MaxDuration: cfg.MaxDuration,
					ConstantFlows: cfg.ConstantFlows, FlowTimeout: cfg.FlowTimeout,
					FlowCount: cfg.FlowCount, PayloadSize: cfg.PayloadSize,
					MinPayloadSize: cfg.MinPayloadSize, MaxPayloadSize: cfg.MaxPayloadSize,
					MTU: cfg.MTU, MSS: cfg.MSS, MetricsPort: cfg.MetricsPort,
					TracingEnabled: cfg.TracingEnabled,
				},
				Traffic: mc.Snapshot(),
				Client:  &client,
			}
		})
		if err != nil {
			logging.Logger.Fatalf("Failed to start local dashboard status: %v", err)
		}
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := statusServer.Stop(ctx); err != nil {
			logging.Logger.Errorf("Failed to stop local dashboard status: %v", err)
		}
	}()

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

	stats := runFlowSchedulerTracked(mainCtx, cfg.Rate, cfg.MaxConcurrent, cfg.FlowCount, func(ctx context.Context) {
		// #nosec G404 - math/rand is sufficient for flow scheduling randomization
		pp := availablePorts[rand.IntN(len(availablePorts))]
		sampleLatency := false
		if statusTracker != nil {
			sampleLatency = statusTracker.FlowStarted(pp.statusIndex)
		}
		duration := constantDuration
		if !cfg.ConstantFlows {
			// #nosec G404 - math/rand is sufficient for flow scheduling randomization
			duration = cfg.MinDuration + rand.Float64()*(cfg.MaxDuration-cfg.MinDuration)
		}
		outcome := generateFlowObserved(ctx, cfg, mc, pp, duration, flowObserver{tracker: statusTracker, sampleLatency: sampleLatency})
		if statusTracker != nil {
			switch outcome {
			case flowCompleted:
				statusTracker.FlowCompleted()
			case flowCanceled:
				statusTracker.FlowCanceled()
			case flowFailed:
				statusTracker.FlowFailed(pp.statusIndex)
			}
		}
	}, statusTracker)

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
