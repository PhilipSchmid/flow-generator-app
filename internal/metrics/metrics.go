package metrics

import (
	"net/http"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	TCPConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "tcp_connections_active",
		Help: "Number of active TCP connections",
	})
	UDPPackets = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "udp_packets_received_total",
		Help: "Total number of UDP packets received",
	})
	FlowsReceived = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "flows_received_total",
		Help: "Total number of flows received",
	})
)

func InitMetrics() {
	prometheus.MustRegister(TCPConnections, UDPPackets, FlowsReceived)
}

func StartMetricsServer(port string) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			if logging.Logger != nil {
				logging.Logger.Errorf("Failed to start metrics server: %v", err)
			}
		}
	}()
	return nil
}
