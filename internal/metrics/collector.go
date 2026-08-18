package metrics

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/olekukonko/tablewriter"
	"github.com/prometheus/client_golang/prometheus"
)

// MetricsCollector manages Prometheus and local metrics.
type MetricsCollector struct {
	// Prometheus metrics for real-time monitoring
	RequestsReceived              *prometheus.CounterVec
	RequestsSent                  *prometheus.CounterVec
	BytesReceived                 *prometheus.CounterVec
	BytesSent                     *prometheus.CounterVec
	TCPConnectionsOpenedPerSecond prometheus.Counter
	UDPPacketsReceived            prometheus.Counter
	ActiveTCPConnections          prometheus.Gauge

	// Local counters for termination output
	totalRequestsReceived uint64
	totalRequestsSent     uint64
	requestsReceived      sync.Map
	requestsSent          sync.Map
	bytesReceived         sync.Map
	bytesSent             sync.Map
	totalTCPSent          uint64
	totalTCPReceived      uint64
	totalUDPReceived      uint64
	totalUDPSent          uint64
	activeTCPConnections  atomic.Int64
}

// PortSnapshot contains cumulative application traffic for one protocol/port.
type PortSnapshot struct {
	Protocol         string `json:"protocol"`
	Port             string `json:"port"`
	RequestsReceived uint64 `json:"requests_received"`
	RequestsSent     uint64 `json:"requests_sent"`
	BytesReceived    uint64 `json:"bytes_received"`
	BytesSent        uint64 `json:"bytes_sent"`
}

// Snapshot is an immutable, process-local view of application traffic.
type Snapshot struct {
	TotalRequestsReceived uint64         `json:"total_requests_received"`
	TotalRequestsSent     uint64         `json:"total_requests_sent"`
	TotalTCPReceived      uint64         `json:"total_tcp_received"`
	TotalTCPSent          uint64         `json:"total_tcp_sent"`
	TotalUDPReceived      uint64         `json:"total_udp_received"`
	TotalUDPSent          uint64         `json:"total_udp_sent"`
	ActiveTCPConnections  int64          `json:"active_tcp_connections"`
	Ports                 []PortSnapshot `json:"ports"`
}

type prometheusMetrics struct {
	requestsReceived     *prometheus.CounterVec
	requestsSent         *prometheus.CounterVec
	bytesReceived        *prometheus.CounterVec
	bytesSent            *prometheus.CounterVec
	tcpConnectionsOpened prometheus.Counter
	udpPacketsReceived   prometheus.Counter
	activeTCPConnections prometheus.Gauge
}

var (
	defaultMetrics     prometheusMetrics
	defaultMetricsOnce sync.Once
)

func newPrometheusMetrics() prometheusMetrics {
	return prometheusMetrics{
		requestsReceived: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "requests_received_total", Help: "Total requests received"},
			[]string{"protocol", "port"},
		),
		requestsSent: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "requests_sent_total", Help: "Total requests sent"},
			[]string{"protocol", "port"},
		),
		bytesReceived: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "bytes_received_total", Help: "Total bytes received"},
			[]string{"protocol", "port"},
		),
		bytesSent: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "bytes_sent_total", Help: "Total bytes sent"},
			[]string{"protocol", "port"},
		),
		tcpConnectionsOpened: prometheus.NewCounter(
			prometheus.CounterOpts{Name: "tcp_connections_opened_total", Help: "Total TCP connections opened"},
		),
		udpPacketsReceived: prometheus.NewCounter(
			prometheus.CounterOpts{Name: "udp_packets_received_total", Help: "Total UDP packets received"},
		),
		activeTCPConnections: prometheus.NewGauge(
			prometheus.GaugeOpts{Name: "active_tcp_connections", Help: "Current active TCP connections"},
		),
	}
}

