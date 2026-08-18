package dashboard

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	statusapi "github.com/PhilipSchmid/flow-generator-app/internal/status"
)

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
	rateValues := activityValues(window, snapshot.Role)
	flow := summarize(rateValues)
	tx := summarize(values(window, func(s sample) float64 { return s.BytesTX * 8 }))
	rx := summarize(values(window, func(s sample) float64 { return s.BytesRX * 8 }))
	active := summarize(values(window, func(s sample) float64 { return s.Active }))
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
	meta := fmt.Sprintf("%s · uptime %s", snapshot.Version, compactDuration(uptime))
	if width >= 100 {
		meta += fmt.Sprintf(" · updated %s ago", compactDuration(age))
	}
	header := styled(colors.primary).Bold(true).Render(title) + "  " +
		styled(stateColor).Bold(true).Render("● "+state) + "  " +
		styled(colors.muted).Render(meta)
	if !m.connected {
		header += "  " + styled(colors.danger).Render("last sample")
	}

	var body []string
	body = append(body, header)
	body = append(body, m.configurationLine(snapshot, colors, width))
	body = append(body, m.cards(snapshot, flow, active, colors, width))
	body = append(body, m.windowAverages(snapshot.Role, colors))
	body = append(body, m.windowSelector(colors))

	if width >= 90 && m.height >= 24 {
		chartWidth := width - 4
		if width >= 120 {
			chartWidth = (width - 1) / 2
		}
		rateTitle := "Requests/s"
		reference := 0.0
		if snapshot.Role == "client" {
			rateTitle = "Flow starts/s"
			reference = snapshot.Configuration.Rate
		}
		flowChart := chartPanel(rateTitle, rateValues, chartWidth, reference, formatRate(flow.Current), colors.primary, colors)
		payloadChart := chartPanel("Payload throughput", values(window, func(s sample) float64 { return (s.BytesTX + s.BytesRX) * 8 }), chartWidth, 0, "TX "+formatBits(tx.Current)+" · RX "+formatBits(rx.Current), colors.tx, colors)
		if width >= 120 {
			activeTitle := "Active TCP"
			if snapshot.Role == "client" {
				activeTitle = "Active flows"
			}
			activeChart := chartPanel(activeTitle, values(window, func(s sample) float64 { return s.Active }), chartWidth, 0, formatFloatCount(active.Current), colors.tcp, colors)
			body = append(body, lipgloss.JoinHorizontal(lipgloss.Top, flowChart, " ", activeChart))
			if snapshot.Role == "client" {
				rttValues := latencySeries(window, 0.95)
				rttCurrent := "—"
				if latencyCount > 0 {
					rttCurrent = formatLatency(latency.P95)
				}
				rttChart := chartPanel("Echo RTT p95", rttValues, chartWidth, 0, rttCurrent, colors.udp, colors)
				body = append(body, lipgloss.JoinHorizontal(lipgloss.Top, payloadChart, " ", rttChart))
			} else {
				body = append(body, payloadChart)
			}
		} else {
			body = append(body, flowChart, payloadChart)
		}
	}

	if m.height >= 28 || width < 90 {
		body = append(body, m.distributionTable(snapshot, flow, tx, active, latency, latencyCount, colors, width))
	}
	if errors := m.errorLine(snapshot, colors); errors != "" {
		body = append(body, errors)
	}
	body = append(body, m.portTable(snapshot, colors, width))
	if m.showHelp {
		body = append(body, styled(colors.muted).Render("p50 is the median. Rate percentiles summarize one-second samples; RTT percentiles summarize sampled echoes."))
	}
	body = append(body, styled(colors.muted).Render("←/→ or 1/5/0 window · q quit · ? help"))
	return clipLines(strings.Join(body, "\n"), m.height)
}

