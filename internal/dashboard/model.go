package dashboard

import (
	"time"

	tea "charm.land/bubbletea/v2"
	statusapi "github.com/PhilipSchmid/flow-generator-app/internal/status"
)

var windows = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}

type fetchResult struct {
	snapshot        statusapi.Snapshot
	err             error
	continueRefresh bool
}

type refreshMsg time.Time

// Model is the Bubble Tea dashboard model.
type Model struct {
	client      *Client
	snapshot    *statusapi.Snapshot
	history     history
	connected   bool
	lastError   error
	width       int
	height      int
	windowIndex int
	dark        bool
	color       bool
	showHelp    bool
}

func NewModel(client *Client, color bool) Model {
	return Model{client: client, dark: true, color: color}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.fetch(true), tea.RequestBackgroundColor)
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
	case tea.BackgroundColorMsg:
		m.dark = message.IsDark()
	case tea.KeyPressMsg:
		key := message.String()
		switch key {
		case "q", "ctrl+c", "f10":
			return m, tea.Quit
		}
		if m.showHelp {
			switch key {
			case "esc", "?", "f1":
				m.showHelp = false
			}
			return m, nil
		}
		switch key {
		case "left", "h", "shift+tab":
			if m.windowIndex > 0 {
				m.windowIndex--
			} else if message.String() == "shift+tab" {
				m.windowIndex = len(windows) - 1
			}
		case "right", "l", "tab":
			if m.windowIndex < len(windows)-1 {
				m.windowIndex++
			} else if message.String() == "tab" {
				m.windowIndex = 0
			}
		case "1":
			m.windowIndex = 0
		case "5":
			m.windowIndex = 1
		case "0":
			m.windowIndex = 2
		case "?", "f1":
			m.showHelp = !m.showHelp
		case "r":
			return m, m.fetch(false)
		}
	case fetchResult:
		if message.err != nil {
			m.connected = false
			m.lastError = message.err
		} else {
			m.connected = true
			m.lastError = nil
			m.history.add(message.snapshot)
			copy := message.snapshot
			m.snapshot = &copy
		}
		if message.continueRefresh {
			return m, tea.Tick(time.Second, func(now time.Time) tea.Msg { return refreshMsg(now) })
		}
		return m, nil
	case refreshMsg:
		return m, m.fetch(true)
	}
	return m, nil
}

func (m Model) View() tea.View {
	content := m.render()
	view := tea.NewView(content)
	view.AltScreen = true
	view.WindowTitle = "Flow Generator Dashboard"
	return view
}

func (m Model) fetch(continueRefresh bool) tea.Cmd {
	return func() tea.Msg {
		snapshot, err := m.client.Fetch()
		return fetchResult{snapshot: snapshot, err: err, continueRefresh: continueRefresh}
	}
}

func (m Model) selectedWindow() time.Duration { return windows[m.windowIndex] }

func windowLabel(duration time.Duration) string {
	switch duration {
	case time.Minute:
		return "1m"
	case 5 * time.Minute:
		return "5m"
	case 15 * time.Minute:
		return "15m"
	default:
		return duration.String()
	}
}