// NewMetricsCollector initializes the collector and registers Prometheus metrics.
func NewMetricsCollector() *MetricsCollector {
	defaultMetricsOnce.Do(func() {
		defaultMetrics = newPrometheusMetrics()
		prometheus.MustRegister(
			defaultMetrics.requestsReceived,
			defaultMetrics.requestsSent,
			defaultMetrics.bytesReceived,
			defaultMetrics.bytesSent,
			defaultMetrics.tcpConnectionsOpened,
			defaultMetrics.udpPacketsReceived,
			defaultMetrics.activeTCPConnections,
		)
	})

	return &MetricsCollector{
		RequestsReceived:              defaultMetrics.requestsReceived,
		RequestsSent:                  defaultMetrics.requestsSent,
		BytesReceived:                 defaultMetrics.bytesReceived,
		BytesSent:                     defaultMetrics.bytesSent,
		TCPConnectionsOpenedPerSecond: defaultMetrics.tcpConnectionsOpened,
		UDPPacketsReceived:            defaultMetrics.udpPacketsReceived,
		ActiveTCPConnections:          defaultMetrics.activeTCPConnections,
	}
}

// IncRequestsReceived increments requests received counters.
func (mc *MetricsCollector) IncRequestsReceived(protocol, port string) {
	mc.RequestsReceived.WithLabelValues(protocol, port).Inc()
	atomic.AddUint64(&mc.totalRequestsReceived, 1)
	switch protocol {
	case "tcp":
		atomic.AddUint64(&mc.totalTCPReceived, 1)
	case "udp":
		atomic.AddUint64(&mc.totalUDPReceived, 1)
	}
	mc.updateSyncMap(&mc.requestsReceived, protocol, port, 1)
}

// IncRequestsSent increments requests sent counters.
func (mc *MetricsCollector) IncRequestsSent(protocol, port string) {
	mc.RequestsSent.WithLabelValues(protocol, port).Inc()
	atomic.AddUint64(&mc.totalRequestsSent, 1)
	switch protocol {
	case "tcp":
		atomic.AddUint64(&mc.totalTCPSent, 1)
	case "udp":
		atomic.AddUint64(&mc.totalUDPSent, 1)
	}
	mc.updateSyncMap(&mc.requestsSent, protocol, port, 1)
}

// AddBytesReceived adds bytes to received counters.
func (mc *MetricsCollector) AddBytesReceived(protocol, port string, n int) {
	if n < 0 {
		return
	}
	mc.BytesReceived.WithLabelValues(protocol, port).Add(float64(n))
	mc.updateSyncMap(&mc.bytesReceived, protocol, port, uint64(n))
}

// AddBytesSent adds bytes to sent counters.
func (mc *MetricsCollector) AddBytesSent(protocol, port string, n int) {
	if n < 0 {
		return
	}
	mc.BytesSent.WithLabelValues(protocol, port).Add(float64(n))
	mc.updateSyncMap(&mc.bytesSent, protocol, port, uint64(n))
}

// IncTCPConnectionsOpened increments the TCP connections opened counter.
func (mc *MetricsCollector) IncTCPConnectionsOpened() {
	mc.TCPConnectionsOpenedPerSecond.Inc()
}

// IncUDPPacketsReceived increments the UDP packets received counter.
func (mc *MetricsCollector) IncUDPPacketsReceived() {
	mc.UDPPacketsReceived.Inc()
}

// TotalRequestsReceived returns the process-local receive count.
func (mc *MetricsCollector) TotalRequestsReceived() uint64 {
	return atomic.LoadUint64(&mc.totalRequestsReceived)
}

// IncActiveTCPConnections increments the Prometheus and process-local gauges.
func (mc *MetricsCollector) IncActiveTCPConnections() {
	mc.ActiveTCPConnections.Inc()
	mc.activeTCPConnections.Add(1)
}

// DecActiveTCPConnections decrements the Prometheus and process-local gauges.
func (mc *MetricsCollector) DecActiveTCPConnections() {
	mc.ActiveTCPConnections.Dec()
	mc.activeTCPConnections.Add(-1)
}

// SetActiveTCPConnections updates the Prometheus and process-local gauges.
func (mc *MetricsCollector) SetActiveTCPConnections(n int) {
	mc.ActiveTCPConnections.Set(float64(n))
	mc.activeTCPConnections.Store(int64(n))
}

