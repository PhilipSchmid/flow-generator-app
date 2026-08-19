package dashboard

import (
	"math"
	"sort"
	"time"

	statusapi "github.com/PhilipSchmid/flow-generator-app/internal/status"
)

const maxHistorySamples = 15 * 60

type portSample struct {
	Protocol   string
	Port       string
	Activity   float64
	FlowRate   float64
	PacketRate float64
	BytesTX    float64
	BytesRX    float64
	Failures   float64
}

type sample struct {
	At          time.Time
	FlowRate    float64
	TCPRate     float64
	UDPRate     float64
	BytesTX     float64
	BytesRX     float64
	Active      float64
	SkippedRate float64
	FailureRate float64
	SuccessRate float64
	Errors      errorRates
	TCPLatency  statusapi.LatencySnapshot
	UDPLatency  statusapi.LatencySnapshot
	Ports       []portSample
}

type errorRates struct {
	Dial, Read, Write, Mismatch, MTU, Accept float64
}

type history struct {
	samples  []sample
	previous *statusapi.Snapshot
}

func (h *history) add(snapshot statusapi.Snapshot) {
	if h.previous == nil || h.previous.StartedAt != snapshot.StartedAt || h.previous.Role != snapshot.Role {
		h.samples = h.samples[:0]
		copy := snapshot
		h.previous = &copy
		return
	}
	seconds := snapshot.SampledAt.Sub(h.previous.SampledAt).Seconds()
	if seconds <= 0 {
		copy := snapshot
		h.previous = &copy
		return
	}
	current := sample{At: snapshot.SampledAt, Active: float64(snapshot.Traffic.ActiveTCPConnections)}
	if snapshot.Client != nil && h.previous.Client != nil {
		current.FlowRate = deltaRate(snapshot.Client.FlowsStarted, h.previous.Client.FlowsStarted, seconds)
		current.Active = float64(snapshot.Client.FlowsActive)
		current.SkippedRate = deltaRate(snapshot.Client.StartsSkippedAtCapacity, h.previous.Client.StartsSkippedAtCapacity, seconds)
		current.FailureRate = deltaRate(snapshot.Client.FlowsFailed, h.previous.Client.FlowsFailed, seconds)
		current.SuccessRate = deltaRate(snapshot.Client.FlowsCompleted, h.previous.Client.FlowsCompleted, seconds)
		current.TCPLatency = latencyDelta(snapshot.Client.TCPLatency, h.previous.Client.TCPLatency)
		current.UDPLatency = latencyDelta(snapshot.Client.UDPLatency, h.previous.Client.UDPLatency)
	}
	current.Errors = errorRateDelta(snapshotErrors(snapshot), snapshotErrors(*h.previous), seconds)
	current.BytesTX, current.BytesRX, current.TCPRate, current.UDPRate, current.Ports = trafficDelta(snapshot, *h.previous, seconds)
	h.samples = append(h.samples, current)
	if len(h.samples) > maxHistorySamples {
		copy(h.samples, h.samples[len(h.samples)-maxHistorySamples:])
		h.samples = h.samples[:maxHistorySamples]
	}
	copy := snapshot
	h.previous = &copy
}

func snapshotErrors(snapshot statusapi.Snapshot) statusapi.ErrorCounts {
	if snapshot.Client != nil {
		return snapshot.Client.Errors
	}
	if snapshot.Server != nil {
		return snapshot.Server.Errors
	}
	return statusapi.ErrorCounts{}
}

func errorRateDelta(current, previous statusapi.ErrorCounts, seconds float64) errorRates {
	return errorRates{
		Dial: deltaRate(current.Dial, previous.Dial, seconds), Read: deltaRate(current.Read, previous.Read, seconds),
		Write: deltaRate(current.Write, previous.Write, seconds), Mismatch: deltaRate(current.Mismatch, previous.Mismatch, seconds),
		MTU: deltaRate(current.MTU, previous.MTU, seconds), Accept: deltaRate(current.Accept, previous.Accept, seconds),
	}
}

func (h *history) window(duration time.Duration) []sample {
	if len(h.samples) == 0 {
		return nil
	}
	cutoff := h.samples[len(h.samples)-1].At.Add(-duration)
	index := sort.Search(len(h.samples), func(i int) bool { return !h.samples[i].At.Before(cutoff) })
	return h.samples[index:]
}

func trafficDelta(current, previous statusapi.Snapshot, seconds float64) (tx, rx, tcp, udp float64, ports []portSample) {
	previousPorts := make(map[string]portTotals, len(previous.Traffic.Ports))
	for _, port := range previous.Traffic.Ports {
		previousPorts[port.Protocol+":"+port.Port] = portTotals{
			requestsReceived: port.RequestsReceived, requestsSent: port.RequestsSent,
			bytesReceived: port.BytesReceived, bytesSent: port.BytesSent,
		}
	}
	for _, port := range current.Traffic.Ports {
		old := previousPorts[port.Protocol+":"+port.Port]
		requests := deltaRate(max64(port.RequestsReceived, port.RequestsSent), max64(old.requestsReceived, old.requestsSent), seconds)
		portTX := deltaRate(port.BytesSent, old.bytesSent, seconds)
		portRX := deltaRate(port.BytesReceived, old.bytesReceived, seconds)
		ports = append(ports, portSample{Protocol: port.Protocol, Port: port.Port, Activity: requests, PacketRate: requests, BytesTX: portTX, BytesRX: portRX})
		tx += portTX
		rx += portRX
		switch port.Protocol {
		case "tcp":
			tcp += requests
		case "udp":
			udp += requests
		}
	}
	if current.Client != nil && previous.Client != nil {
		previousFlows := make(map[string]statusapi.PortFlowSnapshot, len(previous.Client.PortFlows))
		for _, port := range previous.Client.PortFlows {
			previousFlows[port.Protocol+":"+stringPort(port.Port)] = port
		}
		for i := range ports {
			for _, flowPort := range current.Client.PortFlows {
				if ports[i].Protocol == flowPort.Protocol && ports[i].Port == stringPort(flowPort.Port) {
					old := previousFlows[ports[i].Protocol+":"+ports[i].Port]
					ports[i].FlowRate = deltaRate(flowPort.Started, old.Started, seconds)
					ports[i].Activity = ports[i].FlowRate
					ports[i].Failures = deltaRate(flowPort.Failed, old.Failed, seconds)
					break
				}
			}
		}
	}
	return tx, rx, tcp, udp, ports
}

