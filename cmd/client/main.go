package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/pkg/config"
	"github.com/PhilipSchmid/flow-generator-app/pkg/logging"
	"github.com/PhilipSchmid/flow-generator-app/pkg/metrics"
	"github.com/PhilipSchmid/flow-generator-app/pkg/tracing"

	"github.com/spf13/pflag"
)

// ProtocolPort combines a protocol and its associated port
type ProtocolPort struct {
	Protocol string
	Port     int
}

// Global variables
var payloadCache []byte
var cfg *config.ClientConfig
var mc *metrics.MetricsCollector

// init initializes the payload cache with random bytes
func init() {
	src := rand.New(rand.NewPCG(0, 0))
	payloadCache = make([]byte, 1<<20) // 1MB
	for i := range payloadCache {
		payloadCache[i] = byte(src.IntN(256)) // Random bytes (0-255)
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
func getPayloadSize(src *rand.Rand) int {
	if size := cfg.PayloadSize; size > 0 {
		return size // Fixed size
	}
	minSize := cfg.MinPayloadSize
	maxSize := cfg.MaxPayloadSize
	if minSize > 0 && maxSize > minSize {
		return minSize + src.IntN(maxSize-minSize+1)
	}
	return 5 // Default to 5 bytes
}

// generateFlow generates network traffic to the server and reads the echoed response
func generateFlow(mainCtx context.Context, server string, pp ProtocolPort, duration float64, src *rand.Rand, mtu int, mss int, wg *sync.WaitGroup) {
	defer wg.Done()

	payloadSize := getPayloadSize(src)
	if payloadSize > len(payloadCache) {
		payloadSize = len(payloadCache)
	}
	payload := payloadCache[:payloadSize]

	logging.Logger.Debugf("Starting %s flow for %f seconds to %s on port %d with payload size %d bytes", pp.Protocol, duration, server, pp.Port, payloadSize)

	// Create a context for this flow with its own timeout
	flowCtx, flowCancel := context.WithTimeout(mainCtx, time.Duration(duration*float64(time.Second)))
	defer flowCancel()

	addr := constructAddress(server, pp.Port)
	portStr := strconv.Itoa(pp.Port)
	if pp.Protocol == "tcp" {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			logging.Logger.Warnf("Failed to connect to %s:%d (TCP): %v", server, pp.Port, err)
			return
		}
		defer func() { _ = conn.Close() }()

		if len(payload) > mss {
			logging.Logger.Debugf("TCP payload size %d exceeds MSS %d, will be segmented", len(payload), mss)
		}

		nSent, err := conn.Write(payload)
		if err != nil {
			logging.Logger.Warnf("Failed to write to TCP connection: %v", err)
			return
		}
		mc.IncRequestsSent("tcp", portStr)
		mc.AddBytesSent("tcp", portStr, nSent)
		mc.TCPConnectionsOpenedPerSecond.Inc()

		totalReceived := 0
		buf := make([]byte, 1024)
		for totalReceived < payloadSize {
			n, err := conn.Read(buf)
			if err != nil {
				logging.Logger.Warnf("Failed to read full TCP response: %v", err)
				break
			}
			totalReceived += n
			mc.AddBytesReceived("tcp", portStr, n)
		}
		if totalReceived != payloadSize {
			logging.Logger.Warnf("TCP byte mismatch: sent %d bytes, received %d bytes", payloadSize, totalReceived)
		}

		// Wait for the flow's context to be done (timeout or mainCtx cancellation)
		<-flowCtx.Done()
		logging.Logger.Debugf("TCP flow to %s:%d ended after %f seconds", server, pp.Port, duration)
	} else { // udp
		localAddr, _ := net.ResolveUDPAddr("udp", ":0")
		remoteAddr, _ := net.ResolveUDPAddr("udp", addr)
		conn, err := net.DialUDP("udp", localAddr, remoteAddr)
		if err != nil {
			logging.Logger.Warnf("Failed to connect to %s:%d (UDP): %v", server, pp.Port, err)
			return
		}
		defer func() { _ = conn.Close() }()

		startTime := time.Now()
		for time.Since(startTime) < time.Duration(duration*float64(time.Second)) {
			if len(payload) > mtu {
				logging.Logger.Warnf("UDP payload size %d exceeds MTU %d, skipping send", len(payload), mtu)
				continue
			}

			nSent, err := conn.Write(payload)
			if err != nil {
				logging.Logger.Warnf("Failed to write to UDP connection: %v", err)
				continue
			}
			mc.IncRequestsSent("udp", portStr)
			mc.AddBytesSent("udp", portStr, nSent)

			buf := make([]byte, payloadSize)
			if err := conn.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
				logging.Logger.Warnf("Failed to set read deadline for UDP connection: %v", err)
			}
			nReceived, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				if err.(net.Error).Timeout() {
					logging.Logger.Debugf("Timeout waiting for UDP response from %s:%d", server, pp.Port)
				} else {
					logging.Logger.Warnf("Failed to read from UDP connection: %v", err)
				}
			} else {
				mc.AddBytesReceived("udp", portStr, nReceived)
				if nReceived != payloadSize {
					logging.Logger.Warnf("UDP byte mismatch: sent %d bytes, received %d bytes", payloadSize, nReceived)
				}
			}

			select {
			case <-time.After(100 * time.Millisecond):
			case <-flowCtx.Done():
				logging.Logger.Debugf("UDP flow to %s:%d canceled", server, pp.Port)
				return
			}
		}
		logging.Logger.Debugf("UDP flow to %s:%d ended after %f seconds", server, pp.Port, duration)
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

	// Load configuration
	cfg = config.LoadClientConfig()

	// Initialize logger
	logging.InitLogger(cfg.LogFormat, cfg.LogLevel)
	defer func() {
		if err := logging.Logger.Sync(); err != nil {
			if err.Error() != "sync /dev/stderr: inappropriate ioctl for device" {
				fmt.Fprintf(os.Stderr, "Failed to sync logger: %v\n", err)
			}
		}
	}()

	// Initialize MetricsCollector
	mc = metrics.NewMetricsCollector()

	// Handle termination signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		logging.Logger.Info("Application terminated.")
		mc.LogMetrics(cfg.LogFormat)
		os.Exit(0)
	}()

	// Initialize tracing if enabled
	if cfg.TracingEnabled {
		tracing.InitTracer("flow-generator", cfg.JaegerEndpoint)
	}

	// Configuration variables
	server := cfg.Server
	rate := cfg.Rate
	maxConcurrent := cfg.MaxConcurrent
	protocol := cfg.Protocol
	minDuration := cfg.MinDuration
	maxDuration := cfg.MaxDuration
	constantFlows := cfg.ConstantFlows
	tcpPorts := parsePorts(cfg.TCPPorts)
	udpPorts := parsePorts(cfg.UDPPorts)
	mtu := cfg.MTU
	mss := cfg.MSS
	flowTimeout := cfg.FlowTimeout
	flowCount := cfg.FlowCount

	// Build list of available ports
	var availablePorts []ProtocolPort
	if protocol == "tcp" || protocol == "both" {
		for _, p := range tcpPorts {
			availablePorts = append(availablePorts, ProtocolPort{"tcp", p})
		}
	}
	if protocol == "udp" || protocol == "both" {
		for _, p := range udpPorts {
			availablePorts = append(availablePorts, ProtocolPort{"udp", p})
		}
	}

	if len(availablePorts) == 0 {
		logging.Logger.Error("No valid ports available for the selected protocol")
		os.Exit(1)
	}

	// Initialize flow counter and WaitGroup
	var flowCounter uint64
	var wg sync.WaitGroup

	// Create a main context with cancellation for controlling flow generation
	mainCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Apply flow timeout if set
	if flowTimeout > 0 {
		var timeoutCancel context.CancelFunc
		mainCtx, timeoutCancel = context.WithTimeout(mainCtx, time.Duration(flowTimeout*float64(time.Second)))
		defer timeoutCancel()
	}

	sem := make(chan struct{}, maxConcurrent)
	ticker := time.NewTicker(time.Duration(1e9/rate) * time.Nanosecond)
	src := rand.New(rand.NewPCG(0, 0))

	for {
		select {
		case <-ticker.C:
			// Check if flow count limit is reached
			if flowCount > 0 && atomic.LoadUint64(&flowCounter) >= uint64(flowCount) {
				logging.Logger.Info("Flow count limit reached, stopping flow generation")
				cancel() // Stop generating new flows
				continue
			}
			select {
			case sem <- struct{}{}:
				// Increment flow counter atomically
				atomic.AddUint64(&flowCounter, 1)
				wg.Add(1) // Track this flow
				go func() {
					defer func() { <-sem }()
					pp := availablePorts[src.IntN(len(availablePorts))]
					var duration float64
					if constantFlows {
						duration = float64(maxConcurrent) / rate
						if duration < minDuration {
							logging.Logger.Warnf("Duration %f less than min_duration %f; adjusting max_concurrent may be required", duration, minDuration)
						}
					} else {
						duration = minDuration + src.Float64()*(maxDuration-minDuration)
					}
					generateFlow(mainCtx, server, pp, duration, src, mtu, mss, &wg)
				}()
			default:
				logging.Logger.Debugf("Max concurrent flows (%d) reached, skipping flow generation", maxConcurrent)
			}
		case <-mainCtx.Done():
			ticker.Stop()
			logging.Logger.Info("Flow generation stopped, waiting for active flows to complete")
			wg.Wait() // Wait for all active flows to finish
			logging.Logger.Info("All flows completed")
			mc.LogMetrics(cfg.LogFormat) // Log metrics after flows complete
			return
		}
	}
}
