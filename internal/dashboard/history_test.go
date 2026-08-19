package dashboard

import (
	"testing"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/metrics"
	statusapi "github.com/PhilipSchmid/flow-generator-app/internal/status"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHistoryCalculatesRatesAndLatency(t *testing.T) {
	started := time.Now().UTC().Add(-time.Minute)
	first := dashboardSnapshot(started, started.Add(time.Second), 100, 1000, 10)
	second := dashboardSnapshot(started, started.Add(2*time.Second), 120, 1400, 12)
	first.Client.TCPLatency = latencyWith(10, 10*time.Millisecond, 2*time.Millisecond)
	second.Client.TCPLatency = latencyWith(15, 20*time.Millisecond, 2*time.Millisecond)
	first.Client.Errors = statusapi.ErrorCounts{Dial: 10, Read: 100}
	second.Client.Errors = statusapi.ErrorCounts{Dial: 12, Read: 120}

	var samples history
	samples.add(first)
	samples.add(second)
	require.Len(t, samples.samples, 1)
	current := samples.samples[0]
	assert.InDelta(t, 20, current.FlowRate, 0.001)
	assert.InDelta(t, 400, current.BytesTX, 0.001)
	assert.Equal(t, float64(12), current.Active)
	assert.Equal(t, float64(2), current.Errors.Dial)
	assert.Equal(t, float64(20), current.Errors.Read)
	assert.Equal(t, uint64(5), current.TCPLatency.Count)

	latency, count := latencySummary(samples.samples)
	assert.Equal(t, uint64(5), count)
	assert.Greater(t, latency.P50, float64(0))
}

func TestHistoryClearsOnRestart(t *testing.T) {
	started := time.Now().UTC()
	var samples history
	samples.add(dashboardSnapshot(started, started.Add(time.Second), 1, 1, 1))
	samples.add(dashboardSnapshot(started, started.Add(2*time.Second), 2, 2, 1))
	require.Len(t, samples.samples, 1)
	samples.add(dashboardSnapshot(started.Add(time.Hour), started.Add(time.Hour+time.Second), 1, 1, 1))
	assert.Empty(t, samples.samples)
}

func TestSummarizePercentiles(t *testing.T) {
	summary := summarize([]float64{1, 2, 3, 4, 100})
	assert.Equal(t, float64(3), summary.P50)
	assert.Equal(t, float64(100), summary.P90)
	assert.Equal(t, float64(100), summary.P99)
	assert.Equal(t, float64(22), summary.Average)
}

func TestLatencySeriesCombinesProtocols(t *testing.T) {
	samples := []sample{{
		TCPLatency: latencyWith(10, 20*time.Millisecond, 2*time.Millisecond),
		UDPLatency: latencyWith(10, 80*time.Millisecond, 8*time.Millisecond),
	}}

	series := latencySeries(samples, 0.95)
	require.Len(t, series, 1)
	assert.GreaterOrEqual(t, series[0], float64(8*time.Millisecond))
}

func dashboardSnapshot(started, sampled time.Time, flows, bytes uint64, active int64) statusapi.Snapshot {
	return statusapi.Snapshot{
		SchemaVersion: statusapi.SchemaVersion, Role: "client", Version: "test",
		StartedAt: started, SampledAt: sampled, State: "running",
		Configuration: statusapi.Configuration{Target: "echo", Protocol: "tcp", Rate: 20, MaxConcurrent: 50, TCPPorts: []int{8080}},
		Traffic:       metrics.Snapshot{Ports: []metrics.PortSnapshot{{Protocol: "tcp", Port: "8080", RequestsSent: flows, BytesSent: bytes, BytesReceived: bytes}}},
		Client:        &statusapi.ClientSnapshot{FlowsStarted: flows, FlowsActive: active, PortFlows: []statusapi.PortFlowSnapshot{{Protocol: "tcp", Port: 8080, Started: flows}}},
	}
}

func latencyWith(count uint64, sum, value time.Duration) statusapi.LatencySnapshot {
	buckets := make([]uint64, 256)
	for i := range buckets {
		if statusapi.LatencyBucketUpperBound(i) >= value {
			buckets[i] = count
			break
		}
	}
	return statusapi.LatencySnapshot{Count: count, SumNanos: uint64(sum), Buckets: buckets}
}
