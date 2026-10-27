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
	disabled        func(m *Model) (bool, string)
}

// Form field indices (navigation order in the setup wizard).
const (
	numWizardCores   = 4 // hysteria2, mieru, amneziawg, fptn
	formFieldIP      = 0
	formFieldPort    = 1
	formFieldBBR     = 2
	formCoreBase     = 3
	formFieldProceed = formCoreBase + numWizardCores // 7
	numFormFields    = formFieldProceed + 1          // 8
)

var wizardCoreLabels = [numWizardCores]string{"hysteria2", "mieru", "amneziawg", "fptn"}

// setupForm holds the editable parameters collected by the setup wizard.
type setupForm struct {
	ipVal   string
	portVal string
	bbr     bool
	coreOn  [numWizardCores]bool
	focused int
	editing bool   // true while user is typing into a text field
	editBuf string // in-progress text before commit
}

// Domain field indices (navigation order in the domain/proxy form).
const (
	dfDomain = iota
	dfTarget
	dfToken
	dfDNS
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

	domain domainForm

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
