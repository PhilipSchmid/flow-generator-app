package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/config"
	"github.com/PhilipSchmid/flow-generator-app/internal/handlers"
	"github.com/PhilipSchmid/flow-generator-app/internal/health"
	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/PhilipSchmid/flow-generator-app/internal/metrics"
	"github.com/PhilipSchmid/flow-generator-app/internal/server"
	statusapi "github.com/PhilipSchmid/flow-generator-app/internal/status"
	"github.com/PhilipSchmid/flow-generator-app/internal/tracing"
	"github.com/PhilipSchmid/flow-generator-app/internal/version"

	"github.com/spf13/pflag"
)

const progressLogInterval = 30 * time.Second

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
	pflag.String("health-port", "", "Port for the health check server")
	pflag.Bool("tracing-enabled", false, "Enable tracing")
	pflag.String("jaeger-endpoint", "", "Jaeger endpoint")
	pflag.String("tcp-ports-server", "", "Comma-separated list of TCP ports")
	pflag.String("udp-ports-server", "", "Comma-separated list of UDP ports")

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
	statusTracker := &statusapi.ServerTracker{}

	// Initialize tracing if enabled
	if cfg.TracingEnabled {
		tracerProvider, traceErr := tracing.InitTracer(context.Background(), "echo-server", cfg.JaegerEndpoint)
		if traceErr != nil {
			logging.Logger.Fatalf("Failed to initialize tracing: %v", traceErr)
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := tracing.Shutdown(ctx, tracerProvider); err != nil {
				logging.Logger.Errorf("Failed to flush tracing: %v", err)
			}
		}()
	}

	// Start metrics server
	metricsServer, err := metrics.StartMetricsServer(cfg.MetricsPort)
	if err != nil {
		logging.Logger.Fatalf("Failed to start metrics server: %v", err)
	}

	// Start health check server
	healthChecker := health.NewChecker()
	if err := healthChecker.Start(cfg.HealthPort); err != nil {
		logging.Logger.Fatalf("Failed to start health check server: %v", err)
	}

	// Create server manager
	manager := server.NewManager()

	// Create handlers
	tcpHandler := handlers.NewTCPHandlerWithStatus(mc, statusTracker)
	udpHandler := handlers.NewUDPHandlerWithStatus(mc, statusTracker)

	// Parse and create TCP servers
	tcpPorts := parsePorts(cfg.TCPPortsServer)
	for _, port := range tcpPorts {
		tcpServer := server.NewTCPServerWithStatus(port, tcpHandler, statusTracker)
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

	statusServer, err := statusapi.Start(cfg.StatusPort, func() statusapi.Snapshot {
		state := "not_ready"
		if healthChecker.Ready() {
			state = "ready"
		}
		return statusapi.Snapshot{
			SchemaVersion: statusapi.SchemaVersion,
			Role:          "server",
			Version:       version.Short(),
			SampledAt:     time.Now().UTC(),
			StartedAt:     startedAt,
			State:         state,
			Configuration: statusapi.Configuration{
				TCPPorts: tcpPorts, UDPPorts: udpPorts,
				HealthPort: cfg.HealthPort, MetricsPort: cfg.MetricsPort,
				TracingEnabled: cfg.TracingEnabled,
			},
			Traffic: mc.Snapshot(),
			Server: &statusapi.ServerSnapshot{
				Ready: healthChecker.Ready(), Healthy: healthChecker.Healthy(),
				ActiveTCPClients: statusTracker.ActiveTCPClients(), Errors: statusTracker.Errors(),
			},
		}
	})
	if err != nil {
		logging.Logger.Fatalf("Failed to start local dashboard status: %v", err)
	}

	// Handle termination signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	progressTicker := time.NewTicker(progressLogInterval)
	defer progressTicker.Stop()

	// Wait for termination while emitting a low-frequency aggregate heartbeat.
	var sig os.Signal
	for sig == nil {
		select {
		case sig = <-sigChan:
		case <-progressTicker.C:
			logging.Logger.Infow("Echo server progress",
				"requests_received", mc.TotalRequestsReceived(),
			)
		}
	}
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
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := metricsServer.Stop(shutdownCtx); err != nil {
		logging.Logger.Errorf("Error stopping metrics server: %v", err)
	}
	if err := statusServer.Stop(shutdownCtx); err != nil {
		logging.Logger.Errorf("Error stopping local dashboard status: %v", err)
	}

	// Flush metrics
	mc.FlushMetrics()

	logging.Logger.Info("Echo server shutdown complete")
}