func (m Model) healthState(colors palette) (string, interface {
	RGBA() (uint32, uint32, uint32, uint32)
}) {
	if !m.connected {
		return "DISCONNECTED", colors.danger
	}
	if m.snapshot.Server != nil {
		if m.snapshot.Server.Ready && m.snapshot.Server.Healthy {
			return "READY", colors.healthy
		}
		return "NOT READY", colors.warning
	}
	recent := m.history.window(5 * time.Second)
	if len(recent) < 5 {
		return "WARMING UP", colors.warning
	}
	flows := summarize(values(recent, func(s sample) float64 { return s.FlowRate })).Average
	failures := summarize(values(recent, func(s sample) float64 { return s.FailureRate })).Average
	successes := summarize(values(recent, func(s sample) float64 { return s.SuccessRate })).Average
	skipped := summarize(values(recent, func(s sample) float64 { return s.SkippedRate })).Average
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
	label := "Flow avg"
	if role == "server" {
		label = "Request avg"
	}
	parts := []string{label}
	for _, duration := range windows {
		average := summarize(activityValues(m.history.window(duration), role)).Average
		parts = append(parts, fmt.Sprintf("%s %s", windowLabel(duration), formatRate(average)))
	}
	return styled(colors.muted).Render(strings.Join(parts, " · "))
}

func (m Model) cards(snapshot statusapi.Snapshot, flow, active distribution, colors palette, width int) string {
	cardCount := 4
	if width < 80 {
		cardCount = 3
	}
	cardWidth := maxInt(10, width/cardCount-4)
	card := func(label, value, detail string, accent interface {
		RGBA() (uint32, uint32, uint32, uint32)
	}) string {
		content := styled(colors.muted).Render(label) + "\n" + styled(accent).Bold(true).Render(value)
		if detail != "" {
			content += "\n" + styled(colors.muted).Render(detail)
		}
		return lipgloss.NewStyle().Width(cardWidth).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(colors.border).Render(content)
	}
	if snapshot.Client != nil {
		target := snapshot.Configuration.Rate
		attainment := 0.0
		if target > 0 {
			attainment = flow.Current / target * 100
		}
		cards := []string{
			card("TARGET", formatRate(target), "flow starts", colors.primary),
			card("ACHIEVED", formatRate(flow.Current), fmt.Sprintf("%.2f%%", attainment), colors.healthy),
			card("ACTIVE", formatCount(uint64(maxFloat(active.Current, 0))), fmt.Sprintf("of %s", formatCount(uint64(snapshot.Configuration.MaxConcurrent))), colors.tcp),
		}
		if width >= 80 {
			cards = append(cards, card("FAIL / SKIP", fmt.Sprintf("%s / %s", formatCount(snapshot.Client.FlowsFailed), formatCount(snapshot.Client.StartsSkippedAtCapacity)), "lifetime", colors.warning))
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, cards...)
	}
	traffic := snapshot.Traffic
	cards := []string{
		card("TCP ACTIVE", formatCount(uint64(maxInt64(traffic.ActiveTCPConnections, 0))), "connections", colors.tcp),
		card("TCP TOTAL", formatCount(traffic.TotalTCPReceived), "connections", colors.tcp),
		card("UDP TOTAL", formatCount(traffic.TotalUDPReceived), "packets", colors.udp),
	}
	if width >= 80 {
		cards = append(cards, card("ERRORS", formatCount(totalErrors(snapshot.Server.Errors)), "lifetime", colors.danger))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cards...)
}

func (m Model) windowSelector(colors palette) string {
	parts := []string{styled(colors.muted).Render("Window")}
	for i, duration := range windows {
		style := lipgloss.NewStyle().Padding(0, 1).Foreground(colors.muted)
		if i == m.windowIndex {
			style = style.Foreground(colors.text).Background(colors.primary).Bold(true)
		}
		parts = append(parts, style.Render(windowLabel(duration)))
	}
	return strings.Join(parts, " ")
}

