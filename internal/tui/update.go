package tui

import (
	"context"
	"os"
	"os/exec"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/abbasnaqdi/ng-ray/internal/cores"
	"github.com/abbasnaqdi/ng-ray/internal/state"
	"github.com/abbasnaqdi/ng-ray/internal/system"
)

func New() Model {
	self, _ := os.Executable()
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinStyle
	m := Model{self: self, spin: sp, vp: viewport.New(0, 0)}
	m.items = buildItems()
	if st, err := state.Load(state.Path()); err == nil {
		m.ip = st.IP
	}
	return m
}

func (m Model) Init() tea.Cmd { return tea.Batch(checkStatus, m.spin.Tick) }

// checkStatus counts installed cores and which have an update available.
func checkStatus() tea.Msg {
	ctx := context.Background()
	sys := system.New()
	info := statusInfo{updates: map[string]string{}}
	for _, c := range cores.Registry() {
		if ver, ok := c.Installed(ctx, sys); ok {
			info.installed++
			if latest, err := sys.LatestVersion(ctx, c.Repo()); err == nil && latest != "" && latest != ver {
				info.updates[c.Name()] = latest
			}
		}
	}
	return info
}

func waitFor(ch chan tea.Msg) tea.Cmd { return func() tea.Msg { return <-ch } }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.vp.Width = msg.Width - 4
		m.vp.Height = msg.Height - 6
		return m, nil

	case statusInfo:
		m.installed, m.updates, m.checked = msg.installed, msg.updates, true
		return m, nil

	case spinner.TickMsg:
		if m.running {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			return m, cmd
		}
		return m, nil

	case taskLine:
		m.lines = append(m.lines, string(msg))
		m.vp.SetContent(m.joinWrapped())
		m.vp.GotoBottom()
		return m, waitFor(m.taskCh)

	case taskEnd:
		m.running = false
		m.taskErr = msg.err
		m.vp.SetContent(m.joinWrapped())
		m.vp.GotoBottom()
		return m, nil

	case tea.KeyMsg:
		if m.screen == screenTask {
			return m.updateTask(msg)
		}
		return m.updateMenu(msg)
	}

	if m.screen == screenTask {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) updateMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "enter", " ":
		return m.start(m.items[m.cursor])
	default:
		for i, it := range m.items {
			if it.key == msg.String() {
				m.cursor = i
				return m.start(it)
			}
		}
	}
	return m, nil
}

func (m Model) updateTask(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace", "q":
		if m.cancel != nil {
			m.cancel()
		}
		m.screen = screenMenu
		m.running = false
		return m, checkStatus // refresh header after an action
	case "ctrl+c":
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m Model) start(it item) (tea.Model, tea.Cmd) {
	if dis, _ := m.disabled(it); dis {
		return m, nil
	}
	if len(it.exec) > 0 {
		c := exec.Command(m.self, it.exec...)
		return m, tea.ExecProcess(c, func(error) tea.Msg { return checkStatus() })
	}
	ch := make(chan tea.Msg, 256)
	ctx, cancel := context.WithCancel(context.Background())
	m.taskCh, m.cancel = ch, cancel
	m.screen, m.taskName, m.running, m.lines, m.taskErr = screenTask, it.name, true, nil, nil
	m.vp.SetContent("")
	emit := func(s string) { ch <- taskLine(s) }
	go func() { ch <- taskEnd{it.task(ctx, emit)} }()
	return m, tea.Batch(waitFor(ch), m.spin.Tick)
}

func (m Model) disabled(it item) (bool, string) {
	if it.disabled == nil {
		return false, ""
	}
	return it.disabled(&m)
}
