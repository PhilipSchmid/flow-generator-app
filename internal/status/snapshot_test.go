package status

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLatencyBucketsAreMonotonic(t *testing.T) {
	previous := time.Duration(0)
	for i := 0; i < latencyBuckets; i++ {
		upper := LatencyBucketUpperBound(i)
		assert.GreaterOrEqual(t, upper, previous)
		previous = upper
	}
	for _, duration := range []time.Duration{time.Nanosecond, time.Microsecond, time.Millisecond, time.Second, time.Minute} {
		index := latencyBucket(uint64(duration))
		assert.GreaterOrEqual(t, LatencyBucketUpperBound(index), duration)
	}
}

func TestClientTrackerSnapshot(t *testing.T) {
	tracker := NewClientTracker([]PortFlowSnapshot{{Protocol: "tcp", Port: 8080}})
	assert.True(t, tracker.FlowStarted(0))
	tracker.FlowStarted(0)
	tracker.FlowStarted(0)
	tracker.FlowCompleted()
	tracker.FlowCanceled()
	tracker.FlowFailed(0)
	tracker.StartSkipped()
	tracker.RecordDialError()
	tracker.RecordMismatch()
	tracker.ObserveLatency("tcp", 2*time.Millisecond)

	snapshot := tracker.Snapshot()
	assert.Equal(t, uint64(3), snapshot.FlowsStarted)
	assert.Zero(t, snapshot.FlowsActive)
	assert.Equal(t, uint64(1), snapshot.FlowsCompleted)
	assert.Equal(t, uint64(1), snapshot.FlowsCanceled)
	assert.Equal(t, uint64(1), snapshot.FlowsFailed)
	assert.Equal(t, uint64(1), snapshot.StartsSkippedAtCapacity)
	assert.Equal(t, uint64(1), snapshot.Errors.Dial)
	assert.Equal(t, uint64(1), snapshot.Errors.Mismatch)
	require.Len(t, snapshot.PortFlows, 1)
	assert.Equal(t, uint64(3), snapshot.PortFlows[0].Started)
	assert.Equal(t, uint64(1), snapshot.PortFlows[0].Failed)
	assert.Equal(t, uint64(1), snapshot.TCPLatency.Count)
	assert.Equal(t, uint64(2*time.Millisecond), snapshot.TCPLatency.SumNanos)
}

func TestClientTrackerLatencySamplingUsesElapsedTime(t *testing.T) {
	tracker := NewClientTracker(nil)
	now := int64(time.Second)
	assert.True(t, tracker.sampleLatencyAt(now))
	assert.False(t, tracker.sampleLatencyAt(now+int64(latencySamplePeriod)-1))
	assert.True(t, tracker.sampleLatencyAt(now+int64(latencySamplePeriod)))
}

func TestLatencySnapshotIsInternallyConsistentDuringUpdates(t *testing.T) {
	var histogram latencyHistogram
	var writers sync.WaitGroup
	writers.Add(1)
	go func() {
		defer writers.Done()
		for range 10_000 {
			histogram.observe(time.Microsecond)
		}
	}()
	for range 1_000 {
		snapshot := histogram.snapshot()
		var count uint64
		for _, bucket := range snapshot.Buckets {
			count += bucket
		}
		assert.Equal(t, count, snapshot.Count)
		if snapshot.Count > 0 {
			assert.Greater(t, snapshot.SumNanos, uint64(0))
		}
	}
	writers.Wait()
}

func TestClientTrackerIgnoresUnknownPortIndex(t *testing.T) {
	tracker := NewClientTracker(nil)
	assert.NotPanics(t, func() {
		tracker.FlowStarted(-1)
		tracker.FlowFailed(42)
	})
}

func TestServerTrackerCountsUniqueActiveTCPClientIPs(t *testing.T) {
	tracker := &ServerTracker{}
	first := tracker.TCPClientConnected(&net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 40001})
	second := tracker.TCPClientConnected(&net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 40002})
	third := tracker.TCPClientConnected(&net.TCPAddr{IP: net.ParseIP("192.0.2.11"), Port: 40003})
	assert.Equal(t, uint64(2), tracker.ActiveTCPClients())

	tracker.TCPClientDisconnected(first)
	assert.Equal(t, uint64(2), tracker.ActiveTCPClients())
	tracker.TCPClientDisconnected(second)
	assert.Equal(t, uint64(1), tracker.ActiveTCPClients())
	tracker.TCPClientDisconnected(third)
	assert.Zero(t, tracker.ActiveTCPClients())
}

func BenchmarkClientTrackerFlowLifecycle(b *testing.B) {
	tracker := NewClientTracker([]PortFlowSnapshot{{Protocol: "tcp", Port: 8080}})
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			tracker.FlowStarted(0)
			tracker.FlowCompleted()
		}
	})
}

func BenchmarkClientTrackerSnapshot(b *testing.B) {
	tracker := NewClientTracker([]PortFlowSnapshot{
		{Protocol: "tcp", Port: 8080},
		{Protocol: "udp", Port: 9000},
	})
	tracker.FlowStarted(0)
	tracker.ObserveLatency("tcp", time.Millisecond)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = tracker.Snapshot()
	}
}

func BenchmarkServerTrackerTCPClientLifecycle(b *testing.B) {
	tracker := &ServerTracker{}
	peer := &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 40001}
	key := tracker.TCPClientConnected(peer)
	tracker.TCPClientDisconnected(key)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		key := tracker.TCPClientConnected(peer)
		tracker.TCPClientDisconnected(key)
	}
}
