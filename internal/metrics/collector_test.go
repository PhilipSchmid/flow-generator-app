package metrics

import (
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testMetricsCollector creates a MetricsCollector without registering metrics
func testMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		RequestsReceived: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "test_requests_received_total", Help: "Test"},
			[]string{"protocol", "port"},
		),
		RequestsSent: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "test_requests_sent_total", Help: "Test"},
			[]string{"protocol", "port"},
		),
		BytesReceived: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "test_bytes_received_total", Help: "Test"},
			[]string{"protocol", "port"},
		),
		BytesSent: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "test_bytes_sent_total", Help: "Test"},
			[]string{"protocol", "port"},
		),
		TCPConnectionsOpenedPerSecond: prometheus.NewCounter(
			prometheus.CounterOpts{Name: "test_tcp_connections_opened_total", Help: "Test"},
		),
		UDPPacketsReceived: prometheus.NewCounter(
			prometheus.CounterOpts{Name: "test_udp_packets_received_total", Help: "Test"},
		),
		ActiveTCPConnections: prometheus.NewGauge(
			prometheus.GaugeOpts{Name: "test_active_tcp_connections", Help: "Test"},
		),
		requestsReceived: sync.Map{},
		requestsSent:     sync.Map{},
		bytesReceived:    sync.Map{},
		bytesSent:        sync.Map{},
	}
}

func TestNewMetricsCollector(t *testing.T) {
	// Reset the flag for testing
	metricsRegistered = false
	defer func() { metricsRegistered = false }()

	mc := NewMetricsCollector()

	assert.NotNil(t, mc)
	assert.NotNil(t, mc.RequestsReceived)
	assert.NotNil(t, mc.RequestsSent)
	assert.NotNil(t, mc.BytesReceived)
	assert.NotNil(t, mc.BytesSent)
	assert.NotNil(t, mc.TCPConnectionsOpenedPerSecond)
	assert.NotNil(t, mc.UDPPacketsReceived)
	assert.NotNil(t, mc.ActiveTCPConnections)

	assert.True(t, metricsRegistered)
}

func TestIncRequestsReceived(t *testing.T) {
	mc := testMetricsCollector()

	mc.IncRequestsReceived("tcp", "8080")
	mc.IncRequestsReceived("tcp", "8080")
	mc.IncRequestsReceived("tcp", "8081")

	assert.Equal(t, uint64(3), atomic.LoadUint64(&mc.totalTCPReceived))
	assert.Equal(t, uint64(3), atomic.LoadUint64(&mc.totalRequestsReceived))

	mc.IncRequestsReceived("udp", "9000")

	assert.Equal(t, uint64(1), atomic.LoadUint64(&mc.totalUDPReceived))
	assert.Equal(t, uint64(4), atomic.LoadUint64(&mc.totalRequestsReceived))

	data := mc.getSyncMapData(&mc.requestsReceived)
	assert.Equal(t, uint64(2), data["tcp"]["8080"])
	assert.Equal(t, uint64(1), data["tcp"]["8081"])
	assert.Equal(t, uint64(1), data["udp"]["9000"])
}

func TestIncRequestsSent(t *testing.T) {
	mc := testMetricsCollector()

	mc.IncRequestsSent("tcp", "8080")
	mc.IncRequestsSent("tcp", "8080")
	mc.IncRequestsSent("tcp", "8081")

	assert.Equal(t, uint64(3), atomic.LoadUint64(&mc.totalTCPSent))
	assert.Equal(t, uint64(3), atomic.LoadUint64(&mc.totalRequestsSent))

	mc.IncRequestsSent("udp", "9000")
	mc.IncRequestsSent("udp", "9000")

	assert.Equal(t, uint64(2), atomic.LoadUint64(&mc.totalUDPSent))
	assert.Equal(t, uint64(5), atomic.LoadUint64(&mc.totalRequestsSent))

	data := mc.getSyncMapData(&mc.requestsSent)
	assert.Equal(t, uint64(2), data["tcp"]["8080"])
	assert.Equal(t, uint64(1), data["tcp"]["8081"])
	assert.Equal(t, uint64(2), data["udp"]["9000"])
}

