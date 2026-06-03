package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/abbasnaqdi/ng-ray/internal/version"
)

// ── Palette ───────────────────────────────────────────────────────────────────

var (
	colorCyan   = lipgloss.Color("51")
	colorPurple = lipgloss.Color("141")
	colorGreen  = lipgloss.Color("42")
	colorRed    = lipgloss.Color("203")
	colorOrange = lipgloss.Color("214")
	colorGray   = lipgloss.Color("244")
	colorDark   = lipgloss.Color("240")
	colorPink   = lipgloss.Color("213")
	colorBlack  = lipgloss.Color("0")

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorCyan)
	accentStyle = lipgloss.NewStyle().Foreground(colorPurple)
	dimStyle    = lipgloss.NewStyle().Foreground(colorGray)
	mutedStyle  = lipgloss.NewStyle().Foreground(colorDark)
	keyStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorPink)
	selStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorBlack).Background(colorCyan)
	disStyle    = lipgloss.NewStyle().Foreground(colorDark)
	okStyle     = lipgloss.NewStyle().Foreground(colorGreen)
	errStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorRed)
	spinStyle   = lipgloss.NewStyle().Foreground(colorPurple)
	badge       = lipgloss.NewStyle().Foreground(colorBlack).Background(colorOrange).Padding(0, 1)

	panel     = lipgloss.NewStyle().Padding(1, 3)
	taskPanel = lipgloss.NewStyle().Padding(0, 2)
	formPanel = lipgloss.NewStyle().Padding(1, 3)
)

func (m Model) View() string {
	switch m.screen {
	case screenTask:
		return m.viewTask()
	case screenSetupForm:
		return m.viewSetupForm()
	default:
		return m.viewMenu()
	}
}

// ── Menu ──────────────────────────────────────────────────────────────────────

func (m Model) viewMenu() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("◈ ng-ray") + "  " + dimStyle.Render(version.Version) + "\n")
	b.WriteString(m.statusLine() + "\n\n")
	b.WriteString(dimStyle.Render(strings.Repeat("─", 54)) + "\n\n")

	for i, it := range m.items {
		b.WriteString(m.menuRow(i, it))
		if i == 3 {
			b.WriteString("\n")
		}
	}

	b.WriteString("\n" + dimStyle.Render(strings.Repeat("─", 54)) + "\n")
	b.WriteString(mutedStyle.Render("  ↑↓ navigate  ·  1-9 jump  ·  enter run  ·  q quit"))
	return panel.Render(b.String())
}

func (m Model) menuRow(i int, it item) string {
	dis, reason := m.disabled(it)
	selected := i == m.cursor

	switch {
	case dis:
		return disStyle.Render(fmt.Sprintf("  %s  %-28s %s\n", it.key, it.name, reason))
	case selected:
		return selStyle.Render(fmt.Sprintf(" %s  %-27s", it.key, it.name)) + "\n"
	default:
		return fmt.Sprintf("  %s  %s\n", keyStyle.Render(it.key), it.name)
	}
}

func (m Model) statusLine() string {
	server := mutedStyle.Render("not provisioned")
	if m.ip != "" {
		server = dimStyle.Render(m.ip)
	}
	if !m.checked {
		return server + "  " + mutedStyle.Render("scanning…")
	}
	if len(m.updates) > 0 {
		var names []string
		for n := range m.updates {
			names = append(names, n)
		}
		sort.Strings(names)
		return server + "  " + badge.Render("⬆ "+strings.Join(names, " "))
	}
	if m.installed > 0 {
		return server + "  " + okStyle.Render("✓ up to date")
	}
	return server
}

// ── Setup wizard form ─────────────────────────────────────────────────────────

