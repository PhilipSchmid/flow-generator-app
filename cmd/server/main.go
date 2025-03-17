package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/PhilipSchmid/flow-generator-app/pkg/config"
	"github.com/PhilipSchmid/flow-generator-app/pkg/logging"
	"github.com/PhilipSchmid/flow-generator-app/pkg/metrics"
	"github.com/PhilipSchmid/flow-generator-app/pkg/tracing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func handleTCP(conn net.Conn) {
	defer conn.Close()
	metrics.TCPConnections.Inc()
	defer metrics.TCPConnections.Dec()
	buf := make([]byte, 1024)
	metrics.FlowsReceived.Inc()
	logging.Logger.Debugf("Accepted TCP connection on %s from %s", conn.LocalAddr().String(), conn.RemoteAddr().String())
	for {
		n, err := conn.Read(buf)
		if err != nil {
			logging.Logger.Debugf("TCP connection from %s closed: %v", conn.RemoteAddr().String(), err)
			return
		}
		_, err = conn.Write(buf[:n])
		if err != nil {
			logging.Logger.Debugf("Failed to write to TCP connection from %s: %v", conn.RemoteAddr().String(), err)
			return
		}
	}
}

func handleUDP(conn *net.UDPConn) {
	buf := make([]byte, 1024)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			logging.Logger.Infof("UDP connection closed: %v", err)
			return
		}
		metrics.FlowsReceived.Inc()
		logging.Logger.Debugf("Received UDP packet from %s", addr.String())
		metrics.UDPPackets.Inc()
		_, err = conn.WriteToUDP(buf[:n], addr)
		if err != nil {
			logging.Logger.Debugf("Failed to write UDP packet to %s: %v", addr.String(), err)
			continue
		}
	}
}

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
			listener.Close()
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
			conn.Close()
		}()
		handleUDP(conn)
	}
}

// parsePorts converts a comma-separated string of ports into a slice of integers
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
	// Define command-line flags using pflag
	pflag.String("log_level", "info", "Log level: debug, info, warn, error")
	pflag.String("log_format", "human", "Log format: human or json")
	pflag.String("metrics_port", "9090", "Port for the metrics server")
	pflag.Bool("tracing_enabled", false, "Enable tracing")
	pflag.String("jaeger_endpoint", "http://localhost:14268/api/traces", "Jaeger endpoint for tracing")
	pflag.String("tcp_ports", "8080", "Comma-separated list of TCP ports to listen on")
	pflag.String("udp_ports", "", "Comma-separated list of UDP ports to listen on")

	// Bind pflag flags to Viper
	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		logging.Logger.Fatal("Failed to bind command-line flags: %v", err)
	}

	// Parse the command-line flags
	pflag.Parse()

	// Initialize configuration
	config.InitConfig()

	// Initialize logger
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
		tracing.InitTracer("echo-server", viper.GetString("jaeger_endpoint"))
	}

	// Parse configurable ports
	tcpPortsStr := viper.GetString("tcp_ports")
	udpPortsStr := viper.GetString("udp_ports")
	tcpPorts := parsePorts(tcpPortsStr)
	udpPorts := parsePorts(udpPortsStr)

	if len(tcpPorts) == 0 && len(udpPorts) == 0 {
		logging.Logger.Fatal("No valid TCP or UDP ports specified")
	}

	// Start servers with context for graceful shutdown
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
