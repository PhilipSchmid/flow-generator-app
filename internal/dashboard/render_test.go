package dashboard

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/PhilipSchmid/flow-generator-app/internal/metrics"
	statusapi "github.com/PhilipSchmid/flow-generator-app/internal/status"
	"github.com/stretchr/testify/assert"
)

func TestRenderClientDashboardWithoutColor(t *testing.T) {
	started := time.Now().UTC().Add(-time.Minute)
	first := dashboardSnapshot(started, started.Add(time.Second), 100, 1000, 10)
	second := dashboardSnapshot(started, started.Add(2*time.Second), 120, 1400, 12)
	model := Model{snapshot: &second, connected: true, width: 120, height: 40, color: false, dark: true}
	model.history.add(first)
	model.history.add(second)

	rendered := model.render()
	assert.Contains(t, rendered, "FLOW GENERATOR")
	assert.Contains(t, rendered, "LOAD ATTAINMENT")
	assert.Contains(t, rendered, "ROLLING FLOW AVG · 1m")
	assert.Contains(t, rendered, "WINDOW STATISTICS")
	assert.NotContains(t, rendered, "vtest")
	assert.Contains(t, rendered, "TCP")
	assert.Contains(t, rendered, "8080")
	assert.Len(t, strings.Split(rendered, "\n"), 40)
	for _, line := range strings.Split(rendered, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(line), 120, "line exceeds terminal width: %q", line)
	}
}

func TestRenderMissingLatencyAsUnavailable(t *testing.T) {
	started := time.Now().UTC().Add(-time.Minute)
	first := dashboardSnapshot(started, started.Add(time.Second), 100, 1000, 10)
	second := dashboardSnapshot(started, started.Add(2*time.Second), 120, 1400, 12)
	model := Model{snapshot: &second, connected: true, width: 120, height: 40, color: false, dark: true}
	model.history.add(first)
	model.history.add(second)

	rendered := model.render()
	assert.Contains(t, rendered, "ECHO RTT")
	assert.Contains(t, rendered, "—")
}

func TestCompactConfigurationLine(t *testing.T) {
	started := time.Now().UTC().Add(-time.Minute)
	snapshot := dashboardSnapshot(started, started.Add(time.Second), 100, 1000, 10)
	snapshot.Configuration.PayloadSize = 256
	model := Model{snapshot: &snapshot, connected: true, width: 80, height: 30, color: false, dark: true}

	rendered := model.render()
	assert.Contains(t, rendered, "TCP 8080")
	assert.Contains(t, rendered, "256 B")
	for _, line := range strings.Split(rendered, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(line), 80, "line exceeds terminal width: %q", line)
	}
}

func TestRenderServerRuntimeDetails(t *testing.T) {
	started := time.Now().UTC().Add(-5 * time.Minute)
	first := serverDashboardSnapshot(started, started.Add(time.Second), 100)
	second := serverDashboardSnapshot(started, started.Add(2*time.Second), 140)
	model := Model{snapshot: &second, connected: true, width: 160, height: 48, color: false, dark: true}
	model.history.add(first)
	model.history.add(second)

	rendered := model.render()
	assert.Contains(t, rendered, "ECHO SERVER")
	assert.Contains(t, rendered, "version v1.2.3")
	assert.Contains(t, rendered, "ACCEPTING TRAFFIC")
	assert.Contains(t, rendered, "active client IPs")
	assert.Contains(t, rendered, "TCP connections")
	assert.Contains(t, rendered, "PORT ACTIVITY")
}

func TestRenderSmallTerminal(t *testing.T) {
	model := Model{width: 40, height: 10, color: false}
	assert.Contains(t, model.render(), "Terminal too small")
}