// Snapshot returns cumulative traffic without reading from Prometheus.
// It allocates only for the low-frequency status request path.
func (mc *MetricsCollector) Snapshot() Snapshot {
	requestsReceived := mc.getSyncMapData(&mc.requestsReceived)
	requestsSent := mc.getSyncMapData(&mc.requestsSent)
	bytesReceived := mc.getSyncMapData(&mc.bytesReceived)
	bytesSent := mc.getSyncMapData(&mc.bytesSent)

	type portKey struct {
		protocol string
		port     string
	}
	ports := make(map[portKey]*PortSnapshot)
	add := func(data map[string]map[string]uint64, assign func(*PortSnapshot, uint64)) {
		for protocol, protocolPorts := range data {
			for port, value := range protocolPorts {
				key := portKey{protocol: protocol, port: port}
				entry := ports[key]
				if entry == nil {
					entry = &PortSnapshot{Protocol: protocol, Port: port}
					ports[key] = entry
				}
				assign(entry, value)
			}
		}
	}
	add(requestsReceived, func(entry *PortSnapshot, value uint64) { entry.RequestsReceived = value })
	add(requestsSent, func(entry *PortSnapshot, value uint64) { entry.RequestsSent = value })
	add(bytesReceived, func(entry *PortSnapshot, value uint64) { entry.BytesReceived = value })
	add(bytesSent, func(entry *PortSnapshot, value uint64) { entry.BytesSent = value })

	portSnapshots := make([]PortSnapshot, 0, len(ports))
	for _, entry := range ports {
		portSnapshots = append(portSnapshots, *entry)
	}
	sort.Slice(portSnapshots, func(i, j int) bool {
		if portSnapshots[i].Protocol != portSnapshots[j].Protocol {
			return portSnapshots[i].Protocol < portSnapshots[j].Protocol
		}
		left, leftErr := strconv.Atoi(portSnapshots[i].Port)
		right, rightErr := strconv.Atoi(portSnapshots[j].Port)
		if leftErr == nil && rightErr == nil {
			return left < right
		}
		return portSnapshots[i].Port < portSnapshots[j].Port
	})

	return Snapshot{
		TotalRequestsReceived: atomic.LoadUint64(&mc.totalRequestsReceived),
		TotalRequestsSent:     atomic.LoadUint64(&mc.totalRequestsSent),
		TotalTCPReceived:      atomic.LoadUint64(&mc.totalTCPReceived),
		TotalTCPSent:          atomic.LoadUint64(&mc.totalTCPSent),
		TotalUDPReceived:      atomic.LoadUint64(&mc.totalUDPReceived),
		TotalUDPSent:          atomic.LoadUint64(&mc.totalUDPSent),
		ActiveTCPConnections:  mc.activeTCPConnections.Load(),
		Ports:                 portSnapshots,
	}
}

// updateSyncMap updates a sync.Map with protocol/port counts using pointers.
func (mc *MetricsCollector) updateSyncMap(m *sync.Map, protocol, port string, delta uint64) {
	portsValue, ok := m.Load(protocol)
	if !ok {
		portsValue, _ = m.LoadOrStore(protocol, &sync.Map{})
	}
	portsMap := portsValue.(*sync.Map)
	counterValue, ok := portsMap.Load(port)
	if !ok {
		counterValue, _ = portsMap.LoadOrStore(port, &atomic.Uint64{})
	}
	counter := counterValue.(*atomic.Uint64)
	counter.Add(delta)
}

