package main

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/PhilipSchmid/flow-generator-app/internal/config"
	"github.com/PhilipSchmid/flow-generator-app/internal/logging"
	"github.com/PhilipSchmid/flow-generator-app/internal/metrics"
	statusapi "github.com/PhilipSchmid/flow-generator-app/internal/status"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestConstructAddress(t *testing.T) {
	tests := []struct {
		name     string
		server   string
		port     int
		expected string
	}{
		{"IPv4 address", "192.168.1.1", 8080, "192.168.1.1:8080"},
		{"IPv6 address", "2001:db8::1", 8080, "[2001:db8::1]:8080"},
		{"hostname", "example.com", 8080, "example.com:8080"},
		{"localhost", "localhost", 8080, "localhost:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := constructAddress(tt.server, tt.port)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetPayloadSize(t *testing.T) {
	tests := []struct {
		name     string
		cfg      config.ClientConfig
		expected int
	}{
		{
			name: "fixed payload size",
			cfg: config.ClientConfig{
				PayloadSize: 1024,
			},
			expected: 1024,
		},
		{
			name:     "default payload size",
			cfg:      config.ClientConfig{},
			expected: 5,
		},
		{
			name: "random payload size",
			cfg: config.ClientConfig{
				MinPayloadSize: 100,
				MaxPayloadSize: 200,
			},
			expected: -1, // Will check range
		},
		{
			name: "equal payload range",
			cfg: config.ClientConfig{
				MinPayloadSize: 128,
				MaxPayloadSize: 128,
			},
			expected: 128,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size := getPayloadSize(&tt.cfg)

			if tt.expected == -1 {
				assert.GreaterOrEqual(t, size, tt.cfg.MinPayloadSize)
				assert.LessOrEqual(t, size, tt.cfg.MaxPayloadSize)
			} else {
				assert.Equal(t, tt.expected, size)
			}
		})
	}
}

func TestClientConfiguration(t *testing.T) {
	testCfg := &config.ClientConfig{
		CommonConfig: config.CommonConfig{
			LogLevel:    "info",
			LogFormat:   "json",
			MetricsPort: "9091",
		},
		Server:         "localhost",
		Rate:           10.0,
		MaxConcurrent:  100,
		Protocol:       "both",
		MinDuration:    1.0,
		MaxDuration:    5.0,
		TCPPorts:       "8080,8081",
		UDPPorts:       "9000",
		PayloadSize:    100,
		MinPayloadSize: 50,
		MaxPayloadSize: 200,
		MTU:            1500,
		MSS:            1460,
	}

	err := testCfg.Validate()
	assert.NoError(t, err)

	invalidCfg := *testCfg
	invalidCfg.Server = ""
	err = invalidCfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "server address cannot be empty")

	invalidCfg = *testCfg
	invalidCfg.Rate = -1
	err = invalidCfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate must be positive")
}

func TestProtocolPort(t *testing.T) {
	pp := newProtocolPort("localhost", "tcp", 8080)

	assert.Equal(t, "tcp", pp.Protocol)
	assert.Equal(t, 8080, pp.Port)
	assert.Equal(t, "localhost:8080", pp.address)
	assert.Equal(t, "8080", pp.portStr)
}

func TestParsePorts(t *testing.T) {
	logging.InitLogger("json", "error")

	tests := []struct {
		name     string
		input    string
		expected []int
	}{
		{"single port", "8080", []int{8080}},
		{"multiple ports", "8080,8081,8082", []int{8080, 8081, 8082}},
		{"with spaces", " 8080 , 8081 , 8082 ", []int{8080, 8081, 8082}},
		{"empty string", "", []int{}},
		{"invalid port", "8080,invalid,8081", []int{8080, 8081}},
		{"out of range port", "8080,70000,8081", []int{8080, 8081}},
		{"negative port", "8080,-1,8081", []int{8080, 8081}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parsePorts(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateFlow(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				buf := make([]byte, 1024)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					_, _ = c.Write(buf[:n])
				}
			}(conn)
		}
	}()

	serverAddr := listener.Addr().(*net.TCPAddr)

	testCfg := &config.ClientConfig{
		Server:      "127.0.0.1",
		PayloadSize: 100,
		MTU:         1500,
		MSS:         1460,
	}
	mc := metrics.NewMetricsCollector()

	ctx := context.Background()
	pp := newProtocolPort("127.0.0.1", "tcp", serverAddr.Port)
	tracker := statusapi.NewClientTracker([]statusapi.PortFlowSnapshot{{Protocol: "tcp", Port: serverAddr.Port}})
	sample := tracker.FlowStarted(0)
	outcome := generateFlowObserved(ctx, testCfg, mc, pp, 0.1, flowObserver{tracker: tracker, sampleLatency: sample})
	assert.Equal(t, flowCompleted, outcome)
	tracker.FlowCompleted()
	snapshot := tracker.Snapshot()
	assert.Equal(t, uint64(1), snapshot.FlowsCompleted)
	assert.Equal(t, uint64(1), snapshot.TCPLatency.Count)
}

