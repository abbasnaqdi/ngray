package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/abbasnaqdi/ng-ray/internal/version"
)

// ── Palette ───────────────────────────────────────────────────────────────────

// Warm palette inspired by Claude Code / Codex, ADAPTIVE: every colour is
// defined for dark AND light terminals, so body text is never washed out
// (near-white 254 was invisible on a light background). Colour is used
// sparingly; the layout carries the hierarchy.
var (
	colorAccent = lipgloss.AdaptiveColor{Light: "130", Dark: "209"} // orange — focus / primary
	colorText   = lipgloss.AdaptiveColor{Light: "234", Dark: "254"} // body text, readable on any bg
	colorMuted  = lipgloss.AdaptiveColor{Light: "242", Dark: "245"} // inactive labels / status
	colorDim    = lipgloss.AdaptiveColor{Light: "244", Dark: "239"} // rules, hints, meta
	colorOK     = lipgloss.AdaptiveColor{Light: "28", Dark: "114"}
	colorErr    = lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	colorWarn   = lipgloss.AdaptiveColor{Light: "130", Dark: "214"}
	colorSelBg  = lipgloss.AdaptiveColor{Light: "252", Dark: "236"}

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	accentStyle = lipgloss.NewStyle().Foreground(colorAccent)
	dimStyle    = lipgloss.NewStyle().Foreground(colorDim)
	mutedStyle  = lipgloss.NewStyle().Foreground(colorMuted)
	textStyle   = lipgloss.NewStyle().Foreground(colorText)
	keyStyle    = lipgloss.NewStyle().Foreground(colorMuted)
	selStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorText).Background(colorSelBg)
	disStyle    = lipgloss.NewStyle().Foreground(colorMuted)
	okStyle     = lipgloss.NewStyle().Foreground(colorOK)
	errStyle    = lipgloss.NewStyle().Foreground(colorErr)
	warnStyle   = lipgloss.NewStyle().Foreground(colorWarn)
	spinStyle   = lipgloss.NewStyle().Foreground(colorAccent)
	badge       = lipgloss.NewStyle().Foreground(colorWarn)

	panel     = lipgloss.NewStyle().Padding(1, 2)
	taskPanel = lipgloss.NewStyle().Padding(0, 2)
	formPanel = lipgloss.NewStyle().Padding(1, 2)
)

func (m Model) View() string {
	switch m.screen {
	case screenTask:
		return m.viewTask()
	case screenSetupForm:
		return m.viewSetupForm()
	case screenDomainForm:
		return m.viewDomainForm()
	case screenCleanup:
		return m.viewCleanup()
	default:
		return m.viewMenu()
	}
}

// ── Menu ──────────────────────────────────────────────────────────────────────

