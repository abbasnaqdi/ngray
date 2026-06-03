package tui

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/cores"
	"github.com/abbasnaqdi/ng-ray/internal/firewall"
	"github.com/abbasnaqdi/ng-ray/internal/links"
	"github.com/abbasnaqdi/ng-ray/internal/server"
	"github.com/abbasnaqdi/ng-ray/internal/sni"
	"github.com/abbasnaqdi/ng-ray/internal/state"
	"github.com/abbasnaqdi/ng-ray/internal/system"
)

func buildItems() []item {
	return []item{
		{key: "1", name: "Server setup / repair",
			desc: "Install + configure every enabled core, build the nginx SNI mux and the subscribe link. Needs root for a real server; runs in sandbox otherwise.",
			task: taskSetup},
		{key: "2", name: "Client links · QR · subscribe",
			desc: "Show the share links and the fixed subscribe URL from the last setup. Import them into v2rayN / nekoray / husi.",
			task: taskLinks},
		{key: "3", name: "Test SNIs (live)",
			desc: "Probe candidate domains for REALITY fitness (TLS1.3 + h2 + X25519) with live results. Pick OK ones for your configs.",
			task: taskSNI},
		{key: "4", name: "Scan CDN edges",
			desc: "Find CDN edge IPs reachable from this network (survive MCI). Opens the scanner: ↑/↓ move, space toggle, enter start, q quit.",
			exec: []string{"scan"}},
		{key: "5", name: "Status",
			desc: "Installed version, service state and latest release for each core.",
			task: taskStatus},
		{key: "6", name: "Update cores",
			desc: "Update installed cores to their latest release and restart them.",
			task: taskUpdate,
			disabled: func(m *Model) (bool, string) {
				if !m.checked {
					return true, "(checking…)"
				}
				if m.installed == 0 {
					return true, "(nothing installed yet)"
				}
				return false, ""
			}},
		{key: "7", name: "Firewall",
			desc: "Preview the default-drop ruleset (ssh + the ports your config uses); applies it when run as root.",
			task: taskFirewall},
		{key: "8", name: "Doctor",
			desc: "Diagnose cores, nginx ownership, BBR and any foreign panels (x3-ui etc. are left untouched).",
			task: taskDoctor},
		{key: "9", name: "Regenerate credentials",
			desc: "Clear all keys, secrets and the subscribe token so the next setup regenerates them.",
			task: taskRegen},
	}
}

func taskSetup(ctx context.Context, emit func(string)) error {
	sandbox := os.Geteuid() != 0
	if sandbox {
		emit(badge.Render("sandbox") + " not root — writing under ./sandbox; run `sudo ngray` for a real server")
	}
	d, err := server.Setup(ctx, server.Options{
		ConfigPath:     config.Path(),
		StatePath:      state.Path(),
		CandidatesPath: candidatesPath(),
		Sandbox:        sandbox,
		Confirm:        func(string) bool { return true },
		Log:            func(f string, a ...any) { emit(dimStyle.Render("• ") + fmt.Sprintf(f, a...)) },
	})
	if err != nil {
		return err
	}
	emit("")
	emit(okStyle.Render(fmt.Sprintf("✓ %d endpoint(s)", len(d.Endpoints))))
	for _, ep := range d.Endpoints {
		if ep.URI() == "" {
			continue
		}
		emit("")
		emit(keyStyle.Render(ep.Tag))
		emit("  " + ep.URI())
	}
	// Notes: fptn token, mieru config, AmneziaWG conf
	for k, v := range d.Notes {
		emit("")
		emit(keyStyle.Render("── " + k + " ──"))
		for _, line := range strings.Split(strings.TrimSpace(v), "\n") {
			emit(dimStyle.Render(line))
		}
	}
	if d.SubURL != "" {
		emit("")
		emit("subscribe: " + okStyle.Render(d.SubURL))
		emit(links.QR(d.SubURL))
	}
	return nil
}

func taskLinks(ctx context.Context, emit func(string)) error {
	st, err := state.Load(state.Path())
	if err != nil {
		return err
	}
	cfg, _ := config.LoadOrDefault(config.Path())
	var buf bytes.Buffer
	if err := server.RenderClient(&buf, st, cfg, false); err != nil {
		return err
	}
	emitBuf(emit, &buf)
	return nil
}

