package tui

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/abbasnaqdi/ng-ray/internal/cf"
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
	f := setupForm{
		ipVal:   ip,
		portVal: strconv.Itoa(cfg.Nginx.Port),
		bbr:     cfg.Server.EnableBBR,
		coreOn: [numWizardCores]bool{
			cfg.Cores.Hysteria2.Enabled,
			cfg.Cores.Mieru.Enabled,
		},
	}
	// Domain prefill: pre-tick only when a domain is configured AND usable
	// (cert/key already on disk), so setup doesn't stall without a token.
	if cdn := cfg.Cores.Xray.CDN; cdn != nil && cdn.Domain != "" {
		f.domainVal = cdn.Domain
		f.domainOn = cdn.Enabled && fileExistsLoose(cdn.CertFile) && fileExistsLoose(cdn.KeyFile)
	}
	return f
}

// fileExistsLoose is a tiny os.Stat probe so the TUI package does not need any
// heavy machinery for the domain prefill decision.
func fileExistsLoose(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func newDomainForm(cfg *config.Config) domainForm {
	d := domainForm{dns: true}
	if cfg == nil {
		return d
	}
	if cdn := cfg.Cores.Xray.CDN; cdn != nil && cdn.Domain != "" {
		d.domain = cdn.Domain
	}
	for _, r := range cfg.ProxyRoutes {
		if d.domain != "" && r.Domain == d.domain && r.Target != "" {
			d.target = r.Target
		}
	}
	d.cfToken = cf.TokenFromEnvOrFile() // masked in the view; never echoed
	return d
}

func New() Model {
	self, _ := os.Executable()
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = spinStyle

	cfg, _ := config.LoadOrDefault(config.Path())

	m := Model{
		self:   self,
		spin:   sp,
		vp:     viewport.New(0, 0),
		form:   newSetupForm(cfg),
		domain: newDomainForm(cfg),
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
		// After a successful self-update the running image is stale; offer a
		// one-key restart (the task view can't swap mid-render).
		if m.taskErr == nil && m.taskName == "Update ngray" {
			m.restartAfter = true
		}
		m.vp.SetContent(m.joinWrapped())
		m.vp.GotoBottom()
		return m, nil

	case tea.KeyMsg:
		switch m.screen {
		case screenTask:
			return m.updateTask(msg)
		case screenSetupForm:
			return m.updateForm(msg)
		case screenDomainForm:
			return m.updateDomainForm(msg)
		case screenCleanup:
			return m.updateCleanup(msg)
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
	// The regen item stays armed only while key "9" is held; any other key
	// (including cursor movement) re-arms the confirm.
	m.armRegen = m.armRegen && msg.String() == "9"
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
		it := m.items[m.cursor]
		if it.key == "9" && !m.armRegen && it.task != nil {
			m.armRegen = true
			return m, nil
		}
		return m.selectItem(it)
	default:
		for i, it := range m.items {
			if it.key == msg.String() {
				m.cursor = i
				if it.task != nil && msg.String() == "9" && !m.armRegen {
					m.armRegen = true
					return m, nil
				}
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
	if it.domainBefore {
		// Keep the in-memory form: visits cache what the user typed (the
		// swp token stays in memory only, the domain/target prefill from
		// config). A fresh prefill happens once at New().
		m.screen = screenDomainForm
		return m, nil
	}
	if it.cleanupBefore {
		m.screen = screenCleanup
		return m, nil
	}
	return m.start(it)
}

// ── Setup wizard form ─────────────────────────────────────────────────────────

// setupRotateSub is set by submitForm and consumed by taskSetup (the wizard is
// a single instance per process, so a package switch is safe and avoids
// threading form state through every item closure).
var setupRotateSub bool

// clipTarget is the URL the 'y' key copies (OSC 52) when a links sheet is on
// screen — set by taskLinks/taskSetup, cleared by other tasks.
var clipTarget string

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
			// Skip the hidden domain-value row while fronting is off.
			if f.focused == formFieldDomainVal && !f.domainOn {
				f.focused--
			}
		}
	case "down", "j", "tab":
		if f.focused < numFormFields-1 {
			f.focused++
			if f.focused == formFieldDomainVal && !f.domainOn {
				f.focused++
			}
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
		case f.focused == formFieldDomainOn:
			f.domainOn = !f.domainOn
		case f.focused == formFieldDomainVal:
			f.editing = true
			f.editBuf = f.domainVal
		case f.focused >= formCoreBase && f.focused < formFieldRotate:
			f.coreOn[f.focused-formCoreBase] = !f.coreOn[f.focused-formCoreBase]
		case f.focused == formFieldRotate:
			f.rotate = !f.rotate
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
		case formFieldDomainVal:
			if v := strings.TrimSpace(strings.ToLower(f.editBuf)); v != "" {
				f.domainVal = v
			}
		}
		f.editing = false
		if msg.String() == "tab" && f.focused < numFormFields-1 {
			f.focused++
		}
	case "esc":
		f.editing = false // discard, keep old value
	case "backspace", "ctrl+h":
		f.editBuf = runesCut(f.editBuf)
	default:
		f.editBuf = appendKey(f.editBuf, msg)
	}
	return m, nil
}

// appendKey appends printable input to buf. Typed characters and an entire
// terminal paste both arrive as KeyRunes (a paste is one multi-rune msg with
// Paste=true); space has its own key type. Control keys land in the enclosing
// switch above and must not be appended verbatim (their String() is "ctrl+a").
func appendKey(buf string, msg tea.KeyMsg) string {
	switch msg.Type {
	case tea.KeyRunes:
		if len(msg.Runes) > 0 {
			// Not msg.String(): for multi-rune input bubbletea renders
			// "[content]" (bracketed display form), which would corrupt
			// pasted values.
			return buf + string(msg.Runes)
		}
	case tea.KeySpace:
		return buf + " "
	}
	return buf
}

// runesCut drops the last *rune* (not byte) so multi-byte characters stay
// intact while editing.
func runesCut(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
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
	// Domain fronting toggle: enable the CDN block (cert+DNS is ensured by
	// the setup task itself when the CF token is cached).
	if m.form.domainOn {
		dom := strings.TrimSuffix(strings.TrimSpace(strings.ToLower(m.form.domainVal)), ".")
		if dot := strings.Count(dom, "."); dom != "" && dot >= 1 {
			cdn := cfg.Cores.Xray.CDN
			if cdn == nil {
				cdn = &config.CDN{CertFile: "/etc/ngray/cf-origin.crt", KeyFile: "/etc/ngray/cf-origin.key", WS: true}
				cfg.Cores.Xray.CDN = cdn
			}
			cdn.Enabled = true
			cdn.Domain = dom
		}
	} else if cfg.Cores.Xray.CDN != nil {
		// unchecked → keep the domain but suspend fronting
		cfg.Cores.Xray.CDN.Enabled = false
	}

	_ = config.Save(config.Path(), cfg) // best-effort; setup re-reads the file
	// The setup task reads this package-level switch right before running.
	setupRotateSub = m.form.rotate
	return m.start(m.pendingItem)
}

// ── Domain / proxy form ───────────────────────────────────────────────────────

func (m Model) updateDomainForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := &m.domain
	if d.editing {
		switch msg.String() {
		case "enter", "tab":
			switch d.focused {
			case dfDomain:
				d.domain = d.editBuf
				d.hint = ""
			case dfTarget:
				d.target = d.editBuf
			case dfToken:
				// A clipboard-copied token usually carries the trailing
				// newline / spaces from the dashboard page; strip it or CF
				// auth fails.
				d.cfToken = strings.TrimSpace(d.editBuf)
			}
			d.editing = false
			if msg.String() == "tab" && d.focused < dfProceed {
				d.focused++
			}
		case "esc":
			d.editing = false
			d.hint = ""
		case "backspace", "ctrl+h":
			d.editBuf = runesCut(d.editBuf)
		default:
			d.editBuf = appendKey(d.editBuf, msg)
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.screen = screenMenu
	case "up", "k", "shift+tab":
		if d.focused > 0 {
			d.focused--
		}
	case "down", "j", "tab":
		if d.focused < numDomainFields-1 {
			d.focused++
		}
	case "enter", " ":
		switch d.focused {
		case dfDomain:
			d.editing, d.editBuf = true, d.domain
		case dfTarget:
			d.editing, d.editBuf = true, d.target
		case dfToken:
			d.editing, d.editBuf = true, d.cfToken
		case dfDNS:
			d.dns = !d.dns
		case dfCheck:
			return m.runDomainCheck()
		case dfProceed:
			return m.submitDomainForm()
		}
	}
	return m, nil
}

// validateDomainField checks values right in the form (before the task runs),
// returning an inline hint instead of a dead stop at CF-apply time.
func validateDomainField(d domainForm) string {
	dom := strings.TrimSpace(strings.ToLower(d.domain))
	switch {
	case dom == "":
		return "domain is required — press enter and type or paste it"
	case strings.Contains(dom, "/") || strings.Contains(dom, " "):
		return dom + " doesn't look like a domain (scheme/paths are not allowed here)"
	case strings.Count(dom, ".") < 1:
		return dom + " is missing a TLD — enter the full zone (e.g. example.com)"
	}
	t := strings.TrimSpace(d.target)
	if t == "" {
		return ""
	}
	if !strings.HasPrefix(t, "http://") && !strings.HasPrefix(t, "https://") {
		return "target must start with http:// or https:// — socks only works inside the cores, nginx proxies plain HTTP(S) URLs"
	}
	host, port := "", ""
	if u, err := url.Parse(t); err == nil {
		host, port = u.Hostname(), u.Port()
	}
	if host == "" {
		return "target URL has no host — e.g. http://127.0.0.1:8443"
	}
	if port == "" && strings.HasPrefix(t, "http") {
		return "" // default ports (80/443) are valid proxy_pass targets
	}
	if n, err := strconv.Atoi(port); err == nil && (n <= 0 || n > 65535) {
		return "target port " + port + " is out of range"
	}
	return ""
}

func (m Model) submitDomainForm() (tea.Model, tea.Cmd) {
	d := m.domain
	d.domain = strings.TrimSpace(strings.ToLower(d.domain))
	d.hint = validateDomainField(d)
	if d.hint != "" {
		if strings.TrimSpace(strings.ToLower(d.domain)) == "" {
			d.focused = dfDomain // focus where the problem is
		}
		m.domain = d
		return m, nil
	}
	m.domain = d
	it := item{name: "Domain / proxy", task: func(ctx context.Context, emit func(string)) error {
		return taskDomain(ctx, emit, d)
	}}
	return m.start(it)
}

// ── Task view ─────────────────────────────────────────────────────────────────

func (m Model) updateTask(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.restartAfter && (msg.String() == "enter" || msg.String() == "r") {
		// Re-exec THIS binary (just replaced on disk) from itself: exec
		// hands the tty to the new image and the old process exits after.
		c := exec.Command(m.self)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		return m, tea.ExecProcess(c, func(error) tea.Msg { return tea.Quit() })
	}
	switch msg.String() {
	case "esc", "backspace", "q":
		if m.cancel != nil {
			m.cancel()
		}
		m.screen = screenMenu
		m.running = false
		return m, checkStatus
	case "y":
		// Copy the primary subscribe URL with OSC 52 (works over SSH in
		// modern terminals/tmux with set-clipboard on).
		if clipTarget != "" && m.clipDone {
			if err := copyOSC52(clipTarget); err != nil {
				m.lines = append(m.lines, errStyle.Render("  ✗ clipboard: "+err.Error()))
			} else {
				m.lines = append(m.lines, okStyle.Render("  ✓ copied to the system clipboard — paste with ctrl+shift+v"))
			}
			m.clipDone = false
		}
		return m, nil
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
	clipTarget = "" // a fresh task clears the copy target; taskLinks re-sets it
	m.clipDone = true
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

// copyOSC52 puts a string on the system clipboard by writing the OSC 52
// sequence straight to /dev/tty — it reaches the local/desktop terminal even
// through SSH, and through tmux when set-clipboard is on.
func copyOSC52(s string) error {
	b64 := base64.StdEncoding.EncodeToString([]byte(s))
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer tty.Close()
	_, err = fmt.Fprintf(tty, "\x1b]52;c;%s\x07", b64)
	return err
}