type portTotals struct{ requestsReceived, requestsSent, bytesReceived, bytesSent uint64 }

func deltaRate(current, previous uint64, seconds float64) float64 {
	if current < previous || seconds <= 0 {
		return 0
	}
	return float64(current-previous) / seconds
}

func latencyDelta(current, previous statusapi.LatencySnapshot) statusapi.LatencySnapshot {
	result := statusapi.LatencySnapshot{Buckets: make([]uint64, len(current.Buckets))}
	if current.Count < previous.Count || current.SumNanos < previous.SumNanos {
		return result
	}
	result.Count = current.Count - previous.Count
	result.SumNanos = current.SumNanos - previous.SumNanos
	for i := range result.Buckets {
		if i < len(previous.Buckets) && current.Buckets[i] >= previous.Buckets[i] {
			result.Buckets[i] = current.Buckets[i] - previous.Buckets[i]
		}
	}
	return result
}

type distribution struct {
	Current, Average, P50, P90, P95, P99, Maximum float64
}

func summarize(values []float64) distribution {
	if len(values) == 0 {
		return distribution{}
	}
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	sum := 0.0
	for _, value := range ordered {
		sum += value
	}
	return distribution{
		Current: values[len(values)-1], Average: sum / float64(len(values)),
		P50: percentile(ordered, 0.50), P90: percentile(ordered, 0.90),
		P95: percentile(ordered, 0.95), P99: percentile(ordered, 0.99),
		Maximum: ordered[len(ordered)-1],
	}
}

func percentile(sorted []float64, quantile float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	index := int(math.Ceil(quantile*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func latencySummary(samples []sample) (distribution, uint64) {
	buckets := make([]uint64, 256)
	var count, sum uint64
	for _, sample := range samples {
		for _, latency := range []statusapi.LatencySnapshot{sample.TCPLatency, sample.UDPLatency} {
			count += latency.Count
			sum += latency.SumNanos
			for i, value := range latency.Buckets {
				if i < len(buckets) {
					buckets[i] += value
				}
			}
		}
	}
	result := distribution{}
	if count == 0 {
		return result, 0
	}
	result.Average = float64(sum) / float64(count)
	result.P50 = float64(histogramPercentile(buckets, count, 0.50))
	result.P90 = float64(histogramPercentile(buckets, count, 0.90))
	result.P95 = float64(histogramPercentile(buckets, count, 0.95))
	result.P99 = float64(histogramPercentile(buckets, count, 0.99))
	for i := len(buckets) - 1; i >= 0; i-- {
		if buckets[i] > 0 {
			result.Maximum = float64(statusapi.LatencyBucketUpperBound(i))
			break
		}
	}
	result.Current = result.P50
	return result, count
}

func histogramPercentile(buckets []uint64, count uint64, quantile float64) time.Duration {
	if count == 0 {
		return 0
	}
	target := uint64(math.Ceil(float64(count) * quantile))
	var cumulative uint64
	for i, value := range buckets {
		cumulative += value
		if cumulative >= target {
			return statusapi.LatencyBucketUpperBound(i)
		}
	}
	return statusapi.LatencyBucketUpperBound(len(buckets) - 1)
}

func values(samples []sample, pick func(sample) float64) []float64 {
	result := make([]float64, len(samples))
	for i := range samples {
		result[i] = pick(samples[i])
	}
	return result
}

func activityValues(samples []sample, role string) []float64 {
	if role == "server" {
		return values(samples, func(sample sample) float64 { return sample.TCPRate + sample.UDPRate })
	}
	return values(samples, func(sample sample) float64 { return sample.FlowRate })
}

func latencySeries(samples []sample, quantile float64) []float64 {
	result := make([]float64, len(samples))
	for i, sample := range samples {
		var buckets [256]uint64
		var count uint64
		for _, latency := range []statusapi.LatencySnapshot{sample.TCPLatency, sample.UDPLatency} {
			count += latency.Count
			for bucket, value := range latency.Buckets {
				if bucket < len(buckets) {
					buckets[bucket] += value
				}
			}
		}
		if count > 0 {
			result[i] = float64(histogramPercentile(buckets[:], count, quantile))
		}
	}
	return result
}

func max64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

func stringPort(port int) string {
	if port == 0 {
		return "0"
	}
	var buffer [20]byte
	index := len(buffer)
	for port > 0 {
		index--
		buffer[index] = byte('0' + port%10)
		port /= 10
	}
	return string(buffer[index:])
}
