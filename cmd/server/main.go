package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/PhilipSchmid/flow-generator-app/pkg/config"
	"github.com/PhilipSchmid/flow-generator-app/pkg/logging"
	"github.com/PhilipSchmid/flow-generator-app/pkg/metrics"
	"github.com/PhilipSchmid/flow-generator-app/pkg/tracing"

	"github.com/spf13/pflag"
)

// Global variables
var cfg *config.ServerConfig
var mc *metrics.MetricsCollector

// handleTCP processes incoming TCP connections
func handleTCP(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	mc.ActiveTCPConnections.Inc()
	defer mc.ActiveTCPConnections.Dec()

	port := conn.LocalAddr().(*net.TCPAddr).Port
	portStr := strconv.Itoa(port)
	protocol := "tcp"

	mc.IncRequestsReceived(protocol, portStr)
	mc.TCPConnectionsOpenedPerSecond.Inc()

	logging.Logger.Debugf("Accepted TCP connection on %s from %s", conn.LocalAddr().String(), conn.RemoteAddr().String())
	buf := make([]byte, 1024)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			logging.Logger.Debugf("TCP connection from %s closed: %v", conn.RemoteAddr().String(), err)
			return
		}
		mc.AddBytesReceived(protocol, portStr, n)

		n, err = conn.Write(buf[:n])
		if err != nil {
			logging.Logger.Debugf("Failed to write to TCP connection from %s: %v", conn.RemoteAddr().String(), err)
			return
		}
		mc.AddBytesSent(protocol, portStr, n)
	}
}

// handleUDP processes incoming UDP packets
func handleUDP(conn *net.UDPConn) {
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

		mc.IncRequestsReceived(protocol, portStr)
		mc.UDPPacketsReceived.Inc()
		mc.AddBytesReceived(protocol, portStr, n)

		logging.Logger.Debugf("Received UDP packet from %s", addr.String())

		n, err = conn.WriteToUDP(buf[:n], addr)
		if err != nil {
			logging.Logger.Debugf("Failed to write UDP packet to %s: %v", addr.String(), err)
			continue
		}
		mc.AddBytesSent(protocol, portStr, n)
	}
}

// startServer starts a server for the specified network and address
func startServer(ctx context.Context, wg *sync.WaitGroup, network, address string) {
	defer wg.Done()
	if network == "tcp" || network == "tcp6" {
		listener, err := net.Listen(network, address)
		if err != nil {
			logging.Logger.Errorf("Failed to listen on %s:%s: %v", network, address, err)
			return
		}
		logging.Logger.Infof("Listening on %s:%s", network, address)
		go func() {
			<-ctx.Done()
			defer func() { _ = listener.Close() }()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				conn, err := listener.Accept()
				if err != nil {
					continue
				}
				go handleTCP(conn)
			}
		}
	} else if network == "udp" || network == "udp6" {
		addr, err := net.ResolveUDPAddr(network, address)
		if err != nil {
			logging.Logger.Errorf("Failed to resolve %s address %s: %v", network, address, err)
			return
		}
		conn, err := net.ListenUDP(network, addr)
		if err != nil {
			logging.Logger.Errorf("Failed to listen on %s:%s: %v", network, address, err)
			return
		}
		logging.Logger.Infof("Listening on %s:%s", network, address)
		go func() {
			<-ctx.Done()
			defer func() { _ = conn.Close() }()
		}()
		handleUDP(conn)
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
	pflag.String("tcp_ports_server", "", "Comma-separated list of TCP ports")
	pflag.String("udp_ports_server", "", "Comma-separated list of UDP ports")

	// Parse flags
	pflag.Parse()

	// Load configuration
	cfg = config.LoadServerConfig()

	// Initialize logger
	logging.InitLogger(cfg.LogFormat, cfg.LogLevel)
	defer func() {
		if err := logging.Logger.Sync(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to sync logger: %v\n", err)
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

	// Start metrics server
	metrics.StartMetricsServer(cfg.MetricsPort)

	// Initialize tracing if enabled
	if cfg.TracingEnabled {
		tracing.InitTracer("echo-server", cfg.JaegerEndpoint)
	}

	// Parse ports
	tcpPorts := parsePorts(cfg.TCPPortsServer)
	udpPorts := parsePorts(cfg.UDPPortsServer)

	if len(tcpPorts) == 0 && len(udpPorts) == 0 {
		logging.Logger.Fatal("No valid TCP or UDP ports specified")
	}

	// Start servers with context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	for _, port := range tcpPorts {
		address := fmt.Sprintf(":%d", port)
		wg.Add(1)
		go startServer(ctx, &wg, "tcp", address)
	}

	for _, port := range udpPorts {
		address := fmt.Sprintf(":%d", port)
		wg.Add(1)
		go startServer(ctx, &wg, "udp", address)
	}

	wg.Wait()
}
