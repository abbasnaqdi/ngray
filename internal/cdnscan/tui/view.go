package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/abbasnaqdi/ng-ray/internal/cdnscan/scanner"
)

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	red    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	cyan   = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	dim    = lipgloss.NewStyle().Faint(true)
	bold   = lipgloss.NewStyle().Bold(true)
	gray   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	sel    = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true).Background(lipgloss.Color("17"))
)

// CDN label colors
var cdnColors = map[string]lipgloss.Style{
	"Akamai":     lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
	"Cloudflare": lipgloss.NewStyle().Foreground(lipgloss.Color("208")),
	"Fastly":     lipgloss.NewStyle().Foreground(lipgloss.Color("46")),
	"ArvanCloud": lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
	"Gcore":      lipgloss.NewStyle().Foreground(lipgloss.Color("141")),
	"CloudFront": lipgloss.NewStyle().Foreground(lipgloss.Color("220")),
}

func cdnLabel(name string) string {
	if s, ok := cdnColors[name]; ok {
		return s.Render(name)
	}
	return cyan.Render(name)
}

// ── View dispatch ─────────────────────────────────────────────────────────────

func (m Model) View() string {
	switch m.screen {
	case ScreenConfig:
		return m.viewConfig()
	case ScreenScan:
		return m.viewScan()
	case ScreenDone:
		return m.viewDone()
	}
	return ""
}

// ── Config screen ─────────────────────────────────────────────────────────────

func (m Model) viewConfig() string {
	w := m.width
	if w < 60 {
		w = 80
	}

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(bold.Render("  CDN Fronting Scanner") + "\n\n")

	// ── Network info ──────────────────────────────────────────────────────────
	b.WriteString(dim.Render("  Network") + "\n")
	for _, i := range m.ifaces {
		tag := ""
		if i.IsVPN {
			tag = "  " + yellow.Render("VPN")
		}
		b.WriteString(fmt.Sprintf("    %-16s %s%s\n", gray.Render(i.Name), i.Addr, tag))
	}
	if m.proxyAddr != "" {
		b.WriteString(fmt.Sprintf("    %-16s %s\n", gray.Render("proxy"), dim.Render(m.proxyAddr)))
	}
	b.WriteString("\n")

	// ── Row 0: Route selection ────────────────────────────────────────────────
	rowRoute := renderConfigRow(m, 0, routeLabel(m))
	b.WriteString(rowRoute + "\n\n")

	// ── Rows 1..N: CDN checkboxes ─────────────────────────────────────────────
	b.WriteString(dim.Render("  CDNs to scan:") + "\n")
	for i, s := range m.cdnStates {
		row := 1 + i
		check := green.Render("[✓]")
		if !s.Enabled {
			check = gray.Render("[ ]")
		}
		var total int64
		for _, cidr := range s.Provider.Ranges {
			n, _ := scanner.CountHosts(cidr)
			total += n
		}
		label := fmt.Sprintf("  %s  %-12s  %d ranges · %s IPs  →  %s",
			check,
			cdnLabel(s.Provider.Name),
			len(s.Provider.Ranges),
			fmtNum(total),
			dim.Render(s.Provider.OutputFile),
		)
		b.WriteString(renderConfigRow(m, row, label) + "\n")
	}

	// ── Start button ──────────────────────────────────────────────────────────
	b.WriteString("\n")
	startLabel := fmt.Sprintf("  %s", bold.Render("[ Start Scan ]"))
	b.WriteString(renderConfigRow(m, m.startRow(), startLabel) + "\n")

	// ── Footer ────────────────────────────────────────────────────────────────
	b.WriteString("\n")
	b.WriteString(sep(w))
	b.WriteString(dim.Render("  [↑↓ / j k] navigate   [space] toggle   [enter] start   [q] quit") + "\n")

	return b.String()
}

func routeLabel(m Model) string {
	directCheck := gray.Render("[ ]")
	proxyCheck := gray.Render("[ ]")
	if !m.useProxy {
		directCheck = green.Render("[✓]")
	} else {
		proxyCheck = green.Render("[✓]")
	}

	phyName := m.selIface.Name
	if phyName == "" {
		phyName = "default"
	}
	direct := fmt.Sprintf("%s Direct (%s) — tests Iran ISP access", directCheck, phyName)
	proxy := fmt.Sprintf("%s Via proxy (%s) — full global discovery", proxyCheck, dim.Render(m.proxyAddr))

	return fmt.Sprintf("  %s    %s", direct, proxy)
}