func chartPanel(title string, values []float64, width int, reference float64, current string, accent interface {
	RGBA() (uint32, uint32, uint32, uint32)
}, colors palette) string {
	inner := maxInt(width-4, 10)
	content := styled(colors.muted).Render(title) + "  " + styled(accent).Bold(true).Render(current) + "\n" + styled(accent).Render(sparkline(values, inner, reference))
	return lipgloss.NewStyle().Width(inner).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(colors.border).Render(content)
}

func (m Model) distributionTable(snapshot statusapi.Snapshot, flow, tx, active, latency distribution, latencyCount uint64, colors palette, width int) string {
	var builder strings.Builder
	builder.WriteString(styled(colors.muted).Render("Selected-window distribution · " + windowLabel(m.selectedWindow())))
	builder.WriteString("\n")
	builder.WriteString(styled(colors.muted).Render(fmt.Sprintf("%-12s %9s %9s %9s %9s %9s %9s", "METRIC", "AVG", "P50", "P90", "P95", "P99", "MAX")))
	builder.WriteString("\n")
	row := func(label string, value distribution, format func(float64) string) {
		_, _ = fmt.Fprintf(&builder, "%-12s %9s %9s %9s %9s %9s %9s\n", label, format(value.Average), format(value.P50), format(value.P90), format(value.P95), format(value.P99), format(value.Maximum))
	}
	if snapshot.Role == "client" {
		row("Flow/s", flow, formatFloatRate)
	} else {
		row("Requests/s", flow, formatFloatRate)
	}
	row("Payload TX", tx, formatBits)
	row("Active", active, formatFloatCount)
	if snapshot.Role == "client" {
		formatRTT := func(value float64) string { return formatLatency(value) }
		row("Echo RTT", withLatencyThresholds(latency, latencyCount), formatRTT)
		builder.WriteString(styled(colors.muted).Render(fmt.Sprintf("RTT samples: %s", formatCount(latencyCount))))
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(strings.TrimRight(builder.String(), "\n"))
}

func (m Model) portTable(snapshot statusapi.Snapshot, colors palette, width int) string {
	if len(m.history.samples) == 0 {
		return styled(colors.muted).Render("Ports · waiting for rate samples")
	}
	ports := append([]portSample(nil), m.history.samples[len(m.history.samples)-1].Ports...)
	sort.Slice(ports, func(i, j int) bool {
		left := ports[i].BytesTX + ports[i].BytesRX + ports[i].Activity
		right := ports[j].BytesTX + ports[j].BytesRX + ports[j].Activity
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
	builder.WriteString(styled(colors.muted).Render("Ports · ranked by current activity"))
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
		line := fmt.Sprintf("\n%s :%-5s %10s %-9s  TX %10s  RX %10s", strings.ToUpper(port.Protocol), port.Port, formatFloatRate(activity), activityLabel, formatBits(port.BytesTX*8), formatBits(port.BytesRX*8))
		if snapshot.Role == "client" && port.Protocol == "udp" {
			line += fmt.Sprintf("  packets %s/s", formatFloatRate(port.PacketRate))
		}
		if port.Failures > 0 {
			line += fmt.Sprintf("  fail %s/s", formatFloatRate(port.Failures))
		}
		builder.WriteString(styled(protocolColor).Render(line))
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(builder.String())
}

func (m Model) errorLine(snapshot statusapi.Snapshot, colors palette) string {
	var errors statusapi.ErrorCounts
	if snapshot.Client != nil {
		errors = snapshot.Client.Errors
	} else if snapshot.Server != nil {
		errors = snapshot.Server.Errors
	}
	if totalErrors(errors) == 0 {
		return ""
	}
	return styled(colors.danger).Render(fmt.Sprintf(
		"Errors · dial %s · accept %s · read %s · write %s · mismatch %s · MTU %s",
		formatCount(errors.Dial), formatCount(errors.Accept), formatCount(errors.Read),
		formatCount(errors.Write), formatCount(errors.Mismatch), formatCount(errors.MTU),
	))
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