func TestHelpRendersAsCenteredModal(t *testing.T) {
	started := time.Now().UTC().Add(-time.Minute)
	first := dashboardSnapshot(started, started.Add(time.Second), 100, 1000, 10)
	second := dashboardSnapshot(started, started.Add(2*time.Second), 120, 1400, 12)
	model := Model{snapshot: &second, connected: true, width: 120, height: 40, color: false, dark: true, showHelp: true}
	model.history.add(first)
	model.history.add(second)

	lines := strings.Split(model.render(), "\n")
	assert.Len(t, lines, 40)
	assert.Contains(t, strings.Join(lines, "\n"), "DASHBOARD HELP")
	assert.Contains(t, strings.Join(lines, "\n"), "Esc / ? / F1  close")
	assert.Contains(t, lines[1], "FLOW GENERATOR", "modal should overlay rather than replace the dashboard")
	var helpRow int
	for row, line := range lines {
		assert.LessOrEqual(t, lipgloss.Width(line), 120, "line exceeds terminal width: %q", line)
		if strings.Contains(line, "DASHBOARD HELP") {
			helpRow = row
			assert.Greater(t, strings.Index(line, "DASHBOARD HELP"), 20)
		}
	}
	assert.Greater(t, helpRow, 8)
	assert.Less(t, helpRow, 24)
}

func TestSparklineDownsamplesToWidth(t *testing.T) {
	values := make([]float64, 100)
	for i := range values {
		values[i] = float64(i)
	}
	line := sparkline(values, 20, 0, 100)
	assert.Equal(t, 20, len([]rune(line)))
	assert.False(t, strings.Contains(line, " "))
}

func TestSparklineRightAlignsShortHistory(t *testing.T) {
	line := sparkline([]float64{1, 2, 3}, 30, 0, 60)
	assert.Equal(t, 30, len([]rune(line)))
	assert.Equal(t, ' ', []rune(line)[0])
	assert.NotEqual(t, ' ', []rune(line)[29])
}

func TestLineChartUsesRequestedDimensions(t *testing.T) {
	chart := lineChart([]float64{1, 10, 2, 8}, 24, 4, 0, 60)
	lines := strings.Split(chart, "\n")
	assert.Len(t, lines, 4)
	for _, line := range lines {
		assert.Equal(t, 24, lipgloss.Width(line))
	}
	assert.NotContains(t, chart, "●")
	assert.NotContains(t, chart, "│")
	assert.True(t, strings.ContainsFunc(chart, func(r rune) bool {
		return r >= 0x2800 && r <= 0x28ff
	}), "chart should contain a Braille trace")
}

func TestTimeAxisLabelsSelectedWindow(t *testing.T) {
	axis := timeAxis(40, 5*time.Minute)
	assert.Equal(t, 40, lipgloss.Width(axis))
	assert.Contains(t, axis, "−5m")
	assert.Contains(t, axis, "−2m30s")
	assert.True(t, strings.HasSuffix(axis, "now"))
}

func TestTimeRangeSelectorUsesSegmentedTabs(t *testing.T) {
	model := Model{windowIndex: 1}
	colors := newPalette(true, false)
	wide := model.windowSelector(colors, 200)
	assert.Contains(t, wide, "TIME RANGE")
	assert.Contains(t, wide, "‹")
	assert.Contains(t, wide, "1 MIN")
	assert.Contains(t, wide, "5 MIN")
	assert.Contains(t, wide, "15 MIN")
	assert.Contains(t, wide, "›")

	compact := model.windowSelector(colors, 100)
	assert.Contains(t, compact, "1m")
	assert.NotContains(t, compact, "1 MIN")
}

func TestChartBoundsHandleEmptyHistory(t *testing.T) {
	minimum, maximum := chartBounds(nil, 100)
	assert.Zero(t, minimum)
	assert.Equal(t, 100.0, maximum)
}

func TestTimelineSamplesUseTheSelectedWindowScale(t *testing.T) {
	points := timelineSamples([]float64{1, 2, 3}, 120, 60)
	assert.Len(t, points, 3)
	assert.Greater(t, points[0].x, 110)
	assert.Equal(t, 119, points[len(points)-1].x)

	fullWindow := make([]float64, 60)
	for i := range fullWindow {
		fullWindow[i] = float64(i)
	}
	points = timelineSamples(fullWindow, 120, 60)
	assert.Equal(t, 0, points[0].x)
	assert.Equal(t, 119, points[len(points)-1].x)
}

