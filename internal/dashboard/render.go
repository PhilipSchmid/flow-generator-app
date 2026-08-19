package dashboard

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	statusapi "github.com/PhilipSchmid/flow-generator-app/internal/status"
)

const (
	payloadBalanceWindow      = 5 * time.Second
	payloadBalanceMinDuration = 3 * time.Second
	payloadBalanceThreshold   = 0.05
)

type payloadBalance struct {
	TX, RX, Total float64
	Gap           float64
	Ready         bool
	Diverged      bool
}

func (m Model) render() string {
	colors := newPalette(m.dark, m.color)
	if m.width > 0 && (m.width < 60 || m.height < 16) {
		return centeredMessage(m.width, m.height, styled(colors.warning).Bold(true).Render("Terminal too small\nResize to at least 60×16"))
	}
	if m.snapshot == nil {
		message := "Connecting to the local flow dashboard status…"
		if m.lastError != nil {
			message += "\n" + m.lastError.Error()
		}
		return centeredMessage(m.width, m.height, styled(colors.muted).Render(message))
	}

	width := m.width
	if width <= 0 {
		width = 100
	}
	snapshot := *m.snapshot
	window := m.history.window(m.selectedWindow())
	flow := summarizeSamples(window, func(s sample) float64 {
		if snapshot.Role == "server" {
			return s.TCPRate + s.UDPRate
		}
		return s.FlowRate
	})
	tx := summarizeSamples(window, func(s sample) float64 { return s.BytesTX * 8 })
	rx := summarizeSamples(window, func(s sample) float64 { return s.BytesRX * 8 })
	io := summarizeSamples(window, func(s sample) float64 { return (s.BytesTX + s.BytesRX) * 8 })
	payload := payloadBalanceFor(m.history.window(payloadBalanceWindow))
	active := summarizeSamples(window, func(s sample) float64 { return s.Active })
	latency, latencyCount := latencySummary(window)
	state, stateColor := m.healthState(colors)

	title := "Echo Server"
	if snapshot.Role == "client" {
		title = "Flow Generator"
	}
	uptime := snapshot.SampledAt.Sub(snapshot.StartedAt).Round(time.Second)
	age := time.Since(snapshot.SampledAt).Round(100 * time.Millisecond)
	if age < 0 {
		age = 0
	}
	var body []string
	body = append(body, m.heading(snapshot, title, state, stateColor, uptime, age, colors, width))
	if snapshot.Role == "client" {
		body = append(body, m.clientLoadPanel(snapshot, flow, active, colors, width))
	} else {
		body = append(body, m.serverSummaryPanel(snapshot, flow, colors, width))
	}
	body = append(body, m.windowBar(snapshot.Role, colors, width))

	if width >= 90 && m.height >= 24 {
		expectedSamples := int(m.selectedWindow() / time.Second)
		chartEnd := snapshot.SampledAt
		if !m.connected {
			chartEnd = time.Now().UTC()
		}
		rateChartValues := chartValues(window, chartEnd, m.selectedWindow(), func(sample sample) (float64, bool) {
			if snapshot.Role == "server" {
				return sample.TCPRate + sample.UDPRate, true
			}
			return sample.FlowRate, true
		})
		ioChartValues := chartValues(window, chartEnd, m.selectedWindow(), func(sample sample) (float64, bool) {
			return (sample.BytesTX + sample.BytesRX) * 8, true
		})
		chartWidth := width - 4
		if width >= 120 {
			chartWidth = (width - 1) / 2
		}
		chartHeight := 1
		if m.height >= 32 {
			chartHeight = 3
		}
		if m.height >= 40 {
			chartHeight = 6
		}
		if m.height >= 50 {
			chartHeight = 8
		}
		if m.height >= 60 {
			chartHeight = 10
		}
		rateTitle := "Requests/s"
		reference := 0.0
		if snapshot.Role == "client" {
			rateTitle = "Flow starts/s"
			reference = snapshot.Configuration.Rate
		}
		flowChart := chartPanel(rateTitle, rateChartValues, chartWidth, chartHeight, reference, expectedSamples, formatRate(flow.Current), formatFloatRate, colors.primary, colors)
		payloadCurrent := formatBits(io.Current) + " · BALANCED"
		payloadAccent := colors.tx
		if !payload.Ready {
			payloadCurrent = formatBits(io.Current) + " · CHECKING"
		} else if payload.Diverged {
			payloadCurrent = fmt.Sprintf("5s TX %s · RX %s · GAP %.1f%%", formatBits(payload.TX), formatBits(payload.RX), payload.Gap*100)
			payloadAccent = colors.danger
		}
		payloadChart := chartPanel("Payload I/O", ioChartValues, chartWidth, chartHeight, 0, expectedSamples, payloadCurrent, formatTableBits, payloadAccent, colors)
		if width >= 120 {
			activeTitle := "Active TCP"
			if snapshot.Role == "client" {
				activeTitle = "Active flows"
			}
			activeChartValues := chartValues(window, chartEnd, m.selectedWindow(), func(sample sample) (float64, bool) { return sample.Active, true })
			activeChart := chartPanel(activeTitle, activeChartValues, chartWidth, chartHeight, 0, expectedSamples, formatFloatCount(active.Current), formatFloatCount, colors.tcp, colors)
			body = append(body, lipgloss.JoinHorizontal(lipgloss.Top, flowChart, " ", activeChart))
			if snapshot.Role == "client" {
				rttValues := chartValues(window, chartEnd, m.selectedWindow(), func(sample sample) (float64, bool) {
					return latencyValue(sample, 0.95)
				})
				rttCurrent := "—"
				if latencyCount > 0 {
					rttCurrent = formatLatency(latency.P95)
				}
				rttChart := chartPanel("Echo RTT p95", rttValues, chartWidth, chartHeight, 0, expectedSamples, rttCurrent, formatLatency, colors.udp, colors)
				body = append(body, lipgloss.JoinHorizontal(lipgloss.Top, payloadChart, " ", rttChart))
			} else {
				body = append(body, chartPanel("Payload I/O", ioChartValues, width, chartHeight, 0, expectedSamples, payloadCurrent, formatTableBits, payloadAccent, colors))
			}
		} else {
			body = append(body, flowChart, payloadChart)
		}
	}

	if errors := m.errorLine(snapshot, colors); errors != "" {
		body = append(body, errors)
	}
	detailWidth := width
	if width >= 90 {
		detailWidth = maxInt(width-4, 10)
	}
	if width >= 160 {
		detailWidth = maxInt((width-1)/2-4, 10)
	}
	distribution := m.distributionTable(snapshot, flow, io, tx, rx, active, latency, latencyCount, payload, colors, detailWidth)
	ports := m.portTable(snapshot, payload, colors, detailWidth)
	if m.height >= 28 || width < 90 {
		switch {
		case width >= 160:
			panelWidth := (width - 1) / 2
			panelHeight := maxInt(lineCount(distribution), lineCount(ports))
			body = append(body, lipgloss.JoinHorizontal(lipgloss.Top,
				sectionPanel(distribution, panelWidth, panelHeight, colors), " ",
				sectionPanel(ports, panelWidth, panelHeight, colors),
			))
		case width >= 90:
			body = append(body,
				sectionPanel(distribution, width, lineCount(distribution), colors),
				sectionPanel(ports, width, lineCount(ports), colors),
			)
		default:
			body = append(body, distribution, ports)
		}
	}
	rendered := withFooter(body, m.footer(colors, width), m.height)
	if m.showHelp {
		return overlayModal(rendered, m.helpModal(colors, width), width, m.height)
	}
	return rendered
}