// TestDrainStragglerReplyHalfClosesTCP verifies the real TCP shutdown path:
// the peer sees a clean EOF from CloseWrite, can still send its final reply,
// and the client drains that reply before returning.
func TestDrainStragglerReplyHalfClosesTCP(t *testing.T) {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	type serverResult struct {
		deadlineErr error
		readErr     error
		writeErr    error
	}

	accepted := make(chan struct{})
	result := make(chan serverResult, 1)
	go func() {
		conn, acceptErr := listener.AcceptTCP()
		if acceptErr != nil {
			result <- serverResult{readErr: acceptErr}
			return
		}
		defer func() { _ = conn.Close() }()
		close(accepted)

		deadlineErr := conn.SetReadDeadline(time.Now().Add(2 * tcpCloseGracePeriod))
		_, readErr := conn.Read(make([]byte, 1))
		if readErr != io.EOF {
			result <- serverResult{deadlineErr: deadlineErr, readErr: readErr}
			return
		}

		_, writeErr := conn.Write([]byte("late"))
		result <- serverResult{deadlineErr: deadlineErr, readErr: readErr, writeErr: writeErr}
	}()

	client, err := net.DialTCP("tcp", nil, listener.Addr().(*net.TCPAddr))
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	<-accepted

	buf := make([]byte, 16)
	drainStragglerReply(client, buf)

	server := <-result
	require.NoError(t, server.deadlineErr)
	require.ErrorIs(t, server.readErr, io.EOF, "server should observe the client's FIN, not a reset")
	require.NoError(t, server.writeErr, "server should be able to send after the client half-closes")

	n, err := client.Read(buf)
	require.Zero(t, n, "drain should consume the server's final reply")
	require.ErrorIs(t, err, io.EOF)
}

// TestDrainStragglerReplyBoundedWhenPeerSilent verifies drainStragglerReply
// returns once its grace window elapses instead of blocking indefinitely
// when the peer never replies at all.
func TestDrainStragglerReplyBoundedWhenPeerSilent(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	done := make(chan struct{})
	go func() {
		drainStragglerReply(client, make([]byte, 16))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(tcpCloseGracePeriod + 500*time.Millisecond):
		t.Fatal("drainStragglerReply did not return within its grace window")
	}
}

func TestMetricsCollectorInterface(t *testing.T) {
	mc := metrics.NewMetricsCollector()

	assert.NotPanics(t, func() {
		mc.IncRequestsReceived("tcp", "8080")
		mc.IncRequestsSent("tcp", "8080")
		mc.IncRequestsReceived("udp", "9000")
		mc.IncRequestsSent("udp", "9000")
		mc.AddBytesReceived("tcp", "8080", 1024)
		mc.AddBytesSent("tcp", "8080", 1024)
		mc.AddBytesReceived("udp", "9000", 512)
		mc.AddBytesSent("udp", "9000", 512)
	})
}

func TestRunFlowSchedulerDrainsFlowCount(t *testing.T) {
	flowDuration := 75 * time.Millisecond
	startedAt := time.Now()
	stats := runFlowScheduler(context.Background(), 1000, 1, 1, func(context.Context) {
		time.Sleep(flowDuration)
	})

	assert.True(t, stats.limitReached)
	assert.Equal(t, uint64(1), stats.started)
	assert.GreaterOrEqual(t, time.Since(startedAt), flowDuration)
}

func TestGenerateFlowStopsWhenParentIsCanceled(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- conn
		}
	}()

	cfg := &config.ClientConfig{Server: "127.0.0.1", PayloadSize: 64, MTU: 1500, MSS: 1460}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan flowOutcome)
	go func() {
		done <- generateFlow(ctx, cfg, metrics.NewMetricsCollector(), newProtocolPort("127.0.0.1", "tcp", listener.Addr().(*net.TCPAddr).Port), 10)
	}()
	serverConn := <-accepted
	defer func() { _ = serverConn.Close() }()
	cancel()

	select {
	case outcome := <-done:
		assert.Equal(t, flowCanceled, outcome)
	case <-time.After(time.Second):
		t.Fatal("flow did not stop after parent cancellation")
	}
}

