package status

import (
	"math"
	"math/bits"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/metrics"
)

const (
	// SchemaVersion changes only when the status JSON contract is incompatible.
	SchemaVersion       = 1
	latencyBuckets      = 252
	maxSamplesPerSecond = 1000
)

// Configuration contains non-secret settings useful while diagnosing traffic.
type Configuration struct {
	Target         string  `json:"target,omitempty"`
	Protocol       string  `json:"protocol,omitempty"`
	TCPPorts       []int   `json:"tcp_ports,omitempty"`
	UDPPorts       []int   `json:"udp_ports,omitempty"`
	Rate           float64 `json:"rate,omitempty"`
	MaxConcurrent  int     `json:"max_concurrent,omitempty"`
	MinDuration    float64 `json:"min_duration,omitempty"`
	MaxDuration    float64 `json:"max_duration,omitempty"`
	ConstantFlows  bool    `json:"constant_flows,omitempty"`
	FlowTimeout    float64 `json:"flow_timeout,omitempty"`
	FlowCount      int     `json:"flow_count,omitempty"`
	PayloadSize    int     `json:"payload_size,omitempty"`
	MinPayloadSize int     `json:"min_payload_size,omitempty"`
	MaxPayloadSize int     `json:"max_payload_size,omitempty"`
	MTU            int     `json:"mtu,omitempty"`
	MSS            int     `json:"mss,omitempty"`
	HealthPort     string  `json:"health_port,omitempty"`
	MetricsPort    string  `json:"metrics_port"`
	TracingEnabled bool    `json:"tracing_enabled"`
}

// ErrorCounts contains cumulative operational failures.
type ErrorCounts struct {
	Dial     uint64 `json:"dial"`
	Read     uint64 `json:"read"`
	Write    uint64 `json:"write"`
	Mismatch uint64 `json:"mismatch"`
	MTU      uint64 `json:"mtu"`
	Accept   uint64 `json:"accept"`
}

// LatencySnapshot is a cumulative sampled echo-latency histogram.
type LatencySnapshot struct {
	Count    uint64   `json:"count"`
	SumNanos uint64   `json:"sum_nanos"`
	Buckets  []uint64 `json:"buckets"`
}

// PortFlowSnapshot contains cumulative client flow outcomes for one target.
type PortFlowSnapshot struct {
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
	Started  uint64 `json:"started"`
	Failed   uint64 `json:"failed"`
}

// ClientSnapshot contains client-specific scheduler and outcome state.
type ClientSnapshot struct {
	FlowsStarted            uint64             `json:"flows_started"`
	FlowsActive             int64              `json:"flows_active"`
	FlowsCompleted          uint64             `json:"flows_completed"`
	FlowsCanceled           uint64             `json:"flows_canceled"`
	FlowsFailed             uint64             `json:"flows_failed"`
	StartsSkippedAtCapacity uint64             `json:"starts_skipped_at_capacity"`
	LimitReached            bool               `json:"limit_reached"`
	Errors                  ErrorCounts        `json:"errors"`
	PortFlows               []PortFlowSnapshot `json:"port_flows"`
	TCPLatency              LatencySnapshot    `json:"tcp_latency"`
	UDPLatency              LatencySnapshot    `json:"udp_latency"`
}

// ServerSnapshot contains server-specific health and error state.
type ServerSnapshot struct {
	Ready            bool        `json:"ready"`
	Healthy          bool        `json:"healthy"`
	ActiveTCPClients uint64      `json:"active_tcp_clients"`
	Errors           ErrorCounts `json:"errors"`
}

// Snapshot is the versioned response served to the local dashboard.
type Snapshot struct {
	SchemaVersion int              `json:"schema_version"`
	Role          string           `json:"role"`
	Version       string           `json:"version"`
	SampledAt     time.Time        `json:"sampled_at"`
	StartedAt     time.Time        `json:"started_at"`
	State         string           `json:"state"`
	Configuration Configuration    `json:"configuration"`
	Traffic       metrics.Snapshot `json:"traffic"`
	Client        *ClientSnapshot  `json:"client,omitempty"`
	Server        *ServerSnapshot  `json:"server,omitempty"`
}

