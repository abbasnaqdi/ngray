package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/abbasnaqdi/ng-ray/internal/cdnscan/cdn"
	"github.com/abbasnaqdi/ng-ray/internal/cdnscan/network"
)

// ── Screens ───────────────────────────────────────────────────────────────────

type Screen int

const (
	ScreenConfig Screen = iota
	ScreenScan
	ScreenDone
)

// ── Phase per CDN ─────────────────────────────────────────────────────────────

type Phase int

const (
	PhaseIdle Phase = iota
	PhaseDiscovery
	PhaseVerify
	PhaseDone
)

// ── Per-CDN scan state ────────────────────────────────────────────────────────

type CDNState struct {
	Provider    *cdn.Provider
	Enabled     bool // user can toggle this on the config screen
	Phase       Phase
	DiscScanned int64
	DiscFound   int64
	DiscTotal   int64
	VerVerified int64
	VerClean    int64
	VerTotal    int64
}

// ── Log entry ─────────────────────────────────────────────────────────────────

type LogEntry struct {
	CDN     string
	IP      string
	Clean   bool
	Latency time.Duration
	Status  string
}

// ── Messages sent from scanner goroutines to the TUI ─────────────────────────

type MsgPhase struct {
	CDN   string
	Phase Phase
}
type MsgDiscoverStats struct {
	CDN     string
	Scanned int64
	Found   int64
	Total   int64
}
type MsgVerifyResult struct {
	CDN      string
	IP       string
	IsClean  bool
	Latency  time.Duration
	Status   string
	Verified int64
	CleanN   int64
	Total    int64
}
type MsgDone struct{ CDN string }
type MsgAllDone struct{}
type tickMsg time.Time

// ── Config screen cursor layout ───────────────────────────────────────────────
//
//   row 0           : route selection  (space toggles direct/proxy)
//   row 1..N        : CDN checkboxes   (space toggles enabled)
//   row N+1         : [ Start Scan ]   (enter starts)

// ── Main model ────────────────────────────────────────────────────────────────

type Model struct {
	screen Screen
	width  int

	// Config screen state
	ifaces    []network.Interface
	proxyAddr string
	useProxy  bool
	selIface  network.Interface // chosen direct interface
	cdnStates []*CDNState
	cursor    int

	// Scan screen state
	log       []LogEntry
	maxLog    int
	startTime time.Time

	// OnStart is set by main before tea.NewProgram. Called in a goroutine
	// when the user confirms the config screen.
	OnStart func(useProxy bool, iface network.Interface, enabled []*CDNState)

	// VerClean is populated after all CDNs finish, for the post-TUI summary.
	VerClean map[string]int64
}

func New(ifaces []network.Interface, proxy string) Model {
	states := make([]*CDNState, len(cdn.Registry))
	for i, p := range cdn.Registry {
		states[i] = &CDNState{Provider: p, Enabled: true}
	}
	return Model{
		screen:    ScreenConfig,
		ifaces:    ifaces,
		proxyAddr: proxy,
		selIface:  network.PhysicalInterface(ifaces),
		useProxy:  false,
		cdnStates: states,
		cursor:    0,
		maxLog:    16,
		width:     80,
	}
}

// EnabledCDNs returns only the CDN states the user has enabled.
func (m Model) EnabledCDNs() []*CDNState {
	var out []*CDNState
	for _, s := range m.cdnStates {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out
}

func (m *Model) cdnState(name string) *CDNState {
	for _, s := range m.cdnStates {
		if s.Provider.Name == name {
			return s
		}
	}
	return nil
}

// startRow returns the cursor row index for the [ Start Scan ] button.
func (m Model) startRow() int {
	return 1 + len(m.cdnStates)
}

func (m Model) Init() tea.Cmd {
	return tickCmd()
}

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = v.Width

	case tickMsg:
		return m, tickCmd()

	case tea.KeyMsg:
		return m.handleKey(v)

	case MsgPhase:
		if s := m.cdnState(v.CDN); s != nil {
			s.Phase = v.Phase
		}

	case MsgDiscoverStats:
		if s := m.cdnState(v.CDN); s != nil {
			s.DiscScanned = v.Scanned
			s.DiscFound = v.Found
			if v.Total > 0 {
				s.DiscTotal = v.Total
			}
		}

	case MsgVerifyResult:
		if s := m.cdnState(v.CDN); s != nil {
			s.VerVerified = v.Verified
			s.VerClean = v.CleanN
			s.VerTotal = v.Total
		}
		m.log = append(m.log, LogEntry{
			CDN:     v.CDN,
			IP:      v.IP,
			Clean:   v.IsClean,
			Latency: v.Latency,
			Status:  v.Status,
		})
		if len(m.log) > m.maxLog {
			m.log = m.log[1:]
		}

	case MsgDone:
		if s := m.cdnState(v.CDN); s != nil {
			s.Phase = PhaseDone
		}

	case MsgAllDone:
		if m.VerClean == nil {
			m.VerClean = make(map[string]int64)
		}
		for _, s := range m.cdnStates {
			m.VerClean[s.Provider.Name] = s.VerClean
		}
		m.screen = ScreenDone
	}

	return m, nil
}

func (m Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case ScreenConfig:
		return m.configKey(key)
	case ScreenScan, ScreenDone:
		switch key.String() {
		case "q", "Q", "ctrl+c", "esc":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) configKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	totalRows := m.startRow() + 1 // route + CDNs + start button
	switch key.String() {
	case "ctrl+c", "q", "Q":
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j", "tab":
		if m.cursor < totalRows-1 {
			m.cursor++
		}

	case " ": // toggle
		switch {
		case m.cursor == 0: // route row
			m.useProxy = !m.useProxy
		case m.cursor >= 1 && m.cursor <= len(m.cdnStates): // CDN rows
			m.cdnStates[m.cursor-1].Enabled = !m.cdnStates[m.cursor-1].Enabled
		}

	case "enter":
		m.screen = ScreenScan
		m.startTime = time.Now()
		if m.OnStart != nil {
			iface := m.selIface
			if m.useProxy {
				iface = network.Interface{} // empty = no binding, proxy handles routing
			}
			enabled := m.EnabledCDNs()
			go m.OnStart(m.useProxy, iface, enabled)
		}

	case "esc":
		return m, tea.Quit
	}
	return m, nil
}
