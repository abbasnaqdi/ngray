package tui

import (
	"context"
	"os"
	"os/exec"
	"strconv"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/cores"
	"github.com/abbasnaqdi/ng-ray/internal/state"
	"github.com/abbasnaqdi/ng-ray/internal/system"
)

func newSetupForm(cfg *config.Config) setupForm {
	ip := cfg.Server.IP
	if ip == "" {
		ip = "auto"
	}
	return setupForm{
		ipVal:   ip,
		portVal: strconv.Itoa(cfg.Nginx.Port),
		bbr:     cfg.Server.EnableBBR,
		coreOn: [numWizardCores]bool{
			cfg.Cores.Hysteria2.Enabled,
			cfg.Cores.Mieru.Enabled,
			cfg.Cores.AmneziaWG.Enabled,
			cfg.Cores.Fptn.Enabled,
		},
	}
}

func New() Model {
	self, _ := os.Executable()
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = spinStyle

	cfg, _ := config.LoadOrDefault(config.Path())

	m := Model{
		self: self,
		spin: sp,
		vp:   viewport.New(0, 0),
		form: newSetupForm(cfg),
	}
	m.items = buildItems()
	if st, err := state.Load(state.Path()); err == nil {
		m.ip = st.IP
	}
	return m
}

func (m Model) Init() tea.Cmd { return tea.Batch(checkStatus, m.spin.Tick) }

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
		m.vp.Width = msg.Width - 4 // 2px padding each side
		m.vp.Height = msg.Height - 5
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
		// Cap the buffer so a long sweep cannot grow memory or make the
		// per-line re-wrap quadratic.
		if len(m.lines) > 500 {
			m.lines = m.lines[len(m.lines)-500:]
		}
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
		switch m.screen {
		case screenTask:
			return m.updateTask(msg)
		case screenSetupForm:
			return m.updateForm(msg)
		default:
			return m.updateMenu(msg)
		}
	}

	if m.screen == screenTask {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}

// ── Menu ──────────────────────────────────────────────────────────────────────

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
		return m.selectItem(m.items[m.cursor])
	default:
		for i, it := range m.items {
			if it.key == msg.String() {
				m.cursor = i
				return m.selectItem(it)
			}
		}
	}
	return m, nil
}

func (m Model) selectItem(it item) (tea.Model, tea.Cmd) {
	if dis, _ := m.disabled(it); dis {
		return m, nil
	}
	if it.wizardBefore {
		cfg, _ := config.LoadOrDefault(config.Path())
		m.form = newSetupForm(cfg)
		m.pendingItem = it
		m.screen = screenSetupForm
		return m, nil
	}
	return m.start(it)
}

// ── Setup wizard form ─────────────────────────────────────────────────────────

func (m Model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.form.editing {
		return m.updateFormEditing(msg)
	}

	f := &m.form
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.screen = screenMenu
		return m, nil
	case "up", "k", "shift+tab":
		if f.focused > 0 {
			f.focused--
		}
	case "down", "j", "tab":
		if f.focused < numFormFields-1 {
			f.focused++
		}
	case "enter", " ":
		switch {
		case f.focused == formFieldIP || f.focused == formFieldPort:
			f.editing = true
			if f.focused == formFieldIP {
				f.editBuf = f.ipVal
			} else {
				f.editBuf = f.portVal
			}
		case f.focused == formFieldBBR:
			f.bbr = !f.bbr
		case f.focused >= formCoreBase && f.focused < formFieldProceed:
			f.coreOn[f.focused-formCoreBase] = !f.coreOn[f.focused-formCoreBase]
		case f.focused == formFieldProceed:
			return m.submitForm()
		}
	}
	return m, nil
}

func (m Model) updateFormEditing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := &m.form
	switch msg.String() {
	case "enter", "tab":
		switch f.focused {
		case formFieldIP:
			if f.editBuf != "" {
				f.ipVal = f.editBuf
			}
		case formFieldPort:
			if f.editBuf != "" {
				f.portVal = f.editBuf
			}
		}
		f.editing = false
		if msg.String() == "tab" && f.focused < numFormFields-1 {
			f.focused++
		}
	case "esc":
		f.editing = false // discard, keep old value
	case "backspace", "ctrl+h":
		if len(f.editBuf) > 0 {
			f.editBuf = f.editBuf[:len(f.editBuf)-1]
		}
	default:
		if s := msg.String(); len(s) == 1 && s[0] >= 32 && s[0] < 127 {
			f.editBuf += s
		}
	}
	return m, nil
}

func (m Model) submitForm() (tea.Model, tea.Cmd) {
	cfg, _ := config.LoadOrDefault(config.Path())

	if v := m.form.ipVal; v != "" {
		cfg.Server.IP = v
	} else {
		cfg.Server.IP = "auto"
	}
	if port, err := strconv.Atoi(m.form.portVal); err == nil && port > 0 {
		cfg.Nginx.Port = port
	}
	cfg.Server.EnableBBR = m.form.bbr
	cfg.Cores.Hysteria2.Enabled = m.form.coreOn[0]
	cfg.Cores.Mieru.Enabled = m.form.coreOn[1]
	cfg.Cores.AmneziaWG.Enabled = m.form.coreOn[2]
	cfg.Cores.Fptn.Enabled = m.form.coreOn[3]

	_ = config.Save(config.Path(), cfg) // best-effort; setup re-reads the file
	return m.start(m.pendingItem)
}

// ── Task view ─────────────────────────────────────────────────────────────────

func (m Model) updateTask(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace", "q":
		if m.cancel != nil {
			m.cancel()
		}
		m.screen = screenMenu
		m.running = false
		return m, checkStatus
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