type errorCounters struct {
	dial     atomic.Uint64
	read     atomic.Uint64
	write    atomic.Uint64
	mismatch atomic.Uint64
	mtu      atomic.Uint64
	accept   atomic.Uint64
}

func (c *errorCounters) snapshot() ErrorCounts {
	return ErrorCounts{
		Dial: c.dial.Load(), Read: c.read.Load(), Write: c.write.Load(),
		Mismatch: c.mismatch.Load(), MTU: c.mtu.Load(), Accept: c.accept.Load(),
	}
}

type latencyHistogram struct {
	sumNanos atomic.Uint64
	buckets  [latencyBuckets]atomic.Uint64
}

func (h *latencyHistogram) observe(duration time.Duration) {
	if duration < 0 {
		return
	}
	nanos := uint64(duration)
	if nanos == 0 {
		nanos = 1
	}
	h.sumNanos.Add(nanos)
	h.buckets[latencyBucket(nanos)].Add(1)
}

func (h *latencyHistogram) snapshot() LatencySnapshot {
	buckets := make([]uint64, latencyBuckets)
	var count uint64
	for i := range h.buckets {
		buckets[i] = h.buckets[i].Load()
		count += buckets[i]
	}
	return LatencySnapshot{Count: count, SumNanos: h.sumNanos.Load(), Buckets: buckets}
}

func latencyBucket(nanos uint64) int {
	exponent := bits.Len64(nanos) - 1
	base := uint64(1) << exponent
	subBucket := uint64(0)
	if base > 0 {
		subBucket = ((nanos - base) * 4) / base
		if subBucket > 3 {
			subBucket = 3
		}
	}
	index := exponent*4 + int(subBucket)
	if index >= latencyBuckets {
		return latencyBuckets - 1
	}
	return index
}

// LatencyBucketUpperBound returns the inclusive approximation for a bucket.
func LatencyBucketUpperBound(index int) time.Duration {
	if index < 0 {
		return 0
	}
	if index >= latencyBuckets {
		index = latencyBuckets - 1
	}
	exponent := index / 4
	subBucket := index % 4
	base := uint64(1) << exponent
	upper := base + (base*uint64(subBucket+1))/4
	if subBucket == 3 {
		upper = base << 1
	}
	if upper > math.MaxInt64 {
		upper = math.MaxInt64
	}
	return time.Duration(upper)
}

type portFlowCounter struct {
	protocol string
	port     int
	started  atomic.Uint64
	failed   atomic.Uint64
}

// ClientTracker owns low-cost client scheduler and outcome counters.
type ClientTracker struct {
	started      atomic.Uint64
	active       atomic.Int64
	completed    atomic.Uint64
	canceled     atomic.Uint64
	failed       atomic.Uint64
	skipped      atomic.Uint64
	limitReached atomic.Bool
	errors       errorCounters
	ports        []portFlowCounter
	tcpLatency   latencyHistogram
	udpLatency   latencyHistogram
	sampleEvery  uint64
}

// NewClientTracker preallocates counters for configured protocol/port pairs.
func NewClientTracker(rate float64, ports []PortFlowSnapshot) *ClientTracker {
	every := uint64(1)
	if rate > maxSamplesPerSecond {
		every = uint64(rate / maxSamplesPerSecond)
		if float64(every*maxSamplesPerSecond) < rate {
			every++
		}
	}
	t := &ClientTracker{sampleEvery: every, ports: make([]portFlowCounter, len(ports))}
	for i, port := range ports {
		t.ports[i].protocol = port.Protocol
		t.ports[i].port = port.Port
	}
	return t
}

// FlowStarted records one launched flow and returns whether to sample its RTT.
func (t *ClientTracker) FlowStarted(portIndex int) bool {
	started := t.started.Add(1)
	t.active.Add(1)
	if portIndex >= 0 && portIndex < len(t.ports) {
		t.ports[portIndex].started.Add(1)
	}
	return t.sampleEvery == 1 || started%t.sampleEvery == 0
}

