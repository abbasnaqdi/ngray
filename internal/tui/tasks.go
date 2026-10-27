package tui

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/abbasnaqdi/ng-ray/internal/cf"
	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/cores"
	"github.com/abbasnaqdi/ng-ray/internal/firewall"
	"github.com/abbasnaqdi/ng-ray/internal/server"
	"github.com/abbasnaqdi/ng-ray/internal/sni"
	"github.com/abbasnaqdi/ng-ray/internal/state"
	"github.com/abbasnaqdi/ng-ray/internal/system"
)

func buildItems() []item {
	return []item{
		{key: "1", name: "Server setup / repair",
			desc:         "Install + configure every enabled core, nginx SNI mux and subscribe link. Needs root; runs in sandbox otherwise.",
			wizardBefore: true,
			task:         taskSetup},
		{key: "r", name: "Re-select SNIs",
			desc: "Test all SNI candidates and assign the best-fit ones to every core. Run this if connections are failing, then re-run setup.",
			task: taskRegenSNIs},
		{key: "2", name: "Client links · subscribe",
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
		{key: "p", name: "Domain / proxy",
			desc:         "Put a domain in front of the subscription and/or a service: Cloudflare Origin cert, DNS A record, reverse proxy. Token is cached in /etc/ngray/cf_token.",
			domainBefore: true},
		{key: "9", name: "Regenerate credentials",
			desc: "Wipe ALL keys/passwords/ports + the subscribe token (press 9 twice to confirm). Next setup issues fresh ones — every existing client link stops working.",
			task: taskRegen},
	}
}

func taskRegenSNIs(ctx context.Context, emit func(string)) error {
	cfg, err := config.LoadOrDefault(config.Path())
	if err != nil {
		return err
	}
	emit(dimStyle.Render("• ") + "testing SNI candidates — takes ~90s…")
	if err := server.RegenerateSNIs(ctx, candidatesPath(), config.Path(), cfg, func(f string, a ...any) {
		emit(dimStyle.Render("• ") + fmt.Sprintf(f, a...))
	}); err != nil {
		return err
	}
	emit("")
	emit(okStyle.Render("✓ config.yml updated — run Server setup to apply"))
	return nil
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
	emit(okStyle.Render(fmt.Sprintf("✓ %d endpoint(s) — use menu item 2 for individual links", len(d.Endpoints))))
	if d.SubURL != "" || d.SubDomainURL != "" {
		emit("")
		emit(keyStyle.Render("subscribe"))
		// Primary: the CF-fronted domain subscription (no port, trusted
		// edge cert, origin IP hidden).
		if d.SubDomainURL != "" || d.SubDomainSingboxURL != "" {
			emit(keyStyle.Render("  primary — via Cloudflare (no port, trusted cert, IP hidden)"))
			if d.SubDomainSingboxURL != "" {
				emit("  sbJSON  " + okStyle.Render(d.SubDomainSingboxURL))
			}
			if d.SubDomainURL != "" {
				emit("  base64  " + okStyle.Render(d.SubDomainURL))
			}
		}
		// User typed listener info lines use their own indentation.
		if d.SubHTTPURL != "" {
			emit(dimStyle.Render("  fallback — direct IP (use if the domain is blocked)"))
			emit("  HTTP   " + okStyle.Render(d.SubHTTPURL))
			emit(dimStyle.Render("          browser-friendly · no TLS warning"))
		}
		emit("  HTTPS  " + okStyle.Render(d.SubURL))
		if d.SubCertFingerprint != "" {
			emit(dimStyle.Render("  cert   SHA256:" + d.SubCertFingerprint))
			emit(dimStyle.Render("          self-signed · browsers always warn · proxy clients use pinSHA256"))
		}
		if d.SubSingboxURL != "" {
			emit("  sbJSON " + okStyle.Render(d.SubSingboxURL))
		}
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
	if err := server.RenderClient(&buf, st, cfg); err != nil {
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

// taskDomain applies the domain/proxy form: caches the Cloudflare token,
// issues an Origin cert, upserts the DNS record, writes config.yml and then
// runs the normal server setup.
func taskDomain(ctx context.Context, emit func(string), d domainForm) error {
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(d.domain)), ".")
	if domain == "" {
		return fmt.Errorf("domain is required")
	}
	cfg, err := config.LoadOrDefault(config.Path())
	if err != nil {
		return err
	}
	cdn := cfg.Cores.Xray.CDN
	if cdn == nil {
		cdn = &config.CDN{CertFile: "/etc/ngray/cf-origin.crt", KeyFile: "/etc/ngray/cf-origin.key", WS: true}
		cfg.Cores.Xray.CDN = cdn
	}

	if d.cfToken != "" {
		if os.Geteuid() != 0 {
			emit(badge.Render("sandbox") + " CF token needs root to cache in /etc/ngray/cf_token")
		} else if err := writeAtomic("/etc/ngray/cf_token", []byte(d.cfToken), 0o600); err != nil {
			emit(dimStyle.Render("• ") + "warning: could not cache CF token: " + err.Error())
		} else {
			emit(okStyle.Render("✓") + " cached CF token in /etc/ngray/cf_token")
		}
	}
	token := d.cfToken
	if token == "" {
		token = cf.TokenFromEnvOrFile()
	}

	if token != "" {
		c := cf.New(token)
		emit(dimStyle.Render("• ") + "verifying Cloudflare token…")
		if err := c.VerifyToken(ctx); err != nil {
			return fmt.Errorf("Cloudflare token rejected: %w", err)
		}
		emit(okStyle.Render("✓") + " token valid")
		emit(dimStyle.Render("• ") + "requesting Cloudflare Origin certificate…")
		certPEM, keyPEM, err := c.IssueOriginCert(ctx, domain)
		if err != nil {
			return err
		}
		if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
			return fmt.Errorf("Cloudflare returned a mismatched cert/key: %w", err)
		}
		if err := writeAtomic(cdn.KeyFile, keyPEM, 0o600); err != nil {
			return err
		}
		if err := writeAtomic(cdn.CertFile, certPEM, 0o644); err != nil {
			return err
		}
		emit(okStyle.Render("✓") + " Origin certificate installed")

		if d.dns {
			ip := ""
			if st, err := state.Load(state.Path()); err == nil {
				ip = st.IP
			}
			if ip == "" {
				ip = system.New().PublicIP(ctx)
			}
			if ip == "" {
				emit(dimStyle.Render("• ") + "could not detect server IP; skipping DNS record")
			} else if err := c.EnsureARecord(ctx, domain, ip, true); err != nil {
				emit(warnStyle.Render("! ") + "DNS record: " + err.Error())
			} else {
				emit(okStyle.Render("✓") + " DNS A " + domain + " → " + ip + " (proxied)")
			}
		}
	} else {
		emit(warnStyle.Render("! ") + "no Cloudflare token — skipping cert/DNS (use an existing cert or set the token)")
	}

	cdn.Enabled = true
	cdn.Domain = domain
	if target := strings.TrimSpace(d.target); target != "" {
		if !(strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://")) {
			return fmt.Errorf("target must start with http:// or https://")
		}
		var kept []config.ProxyRoute
		for _, r := range cfg.ProxyRoutes {
			if r.Domain != domain {
				kept = append(kept, r)
			}
		}
		cfg.ProxyRoutes = append(kept, config.ProxyRoute{
			Domain: domain, Target: target, CertFile: cdn.CertFile, KeyFile: cdn.KeyFile,
		})
		emit(okStyle.Render("✓") + " proxy route " + domain + " → " + target)
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := config.Save(config.Path(), cfg); err != nil {
		return err
	}
	emit("")
	emit(dimStyle.Render("• ") + "applying (nginx + services)…")
	dpl, err := server.Setup(ctx, server.Options{
		ConfigPath:     config.Path(),
		StatePath:      state.Path(),
		CandidatesPath: candidatesPath(),
		Sandbox:        os.Geteuid() != 0,
		Confirm:        func(string) bool { return true },
		Log:            func(f string, a ...any) { emit(dimStyle.Render("• ") + fmt.Sprintf(f, a...)) },
	})
	if err != nil {
		return err
	}
	emit("")
	emit(okStyle.Render(fmt.Sprintf("✓ applied — %s is live (%d endpoint(s))", domain, len(dpl.Endpoints))))
	if dpl.SubDomainURL != "" {
		emit("  subscribe  " + okStyle.Render(dpl.SubDomainURL))
	}
	return nil
}

// writeAtomic writes data via a temp file + rename and tightens the mode.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".ngray-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		return err
	}
	return os.Rename(name, path)
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
	if _, err := os.Stat("/etc/ngray/snis/candidates.sni"); err == nil {
		return "/etc/ngray/snis/candidates.sni"
	}
	return rel
}