func (m Model) healthState(colors palette) (string, interface {
	RGBA() (uint32, uint32, uint32, uint32)
}) {
	if !m.connected {
		return "DISCONNECTED", colors.danger
	}
	recent := m.history.window(payloadBalanceWindow)
	payload := payloadBalanceFor(recent)
	if m.snapshot.Server != nil {
		if m.snapshot.Server.Ready && m.snapshot.Server.Healthy {
			if payload.Diverged {
				return "I/O IMBALANCE", colors.danger
			}
			return "READY", colors.healthy
		}
		return "NOT READY", colors.warning
	}
	if strings.EqualFold(m.snapshot.State, "draining") {
		return "DRAINING", colors.warning
	}
	if payload.Diverged {
		return "I/O IMBALANCE", colors.danger
	}
	if sampleCoverage(recent) < payloadBalanceWindow {
		return "WARMING UP", colors.warning
	}
	flows := summarizeSamples(recent, func(s sample) float64 { return s.FlowRate }).Average
	failures := summarizeSamples(recent, func(s sample) float64 { return s.FailureRate }).Average
	successes := summarizeSamples(recent, func(s sample) float64 { return s.SuccessRate }).Average
	skipped := summarizeSamples(recent, func(s sample) float64 { return s.SkippedRate }).Average
	if outcomes := successes + failures; outcomes > 0 && failures/outcomes > 0.01 {
		return "DEGRADED", colors.danger
	}
	if skipped > 0 {
		return "CAPACITY LIMITED", colors.warning
	}
	target := m.snapshot.Configuration.Rate
	if target > 0 && flows >= target*0.95 && flows <= target*1.05 {
		return "ON TARGET", colors.healthy
	}
	if target > 0 && flows < target*0.95 {
		return "BELOW TARGET", colors.warning
	}
	return strings.ToUpper(m.snapshot.State), colors.primary
}

func (m Model) heading(snapshot statusapi.Snapshot, title, state string, stateColor interface {
	RGBA() (uint32, uint32, uint32, uint32)
}, uptime, age time.Duration, colors palette, width int) string {
	inner := maxInt(width-4, 10)
	role := strings.ToUpper(snapshot.Role)
	identity := styled(colors.primary).Bold(true).Render(strings.ToUpper(title)) +
		styled(colors.muted).Render("  /  ") + styled(colors.text).Bold(true).Render(role)
	status := styled(stateColor).Bold(true).Render("● " + state)
	if !m.connected {
		status += styled(colors.danger).Bold(true).Render("  LAST SAMPLE")
	}

	meta := styled(colors.muted).Render("version ") + styled(colors.text).Render(snapshot.Version) + styled(colors.muted).Render(
		fmt.Sprintf("  ·  uptime %s  ·  sample %s ago", compactDuration(uptime), compactDuration(age)),
	)
	configuration := m.configurationLine(snapshot, colors, inner)
	lines := []string{joinSides(identity, status, inner)}
	if width >= 100 {
		lines = append(lines, joinSides(configuration, meta, inner))
	} else {
		lines = append(lines, meta, configuration)
	}
	return lipgloss.NewStyle().Width(maxInt(width, 14)).Padding(0, 1).
		Border(lipgloss.RoundedBorder()).BorderForeground(colors.border).
		Render(strings.Join(lines, "\n"))
}

