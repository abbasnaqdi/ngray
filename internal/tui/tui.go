// Package tui is the interactive ngray-toolkit console: a menu plus in-app
// streaming task views with esc-to-go-back, and a setup wizard that collects
// configuration before running the server provisioning task.
package tui

import (
	"context"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type screen int

const (
	screenMenu screen = iota
	screenSetupForm
	screenDomainForm
	screenCleanup
	screenTask
)

type taskLine string
type taskEnd struct{ err error }

type statusInfo struct {
	installed int
	updates   map[string]string
}

// taskFn streams lines via emit; honor ctx for cancellation.
type taskFn func(ctx context.Context, emit func(string)) error

type item struct {
	key, name, desc string
	exec            []string // non-empty → re-exec (full-screen child process)
	task            taskFn
	wizardBefore    bool // show setup wizard before running this task
	domainBefore    bool // show the domain/proxy form before running this task
	cleanupBefore   bool // show the cleanup checklist before running this task
	disabled        func(m *Model) (bool, string)
}

// Form field indices (navigation order in the setup wizard).
const (
	numWizardCores     = 2 // hysteria2, mieru
	formFieldIP        = 0
	formFieldPort      = 1
	formFieldBBR       = 2
	formFieldDomainOn  = 3 // serve through a Cloudflare-fronted domain too
	formFieldDomainVal = 4 // editable domain, reachable only when domainOn
	formCoreBase       = 5
	formFieldRotate    = 7                                 // rotate the subscription token (opt-in)
	formFieldProceed   = formCoreBase + numWizardCores + 1 // 8
	numFormFields      = formFieldProceed + 1              // 9
)

var wizardCoreLabels = [numWizardCores]string{"hysteria2", "mieru"}

// setupForm holds the editable parameters collected by the setup wizard.
type setupForm struct {
	ipVal     string
	portVal   string
	bbr       bool
	domainOn  bool // also front the subscription through the CDN domain
	domainVal string
	coreOn    [numWizardCores]bool
	rotate    bool // opt-in: new subscription token — old links stop working
	focused   int
	editing   bool   // true while user is typing into a text field
	editBuf   string // in-progress text before commit
}

// Domain field indices (navigation order in the domain/proxy form).
const (
	dfDomain = iota
	dfTarget
	dfToken
	dfDNS
	dfCheck // verify DNS/CF reachability (no token needed)
	dfProceed
	numDomainFields
)

// domainForm collects the Cloudflare / reverse-proxy parameters.
type domainForm struct {
	domain  string
	target  string
	cfToken string
	dns     bool
	focused int
	editing bool
	editBuf string
	hint    string // inline error (e.g. "domain is required"); red, one line
}

type Model struct {
	screen screen
	cursor int
	items  []item
	self   string

	armRegen bool // menu key "9" pressed twice → really wipe credentials

	restartAfter bool // self-update finished → next enter re-launches the binary
	clipDone     bool // one 'y'-copy per task run (screens emit feedback once)

	domain domainForm

	cleanup cleanupForm

	// header status (async)
	ip        string
	installed int
	updates   map[string]string
	checked   bool

	// setup wizard
	form        setupForm
	pendingItem item // item queued behind the wizard

	// task view
	taskName string
	lines    []string
	running  bool
	taskErr  error
	taskCh   chan tea.Msg
	cancel   context.CancelFunc
	vp       viewport.Model
	spin     spinner.Model

	width, height int
}

func Run() error {
	_, err := tea.NewProgram(New(), tea.WithAltScreen()).Run()
	return err
}