func taskSNI(ctx context.Context, emit func(string)) error {
	hosts, err := sni.LoadCandidates(candidatesPath())
	if err != nil {
		return err
	}
	emit(dimStyle.Render(fmt.Sprintf("testing %d candidates — live", len(hosts))))
	t := sni.NewTester()
	results := t.TestAll(ctx, hosts, 24, func(r sni.Result) {
		if r.Status == sni.StatusOK {
			emit(okStyle.Render("✓ "+pad(r.Host, 34)) + fmt.Sprintf("ms=%-4d (±%-3d) http=%d", r.MedianMS, r.JitterMS, r.HTTPCode))
		} else {
			emit(dimStyle.Render("· " + pad(r.Host, 34) + string(r.Status)))
		}
	})
	ranked := sni.Ranked(results)
	emit("")
	emit(keyStyle.Render(fmt.Sprintf("best picks for REALITY (%d fit):", len(ranked))))
	for i, r := range ranked {
		if i >= 10 {
			break
		}
		emit(fmt.Sprintf("  %2d. %s %dms (±%d)", i+1, pad(r.Host, 32), r.MedianMS, r.JitterMS))
	}
	return nil
}

func taskStatus(ctx context.Context, emit func(string)) error {
	sys := system.New()
	cfg, _ := config.LoadOrDefault(config.Path())
	for _, c := range cores.Registry() {
		ver, ok := c.Installed(ctx, sys)
		if !ok {
			if cfg != nil && !c.Enabled(cfg) {
				emit(dimStyle.Render(pad(c.Name(), 12) + "disabled  (enable in config.yml)"))
			} else {
				emit(errStyle.Render(pad(c.Name(), 12)) + dimStyle.Render("not installed — run server setup"))
			}
			continue
		}
		latest, _ := sys.LatestVersion(ctx, c.Repo())
		line := pad(c.Name(), 12) + pad(ver, 12)
		if sys.IsActive(ctx, c.Service()) {
			line += okStyle.Render("active")
		} else {
			line += errStyle.Render("inactive")
		}
		if latest != "" && latest != ver {
			line += "  " + badge.Render("⬆ "+latest)
		}
		emit(line)
	}
	return nil
}

func taskUpdate(ctx context.Context, emit func(string)) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("update needs root — run `sudo ngray update`")
	}
	return server.UpdateCores(ctx, func(string) bool { return true },
		func(f string, a ...any) { emit(fmt.Sprintf(f, a...)) })
}

func taskFirewall(ctx context.Context, emit func(string)) error {
	cfg, err := config.LoadOrDefault(config.Path())
	if err != nil {
		return err
	}
	r := firewall.Rules{TCP: []int{cfg.Nginx.Port}}
	if cfg.Cores.Hysteria2.Enabled {
		r.UDP = append(r.UDP, cfg.Cores.Hysteria2.Port)
	}
	if cfg.Cores.Fptn.Enabled {
		r.TCP = append(r.TCP, cfg.Cores.Fptn.Port)
	}
	emit(dimStyle.Render("plan (default-drop inbound):"))
	for _, line := range r.Plan() {
		emit("  • " + line)
	}
	if os.Geteuid() != 0 {
		emit("")
		emit(dimStyle.Render("run `sudo ngray firewall apply --yes` to apply"))
		return nil
	}
	if err := firewall.New(system.New()).Apply(ctx, r); err != nil {
		return err
	}
	emit(okStyle.Render("✓ applied"))
	return nil
}

func taskDoctor(ctx context.Context, emit func(string)) error {
	cfg, err := config.LoadOrDefault(config.Path())
	if err != nil {
		return err
	}
	for _, c := range server.Doctor(ctx, cfg) {
		mark := dimStyle.Render(string(c.Status))
		switch c.Status {
		case server.Pass:
			mark = okStyle.Render(string(c.Status))
		case server.Fail:
			mark = errStyle.Render(string(c.Status))
		}
		emit(mark + " " + pad(c.Name, 14) + c.Detail)
		if c.Fix != "" && c.Status != server.Pass {
			emit(dimStyle.Render("      fix: " + c.Fix))
		}
	}
	return nil
}

func taskRegen(ctx context.Context, emit func(string)) error {
	st, err := state.Load(state.Path())
	if err != nil {
		return err
	}
	st.Keys, st.Secrets, st.Ports, st.SubToken, st.Deploy = nil, nil, nil, "", nil
	if err := st.Save(); err != nil {
		return err
	}
	emit(okStyle.Render("✓ cleared — run Server setup to regenerate"))
	return nil
}

// helpers ------------------------------------------------------------------

func emitBuf(emit func(string), buf *bytes.Buffer) {
	sc := bufio.NewScanner(buf)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		emit(sc.Text())
	}
}

func pad(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}

func candidatesPath() string {
	rel := filepath.Join("snis", "candidates.sni")
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), rel)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return rel
}
