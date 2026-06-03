// Package tui is the interactive ngray-toolkit console: a menu plus in-app
// streaming task views (status, sni test, setup, doctor…) with esc-to-go-back.
// Tasks run in-process and stream their output live, never flashing away.
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
	screenTask
)

// streamed task messages
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
	exec            []string // non-empty → re-exec (full-screen child like the scanner)
	task            taskFn   // otherwise → in-app streaming task
	disabled        func(m *Model) (bool, string)
}

type Model struct {
	screen screen
	cursor int
	items  []item
	self   string

	// header status (async)
	ip        string
	installed int
	updates   map[string]string
	checked   bool

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
