package dashboard

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
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
	assert.Contains(t, rendered, "Flow Generator")
	assert.Contains(t, rendered, "TARGET")
	assert.Contains(t, rendered, "Flow avg · 1m")
	assert.Contains(t, rendered, "Selected-window distribution")
	assert.NotContains(t, rendered, "vtest")
	assert.Contains(t, rendered, "TCP :8080")
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
	assert.Contains(t, rendered, "Echo RTT")
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

func TestRenderSmallTerminal(t *testing.T) {
	model := Model{width: 40, height: 10, color: false}
	assert.Contains(t, model.render(), "Terminal too small")
}

func TestSparklineDownsamplesToWidth(t *testing.T) {
	values := make([]float64, 100)
	for i := range values {
		values[i] = float64(i)
	}
	line := sparkline(values, 20, 0)
	assert.Equal(t, 20, len([]rune(line)))
	assert.False(t, strings.Contains(line, " "))
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
