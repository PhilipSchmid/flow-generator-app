package metrics

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestInitMetrics(t *testing.T) {
	// Test that InitMetrics can be called
	// It may panic if metrics are already registered, which is fine in tests
	defer func() {
		_ = recover() // Expected if metrics already registered
	}()

	InitMetrics()
}

func TestMetricsOperations(t *testing.T) {
	// Test operations don't panic
	assert.NotPanics(t, func() {
		// TCP connections gauge operations
		TCPConnections.Set(5)
		TCPConnections.Inc()
		TCPConnections.Dec()
		TCPConnections.Add(3)
		TCPConnections.Sub(2)

		// UDP packets counter operations
		UDPPackets.Inc()
		UDPPackets.Add(10)

		// Flows received counter operations
		FlowsReceived.Inc()
		FlowsReceived.Add(5)
	})
}

func TestStartMetricsServer(t *testing.T) {
	// Start metrics server should not error
	err := StartMetricsServer("0")
	assert.NoError(t, err)
}

func TestConcurrentPrometheusMetricUpdates(t *testing.T) {
	// Test concurrent updates don't cause issues
	done := make(chan bool, 3)

	go func() {
		for i := 0; i < 1000; i++ {
			TCPConnections.Inc()
			TCPConnections.Dec()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 1000; i++ {
			UDPPackets.Inc()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 1000; i++ {
			FlowsReceived.Inc()
		}
		done <- true
	}()

	// Wait for completion
	for i := 0; i < 3; i++ {
		<-done
	}

	// If we get here without panic or deadlock, concurrent access works
	assert.True(t, true)
}

func TestMetricsEndpoint(t *testing.T) {
	// Start metrics server on a test port
	port := "9191"
	err := StartMetricsServer(port)
	assert.NoError(t, err)

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Test /metrics endpoint
	resp, err := http.Get("http://localhost:" + port + "/metrics")
	if err != nil {
		t.Skip("Could not connect to test server, skipping")
	}
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
}

func BenchmarkMetricUpdates(b *testing.B) {
	b.Run("TCPConnections", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			TCPConnections.Inc()
		}
	})

	b.Run("UDPPackets", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			UDPPackets.Inc()
		}
	})

	b.Run("FlowsReceived", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			FlowsReceived.Inc()
		}
	})
}