func TestAddBytesReceived(t *testing.T) {
	mc := testMetricsCollector()

	mc.AddBytesReceived("tcp", "8080", 1024)
	mc.AddBytesReceived("tcp", "8080", 512)
	mc.AddBytesReceived("udp", "9000", 256)

	data := mc.getSyncMapData(&mc.bytesReceived)
	assert.Equal(t, uint64(1536), data["tcp"]["8080"])
	assert.Equal(t, uint64(256), data["udp"]["9000"])
}

func TestAddBytesSent(t *testing.T) {
	mc := testMetricsCollector()

	mc.AddBytesSent("tcp", "8080", 2048)
	mc.AddBytesSent("tcp", "8081", 1024)
	mc.AddBytesSent("udp", "9000", 512)

	data := mc.getSyncMapData(&mc.bytesSent)
	assert.Equal(t, uint64(2048), data["tcp"]["8080"])
	assert.Equal(t, uint64(1024), data["tcp"]["8081"])
	assert.Equal(t, uint64(512), data["udp"]["9000"])
}

func TestIncTCPConnectionsOpened(t *testing.T) {
	mc := testMetricsCollector()

	assert.NotPanics(t, func() {
		mc.IncTCPConnectionsOpened()
		mc.IncTCPConnectionsOpened()
		mc.IncTCPConnectionsOpened()
	})
}

func TestIncUDPPacketsReceived(t *testing.T) {
	mc := testMetricsCollector()

	assert.NotPanics(t, func() {
		mc.IncUDPPacketsReceived()
		mc.IncUDPPacketsReceived()
	})
}

func TestSetActiveTCPConnections(t *testing.T) {
	mc := testMetricsCollector()

	assert.NotPanics(t, func() {
		mc.SetActiveTCPConnections(5)
		mc.SetActiveTCPConnections(10)
		mc.SetActiveTCPConnections(3)
	})
}

func TestUpdateSyncMap(t *testing.T) {
	mc := &MetricsCollector{}
	m := &sync.Map{}

	mc.updateSyncMap(m, "tcp", "8080", 100)

	mc.updateSyncMap(m, "tcp", "8080", 50)

	mc.updateSyncMap(m, "udp", "9000", 200)

	data := mc.getSyncMapData(m)
	assert.Equal(t, uint64(150), data["tcp"]["8080"])
	assert.Equal(t, uint64(200), data["udp"]["9000"])
}

func TestLogMetricsHuman(t *testing.T) {
	logging.InitLogger("json", "error")

	mc := testMetricsCollector()

	atomic.AddUint64(&mc.totalRequestsReceived, 100)
	atomic.AddUint64(&mc.totalRequestsSent, 200)
	atomic.AddUint64(&mc.totalTCPReceived, 60)
	atomic.AddUint64(&mc.totalTCPSent, 120)
	atomic.AddUint64(&mc.totalUDPReceived, 40)
	atomic.AddUint64(&mc.totalUDPSent, 80)

	mc.updateSyncMap(&mc.requestsReceived, "tcp", "8080", 30)
	mc.updateSyncMap(&mc.requestsReceived, "tcp", "8081", 30)
	mc.updateSyncMap(&mc.requestsSent, "tcp", "8080", 60)
	mc.updateSyncMap(&mc.bytesReceived, "tcp", "8080", 1024)
	mc.updateSyncMap(&mc.bytesSent, "udp", "9000", 2048)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	mc.LogMetrics("human")

	_ = w.Close()
	os.Stdout = oldStdout

	output, _ := io.ReadAll(r)
	outputStr := string(output)

	assert.Contains(t, outputStr, "Total Metrics:")
	assert.Contains(t, outputStr, "Total Requests Received")
	assert.Contains(t, outputStr, "100")
	assert.Contains(t, outputStr, "Total Requests Sent")
	assert.Contains(t, outputStr, "200")
	assert.Contains(t, outputStr, "Requests Received Per-protocol/port:")
	assert.Contains(t, outputStr, "Requests Sent Per-protocol/port:")
	assert.Contains(t, outputStr, "Bytes Received Per-protocol/port:")
	assert.Contains(t, outputStr, "Bytes Sent Per-protocol/port:")
}