func (t *ClientTracker) FlowCompleted() { t.active.Add(-1); t.completed.Add(1) }
func (t *ClientTracker) FlowCanceled()  { t.active.Add(-1); t.canceled.Add(1) }
func (t *ClientTracker) FlowFailed(portIndex int) {
	t.active.Add(-1)
	t.failed.Add(1)
	if portIndex >= 0 && portIndex < len(t.ports) {
		t.ports[portIndex].failed.Add(1)
	}
}
func (t *ClientTracker) StartSkipped()     { t.skipped.Add(1) }
func (t *ClientTracker) SetLimitReached()  { t.limitReached.Store(true) }
func (t *ClientTracker) RecordDialError()  { t.errors.dial.Add(1) }
func (t *ClientTracker) RecordReadError()  { t.errors.read.Add(1) }
func (t *ClientTracker) RecordWriteError() { t.errors.write.Add(1) }
func (t *ClientTracker) RecordMismatch()   { t.errors.mismatch.Add(1) }
func (t *ClientTracker) RecordMTUError()   { t.errors.mtu.Add(1) }

func (t *ClientTracker) ObserveLatency(protocol string, duration time.Duration) {
	switch protocol {
	case "tcp":
		t.tcpLatency.observe(duration)
	case "udp":
		t.udpLatency.observe(duration)
	}
}

func (t *ClientTracker) Snapshot() ClientSnapshot {
	ports := make([]PortFlowSnapshot, len(t.ports))
	for i := range t.ports {
		ports[i] = PortFlowSnapshot{
			Protocol: t.ports[i].protocol, Port: t.ports[i].port,
			Started: t.ports[i].started.Load(), Failed: t.ports[i].failed.Load(),
		}
	}
	return ClientSnapshot{
		FlowsStarted: t.started.Load(), FlowsActive: t.active.Load(),
		FlowsCompleted: t.completed.Load(), FlowsCanceled: t.canceled.Load(),
		FlowsFailed: t.failed.Load(), StartsSkippedAtCapacity: t.skipped.Load(),
		LimitReached: t.limitReached.Load(), Errors: t.errors.snapshot(), PortFlows: ports,
		TCPLatency: t.tcpLatency.snapshot(), UDPLatency: t.udpLatency.snapshot(),
	}
}

// ServerTracker owns counters for server-side failures and connection-level
// status. Client IP tracking runs only at TCP connection boundaries, never in
// the per-request data path.
type ServerTracker struct {
	errors errorCounters

	clientsMu      sync.Mutex
	activeTCPPeers map[string]uint64
}

func (t *ServerTracker) RecordReadError()    { t.errors.read.Add(1) }
func (t *ServerTracker) RecordWriteError()   { t.errors.write.Add(1) }
func (t *ServerTracker) RecordAcceptError()  { t.errors.accept.Add(1) }
func (t *ServerTracker) Errors() ErrorCounts { return t.errors.snapshot() }

// TCPClientConnected records an active TCP peer and returns its normalized IP
// key for the matching disconnect call.
func (t *ServerTracker) TCPClientConnected(address net.Addr) string {
	key := tcpClientIP(address)
	if key == "" {
		return ""
	}
	t.clientsMu.Lock()
	if t.activeTCPPeers == nil {
		t.activeTCPPeers = make(map[string]uint64)
	}
	t.activeTCPPeers[key]++
	t.clientsMu.Unlock()
	return key
}

// TCPClientDisconnected removes one active connection for a TCP peer.
func (t *ServerTracker) TCPClientDisconnected(key string) {
	if key == "" {
		return
	}
	t.clientsMu.Lock()
	if count := t.activeTCPPeers[key]; count > 1 {
		t.activeTCPPeers[key] = count - 1
	} else {
		delete(t.activeTCPPeers, key)
	}
	t.clientsMu.Unlock()
}

func (t *ServerTracker) ActiveTCPClients() uint64 {
	t.clientsMu.Lock()
	count := uint64(len(t.activeTCPPeers))
	t.clientsMu.Unlock()
	return count
}

func tcpClientIP(address net.Addr) string {
	if address == nil {
		return ""
	}
	if tcpAddress, ok := address.(*net.TCPAddr); ok {
		return tcpAddress.IP.String()
	}
	host, _, err := net.SplitHostPort(address.String())
	if err != nil {
		return ""
	}
	return host
}