func TestGenerateUDPFlowRecordsMTUFailure(t *testing.T) {
	cfg := &config.ClientConfig{Server: "127.0.0.1", PayloadSize: 100, MTU: 50, MSS: 40}
	tracker := statusapi.NewClientTracker([]statusapi.PortFlowSnapshot{{Protocol: "udp", Port: 9000}})
	tracker.FlowStarted(0)
	outcome := generateFlowObserved(context.Background(), cfg, metrics.NewMetricsCollector(), newProtocolPort("127.0.0.1", "udp", 9000), 1, flowObserver{tracker: tracker})
	assert.Equal(t, flowFailed, outcome)
	tracker.FlowFailed(0)
	snapshot := tracker.Snapshot()
	assert.Equal(t, uint64(1), snapshot.FlowsFailed)
	assert.Equal(t, uint64(1), snapshot.Errors.MTU)
}

func TestRunFlowSchedulerTracksCapacitySkips(t *testing.T) {
	tracker := statusapi.NewClientTracker(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	runFlowSchedulerTracked(ctx, 100_000, 1, 0, func(context.Context) { time.Sleep(20 * time.Millisecond) }, tracker)
	assert.Greater(t, tracker.Snapshot().StartsSkippedAtCapacity, uint64(0))
}

func TestGenerateFlowDoesNotWarnAfterCancellation(t *testing.T) {
	core, recorded := observer.New(zapcore.WarnLevel)
	oldLogger := logging.Logger
	logging.Logger = zap.New(core).Sugar()
	t.Cleanup(func() { logging.Logger = oldLogger })

	cfg := &config.ClientConfig{Server: "unresolvable.invalid", PayloadSize: 5, MTU: 1500, MSS: 1460}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, protocol := range []string{"tcp", "udp"} {
		tracker := statusapi.NewClientTracker(nil)
		generateFlowObserved(ctx, cfg, metrics.NewMetricsCollector(), newProtocolPort("unresolvable.invalid", protocol, 1), 10, flowObserver{tracker: tracker})
		assert.Equal(t, statusapi.ErrorCounts{}, tracker.Snapshot().Errors)
	}

	assert.Empty(t, recorded.All())
}

func TestExpectedFlowEnd(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	assert.True(t, expectedFlowEnd(canceled, errors.New("connection closed")))
	assert.True(t, expectedFlowEnd(context.Background(), timeoutError{}))
	assert.False(t, expectedFlowEnd(context.Background(), errors.New("connection refused")))
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.ClientConfig
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: config.ClientConfig{
				CommonConfig: config.CommonConfig{
					LogLevel:  "info",
					LogFormat: "json",
				},
				Server:        "127.0.0.1",
				TCPPorts:      "8080",
				Protocol:      "tcp",
				Rate:          10,
				MaxConcurrent: 5,
				MinDuration:   0.1,
				MaxDuration:   1.0,
				MTU:           1500,
				MSS:           1460,
			},
			wantErr: false,
		},
		{
			name: "invalid - no server",
			cfg: config.ClientConfig{
				TCPPorts: "8080",
			},
			wantErr: true,
		},
		{
			name: "invalid - no ports",
			cfg: config.ClientConfig{
				Server: "127.0.0.1",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func BenchmarkGenerateFlow(b *testing.B) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(b, err)
	defer func() { _ = listener.Close() }()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					_, _ = c.Write(buf[:n])
				}
			}(conn)
		}
	}()

	serverAddr := listener.Addr().(*net.TCPAddr)

	testCfg := &config.ClientConfig{
		Server:      "127.0.0.1",
		PayloadSize: 1024,
		MTU:         1500,
		MSS:         1460,
	}
	mc := metrics.NewMetricsCollector()

	ctx := context.Background()
	pp := newProtocolPort("127.0.0.1", "tcp", serverAddr.Port)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		generateFlow(ctx, testCfg, mc, pp, 0.01)
	}
}

func BenchmarkGenerateUDPFlow(b *testing.B) {
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	require.NoError(b, err)
	defer func() { _ = listener.Close() }()

	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, addr, readErr := listener.ReadFromUDPAddrPort(buf)
			if readErr != nil {
				return
			}
			_, _ = listener.WriteToUDPAddrPort(buf[:n], addr)
		}
	}()

	serverAddr := listener.LocalAddr().(*net.UDPAddr)
	testCfg := &config.ClientConfig{
		Server:      "127.0.0.1",
		PayloadSize: 1024,
		MTU:         1500,
		MSS:         1460,
	}
	mc := metrics.NewMetricsCollector()
	pp := newProtocolPort("127.0.0.1", "udp", serverAddr.Port)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		generateFlow(context.Background(), testCfg, mc, pp, 0.01)
	}
}

func BenchmarkFlowScheduler10K(b *testing.B) {
	const flowsPerRun = 1000
	startedAt := time.Now()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stats := runFlowScheduler(context.Background(), 10_000, flowsPerRun, flowsPerRun, func(context.Context) {})
		if stats.started != flowsPerRun || stats.skipped != 0 {
			b.Fatalf("scheduler started %d flows and skipped %d", stats.started, stats.skipped)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(flowsPerRun*b.N)/time.Since(startedAt).Seconds(), "flows/s")
}