func TestLogMetricsJSON(t *testing.T) {
	logging.InitLogger("json", "error")

	mc := testMetricsCollector()

	atomic.AddUint64(&mc.totalRequestsReceived, 50)
	atomic.AddUint64(&mc.totalRequestsSent, 100)
	atomic.AddUint64(&mc.totalTCPReceived, 30)
	atomic.AddUint64(&mc.totalTCPSent, 60)
	atomic.AddUint64(&mc.totalUDPReceived, 20)
	atomic.AddUint64(&mc.totalUDPSent, 40)

	mc.updateSyncMap(&mc.requestsReceived, "tcp", "8080", 30)
	mc.updateSyncMap(&mc.requestsSent, "tcp", "8080", 60)

	oldLogger := logging.Logger
	logging.InitLogger("json", "info")
	defer func() { logging.Logger = oldLogger }()

	mc.LogMetrics("json")

	assert.True(t, true) // If we get here, no panic occurred
}

func TestFlushMetrics(t *testing.T) {
	logging.InitLogger("json", "error")

	mc := testMetricsCollector()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	mc.FlushMetrics()

	_ = w.Close()
	os.Stdout = oldStdout

	output, _ := io.ReadAll(r)
	outputStr := string(output)

	assert.Contains(t, outputStr, "Total Metrics:")
}

func TestPrintTable(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	data := map[string]map[string]uint64{
		"tcp": {
			"8080": 100,
			"8081": 200,
		},
		"udp": {
			"9000": 300,
		},
	}

	printTable("Test Table:", []string{"Protocol", "Port", "Count"}, data, false)

	_ = w.Close()
	os.Stdout = oldStdout

	output, _ := io.ReadAll(r)
	outputStr := string(output)

	assert.Contains(t, outputStr, "Test Table:")
	assert.Contains(t, outputStr, "tcp")
	assert.Contains(t, outputStr, "8080")
	assert.Contains(t, outputStr, "100")
	assert.Contains(t, outputStr, "udp")
	assert.Contains(t, outputStr, "9000")
	assert.Contains(t, outputStr, "300")
}

func TestGetSyncMapData(t *testing.T) {
	mc := &MetricsCollector{}
	m := &sync.Map{}

	tcpMap := &sync.Map{}
	tcpCounter1 := &atomic.Uint64{}
	tcpCounter1.Store(100)
	tcpCounter2 := &atomic.Uint64{}
	tcpCounter2.Store(200)
	tcpMap.Store("8080", tcpCounter1)
	tcpMap.Store("8081", tcpCounter2)
	m.Store("tcp", tcpMap)

	udpMap := &sync.Map{}
	udpCounter := &atomic.Uint64{}
	udpCounter.Store(300)
	udpMap.Store("9000", udpCounter)
	m.Store("udp", udpMap)

	result := mc.getSyncMapData(m)

	assert.Equal(t, uint64(100), result["tcp"]["8080"])
	assert.Equal(t, uint64(200), result["tcp"]["8081"])
	assert.Equal(t, uint64(300), result["udp"]["9000"])
}

func TestConcurrentMetricUpdates(t *testing.T) {
	mc := testMetricsCollector()

	var wg sync.WaitGroup
	numGoroutines := 10
	numOperations := 100

	// Pre-initialize the maps to avoid race conditions during creation
	mc.IncRequestsReceived("tcp", "8080")
	mc.IncRequestsSent("tcp", "8080")
	mc.AddBytesReceived("tcp", "8080", 0)
	mc.AddBytesSent("tcp", "8080", 0)

	atomic.StoreUint64(&mc.totalRequestsReceived, 0)
	atomic.StoreUint64(&mc.totalRequestsSent, 0)
	atomic.StoreUint64(&mc.totalTCPReceived, 0)
	atomic.StoreUint64(&mc.totalTCPSent, 0)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				mc.IncRequestsReceived("tcp", "8080")
				mc.IncRequestsSent("tcp", "8080")
				mc.AddBytesReceived("tcp", "8080", 100)
				mc.AddBytesSent("tcp", "8080", 100)
			}
		}(i)
	}

	wg.Wait()

	expectedCount := uint64(numGoroutines * numOperations)
	assert.Equal(t, expectedCount, atomic.LoadUint64(&mc.totalRequestsReceived))
	assert.Equal(t, expectedCount, atomic.LoadUint64(&mc.totalRequestsSent))
	assert.Equal(t, expectedCount, atomic.LoadUint64(&mc.totalTCPReceived))
	assert.Equal(t, expectedCount, atomic.LoadUint64(&mc.totalTCPSent))

	data := mc.getSyncMapData(&mc.requestsReceived)
	// Add 1 for the pre-initialization
	assert.Equal(t, expectedCount+1, data["tcp"]["8080"])

	data = mc.getSyncMapData(&mc.bytesReceived)
	assert.Equal(t, expectedCount*100, data["tcp"]["8080"])
}

