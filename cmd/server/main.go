package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/PhilipSchmid/flow-generator-app/internal/config"
	"github.com/PhilipSchmid/flow-generator-app/internal/handlers"
	"github.com/PhilipSchmid/flow-generator-app/internal/health"
	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/PhilipSchmid/flow-generator-app/internal/metrics"
	"github.com/PhilipSchmid/flow-generator-app/internal/server"
	"github.com/PhilipSchmid/flow-generator-app/internal/tracing"
	"github.com/PhilipSchmid/flow-generator-app/internal/version"

	"github.com/spf13/pflag"
)

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
	pflag.String("health_port", "", "Port for the health check server")
	pflag.Bool("tracing_enabled", false, "Enable tracing")
	pflag.String("jaeger_endpoint", "", "Jaeger endpoint")
	pflag.String("tcp_ports_server", "", "Comma-separated list of TCP ports")
	pflag.String("udp_ports_server", "", "Comma-separated list of UDP ports")

	// Parse flags
	pflag.Parse()

	// Handle version flag
	if *versionFlag {
		fmt.Println("Echo Server")
		fmt.Println(version.Info())
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.LoadServerConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	logging.InitLogger(cfg.LogFormat, cfg.LogLevel)
	defer func() {
		if err := logging.SyncLogger(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to sync logger: %v\n", err)
		}
	}()

	// Initialize MetricsCollector
	mc := metrics.NewMetricsCollector()

	// Initialize tracing if enabled
	if cfg.TracingEnabled {
		tracing.InitTracer("echo-server", cfg.JaegerEndpoint)
		logging.Logger.Info("Tracing enabled")
	}

	// Start metrics server
	go func() {
		if err := metrics.StartMetricsServer(cfg.MetricsPort); err != nil && err != http.ErrServerClosed {
			logging.Logger.Warnf("Metrics server error: %v", err)
		}
	}()

	// Start health check server
	healthChecker := health.NewChecker()
	if err := healthChecker.Start(cfg.HealthPort); err != nil {
		logging.Logger.Fatalf("Failed to start health check server: %v", err)
	}

	// Create server manager
	manager := server.NewManager()

	// Create handlers
	tcpHandler := handlers.NewTCPHandler(mc)
	udpHandler := handlers.NewUDPHandler(mc)

	// Parse and create TCP servers
	tcpPorts := parsePorts(cfg.TCPPortsServer)
	for _, port := range tcpPorts {
		tcpServer := server.NewTCPServer(port, tcpHandler)
		manager.AddServer(tcpServer)
	}

	// Parse and create UDP servers
	udpPorts := parsePorts(cfg.UDPPortsServer)
	for _, port := range udpPorts {
		udpServer := server.NewUDPServer(port, udpHandler)
		manager.AddServer(udpServer)
	}

	// Start all servers
	if err := manager.Start(); err != nil {
		logging.Logger.Fatalf("Failed to start servers: %v", err)
	}

	// Mark service as ready after all servers are started
	healthChecker.SetReady(true)
	logging.Logger.Info("Echo server is ready")

	// Handle termination signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Wait for termination signal
	sig := <-sigChan
	logging.Logger.Infof("Received signal: %v. Shutting down...", sig)

	// Mark service as not ready during shutdown
	healthChecker.SetReady(false)

	// Stop all servers
	if err := manager.Stop(); err != nil {
		logging.Logger.Errorf("Error stopping servers: %v", err)
	}

	// Stop health check server
	if err := healthChecker.Stop(); err != nil {
		logging.Logger.Errorf("Error stopping health check server: %v", err)
	}

	// Flush metrics
	mc.FlushMetrics()

	logging.Logger.Info("Echo server shutdown complete")
}
