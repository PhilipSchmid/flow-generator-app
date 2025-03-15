package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/pkg/config"
	"github.com/PhilipSchmid/flow-generator-app/pkg/logging"
	"github.com/PhilipSchmid/flow-generator-app/pkg/metrics"
	"github.com/PhilipSchmid/flow-generator-app/pkg/tracing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type ProtocolPort struct {
	Protocol string
	Port     int
}

var payloadCache []byte

func init() {
	src := rand.New(rand.NewPCG(0, 0))
	payloadCache = make([]byte, 1<<20) // 1MB
	for i := range payloadCache {
		payloadCache[i] = byte(src.IntN(256)) // Fill with random bytes (0-255)
	}
}

func constructAddress(server string, port int) string {
	if ip := net.ParseIP(server); ip != nil {
		if ip.To4() == nil { // IPv6 address
			return fmt.Sprintf("[%s]:%d", server, port)
		}
	}
	// IPv4 address or domain name
	return fmt.Sprintf("%s:%d", server, port)
}

func getPayloadSize(src *rand.Rand) int {
	if size := viper.GetInt("payload_size"); size > 0 {
		return size // Fixed size
	}
	minSize := viper.GetInt("min_payload_size")
	maxSize := viper.GetInt("max_payload_size")
	if minSize > 0 && maxSize > minSize {
		return minSize + src.IntN(maxSize-minSize+1) // Use src.IntN
	}
	return 5 // Default to 5 bytes
}

func generateFlow(ctx context.Context, server string, pp ProtocolPort, duration float64, src *rand.Rand, mtu int, mss int) {
	payloadSize := getPayloadSize(src)
	if payloadSize > len(payloadCache) {
		payloadSize = len(payloadCache) // Cap at cache size
	}
	payload := payloadCache[:payloadSize] // Slice the cache

	logging.Logger.Debugf("Starting %s flow for %f seconds to %s on port %d with payload size %d bytes", pp.Protocol, duration, server, pp.Port, payloadSize)

	addr := constructAddress(server, pp.Port)
	if pp.Protocol == "tcp" {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			logging.Logger.Warnf("Failed to connect to %s:%d (TCP): %v", server, pp.Port, err)
			return
		}
		defer conn.Close()

		// Log if TCP payload exceeds MSS (segmentation will occur)
		if len(payload) > mss {
			logging.Logger.Debugf("TCP payload size %d exceeds MSS %d, will be segmented", len(payload), mss)
		}

		if _, err := conn.Write(payload); err != nil {
			logging.Logger.Warnf("Failed to write to TCP connection: %v", err)
			return
		}
		select {
		case <-time.After(time.Duration(duration * float64(time.Second))):
			logging.Logger.Debugf("TCP flow to %s:%d ended after %f seconds", server, pp.Port, duration)
		case <-ctx.Done():
			logging.Logger.Debugf("TCP flow to %s:%d canceled", server, pp.Port)
		}
	} else { // udp
		localAddr, _ := net.ResolveUDPAddr("udp", ":0")
		remoteAddr, _ := net.ResolveUDPAddr("udp", addr)
		conn, err := net.DialUDP("udp", localAddr, remoteAddr)
		if err != nil {
			logging.Logger.Warnf("Failed to connect to %s:%d (UDP): %v", server, pp.Port, err)
			return
		}
		defer conn.Close()

		startTime := time.Now()
		for time.Since(startTime) < time.Duration(duration*float64(time.Second)) {
			// Check if UDP payload exceeds MTU and skip if it does
			if len(payload) > mtu {
				logging.Logger.Warnf("UDP payload size %d exceeds MTU %d, skipping send", len(payload), mtu)
				continue
			}

			if _, err := conn.Write(payload); err != nil {
				logging.Logger.Warnf("Failed to write to UDP connection: %v", err)
				continue
			}
			select {
			case <-time.After(100 * time.Millisecond):
			case <-ctx.Done():
				logging.Logger.Debugf("UDP flow to %s:%d canceled", server, pp.Port)
				return
			}
		}
		logging.Logger.Debugf("UDP flow to %s:%d ended after %f seconds", server, pp.Port, duration)
	}
	metrics.FlowsGenerated.Inc()
}