func (m Model) clientLoadPanel(snapshot statusapi.Snapshot, flow, active distribution, colors palette, width int) string {
	inner := maxInt(width-4, 10)
	target := snapshot.Configuration.Rate
	attainment := 0.0
	if target > 0 {
		attainment = flow.Current / target
	}
	accent := colors.primary
	if attainment >= 0.95 && attainment <= 1.05 {
		accent = colors.healthy
	}
	recent := m.history.window(5 * time.Second)
	recentSkipped := summarizeSamples(recent, func(sample sample) float64 { return sample.SkippedRate }).Average
	if recentSkipped > 0 {
		accent = colors.warning
	}
	if summarizeSamples(recent, func(sample sample) float64 { return sample.FailureRate }).Average > 0 {
		accent = colors.danger
	}
	heading := joinSides(
		styled(colors.muted).Bold(true).Render("LOAD ATTAINMENT"),
		styled(accent).Bold(true).Render(fmt.Sprintf("%.1f%%", attainment*100)),
		inner,
	)
	bar := meterBar(attainment, inner, accent, colors.border)

	activeCount := uint64(maxFloat(active.Current, 0))
	maxConcurrent := uint64(maxInt(snapshot.Configuration.MaxConcurrent, 0))
	headroom := uint64(0)
	if maxConcurrent > activeCount {
		headroom = maxConcurrent - activeCount
	}
	skippedStyle := styled(colors.muted)
	if recentSkipped > 0 {
		skippedStyle = styled(colors.warning).Bold(true)
	}
	rateParts := []string{
		styled(accent).Bold(true).Render(formatRate(flow.Current)) + styled(colors.muted).Render(" started"),
		styled(colors.text).Render(formatRate(target)) + styled(colors.muted).Render(" scheduled"),
		skippedStyle.Render(formatRate(recentSkipped)) + styled(colors.muted).Render(" skipped"),
		styled(colors.tcp).Bold(true).Render(fmt.Sprintf("%s / %s", formatCount(activeCount), formatCount(maxConcurrent))) + styled(colors.muted).Render(" active"),
	}
	if inner >= 100 {
		rateParts = append(rateParts, styled(colors.text).Render(formatCount(headroom))+styled(colors.muted).Render(" headroom"))
	}
	rateLine := strings.Join(rateParts, styled(colors.border).Render("  ·  "))
	lifetimeLine := strings.Join([]string{
		styled(colors.muted).Render("LIFETIME"),
		styled(colors.text).Render(formatCount(snapshot.Client.FlowsStarted)) + styled(colors.muted).Render(" started"),
		styled(colors.healthy).Render(formatCount(snapshot.Client.FlowsCompleted)) + styled(colors.muted).Render(" completed"),
		styled(colors.danger).Render(formatCount(snapshot.Client.FlowsFailed)) + styled(colors.muted).Render(" failed"),
		styled(colors.warning).Render(formatCount(snapshot.Client.StartsSkippedAtCapacity)) + styled(colors.muted).Render(" capacity skips"),
	}, styled(colors.border).Render("  ·  "))
	content := strings.Join([]string{heading, bar, rateLine, lifetimeLine}, "\n")
	return lipgloss.NewStyle().Width(maxInt(width, 14)).Padding(0, 1).
		Border(lipgloss.RoundedBorder()).BorderForeground(colors.border).
		Render(lipgloss.NewStyle().MaxWidth(inner).Render(content))
}

func (m Model) serverSummaryPanel(snapshot statusapi.Snapshot, flow distribution, colors palette, width int) string {
	inner := maxInt(width-4, 10)
	traffic := snapshot.Traffic
	errors := totalErrors(snapshot.Server.Errors)
	line := strings.Join([]string{
		styled(colors.primary).Bold(true).Render(formatRate(flow.Current)) + styled(colors.muted).Render(" requests"),
		styled(colors.tcp).Bold(true).Render(formatCount(uint64(maxInt64(traffic.ActiveTCPConnections, 0)))) + styled(colors.muted).Render(" TCP connections"),
		styled(colors.tcp).Bold(true).Render(formatCount(snapshot.Server.ActiveTCPClients)) + styled(colors.muted).Render(" active client IPs"),
		styled(colors.danger).Render(formatCount(errors)) + styled(colors.muted).Render(" errors"),
	}, styled(colors.border).Render("  ·  "))
	lifetime := strings.Join([]string{
		styled(colors.muted).Render("LIFETIME"),
		styled(colors.text).Render(formatCount(traffic.TotalRequestsReceived)) + styled(colors.muted).Render(" requests"),
		styled(colors.tcp).Render(formatCount(traffic.TotalTCPReceived)) + styled(colors.muted).Render(" TCP"),
		styled(colors.udp).Render(formatCount(traffic.TotalUDPReceived)) + styled(colors.muted).Render(" UDP"),
		styled(colors.text).Render(onOff(snapshot.Configuration.TracingEnabled)) + styled(colors.muted).Render(" tracing"),
	}, styled(colors.border).Render("  ·  "))
	serviceState := styled(colors.warning).Bold(true).Render("NOT READY")
	if snapshot.Server.Ready && snapshot.Server.Healthy {
		serviceState = styled(colors.healthy).Bold(true).Render("ACCEPTING TRAFFIC")
	} else if snapshot.Server.Ready {
		serviceState = styled(colors.danger).Bold(true).Render("UNHEALTHY")
	}
	heading := joinSides(styled(colors.muted).Bold(true).Render("LIVE SERVER"), serviceState, inner)
	content := strings.Join([]string{heading, line, lifetime}, "\n")
	return lipgloss.NewStyle().Width(maxInt(width, 14)).Padding(0, 1).
		Border(lipgloss.RoundedBorder()).BorderForeground(colors.border).
		Render(lipgloss.NewStyle().MaxWidth(inner).Render(content))
}

func (m Model) windowBar(role string, colors palette, width int) string {
	selector := m.windowSelector(colors, width)
	averages := m.windowAverages(role, colors)
	if width < 100 {
		return selector + "\n" + averages
	}
	return joinSides(selector, averages, width)
}