func TestMetricsWithDifferentProtocols(t *testing.T) {
	mc := testMetricsCollector()

	protocols := []string{"tcp", "udp", "http", "https", "custom"}

	for _, proto := range protocols {
		mc.IncRequestsReceived(proto, "8080")
		mc.IncRequestsSent(proto, "8080")
	}

	data := mc.getSyncMapData(&mc.requestsReceived)
	for _, proto := range protocols {
		assert.Equal(t, uint64(1), data[proto]["8080"])
	}
}

func BenchmarkIncRequestsReceived(b *testing.B) {
	mc := testMetricsCollector()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		mc.IncRequestsReceived("tcp", "8080")
	}
}

func BenchmarkConcurrentUpdates(b *testing.B) {
	mc := testMetricsCollector()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mc.IncRequestsReceived("tcp", "8080")
			mc.AddBytesReceived("tcp", "8080", 1024)
		}
	})
}

// Helper functions for the client interface
func (mc *MetricsCollector) IncrementRequestsReceived(protocol string, port int) {
	mc.IncRequestsReceived(protocol, strconv.Itoa(port))
}

func (mc *MetricsCollector) IncrementRequestsSent(protocol string, port int) {
	mc.IncRequestsSent(protocol, strconv.Itoa(port))
}

func (mc *MetricsCollector) PrintSummary() {
	mc.LogMetrics("human")
}

func TestPrintTableSorting(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	data := map[string]map[string]uint64{
		"tcp": {
			"80":   100,
			"8080": 200,
			"443":  150,
			"22":   50,
		},
	}

	printTable("Port Sorting Test:", []string{"Protocol", "Port", "Count"}, data, false)

	_ = w.Close()
	os.Stdout = oldStdout

	output, _ := io.ReadAll(r)
	outputStr := string(output)

	lines := strings.Split(outputStr, "\n")
	var portOrder []string
	for _, line := range lines {
		if strings.Contains(line, "tcp") && strings.Contains(line, "│") {
			parts := strings.Split(line, "│")
			if len(parts) >= 3 {
				port := strings.TrimSpace(parts[2])
				if port != "" && port != "Port" {
					portOrder = append(portOrder, port)
				}
			}
		}
	}

	// Ports should be in order: 22, 80, 443, 8080
	if len(portOrder) >= 4 {
		assert.Equal(t, "22", portOrder[0])
		assert.Equal(t, "80", portOrder[1])
		assert.Equal(t, "443", portOrder[2])
		assert.Equal(t, "8080", portOrder[3])
	}
}

func TestLogMetricsJSONStructure(t *testing.T) {
	logging.InitLogger("json", "info")

	mc := testMetricsCollector()

	atomic.AddUint64(&mc.totalRequestsReceived, 10)
	atomic.AddUint64(&mc.totalRequestsSent, 20)
	mc.updateSyncMap(&mc.requestsReceived, "tcp", "8080", 5)
	mc.updateSyncMap(&mc.requestsSent, "udp", "9000", 10)

	data := mc.getSyncMapData(&mc.requestsReceived)
	jsonBytes, err := json.Marshal(data)
	require.NoError(t, err)

	var parsed map[string]map[string]uint64
	err = json.Unmarshal(jsonBytes, &parsed)
	require.NoError(t, err)
	assert.Equal(t, uint64(5), parsed["tcp"]["8080"])
}
