package metrics

import (
	"net/http"

	"github.com/PhilipSchmid/flow-generator-app/pkg/logging"
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
	FlowsGenerated = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "flows_generated_total",
		Help: "Total number of flows generated",
	})
	FlowsReceived = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "flows_received_total",
		Help: "Total number of flows received",
	})
)

func InitMetrics() {
	prometheus.MustRegister(TCPConnections, UDPPackets, FlowsGenerated, FlowsReceived)
}

func StartMetricsServer(port string) {
	http.Handle("/metrics", promhttp.Handler())
	go func() {
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			logging.Logger.Fatalf("Failed to start metrics server: %v", err)
		}
	}()
}