func (m Model) configurationLine(snapshot statusapi.Snapshot, colors palette, width int) string {
	cfg := snapshot.Configuration
	if snapshot.Role == "client" {
		payload := "5 B fallback"
		if cfg.PayloadSize > 0 {
			payload = formatBytes(float64(cfg.PayloadSize))
		}
		if cfg.MinPayloadSize > 0 {
			payload = fmt.Sprintf("%s–%s", formatBytes(float64(cfg.MinPayloadSize)), formatBytes(float64(cfg.MaxPayloadSize)))
		}
		if width < 100 {
			return styled(colors.muted).Render(fmt.Sprintf("%s · %s · TCP %s · UDP %s · %.1f–%.1fs · %s", compactText(cfg.Target, 20), strings.ToUpper(cfg.Protocol), compactPorts(cfg.TCPPorts), compactPorts(cfg.UDPPorts), cfg.MinDuration, cfg.MaxDuration, payload))
		}
		return styled(colors.muted).Render(fmt.Sprintf("%s · %s · TCP %v · UDP %v · duration %.1f–%.1fs · payload %s", cfg.Target, strings.ToUpper(cfg.Protocol), cfg.TCPPorts, cfg.UDPPorts, cfg.MinDuration, cfg.MaxDuration, payload))
	}
	if width < 100 {
		return styled(colors.muted).Render(fmt.Sprintf("TCP %s · UDP %s · health :%s · metrics :%s", compactPorts(cfg.TCPPorts), compactPorts(cfg.UDPPorts), cfg.HealthPort, cfg.MetricsPort))
	}
	return styled(colors.muted).Render(fmt.Sprintf("TCP %v · UDP %v · health :%s · metrics :%s", cfg.TCPPorts, cfg.UDPPorts, cfg.HealthPort, cfg.MetricsPort))
}

func (m Model) windowAverages(role string, colors palette) string {
	label := "ROLLING FLOW AVG"
	if role == "server" {
		label = "ROLLING REQUEST AVG"
	}
	parts := []string{label}
	for _, duration := range windows {
		samples := m.history.window(duration)
		average := summarizeSamples(samples, func(sample sample) float64 {
			if role == "server" {
				return sample.TCPRate + sample.UDPRate
			}
			return sample.FlowRate
		}).Average
		parts = append(parts, fmt.Sprintf("%s %s", windowLabel(duration), formatRate(average)))
	}
	return styled(colors.muted).Render(strings.Join(parts, " · "))
}

func (m Model) windowSelector(colors palette, width int) string {
	labels := []string{"1 MIN", "5 MIN", "15 MIN"}
	if width < 120 {
		labels = []string{"1m", "5m", "15m"}
	}
	segments := make([]string, 0, len(windows))
	for i := range windows {
		style := lipgloss.NewStyle().Padding(0, 1).Foreground(colors.muted)
		if i == m.windowIndex {
			style = style.Foreground(colors.text).Background(colors.primary).Bold(true).Underline(true)
		}
		segments = append(segments, style.Render(labels[i]))
	}
	divider := styled(colors.border).Render("│")
	chevron := func(value string) string { return styled(colors.primary).Bold(true).Render(value) }
	return styled(colors.muted).Bold(true).Render("TIME RANGE") + "  " +
		chevron("‹") + " " + strings.Join(segments, divider) + " " + chevron("›")
}

func chartPanel(title string, values []float64, width, height int, reference float64, expectedSamples int, current string, formatAxis func(float64) string, accent interface {
	RGBA() (uint32, uint32, uint32, uint32)
}, colors palette) string {
	inner := maxInt(width-4, 10)
	label := strings.ToUpper(title)
	if reference > 0 {
		label += "  ·  ┄ TARGET " + formatRate(reference)
	}
	heading := joinSides(styled(colors.muted).Bold(true).Render(label), styled(accent).Bold(true).Render(current), inner)
	chart := sparkline(values, inner, reference, expectedSamples)
	if height > 1 {
		minimum, maximum := chartBounds(latestSamples(values, expectedSamples), reference)
		axisWidth := chartAxisWidth(minimum, maximum, formatAxis)
		plotWidth := maxInt(inner-axisWidth, 10)
		trace := lineChart(values, plotWidth, height-1, reference, expectedSamples)
		chart = chartWithAxes(trace, minimum, maximum, time.Duration(expectedSamples)*time.Second, formatAxis, accent, colors)
	} else {
		chart = styled(accent).Render(chart)
	}
	content := heading + "\n" + chart
	return lipgloss.NewStyle().Width(maxInt(width, 14)).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(colors.border).Render(content)
}

func chartAxisWidth(minimum, maximum float64, format func(float64) string) int {
	middle := minimum + (maximum-minimum)/2
	return maxInt(maxInt(lipgloss.Width(format(minimum)), lipgloss.Width(format(middle))), lipgloss.Width(format(maximum))) + 2
}

func chartWithAxes(trace string, minimum, maximum float64, window time.Duration, format func(float64) string, accent interface {
	RGBA() (uint32, uint32, uint32, uint32)
}, colors palette) string {
	lines := strings.Split(trace, "\n")
	if len(lines) == 0 {
		return trace
	}
	middle := minimum + (maximum-minimum)/2
	labelWidth := chartAxisWidth(minimum, maximum, format) - 2
	labels := map[int]string{0: format(maximum), len(lines) - 1: format(minimum)}
	if len(lines) > 2 {
		labels[len(lines)/2] = format(middle)
	}

	var chart strings.Builder
	for row, line := range lines {
		if row > 0 {
			chart.WriteByte('\n')
		}
		label := labels[row]
		tick := "│"
		if label != "" {
			tick = "┤"
		}
		_, _ = fmt.Fprintf(&chart, "%s%s", styled(colors.muted).Render(fmt.Sprintf("%*s %s", labelWidth, label, tick)), styled(accent).Render(line))
	}
	chart.WriteByte('\n')
	chart.WriteString(styled(colors.muted).Render(strings.Repeat(" ", labelWidth+1) + "└" + timeAxis(lipgloss.Width(lines[0]), window)))
	return chart.String()
}

