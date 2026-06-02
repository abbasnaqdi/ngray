// Package tui is the colorful interactive menu shown when ngray runs with no
// arguments. Each selection re-execs the matching ngray subcommand so flows
// that own the terminal (setup prompts, the scanner) compose cleanly.
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/abbasnaqdi/ng-ray/internal/cores"
	"github.com/abbasnaqdi/ng-ray/internal/state"
	"github.com/abbasnaqdi/ng-ray/internal/system"
	"github.com/abbasnaqdi/ng-ray/internal/version"
)

type item struct {
	key  string
	name string
	desc string
	args []string
}

var menu = []item{
	{"1", "Server setup / repair", "install + configure every enabled core", []string{"server", "setup"}},
	{"2", "Client links · QR · subscription", "share links and the subscribe URL", []string{"client", "links", "--qr"}},
	{"3", "Test SNIs", "REALITY fitness + latency", []string{"sni", "test"}},
	{"4", "Scan CDN edges", "find IPs that survive MCI", []string{"scan"}},
	{"5", "Status", "core versions and services", []string{"server", "status"}},
	{"6", "Update cores", "update installed cores", []string{"update"}},
	{"7", "Firewall", "apply the safe ruleset", []string{"firewall", "apply"}},
	{"8", "Doctor", "diagnose cores, nginx, BBR", []string{"doctor"}},
	{"9", "Regenerate credentials", "clear keys and secrets", []string{"regen"}},
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("51"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	selStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("51"))
	keyStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	badgeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	borderStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("51")).Padding(0, 2)
)

type updatesMsg map[string]string

type Model struct {
	cursor  int
	self    string
	ip      string
	updates map[string]string
	checked bool
}

func New() Model {
	self, _ := os.Executable()
	m := Model{self: self}
	if st, err := state.Load(state.Path()); err == nil {
		m.ip = st.IP
	}
	return m
}

func (m Model) Init() tea.Cmd { return checkUpdates }

// checkUpdates compares installed cores against their latest release.
func checkUpdates() tea.Msg {
	ctx := context.Background()
	sys := system.New()
	out := map[string]string{}
	for _, c := range cores.Registry() {
		if ver, ok := c.Installed(ctx, sys); ok {
			if latest, err := sys.LatestVersion(ctx, c.Repo()); err == nil && latest != "" && latest != ver {
				out[c.Name()] = latest
			}
		}
	}
	return updatesMsg(out)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case updatesMsg:
		m.updates, m.checked = msg, true
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "0", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(menu)-1 {
				m.cursor++
			}
		case "enter", " ":
			return m, m.run(menu[m.cursor])
		default:
			for i, it := range menu {
				if it.key == msg.String() {
					m.cursor = i
					return m, m.run(it)
				}
			}
		}
	}
	return m, nil
}

func (m Model) run(it item) tea.Cmd {
	c := exec.Command(m.self, it.args...)
	return tea.ExecProcess(c, func(error) tea.Msg { return nil })
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("◆ ng-ray ◆") + "  " + dimStyle.Render(version.Version))
	b.WriteString("\n")
	server := "not provisioned"
	if m.ip != "" {
		server = m.ip
	}
	line := dimStyle.Render(server + "  ·  shoes · xray · sing-box · hysteria2 · fptn")
	if !m.checked {
		line += dimStyle.Render("  ·  checking updates…")
	} else if len(m.updates) > 0 {
		var names []string
		for n := range m.updates {
			names = append(names, n)
		}
		line += "  " + badgeStyle.Render("⬆ update: "+strings.Join(names, ", "))
	}
	b.WriteString(line + "\n\n")

	for i, it := range menu {
		row := fmt.Sprintf(" %s  %-34s %s", keyStyle.Render(it.key), it.name, dimStyle.Render(it.desc))
		if i == m.cursor {
			row = selStyle.Render(fmt.Sprintf(" %s  %-34s %s ", it.key, it.name, it.desc))
		}
		b.WriteString(row + "\n")
	}
	b.WriteString("\n" + dimStyle.Render("↑/↓ move · enter run · number jump · q quit"))
	return borderStyle.Render(b.String())
}

// Run launches the menu.
func Run() error {
	_, err := tea.NewProgram(New(), tea.WithAltScreen()).Run()
	return err
}
