package dashboard

import (
	"image/color"
	"math"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/PhilipSchmid/flow-generator-app/internal/metrics"
	statusapi "github.com/PhilipSchmid/flow-generator-app/internal/status"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestCapacityLimitedDashboardExplainsMissingStarts(t *testing.T) {
	started := time.Now().UTC().Add(-time.Minute)
	first := dashboardSnapshot(started, started.Add(time.Second), 100, 1000, 195)
	second := dashboardSnapshot(started, started.Add(2*time.Second), 197, 1400, 197)
	first.Configuration.Rate, second.Configuration.Rate = 100, 100
	first.Configuration.MaxConcurrent, second.Configuration.MaxConcurrent = 200, 200
	second.Client.StartsSkippedAtCapacity = 3
	model := Model{snapshot: &second, connected: true, width: 200, height: 50, color: false, dark: true}
	model.history.add(first)
	model.history.add(second)

	rendered := model.render()
	assert.Contains(t, rendered, "97.0/s")
	assert.Contains(t, rendered, " started")
	assert.Contains(t, rendered, "100/s scheduled")
	assert.Contains(t, rendered, "3.00/s")
	assert.Contains(t, rendered, " skipped")
	assert.Contains(t, rendered, "┄ TARGET 100/s")
	assert.NotContains(t, rendered, "AUTO RANGE")
}

func TestSignedDashboardCountsClampAtZero(t *testing.T) {
	started := time.Now().UTC().Add(-time.Minute)
	client := dashboardSnapshot(started, started.Add(time.Second), 100, 1000, 0)
	client.Configuration.MaxConcurrent = -1
	colors := newPalette(true, false)

	clientPanel := (Model{}).clientLoadPanel(client, distribution{}, distribution{Current: -1}, colors, 120)
	assert.Equal(t, "0", formatIntCount(-1))
	assert.Contains(t, clientPanel, "0 / 0")
	assert.Contains(t, clientPanel, "0 headroom")

	server := serverDashboardSnapshot(started, started.Add(time.Second), 100)
	server.Traffic.ActiveTCPConnections = -1
	serverPanel := (Model{}).serverSummaryPanel(server, distribution{}, colors, 120)
	assert.Equal(t, "0", formatInt64Count(-1))
	assert.Contains(t, serverPanel, "TCP connections")
}

func TestPayloadIORemainsCombinedWhileBalanced(t *testing.T) {
	started := time.Now().UTC().Add(-time.Minute)
	first := dashboardSnapshot(started, started, 0, 0, 0)
	model := Model{connected: true, width: 200, height: 50, color: false, dark: true}
	model.history.add(first)
	var latest statusapi.Snapshot
	for second := 1; second <= 5; second++ {
		latest = dashboardSnapshot(started, started.Add(time.Duration(second)*time.Second), uint64(second*100), uint64(second*1000), 10)
		model.history.add(latest)
	}
	model.snapshot = &latest

	rendered := model.render()
	assert.Contains(t, rendered, "PAYLOAD I/O")
	assert.Contains(t, rendered, "BALANCED")
	assert.NotContains(t, rendered, "Payload TX")
	assert.NotContains(t, rendered, "Payload RX")
}

func TestPayloadIODisclosesSustainedImbalance(t *testing.T) {
	started := time.Now().UTC().Add(-time.Minute)
	first := dashboardSnapshot(started, started, 0, 0, 0)
	model := Model{connected: true, width: 200, height: 50, color: false, dark: true}
	model.history.add(first)
	var latest statusapi.Snapshot
	for second := 1; second <= 5; second++ {
		latest = dashboardSnapshot(started, started.Add(time.Duration(second)*time.Second), uint64(second*100), uint64(second*1000), 10)
		latest.Traffic.Ports[0].BytesReceived = uint64(second * 400)
		model.history.add(latest)
	}
	model.snapshot = &latest

	rendered := model.render()
	assert.Contains(t, rendered, "I/O IMBALANCE")
	assert.Contains(t, rendered, "5s TX")
	assert.Contains(t, rendered, "GAP 60.0%")
	assert.Contains(t, rendered, "Payload TX")
	assert.Contains(t, rendered, "Payload RX")
}

func TestHistoricalErrorsAreMutedAndLabeledAsLifetime(t *testing.T) {
	started := time.Now().UTC().Add(-time.Minute)
	first := dashboardSnapshot(started, started.Add(time.Second), 100, 1000, 10)
	second := dashboardSnapshot(started, started.Add(2*time.Second), 120, 1400, 12)
	first.Client.Errors = statusapi.ErrorCounts{Dial: 2650, Read: 53500}
	second.Client.Errors = first.Client.Errors
	model := Model{snapshot: &second, connected: true, width: 200, height: 50, color: false, dark: true}
	model.history.add(first)
	model.history.add(second)

	rendered := model.render()
	assert.Contains(t, rendered, "LIFETIME ERRORS · dial 2.65k · read 53.5k")
	assert.NotContains(t, rendered, "RECENT ERRORS")
	assert.NotContains(t, rendered, "accept 0")
	assert.NotContains(t, rendered, "write 0")
}

func TestActiveErrorsShowRollingRatesBeforeLifetimeTotals(t *testing.T) {
	started := time.Now().UTC().Add(-time.Minute)
	first := dashboardSnapshot(started, started.Add(time.Second), 100, 1000, 10)
	second := dashboardSnapshot(started, started.Add(2*time.Second), 120, 1400, 12)
	second.Client.Errors = statusapi.ErrorCounts{Dial: 2, Read: 20}
	model := Model{snapshot: &second, connected: true, width: 200, height: 50, color: false, dark: true}
	model.history.add(first)
	model.history.add(second)

	rendered := model.render()
	recent := strings.Index(rendered, "RECENT ERRORS")
	lifetime := strings.Index(rendered, "LIFETIME ERRORS")
	assert.GreaterOrEqual(t, recent, 0)
	assert.Greater(t, lifetime, recent)
	assert.Contains(t, rendered, "5s rolling · dial 2.00/s · read 20.0/s")
	assert.Contains(t, rendered, "LIFETIME ERRORS · dial 2 · read 20")
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

func TestHelpModalUsesUniformBackground(t *testing.T) {
	colors := newPalette(true, true)
	modal := (Model{}).helpModal(colors, 200)
	canvas := lipgloss.NewCanvas(lipgloss.Width(modal), lipgloss.Height(modal))
	canvas.Compose(lipgloss.NewLayer(modal))
	expected := colorRGBA(colors.surface)

	for _, point := range []struct {
		name string
		x    int
		y    int
	}{
		{name: "padding", x: 2, y: 1},
		{name: "title", x: 3, y: 2},
		{name: "header gap", x: 20, y: 2},
		{name: "heading gap", x: 10, y: 3},
		{name: "key", x: 3, y: 4},
		{name: "column gap", x: 21, y: 4},
		{name: "action", x: 23, y: 4},
		{name: "row tail", x: 50, y: 4},
		{name: "note", x: 3, y: 9},
	} {
		cell := canvas.CellAt(point.x, point.y)
		assert.NotNil(t, cell, point.name)
		assert.Equal(t, expected, colorRGBA(cell.Style.Bg), point.name)
	}
}

func colorRGBA(value color.Color) [4]uint32 {
	if value == nil {
		return [4]uint32{}
	}
	r, g, b, a := value.RGBA()
	return [4]uint32{r, g, b, a}
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

func TestTimeAxisScalesLabelsWithSelectedWindow(t *testing.T) {
	tests := []struct {
		name     string
		window   time.Duration
		expected []string
	}{
		{name: "one minute", window: time.Minute, expected: []string{"−1m", "−50s", "−40s", "−30s", "−20s", "−10s", "now"}},
		{name: "five minutes", window: 5 * time.Minute, expected: []string{"−5m", "−4m30s", "−4m", "−3m30s", "−3m", "−2m30s", "−2m", "−1m30s", "−1m", "−30s", "now"}},
		{name: "fifteen minutes", window: 15 * time.Minute, expected: []string{"−15m", "−14m", "−13m", "−12m", "−11m", "−10m", "−9m", "−8m", "−7m", "−6m", "−5m", "−4m", "−3m", "−2m", "−1m", "now"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			axis := timeAxis(90, tt.window)
			assert.Equal(t, 90, lipgloss.Width(axis))
			for _, label := range tt.expected {
				assert.Contains(t, axis, label)
			}
		})
	}
}

func TestTimeAxisKeepsTicksWhenLabelsDoNotFit(t *testing.T) {
	axis := timeAxis(24, 5*time.Minute)
	assert.Equal(t, 24, lipgloss.Width(axis))
	assert.True(t, strings.HasPrefix(axis, "−5m"))
	assert.True(t, strings.HasSuffix(axis, "now"))
	assert.Contains(t, axis, "┴")
}

func TestTimeAxisKeepsMinuteLabelsOnNarrowerCharts(t *testing.T) {
	labels := []string{"−15m", "−14m", "−13m", "−12m", "−11m", "−10m", "−9m", "−8m", "−7m", "−6m", "−5m", "−4m", "−3m", "−2m", "−1m", "now"}
	for width := 78; width <= 90; width++ {
		axis := timeAxis(width, 15*time.Minute)
		for _, label := range labels {
			assert.Contains(t, axis, label, "width %d", width)
		}
	}
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

func TestPayloadBalanceRequiresSustainedDifference(t *testing.T) {
	balanced := payloadBalanceFor([]sample{
		{BytesTX: 100, BytesRX: 100},
		{BytesTX: 100, BytesRX: 100},
		{BytesTX: 100, BytesRX: 100},
	})
	assert.True(t, balanced.Ready)
	assert.False(t, balanced.Diverged)
	assert.Zero(t, balanced.Gap)
	assert.Equal(t, float64(1600), balanced.Total)

	oneSpike := payloadBalanceFor([]sample{
		{BytesTX: 100, BytesRX: 50},
		{BytesTX: 100, BytesRX: 100},
		{BytesTX: 100, BytesRX: 100},
	})
	assert.False(t, oneSpike.Diverged)

	sustained := payloadBalanceFor([]sample{
		{BytesTX: 100, BytesRX: 50},
		{BytesTX: 100, BytesRX: 50},
		{BytesTX: 100, BytesRX: 50},
	})
	assert.True(t, sustained.Diverged)
	assert.InDelta(t, 0.5, sustained.Gap, 0.001)
}

func TestPayloadBalanceUsesElapsedCoverage(t *testing.T) {
	result := payloadBalanceFor([]sample{
		{Covered: 100 * time.Millisecond, BytesTX: 100, BytesRX: 50},
		{Covered: 100 * time.Millisecond, BytesTX: 100, BytesRX: 50},
		{Covered: 100 * time.Millisecond, BytesTX: 100, BytesRX: 50},
	})
	assert.False(t, result.Ready)
	assert.False(t, result.Diverged)
}

func TestHealthStatePrefersDraining(t *testing.T) {
	model := Model{
		connected: true,
		snapshot:  &statusapi.Snapshot{State: "draining", Client: &statusapi.ClientSnapshot{}},
	}
	state, _ := model.healthState(newPalette(true, false))
	assert.Equal(t, "DRAINING", state)
}

func TestFooterDoesNotClaimLiveWhileDisconnected(t *testing.T) {
	footer := (Model{connected: false}).footer(newPalette(true, false), 120)
	assert.Contains(t, footer, "● STALE")
	assert.NotContains(t, footer, "● LIVE")
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

func TestTimelineSamplesBreakAcrossMissingValues(t *testing.T) {
	points := timelineSamples([]float64{1, math.NaN(), 2}, 20, 3)
	require.Len(t, points, 2)
	assert.False(t, points[1].connect)
}

func TestChartValuesUseSampleTimestamps(t *testing.T) {
	end := time.Now().UTC().Truncate(time.Second)
	series := chartValues([]sample{
		{At: end.Add(-10 * time.Second), FlowRate: 10},
		{At: end, FlowRate: 20},
	}, end, time.Minute, func(sample sample) (float64, bool) { return sample.FlowRate, true })
	require.Len(t, series, 60)
	assert.True(t, math.IsNaN(series[0]))
	assert.Equal(t, float64(10), series[49])
	assert.Equal(t, float64(20), series[59])
}

func TestChartValuesKeepEveryRegularWindowSample(t *testing.T) {
	end := time.Now().UTC().Truncate(time.Second)
	samples := make([]sample, 60)
	for index := range samples {
		samples[index] = sample{At: end.Add(-time.Duration(59-index) * time.Second), FlowRate: float64(index + 1)}
	}
	series := chartValues(samples, end, time.Minute, func(sample sample) (float64, bool) { return sample.FlowRate, true })
	require.Len(t, series, 60)
	for index, value := range series {
		assert.Equal(t, float64(index+1), value)
	}
}

func TestChartValuesToleratePollingJitter(t *testing.T) {
	end := time.Now().UTC().Truncate(time.Second)
	series := chartValues([]sample{
		{At: end.Add(-1001 * time.Millisecond), FlowRate: 10},
		{At: end, FlowRate: 20},
	}, end, time.Minute, func(sample sample) (float64, bool) { return sample.FlowRate, true })
	require.Len(t, series, 60)
	assert.Equal(t, float64(10), series[58])
	assert.Equal(t, float64(20), series[59])
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
	clientHeaders := []string{"PROTO", "PORT", "FLOW/S", "PACKETS/S", "PAYLOAD", "FAIL/S"}
	clientWidths := expandedColumnWidths(95, []int{5, 6, 10, 11, 14, 9})
	clientRow := alignedTableRow(clientHeaders, clientWidths)
	assert.Equal(t, 95, lipgloss.Width(clientRow))
	assert.True(t, strings.HasSuffix(clientRow, "FAIL/S"))

	serverHeaders := []string{"PROTO", "PORT", "REQUESTS/S", "PAYLOAD"}
	serverWidths := expandedColumnWidths(95, []int{5, 6, 12, 14})
	serverRow := alignedTableRow(serverHeaders, serverWidths)
	assert.Equal(t, 95, lipgloss.Width(serverRow))
	assert.True(t, strings.HasSuffix(serverRow, "PAYLOAD"))

	alertHeaders := []string{"PROTO", "PORT", "FLOW/S", "PACKETS/S", "TX", "RX", "GAP", "FAIL/S"}
	alertWidths := expandedColumnWidths(95, []int{5, 5, 8, 9, 9, 9, 7, 7})
	alertRow := alignedTableRow(alertHeaders, alertWidths)
	assert.Equal(t, 95, lipgloss.Width(alertRow))
	assert.Contains(t, alertRow, "GAP")
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