func timeAxis(width int, window time.Duration) string {
	if width <= 0 {
		return ""
	}
	axis := []rune(strings.Repeat("─", width))
	if window <= 0 {
		placeAxisLabel(axis, "now", maxInt(width-3, 0))
		return string(axis)
	}

	type axisLabel struct {
		text      string
		position  int
		majorTick bool
	}
	step := timeAxisStep(window)
	labels := make([]axisLabel, 0, maxInt(int(window/step)-1, 0))
	for elapsed := step; elapsed < window; elapsed += step {
		position := int(math.Round(float64(elapsed) * float64(width-1) / float64(window)))
		if position > 0 && position < width-1 {
			axis[position] = '┴'
		}
		remaining := window - elapsed
		labels = append(labels, axisLabel{
			text:      negativeDurationLabel(remaining),
			position:  position,
			majorTick: remaining%time.Minute == 0,
		})
	}

	left := negativeDurationLabel(window)
	right := "now"
	placeAxisLabel(axis, left, 0)
	placeAxisLabel(axis, right, width-len([]rune(right)))
	occupied := make([]bool, width)
	markAxisLabel(occupied, 0, len([]rune(left)))
	markAxisLabel(occupied, width-len([]rune(right)), len([]rune(right)))

	// Prefer whole-minute labels when a narrow terminal cannot show every
	// configured tick. Minor ticks remain visible on the axis.
	for _, majorOnly := range []bool{true, false} {
		for _, label := range labels {
			if label.majorTick != majorOnly {
				continue
			}
			labelWidth := len([]rune(label.text))
			preferred := label.position - (labelWidth-1)/2
			if start, ok := axisLabelStart(occupied, preferred, labelWidth); ok {
				placeAxisLabel(axis, label.text, start)
				markAxisLabel(occupied, start, labelWidth)
			}
		}
	}
	return string(axis)
}

func timeAxisStep(window time.Duration) time.Duration {
	switch {
	case window <= time.Minute:
		return 10 * time.Second
	case window <= 5*time.Minute:
		return 30 * time.Second
	default:
		return time.Minute
	}
}

func negativeDurationLabel(duration time.Duration) string {
	minutes := int(duration / time.Minute)
	seconds := int(duration/time.Second) % 60
	if minutes == 0 {
		return fmt.Sprintf("−%ds", seconds)
	}
	if seconds == 0 {
		return fmt.Sprintf("−%dm", minutes)
	}
	return fmt.Sprintf("−%dm%02ds", minutes, seconds)
}

func placeAxisLabel(axis []rune, label string, start int) {
	if start < 0 || start >= len(axis) {
		return
	}
	for i, r := range []rune(label) {
		if start+i >= len(axis) {
			return
		}
		axis[start+i] = r
	}
}

func axisLabelFits(occupied []bool, start, width int) bool {
	if start < 0 || width <= 0 || start+width > len(occupied) {
		return false
	}
	from := maxInt(start-1, 0)
	to := minInt(start+width+1, len(occupied))
	for position := from; position < to; position++ {
		if occupied[position] {
			return false
		}
	}
	return true
}

func axisLabelStart(occupied []bool, preferred, width int) (int, bool) {
	for _, start := range []int{preferred, preferred + 1, preferred - 1} {
		if axisLabelFits(occupied, start, width) {
			return start, true
		}
	}
	return 0, false
}

func markAxisLabel(occupied []bool, start, width int) {
	from := maxInt(start, 0)
	to := minInt(start+width, len(occupied))
	for position := from; position < to; position++ {
		occupied[position] = true
	}
}

func payloadBalanceFor(samples []sample) payloadBalance {
	result := payloadBalance{Ready: sampleCoverage(samples) >= payloadBalanceMinDuration}
	if len(samples) == 0 {
		return result
	}

	var weight, imbalancedFor float64
	for _, sample := range samples {
		tx := sample.BytesTX * 8
		rx := sample.BytesRX * 8
		seconds := sample.Covered.Seconds()
		if seconds <= 0 {
			seconds = 1
		}
		weight += seconds
		result.TX += tx * seconds
		result.RX += rx * seconds
		if payloadGap(tx, rx) >= payloadBalanceThreshold {
			imbalancedFor += seconds
		}
	}
	result.TX /= weight
	result.RX /= weight
	result.Total = result.TX + result.RX
	result.Gap = payloadGap(result.TX, result.RX)
	result.Diverged = result.Ready && imbalancedFor >= payloadBalanceMinDuration.Seconds() && result.Gap >= payloadBalanceThreshold
	return result
}

func payloadGap(tx, rx float64) float64 {
	peak := maxFloat(tx, rx)
	if peak <= 0 {
		return 0
	}
	return math.Abs(tx-rx) / peak
}

func formatPayloadGap(tx, rx float64) string {
	return fmt.Sprintf("%.1f%%", payloadGap(tx, rx)*100)
}