func (m Model) viewMenu() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("ngray") + "  " + dimStyle.Render(version.Version) + "\n")
	b.WriteString(m.statusLine() + "\n\n")

	for i, it := range m.items {
		b.WriteString(m.menuRow(i, it))
		if i == 3 {
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	// Explain the highlighted item so the menu is self-documenting.
	if m.cursor >= 0 && m.cursor < len(m.items) {
		if d := m.items[m.cursor].desc; d != "" {
			b.WriteString(dimStyle.Render(wrapWords("    "+d, 56)) + "\n\n")
		}
	}
	if m.armRegen {
		b.WriteString(errStyle.Render("  ⚠ press 9 AGAIN to wipe all credentials and the subscription token") + "\n")
	}
	b.WriteString(dimStyle.Render("    ↑↓ move  ·  1-9 jump  ·  enter run  ·  q quit"))
	return panel.Render(b.String())
}

func (m Model) menuRow(i int, it item) string {
	dis, reason := m.disabled(it)
	selected := i == m.cursor

	const nameW = 26
	switch {
	case dis:
		return disStyle.Render(fmt.Sprintf("    %s  %-*s %s\n", it.key, nameW, it.name, reason))
	case selected:
		return selStyle.Render(fmt.Sprintf("  ❯ %s  %-*s", it.key, nameW, it.name)) + "\n"
	default:
		return "    " + keyStyle.Render(it.key) + "  " + textStyle.Render(fmt.Sprintf("%-*s", nameW, it.name)) + "\n"
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

	b.WriteString(titleStyle.Render("Setup") + "\n")
	b.WriteString(dimStyle.Render("review, then run setup") + "\n\n")

	b.WriteString(formTextRow(f, formFieldIP, "Server IP"))
	b.WriteByte('\n')
	b.WriteString(formTextRow(f, formFieldPort, "Nginx port"))
	b.WriteString("\n\n")

	b.WriteString(formToggleRow(f.focused == formFieldBBR, "BBR congestion", f.bbr))
	b.WriteString("\n")

	domToggle := formToggleRow(f.focused == formFieldDomainOn, "Domain fronting", f.domainOn)
	b.WriteString(domToggle + "\n")
	// Value row (edited like the port field), visible once the toggle is on.
	dval := f.domainVal
	if dval == "" {
		dval = "example.com"
	}
	drow := func() string {
		lbl := fmt.Sprintf("  %-22s", "Domain")
		if f.focused == formFieldDomainOn && f.editing {
			return "   " + accentStyle.Render(lbl) + accentStyle.Render(f.editBuf+"█")
		}
		if f.focused == formFieldDomainOn {
			return "   " + accentStyle.Render(lbl) + textStyle.Render(dval) + dimStyle.Render("  (enter to edit — needs the CF token cached)")
		}
		return "  " + mutedStyle.Render(lbl) + dimStyle.Render(dval)
	}
	if f.domainOn {
		b.WriteString(drow() + "\n")
	}
	b.WriteString("\n")

	b.WriteString(dimStyle.Render("  Cores:") + "\n")
	for i, label := range wizardCoreLabels {
		b.WriteString(formToggleRow(f.focused == formCoreBase+i, fmt.Sprintf("  %-12s", label), f.coreOn[i]))
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	b.WriteString(mutedStyle.Render("  xray · shoes · sing-box  — configured in config.yml") + "\n")

	b.WriteString("\n")
	b.WriteString(formToggleRow(f.focused == formFieldRotate, "Rotate sub link", f.rotate))
	if f.rotate {
		b.WriteString(dimStyle.Render("     ON — a new token is generated after setup; OLD subscribe links 404") + "\n")
	} else {
		b.WriteString(dimStyle.Render("     default off — your existing subscribe URL stays the same") + "\n")
	}
	b.WriteString("\n")

	if f.focused == formFieldProceed {
		b.WriteString(selStyle.Render("  ❯ Proceed with setup  ") + "\n")
	} else {
		b.WriteString(accentStyle.Render("  › Proceed with setup") + "\n")
	}

	b.WriteString("\n" + dimStyle.Render("  tab/↑↓ move  ·  enter edit/run  ·  space toggle  ·  esc back"))
	return formPanel.Render(b.String())
}

func formTextRow(f setupForm, fieldIdx int, label string) string {
	focused := f.focused == fieldIdx
	lbl := fmt.Sprintf("  %-22s", label)

	var val string
	switch fieldIdx {
	case formFieldIP:
		if focused && f.editing {
			val = accentStyle.Render(f.editBuf + "█")
		} else {
			val = dimStyle.Render(f.ipVal)
		}
	case formFieldPort:
		if focused && f.editing {
			val = accentStyle.Render(f.editBuf + "█")
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

// ── Domain / proxy form ───────────────────────────────────────────────────────

func (m Model) viewDomainForm() string {
	d := m.domain
	var b strings.Builder

	b.WriteString(titleStyle.Render("Domain / proxy") + "  " + dimStyle.Render("v"+version.Version) + "\n")
	b.WriteString(dimStyle.Render("serve the subscription behind a trusted domain, with one step for cert+DNS") + "\n\n")
	if d.hint != "" {
		b.WriteString(errStyle.Render("  ✗ "+d.hint) + "\n\n")
	}

	row := func(idx int, label, val, hint string, editing bool) string {
		focused := d.focused == idx
		marker := "    "
		if focused {
			marker = "  ❯ "
		}
		lbl := fmt.Sprintf("%-10s", label)
		if focused && editing {
			return marker + accentStyle.Render(lbl) + accentStyle.Render(d.editBuf+"█")
		}
		if focused {
			return marker + accentStyle.Render(lbl) + textStyle.Render(val) + dimStyle.Render("  "+hint)
		}
		return marker + dimStyle.Render(lbl) + textStyle.Render(val)
	}

	b.WriteString(row(dfDomain, "Domain", dash(d.domain), "enter to edit — punycode is fine", d.editing) + "\n")
	b.WriteString(row(dfTarget, "Target", dash(d.target), "enter to edit — optional", d.editing) + "\n")
	b.WriteString(row(dfPaths, "Path ports", dash(d.paths), "enter to edit — e.g. 20128, 3000", d.editing) + "\n")
	tok := "—"
	if d.cfToken != "" {
		tok = maskToken(d.cfToken)
	}
	b.WriteString(row(dfToken, "CF token", tok, "enter to edit — cached after apply", d.editing) + "\n")

	dnsMark := "○ disabled"
	if d.dns {
		dnsMark = "● enabled"
	}
	if d.focused == dfDNS {
		b.WriteString("  ❯ " + accentStyle.Render(fmt.Sprintf("%-10s", "DNS A")) + okStyle.Render(dnsMark) + "\n")
	} else {
		b.WriteString("    " + dimStyle.Render(fmt.Sprintf("%-10s", "DNS A")) + textStyle.Render(dnsMark) + "\n")
	}

	// One short paragraph below the fields: what applies and when, so the
	// form is self-explanatory without leaving the screen.
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(strings.Join([]string{
		"  Domain  — just the name, NO https:// and NO port. Example:",
		"            vpn.example.com (or www.example.com).",
		"  CF token — a USER API Token from My Profile → API Tokens (starts",
		"            with cfut_, or a legacy ~40-char string; NOT cfat_ —",
		"            account-owned tokens, NOT the Global API Key, NOT the",
		"            Token ID): dash.cloudflare.com/profile/api-tokens →",
		"            Create Token → Edit zone DNS + SSL and Certificates.",
		"            Copy the LONG secret, not the Token ID. Cached",
		"            in /etc/ngray/cf_token (0600) after it verifies.",
		"            Optional: leave empty → a self-signed origin cert is",
		"            generated; set SSL/TLS mode = Full in the CF panel.",
		"  DNS A   — creates/updates the proxied A record → server IP.",
		"  Target  — optional reverse-proxy for a host-SNI route (optional),",
		"            e.g. https://127.0.0.1:8443 — bare host = port 80.",
		"  Path ports — comma list; each port becomes a PATH on this domain:",
		"            20128 → https://DOMAIN/20128/dashboard reaches the service",
		"            at 127.0.0.1:20128/dashboard. Censors block ports; the 443",
		"            path keeps working (works with and without Cloudflare).",
		"  Check   — probes DNS + CF edge + origin WITHOUT the token.",
		"  Apply   — issues cert, writes DNS, installs everything.",
	}, "\n")) + "\n\n")

	if d.focused == dfCheck {
		b.WriteString(selStyle.Render("  ❯ Check now (no token — DNS/CF probe)") + "\n")
	} else {
		b.WriteString(accentStyle.Render("  › Check now (no token — DNS/CF probe)") + "\n")
	}
	if d.focused == dfProceed {
		b.WriteString(selStyle.Render("  ❯ Apply domain / proxy  ") + "\n")
	} else {
		b.WriteString(accentStyle.Render("  › Apply domain / proxy") + "\n")
	}
	b.WriteString("\n" + dimStyle.Render("  tab/↑↓ move  ·  enter edit/run  ·  space toggle  ·  esc back"))
	return formPanel.Render(b.String())
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func maskToken(s string) string {
	if len(s) <= 8 {
		return "••••"
	}
	return s[:4] + "…" + s[len(s)-4:]
}

// ── Task view ─────────────────────────────────────────────────────────────────

func (m Model) viewTask() string {
	var head string
	switch {
	case m.running:
		head = m.spin.View() + " " + titleStyle.Render(m.taskName) + "  " + dimStyle.Render("running…")
	case m.taskErr != nil:
		head = errStyle.Render("✗ " + m.taskName)
	default:
		head = okStyle.Render("✓ "+m.taskName) + "  " + dimStyle.Render("done")
	}
	body := taskPanel.Width(maxInt(m.width-4, 20)).Render(strings.TrimRight(m.vp.View(), "\n"))
	foot := mutedStyle.Render("  esc back  ·  ↑↓ scroll  ·  q quit")
	if clipTarget != "" {
		foot = mutedStyle.Render("  esc back  ·  ↑↓ scroll  ·  y primary  ·  a all  ·  1-9 copy one  ·  q quit")
	}
	foot += "  " + dimStyle.Render("v"+version.Version)
	if m.restartAfter && m.taskErr == nil {
		foot = mutedStyle.Render("  enter restart to the new binary  ·  esc stay here") + "  " + dimStyle.Render("v"+version.Version)
	}
	// Surface the failure as a readable box instead of a single overflow line.
	if m.taskErr != nil {
		reason := wrapWords(m.taskErr.Error(), maxInt(m.width-12, 72))
		box := lipgloss.NewStyle().Padding(0, 1).Render("  " + reason)
		return head + "\n" + body + "\n" + errStyle.Render(box) + "\n" + foot
	}
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
		// URIs must never wrap: a visual line-break would end up inside a
		// mouse-dragged copy, corrupting the link. Long but unwrapped lines
		// scroll horizontally in the viewport instead.
		if wrapped := unwrappable(l); wrapped {
			out = append(out, l)
			continue
		}
		out = append(out, wrapLineSoft(l, w))
	}
	return joinLines(out)
}

// unwrappable reports lines whose content loses meaning when broken: share
// links, subscribe URLs and anything else that looks like a URL with a scheme.
func unwrappable(s string) bool {
	t := strings.TrimLeft(s, " ")
	for _, scheme := range []string{
		"vless://", "vmess://", "trojan://", "anytls://", "hysteria2://", "hysteria://",
		"tuic://", "ss://", "socks5://", "snell://", "http://", "https://", "sb://",
	} {
		if strings.HasPrefix(t, scheme) {
			return true
		}
	}
	return false
}

// wrapWords wraps s at word boundaries to width, keeping any leading spaces of
// s as the indent on continuation lines (so a "  " prefix stays aligned).
func wrapWords(s string, width int) string {
	indent := s[:len(s)-len(strings.TrimLeft(s, " "))]
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}
	var b strings.Builder
	line := indent + words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) > width {
			b.WriteString(line + "\n")
			line = indent + w
		} else {
			line += " " + w
		}
	}
	b.WriteString(line)
	return b.String()
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

// wrapLineSoft wraps at word boundaries when possible and only falls back to
// hard cuts for single long tokens — "any tim\ne" class of breaks disappear.
func wrapLineSoft(s string, width int) string {
	if len(s) <= width {
		return s
	}
	if r := []rune(s); len(r) > 0 { // wrap on runes, not bytes
		if len(r) <= width {
			return s
		}
	}
	var out []string
	for len(s) > width {
		cut := strings.LastIndex(s[:width+1], " ")
		if cut < width/2 {
			cut = width // single long token: hard cut keeps content correct
		}
		out = append(out, s[:cut])
		s = strings.TrimLeft(s[cut:], " ")
	}
	out = append(out, s)
	return strings.Join(out, "\n")
}