// LogMetrics prints all metrics in the specified format upon termination.
func (mc *MetricsCollector) LogMetrics(logFormat string) {
	if logFormat == "human" {
		// Total Metrics Table
		table := tablewriter.NewWriter(os.Stdout)
		table.Header("Metric", "Value")
		_ = table.Append("Total Requests Received", fmt.Sprintf("%d", atomic.LoadUint64(&mc.totalRequestsReceived)))
		_ = table.Append("Total Requests Sent", fmt.Sprintf("%d", atomic.LoadUint64(&mc.totalRequestsSent)))
		_ = table.Append("Total TCP Requests Received", fmt.Sprintf("%d", atomic.LoadUint64(&mc.totalTCPReceived)))
		_ = table.Append("Total TCP Requests Sent", fmt.Sprintf("%d", atomic.LoadUint64(&mc.totalTCPSent)))
		_ = table.Append("Total UDP Requests Received", fmt.Sprintf("%d", atomic.LoadUint64(&mc.totalUDPReceived)))
		_ = table.Append("Total UDP Requests Sent", fmt.Sprintf("%d", atomic.LoadUint64(&mc.totalUDPSent)))
		fmt.Println("Total Metrics:")
		_ = table.Render()

		// Per-Protocol/Port Metrics
		requestsReceived := mc.getSyncMapData(&mc.requestsReceived)
		if len(requestsReceived) > 0 {
			printTable("Requests Received Per-protocol/port:", []string{"Protocol", "Port", "Requests Received"}, requestsReceived, false)
		}

		requestsSent := mc.getSyncMapData(&mc.requestsSent)
		if len(requestsSent) > 0 {
			printTable("Requests Sent Per-protocol/port:", []string{"Protocol", "Port", "Requests Sent"}, requestsSent, false)
		}

		bytesReceived := mc.getSyncMapData(&mc.bytesReceived)
		if len(bytesReceived) > 0 {
			printTable("Bytes Received Per-protocol/port:", []string{"Protocol", "Port", "Bytes Received"}, bytesReceived, false)
		}

		bytesSent := mc.getSyncMapData(&mc.bytesSent)
		if len(bytesSent) > 0 {
			printTable("Bytes Sent Per-protocol/port:", []string{"Protocol", "Port", "Bytes Sent"}, bytesSent, false)
		}
	} else {
		// JSON output for non-human formats
		metricsData := map[string]interface{}{
			"total_requests_received": atomic.LoadUint64(&mc.totalRequestsReceived),
			"total_requests_sent":     atomic.LoadUint64(&mc.totalRequestsSent),
			"total_tcp_received":      atomic.LoadUint64(&mc.totalTCPReceived),
			"total_tcp_sent":          atomic.LoadUint64(&mc.totalTCPSent),
			"total_udp_received":      atomic.LoadUint64(&mc.totalUDPReceived),
			"total_udp_sent":          atomic.LoadUint64(&mc.totalUDPSent),
			"requests_received":       mc.getSyncMapData(&mc.requestsReceived),
			"requests_sent":           mc.getSyncMapData(&mc.requestsSent),
			"bytes_received":          mc.getSyncMapData(&mc.bytesReceived),
			"bytes_sent":              mc.getSyncMapData(&mc.bytesSent),
		}
		jsonData, _ := json.MarshalIndent(metricsData, "", "  ")
		logging.Logger.Infof("Application terminated. Metrics:\n%s", string(jsonData))
	}
}

// printTable prints a sorted table for a given metrics category
func printTable(title string, headers []string, data map[string]map[string]uint64, supportsColor bool) {
	table := tablewriter.NewWriter(os.Stdout)
	table.Header(headers[0], headers[1], headers[2])
	// Sort protocols alphabetically
	var protocols []string
	for protocol := range data {
		protocols = append(protocols, protocol)
	}
	sort.Strings(protocols)
	for _, protocol := range protocols {
		portsMap := data[protocol]
		// Sort ports numerically
		var ports []string
		for port := range portsMap {
			ports = append(ports, port)
		}
		sort.Slice(ports, func(i, j int) bool {
			pi, _ := strconv.Atoi(ports[i])
			pj, _ := strconv.Atoi(ports[j])
			return pi < pj
		})
		for _, port := range ports {
			count := portsMap[port]
			_ = table.Append(protocol, port, fmt.Sprintf("%d", count))
		}
	}
	fmt.Println(title)
	_ = table.Render()
}

// getSyncMapData converts sync.Map to a nested map for JSON output.
func (mc *MetricsCollector) getSyncMapData(m *sync.Map) map[string]map[string]uint64 {
	result := make(map[string]map[string]uint64)
	m.Range(func(key, value interface{}) bool {
		protocol := key.(string)
		portsMap := value.(*sync.Map)
		portsData := make(map[string]uint64)
		portsMap.Range(func(port, counter interface{}) bool {
			portsData[port.(string)] = counter.(*atomic.Uint64).Load()
			return true
		})
		result[protocol] = portsData
		return true
	})
	return result
}

// FlushMetrics logs the final metrics in human-readable format
func (mc *MetricsCollector) FlushMetrics() {
	mc.LogMetrics("human")
}