func (m Model) viewSetupForm() string {
	f := m.form
	var b strings.Builder

	b.WriteString(titleStyle.Render("◈ Setup Wizard") + "\n")
	b.WriteString(dimStyle.Render("Review settings below. Tab/↑↓ move  ·  enter to edit  ·  space to toggle.") + "\n\n")

	b.WriteString(formTextRow(f, formFieldIP, "Server IP"))
	b.WriteByte('\n')
	b.WriteString(formTextRow(f, formFieldPort, "Nginx port"))
	b.WriteString("\n\n")

	b.WriteString(formToggleRow(f.focused == formFieldBBR, "BBR congestion", f.bbr))
	b.WriteString("\n\n")

	b.WriteString(dimStyle.Render("  Cores:") + "\n")
	for i, label := range wizardCoreLabels {
		b.WriteString(formToggleRow(f.focused == formCoreBase+i, fmt.Sprintf("  %-12s", label), f.coreOn[i]))
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	b.WriteString(mutedStyle.Render("  xray · shoes · sing-box  — configured in config.yml") + "\n")

	b.WriteString("\n" + dimStyle.Render(strings.Repeat("─", 54)) + "\n\n")

	if f.focused == formFieldProceed {
		b.WriteString(selStyle.Render("  ›  Proceed with setup  ") + "\n")
	} else {
		b.WriteString(accentStyle.Render("  ›  Proceed with setup") + "\n")
	}

	b.WriteString("\n" + mutedStyle.Render("  tab/↑↓ navigate  ·  enter edit/run  ·  space toggle  ·  esc back"))
	return formPanel.Render(b.String())
}

func formTextRow(f setupForm, fieldIdx int, label string) string {
	focused := f.focused == fieldIdx
	lbl := fmt.Sprintf("  %-22s", label)

	var val string
	switch fieldIdx {
	case formFieldIP:
		if focused && f.editing {
			val = accentStyle.Render(f.editBuf+"█")
		} else {
			val = dimStyle.Render(f.ipVal)
		}
	case formFieldPort:
		if focused && f.editing {
			val = accentStyle.Render(f.editBuf+"█")
		} else {
			val = dimStyle.Render(f.portVal)
		}
	}

	if focused {
		hint := ""
		if !f.editing {
			hint = mutedStyle.Render("  (enter to edit)")
		}
		return keyStyle.Render("▶") + accentStyle.Render(lbl) + val + hint
	}
	return " " + mutedStyle.Render(lbl) + val
}

func formToggleRow(focused bool, label string, on bool) string {
	lbl := fmt.Sprintf("  %-22s", label)
	var check, status string
	if on {
		check = okStyle.Render("●")
		status = okStyle.Render(" enabled")
	} else {
		check = mutedStyle.Render("○")
		status = mutedStyle.Render(" disabled")
	}
	if focused {
		return keyStyle.Render("▶") + accentStyle.Render(lbl) + check + status
	}
	return " " + mutedStyle.Render(lbl) + check + status
}

// ── Task view ─────────────────────────────────────────────────────────────────

func (m Model) viewTask() string {
	var head string
	switch {
	case m.running:
		head = m.spin.View() + " " + titleStyle.Render(m.taskName) + "  " + dimStyle.Render("running…")
	case m.taskErr != nil:
		head = errStyle.Render("✗ "+m.taskName) + "  " + errStyle.Render(m.taskErr.Error())
	default:
		head = okStyle.Render("✓ "+m.taskName) + "  " + dimStyle.Render("done")
	}
	body := taskPanel.Width(maxInt(m.width-4, 20)).Render(m.vp.View())
	foot := mutedStyle.Render("  esc back  ·  ↑↓ scroll  ·  q quit")
	return head + "\n" + body + "\n" + foot
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func joinLines(lines []string) string { return strings.Join(lines, "\n") }

func (m Model) joinWrapped() string {
	w := m.vp.Width
	if w <= 0 {
		return joinLines(m.lines)
	}
	out := make([]string, 0, len(m.lines))
	for _, l := range m.lines {
		out = append(out, wrapLine(l, w))
	}
	return joinLines(out)
}

func wrapLine(s string, width int) string {
	if len(s) <= width {
		return s
	}
	var sb strings.Builder
	for len(s) > width {
		sb.WriteString(s[:width])
		sb.WriteByte('\n')
		s = s[width:]
	}
	sb.WriteString(s)
	return sb.String()
}