// renderConfigRow renders a row with a cursor indicator when selected.
func renderConfigRow(m Model, row int, content string) string {
	if m.cursor == row {
		return sel.Render("▶") + content
	}
	return " " + content
}

// ── Scan screen ───────────────────────────────────────────────────────────────

func (m Model) viewScan() string {
	w := m.width
	if w < 60 {
		w = 80
	}

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(bold.Render("  CDN Fronting Scanner") + "\n\n")

	for _, s := range m.cdnStates {
		if !s.Enabled {
			continue
		}
		b.WriteString(viewCDNRow(s))
	}

	b.WriteString("\n")
	b.WriteString(sep(w))

	if len(m.log) == 0 {
		b.WriteString("\n" + dim.Render("  scanning...") + "\n")
	} else {
		for _, e := range m.log {
			b.WriteString("\n" + logLine(e))
		}
		b.WriteString("\n")
	}

	b.WriteString(sep(w))
	elapsed := time.Since(m.startTime).Round(time.Second)
	b.WriteString(fmt.Sprintf("\n  %s  %s\n", dim.Render(elapsed.String()), dim.Render("[q] quit")))

	return b.String()
}

func viewCDNRow(s *CDNState) string {
	label := fmt.Sprintf("  %-12s", cdnLabel(s.Provider.Name))
	switch s.Phase {
	case PhaseIdle:
		return label + dim.Render("  waiting...") + "\n"
	case PhaseDiscovery:
		bar := progressBar(s.DiscScanned, s.DiscTotal, 20)
		return fmt.Sprintf("%s  P1  %s  %s/%s  open: %s\n",
			label, bar,
			yellow.Render(fmtNum(s.DiscScanned)),
			dim.Render(fmtNum(s.DiscTotal)),
			cyan.Render(fmtNum(s.DiscFound)),
		)
	case PhaseVerify:
		bar := progressBar(s.VerVerified, s.VerTotal, 20)
		return fmt.Sprintf("%s  P2  %s  %s/%s  clean: %s\n",
			label, bar,
			yellow.Render(fmtNum(s.VerVerified)),
			dim.Render(fmtNum(s.VerTotal)),
			green.Render(fmtNum(s.VerClean)),
		)
	case PhaseDone:
		return fmt.Sprintf("%s  %s  clean: %s  → %s\n",
			label, green.Render("done"),
			green.Render(fmtNum(s.VerClean)),
			dim.Render(s.Provider.OutputFile),
		)
	}
	return label + "\n"
}

// ── Done screen ───────────────────────────────────────────────────────────────

func (m Model) viewDone() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(bold.Render("  Scan complete") + "\n\n")
	for _, s := range m.cdnStates {
		if !s.Enabled {
			continue
		}
		b.WriteString(fmt.Sprintf("  %-12s  %s  → %s\n",
			cdnLabel(s.Provider.Name),
			green.Render(fmt.Sprintf("%d clean IPs", s.VerClean)),
			dim.Render(s.Provider.OutputFile),
		))
	}
	b.WriteString("\n" + dim.Render("  [q] quit") + "\n")
	return b.String()
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func sep(w int) string {
	return "  " + gray.Render(strings.Repeat("─", w-4)) + "\n"
}

func progressBar(current, total int64, width int) string {
	var pct float64
	if total > 0 {
		pct = float64(current) / float64(total)
		if pct > 1 {
			pct = 1
		}
	}
	filled := int(pct * float64(width))
	if filled > width {
		filled = width
	}
	return gray.Render("[") +
		cyan.Render(strings.Repeat("█", filled)) +
		gray.Render(strings.Repeat("░", width-filled)) +
		gray.Render("]")
}

func logLine(e LogEntry) string {
	label := cdnLabel(e.CDN)
	if e.Clean {
		return fmt.Sprintf("  %s  %-11s  %-18s  %-10s  %s",
			green.Render("✓"), label, e.IP,
			yellow.Render(e.Latency.Round(time.Millisecond).String()),
			dim.Render(e.Status),
		)
	}
	return fmt.Sprintf("  %s  %-11s  %-18s  %-10s  %s",
		red.Render("✗"), label, e.IP,
		dim.Render("—"),
		dim.Render(e.Status),
	)
}

func fmtNum(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
