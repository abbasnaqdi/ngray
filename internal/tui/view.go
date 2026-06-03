package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/abbasnaqdi/ng-ray/internal/version"
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("51"))
	tagStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	keyStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("213"))
	selStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("51"))
	descStyle  = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("109"))
	badge      = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("214")).Padding(0, 1)
	disStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203"))
	spinStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("213"))
	panel      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("51")).Padding(0, 2)
	taskPanel  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("60")).Padding(0, 1)
)

func joinLines(lines []string) string { return strings.Join(lines, "\n") }

func (m Model) View() string {
	if m.screen == screenTask {
		return m.viewTask()
	}
	return m.viewMenu()
}

func (m Model) viewMenu() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("◆ ngray toolkit") + "  " + tagStyle.Render(version.Version) + "\n")

	server := "not provisioned"
	if m.ip != "" {
		server = m.ip
	}
	status := tagStyle.Render(server + "  ·  shoes · xray · sing-box · hysteria2 · fptn")
	if !m.checked {
		status += tagStyle.Render("  ·  scanning…")
	} else if len(m.updates) > 0 {
		var names []string
		for n := range m.updates {
			names = append(names, n)
		}
		sort.Strings(names)
		status += "  " + badge.Render("⬆ "+strings.Join(names, " "))
	} else if m.installed > 0 {
		status += "  " + okStyle.Render("✓ up to date")
	}
	b.WriteString(status + "\n\n")

	for i, it := range m.items {
		dis, reason := m.disabled(it)
		switch {
		case dis:
			b.WriteString(disStyle.Render(fmt.Sprintf(" %s  %-26s %s", it.key, it.name, reason)) + "\n")
		case i == m.cursor:
			b.WriteString(selStyle.Render(fmt.Sprintf(" %s  %s ", it.key, it.name)) + "\n")
		default:
			b.WriteString(fmt.Sprintf(" %s  %s\n", keyStyle.Render(it.key), it.name))
		}
	}

	// guidance for the highlighted item (so tools are never "orphans")
	b.WriteString("\n" + descStyle.Render("→ "+m.items[m.cursor].desc) + "\n")
	b.WriteString(dimStyle.Render("↑/↓ move · enter run · 1-9 jump · q quit"))
	return panel.Render(b.String())
}

func (m Model) viewTask() string {
	var head string
	switch {
	case m.running:
		head = m.spin.View() + " " + titleStyle.Render(m.taskName) + dimStyle.Render("  running…")
	case m.taskErr != nil:
		head = errStyle.Render("✗ "+m.taskName) + "  " + errStyle.Render(m.taskErr.Error())
	default:
		head = okStyle.Render("✓ "+m.taskName) + dimStyle.Render("  done")
	}
	body := taskPanel.Width(maxInt(m.width-2, 20)).Render(m.vp.View())
	foot := dimStyle.Render("esc back · ↑/↓ scroll · q menu")
	return head + "\n" + body + "\n" + foot
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