func TestWideDashboardSpacesPercentileColumns(t *testing.T) {
	started := time.Now().UTC().Add(-time.Minute)
	first := dashboardSnapshot(started, started.Add(time.Second), 100, 1000, 10)
	second := dashboardSnapshot(started, started.Add(2*time.Second), 120, 1400, 12)
	model := Model{snapshot: &second, connected: true, width: 200, height: 50, color: false, dark: true}
	model.history.add(first)
	model.history.add(second)

	var header string
	for _, line := range strings.Split(model.render(), "\n") {
		if strings.Contains(line, "METRIC") && strings.Contains(line, "P50") {
			header = line
			break
		}
	}
	assert.NotEmpty(t, header)
	assert.GreaterOrEqual(t, strings.Index(header, "P50")-strings.Index(header, "AVG"), 12)
}

func TestPortColumnsFillWidePanel(t *testing.T) {
	clientHeaders := []string{"PROTO", "PORT", "FLOW/S", "PACKETS/S", "TX", "RX", "FAIL/S"}
	clientWidths := expandedColumnWidths(95, []int{5, 6, 10, 11, 12, 12, 9})
	clientRow := alignedTableRow(clientHeaders, clientWidths)
	assert.Equal(t, 95, lipgloss.Width(clientRow))
	assert.True(t, strings.HasSuffix(clientRow, "FAIL/S"))

	serverHeaders := []string{"PROTO", "PORT", "REQUESTS/S", "TX", "RX"}
	serverWidths := expandedColumnWidths(95, []int{5, 6, 12, 12, 12})
	serverRow := alignedTableRow(serverHeaders, serverWidths)
	assert.Equal(t, 95, lipgloss.Width(serverRow))
	assert.True(t, strings.HasSuffix(serverRow, "RX"))
}

func serverDashboardSnapshot(started, sampled time.Time, requests uint64) statusapi.Snapshot {
	return statusapi.Snapshot{
		SchemaVersion: statusapi.SchemaVersion, Role: "server", Version: "v1.2.3",
		StartedAt: started, SampledAt: sampled, State: "ready",
		Configuration: statusapi.Configuration{TCPPorts: []int{8080}, UDPPorts: []int{9000}, HealthPort: "8082", MetricsPort: "9090"},
		Traffic: metrics.Snapshot{
			TotalRequestsReceived: requests, TotalTCPReceived: requests / 2, TotalUDPReceived: requests / 2,
			ActiveTCPConnections: 7,
			Ports:                []metrics.PortSnapshot{{Protocol: "tcp", Port: "8080", RequestsReceived: requests, BytesReceived: requests * 5, BytesSent: requests * 5}},
		},
		Server: &statusapi.ServerSnapshot{Ready: true, Healthy: true, ActiveTCPClients: 3},
	}
}

func BenchmarkRenderFullDashboard(b *testing.B) {
	started := time.Now().UTC().Add(-15 * time.Minute)
	snapshot := dashboardSnapshot(started, started, 0, 0, 0)
	model := Model{snapshot: &snapshot, connected: true, width: 160, height: 48, color: false, dark: true}
	model.history.previous = &snapshot
	for second := 1; second <= maxHistorySamples; second++ {
		next := dashboardSnapshot(
			started,
			started.Add(time.Duration(second)*time.Second),
			uint64(second*100),
			uint64(second*64_000),
			100,
		)
		next.Client.FlowsCompleted = uint64(maxInt(0, second*100-100))
		next.Client.TCPLatency = latencyWith(uint64(second*10), time.Duration(second*10)*time.Millisecond, time.Millisecond)
		model.history.add(next)
		snapshot = next
	}
	model.snapshot = &snapshot

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = model.render()
	}
}