func (m Model) distributionTable(snapshot statusapi.Snapshot, flow, io, tx, rx, active, latency distribution, latencyCount uint64, payload payloadBalance, colors palette, width int) string {
	labelWidth, valueWidth := distributionColumnWidths(width)
	formatRow := func(label string, cells ...string) string {
		var row strings.Builder
		_, _ = fmt.Fprintf(&row, "%-*s", labelWidth, label)
		for _, cell := range cells {
			_, _ = fmt.Fprintf(&row, " %*s", valueWidth, cell)
		}
		return row.String()
	}
	var builder strings.Builder
	builder.WriteString(styled(colors.muted).Bold(true).Render("WINDOW STATISTICS  ·  " + strings.ToUpper(windowLabel(m.selectedWindow()))))
	builder.WriteString("\n")
	builder.WriteString(styled(colors.muted).Render(formatRow("METRIC", "AVG", "P50", "P90", "P95", "P99", "MAX")))
	builder.WriteString("\n")
	row := func(label string, value distribution, format func(float64) string) {
		builder.WriteString(formatRow(label, format(value.Average), format(value.P50), format(value.P90), format(value.P95), format(value.P99), format(value.Maximum)))
		builder.WriteByte('\n')
	}
	if snapshot.Role == "client" {
		row("Flow/s", flow, formatFloatRate)
	} else {
		row("Requests/s", flow, formatFloatRate)
	}
	row("Payload I/O", io, formatTableBits)
	if payload.Diverged {
		row("Payload TX", tx, formatTableBits)
		row("Payload RX", rx, formatTableBits)
	}
	row("Active", active, formatFloatCount)
	if snapshot.Role == "client" {
		formatRTT := func(value float64) string { return formatLatency(value) }
		row("Echo RTT", withLatencyThresholds(latency, latencyCount), formatRTT)
		builder.WriteString(styled(colors.muted).Render(fmt.Sprintf("RTT samples: %s", formatCount(latencyCount))))
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(strings.TrimRight(builder.String(), "\n"))
}

func distributionColumnWidths(width int) (int, int) {
	const valueColumns = 6
	labelWidth := 12
	valueWidth := (width - labelWidth - valueColumns) / valueColumns
	if valueWidth < 8 {
		valueWidth = 8
	}
	if valueWidth > 13 {
		valueWidth = 13
	}
	return labelWidth, valueWidth
}

func (m Model) portTable(snapshot statusapi.Snapshot, payload payloadBalance, colors palette, width int) string {
	if len(m.history.samples) == 0 {
		return styled(colors.muted).Bold(true).Render("PORT ACTIVITY  ·  CURRENT") + "\n" + styled(colors.muted).Render("Waiting for rate samples")
	}
	ports := append([]portSample(nil), m.history.samples[len(m.history.samples)-1].Ports...)
	sort.Slice(ports, func(i, j int) bool {
		left := ports[i].BytesTX + ports[i].BytesRX + ports[i].Activity + ports[i].Failures
		right := ports[j].BytesTX + ports[j].BytesRX + ports[j].Activity + ports[j].Failures
		return left > right
	})
	limit := 6
	if m.height > 42 {
		limit = 12
	}
	if len(ports) < limit {
		limit = len(ports)
	}
	var builder strings.Builder
	builder.WriteString(styled(colors.muted).Bold(true).Render("PORT ACTIVITY  ·  CURRENT"))
	builder.WriteString("\n")
	var columnWidths []int
	if snapshot.Role == "client" {
		if payload.Diverged {
			columnWidths = expandedColumnWidths(width, []int{5, 5, 8, 9, 9, 9, 7, 7})
			builder.WriteString(styled(colors.muted).Render(alignedTableRow([]string{"PROTO", "PORT", "FLOW/S", "PACKETS/S", "TX", "RX", "GAP", "FAIL/S"}, columnWidths)))
		} else {
			columnWidths = expandedColumnWidths(width, []int{5, 6, 10, 11, 14, 9})
			builder.WriteString(styled(colors.muted).Render(alignedTableRow([]string{"PROTO", "PORT", "FLOW/S", "PACKETS/S", "PAYLOAD", "FAIL/S"}, columnWidths)))
		}
	} else {
		if payload.Diverged {
			columnWidths = expandedColumnWidths(width, []int{5, 6, 12, 11, 11, 8})
			builder.WriteString(styled(colors.muted).Render(alignedTableRow([]string{"PROTO", "PORT", "REQUESTS/S", "TX", "RX", "GAP"}, columnWidths)))
		} else {
			columnWidths = expandedColumnWidths(width, []int{5, 6, 12, 14})
			builder.WriteString(styled(colors.muted).Render(alignedTableRow([]string{"PROTO", "PORT", "REQUESTS/S", "PAYLOAD"}, columnWidths)))
		}
	}
	for _, port := range ports[:limit] {
		protocolColor := colors.tcp
		if port.Protocol == "udp" {
			protocolColor = colors.udp
		}
		activityLabel := "conn/s"
		activity := port.Activity
		if snapshot.Role == "client" {
			activityLabel, activity = "flows/s", port.FlowRate
		}
		if snapshot.Role == "server" && port.Protocol == "udp" {
			activityLabel = "packets/s"
		}
		var line string
		if snapshot.Role == "client" {
			packetRate := "—"
			if port.Protocol == "udp" {
				packetRate = formatFloatRate(port.PacketRate)
			}
			if payload.Diverged {
				line = "\n" + alignedTableRow([]string{strings.ToUpper(port.Protocol), port.Port, formatFloatRate(activity), packetRate, formatBits(port.BytesTX * 8), formatBits(port.BytesRX * 8), formatPayloadGap(port.BytesTX, port.BytesRX), formatFloatRate(port.Failures)}, columnWidths)
			} else {
				line = "\n" + alignedTableRow([]string{strings.ToUpper(port.Protocol), port.Port, formatFloatRate(activity), packetRate, formatBits((port.BytesTX + port.BytesRX) * 8), formatFloatRate(port.Failures)}, columnWidths)
			}
		} else {
			if payload.Diverged {
				line = "\n" + alignedTableRow([]string{strings.ToUpper(port.Protocol), port.Port, formatFloatRate(activity) + " " + activityLabel, formatBits(port.BytesTX * 8), formatBits(port.BytesRX * 8), formatPayloadGap(port.BytesTX, port.BytesRX)}, columnWidths)
			} else {
				line = "\n" + alignedTableRow([]string{strings.ToUpper(port.Protocol), port.Port, formatFloatRate(activity) + " " + activityLabel, formatBits((port.BytesTX + port.BytesRX) * 8)}, columnWidths)
			}
		}
		builder.WriteString(styled(protocolColor).Render(line))
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(builder.String())
}

func expandedColumnWidths(width int, minimums []int) []int {
	widths := append([]int(nil), minimums...)
	used := len(widths) - 1
	for _, columnWidth := range widths {
		used += columnWidth
	}
	remaining := width - used
	for remaining > 0 && len(widths) > 1 {
		for column := 1; column < len(widths) && remaining > 0; column++ {
			widths[column]++
			remaining--
		}
	}
	return widths
}

func alignedTableRow(cells []string, widths []int) string {
	if len(cells) == 0 || len(cells) != len(widths) {
		return ""
	}
	var row strings.Builder
	_, _ = fmt.Fprintf(&row, "%-*s", widths[0], cells[0])
	for column := 1; column < len(cells); column++ {
		_, _ = fmt.Fprintf(&row, " %*s", widths[column], cells[column])
	}
	return row.String()
}

func (m Model) errorLine(snapshot statusapi.Snapshot, colors palette) string {
	errors := snapshotErrors(snapshot)
	if totalErrors(errors) == 0 {
		return ""
	}
	lines := make([]string, 0, 2)
	recent := averageErrorRates(m.history.window(5 * time.Second))
	if parts := errorRateParts(recent); len(parts) > 0 {
		lines = append(lines, styled(colors.danger).Bold(true).Render("RECENT ERRORS")+
			styled(colors.muted).Render(" · 5s rolling · ")+
			styled(colors.danger).Render(strings.Join(parts, " · ")))
	}
	lines = append(lines, styled(colors.muted).Render("LIFETIME ERRORS · "+strings.Join(errorCountParts(errors), " · ")))
	return strings.Join(lines, "\n")
}

func averageErrorRates(samples []sample) errorRates {
	var result errorRates
	if len(samples) == 0 {
		return result
	}
	var totalWeight float64
	for _, sample := range samples {
		weight := sample.Covered.Seconds()
		if weight <= 0 {
			weight = 1
		}
		totalWeight += weight
		result.Dial += sample.Errors.Dial * weight
		result.Read += sample.Errors.Read * weight
		result.Write += sample.Errors.Write * weight
		result.Mismatch += sample.Errors.Mismatch * weight
		result.MTU += sample.Errors.MTU * weight
		result.Accept += sample.Errors.Accept * weight
	}
	result.Dial /= totalWeight
	result.Read /= totalWeight
	result.Write /= totalWeight
	result.Mismatch /= totalWeight
	result.MTU /= totalWeight
	result.Accept /= totalWeight
	return result
}

func errorRateParts(rates errorRates) []string {
	parts := make([]string, 0, 6)
	appendRate := func(label string, value float64) {
		if value > 0 {
			parts = append(parts, label+" "+formatRate(value))
		}
	}
	appendRate("dial", rates.Dial)
	appendRate("accept", rates.Accept)
	appendRate("read", rates.Read)
	appendRate("write", rates.Write)
	appendRate("mismatch", rates.Mismatch)
	appendRate("MTU", rates.MTU)
	return parts
}

func errorCountParts(errors statusapi.ErrorCounts) []string {
	parts := make([]string, 0, 6)
	appendCount := func(label string, value uint64) {
		if value > 0 {
			parts = append(parts, label+" "+formatCount(value))
		}
	}
	appendCount("dial", errors.Dial)
	appendCount("accept", errors.Accept)
	appendCount("read", errors.Read)
	appendCount("write", errors.Write)
	appendCount("mismatch", errors.Mismatch)
	appendCount("MTU", errors.MTU)
	return parts
}

func (m Model) helpModal(colors palette, width int) string {
	modalWidth := minInt(maxInt(width-8, 52), 78)
	if modalWidth > width-2 {
		modalWidth = width - 2
	}
	inner := maxInt(modalWidth-6, 24)
	keyWidth := 18
	primaryStyle := styled(colors.primary).Background(colors.surface)
	mutedStyle := styled(colors.muted).Background(colors.surface)
	textStyle := styled(colors.text).Background(colors.surface)
	surfaceStyle := lipgloss.NewStyle().Background(colors.surface)
	space := func(width int) string {
		return surfaceStyle.Render(strings.Repeat(" ", maxInt(width, 0)))
	}
	row := func(keys, action string) string {
		key := primaryStyle.Bold(true).Render(fmt.Sprintf("%-*s", keyWidth, keys))
		return key + space(2) + textStyle.Render(action)
	}
	headerLeft := primaryStyle.Bold(true).Render("DASHBOARD HELP")
	headerRight := mutedStyle.Render("Esc / ? / F1  close")
	header := headerLeft + space(inner-lipgloss.Width(headerLeft)-lipgloss.Width(headerRight)) + headerRight
	content := strings.Join([]string{
		header,
		mutedStyle.Bold(true).Render("KEYS") + space(keyWidth-4+2) + mutedStyle.Bold(true).Render("ACTION"),
		row("← / h", "Previous time range"),
		row("→ / l / Tab", "Next time range"),
		row("1 / 5 / 0", "Select 1m / 5m / 15m"),
		row("r", "Refresh the status sample now"),
		row("q / F10", "Quit; traffic keeps running"),
		mutedStyle.Render("Y ranges auto-scale. The dashed flow line is the configured target."),
	}, "\n")
	return lipgloss.NewStyle().Width(modalWidth).Padding(1, 2).
		Border(lipgloss.DoubleBorder()).BorderForeground(colors.primary).
		Background(colors.surface).
		Render(lipgloss.NewStyle().MaxWidth(inner).Background(colors.surface).Render(content))
}

func overlayModal(background, modal string, width, height int) string {
	modalWidth, modalHeight := lipgloss.Width(modal), lipgloss.Height(modal)
	if height <= 0 {
		height = lipgloss.Height(background)
	}
	x := maxInt((width-modalWidth)/2, 0)
	y := maxInt((height-modalHeight)/2, 0)
	base := lipgloss.NewLayer(background)
	overlay := lipgloss.NewLayer(modal).X(x).Y(y).Z(1)
	return lipgloss.NewCompositor(base, overlay).Render()
}

func (m Model) footer(colors palette, width int) string {
	left := keycap("←→ / Tab", colors) + styled(colors.muted).Render(" range  ") +
		keycap("r", colors) + styled(colors.muted).Render(" refresh  ") +
		keycap("? / F1", colors) + styled(colors.muted).Render(" help  ") +
		keycap("q / F10", colors) + styled(colors.muted).Render(" quit")
	if width < 100 {
		left = keycap("←→", colors) + styled(colors.muted).Render(" range  ") +
			keycap("r", colors) + styled(colors.muted).Render(" refresh  ") +
			keycap("?", colors) + styled(colors.muted).Render(" help  ") +
			keycap("q", colors) + styled(colors.muted).Render(" quit")
	}
	mode := styled(colors.healthy).Bold(true).Render("● LIVE")
	if !m.connected {
		mode = styled(colors.danger).Bold(true).Render("● STALE")
	}
	right := mode + styled(colors.muted).Render("  ·  "+windowLabel(m.selectedWindow()))
	return lipgloss.NewStyle().MaxWidth(width).Render(joinSides(left, right, width))
}

func keycap(label string, colors palette) string {
	return styled(colors.primary).Bold(true).Render("[" + label + "]")
}

func sectionPanel(content string, width, height int, colors palette) string {
	inner := maxInt(width-4, 10)
	content = lipgloss.NewStyle().MaxWidth(inner).Render(content)
	return lipgloss.NewStyle().Width(maxInt(width, 14)).Height(height).Padding(0, 1).
		Border(lipgloss.RoundedBorder()).BorderForeground(colors.border).
		Render(content)
}

func meterBar(value float64, width int, accent, empty interface {
	RGBA() (uint32, uint32, uint32, uint32)
}) string {
	if width <= 0 {
		return ""
	}
	filled := int(math.Round(value * float64(width)))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return styled(accent).Render(strings.Repeat("━", filled)) + styled(empty).Render(strings.Repeat("─", width-filled))
}

func joinSides(left, right string, width int) string {
	if width <= 0 {
		return left + right
	}
	rightWidth := lipgloss.Width(right)
	if rightWidth >= width {
		return lipgloss.NewStyle().MaxWidth(width).Render(right)
	}
	left = lipgloss.NewStyle().MaxWidth(width - rightWidth - 1).Render(left)
	gap := width - lipgloss.Width(left) - rightWidth
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func withFooter(body []string, footer string, height int) string {
	if height <= 0 {
		return strings.Join(append(body, footer), "\n")
	}
	content := strings.Split(clipLines(strings.Join(body, "\n"), maxInt(height-1, 0)), "\n")
	if height == 1 {
		return footer
	}
	for len(content) < height-1 {
		content = append(content, "")
	}
	content = append(content, footer)
	return strings.Join(content, "\n")
}

func lineCount(content string) int {
	if content == "" {
		return 0
	}
	return strings.Count(content, "\n") + 1
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func withLatencyThresholds(value distribution, count uint64) distribution {
	if count == 0 {
		return distribution{Current: -1, Average: -1, P50: -1, P90: -1, P95: -1, P99: -1, Maximum: -1}
	}
	if count < 2 {
		value.P50 = -1
	}
	if count < 10 {
		value.P90 = -1
	}
	if count < 20 {
		value.P95 = -1
	}
	if count < 100 {
		value.P99 = -1
	}
	return value
}

func formatRate(value float64) string      { return formatSI(value, "/s") }
func formatFloatRate(value float64) string { return formatSI(value, "") }
func formatBits(value float64) string      { return formatSI(value, "bit/s") }
func formatTableBits(value float64) string { return formatSI(value, "b/s") }
func formatFloatCount(value float64) string {
	if value < 0 {
		return "—"
	}
	if value < 1000 && value == float64(int64(value)) {
		return fmt.Sprintf("%.0f", value)
	}
	return formatSI(value, "")
}
func formatLatency(value float64) string {
	if value < 0 {
		return "—"
	}
	duration := time.Duration(value)
	if duration < time.Microsecond {
		return fmt.Sprintf("%.0fns", value)
	}
	if duration < time.Millisecond {
		return fmt.Sprintf("%.0fµs", value/float64(time.Microsecond))
	}
	if duration < time.Second {
		return fmt.Sprintf("%.2fms", value/float64(time.Millisecond))
	}
	return fmt.Sprintf("%.2fs", value/float64(time.Second))
}

func formatSI(value float64, suffix string) string {
	units := []string{"", "k", "M", "G", "T"}
	index := 0
	for value >= 1000 && index < len(units)-1 {
		value /= 1000
		index++
	}
	precision := 0
	if value < 100 {
		precision = 1
	}
	if value < 10 {
		precision = 2
	}
	return fmt.Sprintf("%.*f%s%s", precision, value, units[index], suffix)
}

func formatBytes(value float64) string {
	units := []string{"B", "KiB", "MiB", "GiB"}
	index := 0
	for value >= 1024 && index < len(units)-1 {
		value /= 1024
		index++
	}
	if index == 0 || value == float64(uint64(value)) {
		return fmt.Sprintf("%.0f %s", value, units[index])
	}
	return fmt.Sprintf("%.1f %s", value, units[index])
}

func compactPorts(ports []int) string {
	if len(ports) == 0 {
		return "—"
	}
	if len(ports) == 1 {
		return fmt.Sprintf("%d", ports[0])
	}
	return fmt.Sprintf("%d+%d", ports[0], len(ports)-1)
}

func compactText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}

func formatCount(value uint64) string {
	if value < 1000 {
		return fmt.Sprintf("%d", value)
	}
	return formatSI(float64(value), "")
}
func compactDuration(value time.Duration) string {
	if value < time.Second {
		return value.String()
	}
	if value < time.Minute {
		return fmt.Sprintf("%ds", int(value.Seconds()))
	}
	if value < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(value.Minutes()), int(value.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(value.Hours()), int(value.Minutes())%60)
}

func totalErrors(errors statusapi.ErrorCounts) uint64 {
	return errors.Dial + errors.Read + errors.Write + errors.Mismatch + errors.MTU + errors.Accept
}
func centeredMessage(width, height int, message string) string {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, message)
}
func clipLines(content string, height int) string {
	if height <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	if len(lines) <= height {
		return content
	}
	return strings.Join(lines[:height], "\n")
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