func main() {
	// Define command-line flags using pflag
	pflag.String("server", "localhost", "Server address or hostname")
	pflag.Float64("rate", 10.0, "Flow generation rate in flows per second")
	pflag.Int("max_concurrent", 100, "Maximum number of concurrent flows")
	pflag.String("protocol", "both", "Protocol to use (tcp, udp, both)")
	pflag.Float64("min_duration", 1.0, "Minimum flow duration in seconds")
	pflag.Float64("max_duration", 10.0, "Maximum flow duration in seconds")
	pflag.Int("payload_size", 0, "Fixed payload size in bytes (overrides min_payload_size/max_payload_size)")
	pflag.Int("min_payload_size", 0, "Minimum payload size in bytes for dynamic range")
	pflag.Int("max_payload_size", 0, "Maximum payload size in bytes for dynamic range")
	pflag.Int("mtu", 1500, "Maximum Transmission Unit in bytes")
	pflag.Int("mss", 1460, "Maximum Segment Size in bytes")
	pflag.String("log_level", "info", "Log level: debug, info, warn, error")
	pflag.String("log_format", "human", "Log format: human or json")
	pflag.String("metrics_port", "9090", "Port for the metrics server")
	pflag.Bool("tracing_enabled", false, "Enable tracing")
	pflag.String("jaeger_endpoint", "http://localhost:14268/api/traces", "Jaeger endpoint for tracing")

	// Bind pflag flags to Viper
	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		logging.Logger.Fatalf("Failed to bind command-line flags: %v", err)
	}

	// Parse the command-line flags
	pflag.Parse()

	// Initialize configuration (sets defaults and reads config file if present)
	config.InitConfig()

	// Retrieve log format, log level and initialize logger
	logFormat := viper.GetString("log_format")
	logLevel := viper.GetString("log_level")
	logging.InitLogger(logFormat, logLevel)
	defer func() {
		if err := logging.Logger.Sync(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to sync logger: %v\n", err)
		}
	}()

	// Initialize and start metrics server
	metrics.InitMetrics()
	metricsPort := viper.GetString("metrics_port")
	metrics.StartMetricsServer(metricsPort)

	// Initialize tracing if enabled
	if viper.GetBool("tracing_enabled") {
		tracing.InitTracer("flow-generator", viper.GetString("jaeger_endpoint"))
	}

	// Retrieve configuration values
	server := viper.GetString("server")
	rate := viper.GetFloat64("rate")
	maxConcurrent := viper.GetInt("max_concurrent")
	protocol := viper.GetString("protocol")
	minDuration := viper.GetFloat64("min_duration")
	maxDuration := viper.GetFloat64("max_duration")
	mtu := viper.GetInt("mtu")
	mss := viper.GetInt("mss")

	tcpPorts := []int{80, 21, 25, 443, 8080, 8443}
	udpPorts := []int{53, 123, 69}
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
		logging.Logger.Error("No ports available for the selected protocol")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sem := make(chan struct{}, maxConcurrent)
	ticker := time.NewTicker(time.Duration(1e9/rate) * time.Nanosecond)
	src := rand.New(rand.NewPCG(0, 0)) // Define random source

	for {
		select {
		case <-ticker.C:
			select {
			case sem <- struct{}{}:
				go func() {
					defer func() { <-sem }()
					pp := availablePorts[src.IntN(len(availablePorts))]
					duration := minDuration + src.Float64()*(maxDuration-minDuration)
					generateFlow(ctx, server, pp, duration, src, mtu, mss)
				}()
			default:
				logging.Logger.Debugf("Max concurrent flows (%d) reached, skipping flow generation", maxConcurrent)
			}
		case <-ctx.Done():
			return
		}
	}
}
