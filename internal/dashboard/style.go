package dashboard

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

type palette struct {
	primary, tcp, udp, tx, rx color.Color
	healthy, warning, danger  color.Color
	text, muted, border       color.Color
	surface                   color.Color
}

func newPalette(dark, enabled bool) palette {
	if !enabled {
		none := lipgloss.NoColor{}
		return palette{primary: none, tcp: none, udp: none, tx: none, rx: none, healthy: none, warning: none, danger: none, text: none, muted: none, border: none, surface: none}
	}
	choose := lipgloss.LightDark(dark)
	return palette{
		primary: choose(lipgloss.Color("#0F766E"), lipgloss.Color("#5EEAD4")),
		tcp:     choose(lipgloss.Color("#0369A1"), lipgloss.Color("#38BDF8")),
		udp:     choose(lipgloss.Color("#7E22CE"), lipgloss.Color("#A78BFA")),
		tx:      choose(lipgloss.Color("#C2410C"), lipgloss.Color("#FB923C")),
		rx:      choose(lipgloss.Color("#0F766E"), lipgloss.Color("#2DD4BF")),
		healthy: choose(lipgloss.Color("#047857"), lipgloss.Color("#34D399")),
		warning: choose(lipgloss.Color("#B45309"), lipgloss.Color("#FBBF24")),
		danger:  choose(lipgloss.Color("#BE123C"), lipgloss.Color("#FB7185")),
		text:    choose(lipgloss.Color("#0F172A"), lipgloss.Color("#E2E8F0")),
		muted:   choose(lipgloss.Color("#64748B"), lipgloss.Color("#94A3B8")),
		border:  choose(lipgloss.Color("#CBD5E1"), lipgloss.Color("#334155")),
		surface: choose(lipgloss.Color("#F8FAFC"), lipgloss.Color("#111827")),
	}
}

func styled(foreground color.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(foreground)
}
