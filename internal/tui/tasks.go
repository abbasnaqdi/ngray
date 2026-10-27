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

	tea "github.com/charmbracelet/bubbletea"

	"github.com/abbasnaqdi/ng-ray/internal/cf"
	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/cores"
	"github.com/abbasnaqdi/ng-ray/internal/domain"
	"github.com/abbasnaqdi/ng-ray/internal/firewall"
	"github.com/abbasnaqdi/ng-ray/internal/links"
	"github.com/abbasnaqdi/ng-ray/internal/nginx"
	"github.com/abbasnaqdi/ng-ray/internal/server"
	"github.com/abbasnaqdi/ng-ray/internal/sni"
	"github.com/abbasnaqdi/ng-ray/internal/state"
	"github.com/abbasnaqdi/ng-ray/internal/system"
	"github.com/abbasnaqdi/ng-ray/internal/version"
)

func buildItems() []item {
	return []item{
		{key: "1", name: "Server setup / repair",
			desc:         "Install + configure every enabled core, nginx SNI mux and subscribe link. Needs root; runs in sandbox otherwise.",
			wizardBefore: true,
			task:         taskSetup},
		{key: "r", name: "Re-select SNIs",
			desc: "Test all SNI candidates and assign the best-fit ones to every core. Excludes Iran-filtered hosts and high-latency ones, then re-run setup.",
			task: taskRegenSNIs},
		{key: "2", name: "Client links · subscribe",
			desc: "Clean link sheet: the fixed subscribe URLs + one share link per config. Press y to copy the primary link (OSC 52, works over SSH).",
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
			desc: "Full diagnosis: nginx, cores, live subscription probes (auto-repairs), domain reachability, cert expiry, BBR, foreign panels.",
			task: taskDoctor},
		{key: "p", name: "Domain / proxy",
			desc:         "Put a domain in front of the subscription and/or a service: Cloudflare Origin cert, DNS A record, reverse proxy. Token is cached in /etc/ngray/cf_token.",
			domainBefore: true},
		{key: "c", name: "Cleanup / uninstall",
			desc:          "Tick exactly what to roll back: restore the pre-ngray nginx.conf, stop+disable services, remove configs, clear the firewall chain, remove state/certs, delete core binaries or the ngray binary itself.",
			cleanupBefore: true},
		{key: "u", name: "Update ngray",
			desc: "Replace ngray with the latest release (SHA256-verified) or, when no release exists, rebuild from this checkout via scripts/update.sh. Needs root; tells you the version it lands on.",
			task: taskSelfUpdate},
		{key: "9", name: "Regenerate credentials",
			desc: "Wipe ALL keys/passwords/ports + the subscribe token (press 9 twice to confirm). Next setup issues fresh ones — every existing client link stops working.",
			task: taskRegen},
	}
}

// taskSelfUpdate replaces the running ngray with the latest release (or a
// source rebuild when the repo has none yet). The binary swap takes effect on
// the next launch — the TUI keeps running the old image until quit.
func taskSelfUpdate(ctx context.Context, emit func(string)) error {
	err := server.SelfUpdate(ctx, func(f string, a ...any) {
		emit(dimStyle.Render("• ") + fmt.Sprintf(f, a...))
	}, true)
	if err != nil {
		return err
	}
	emit("")
	emit(okStyle.Render("✓ done — press q, then start `sudo ngray` again to run the new binary"))
	return nil
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
	// Domain fronting configured in config.yml: make sure cert+DNS exist
	// before provisioning so the CF-fronted paths come up on the same run.
	cfg, _ := config.LoadOrDefault(config.Path())
	if cdn := cfg.Cores.Xray.CDN; cdn != nil && cdn.Enabled && cdn.Domain != "" {
		if !certOK() {
			emit(dimStyle.Render("• ") + "domain fronting enabled — preparing Cloudflare side…")
			if err := cfEnsure(ctx, emit, cdn.Domain, true, ""); err != nil {
				return fmt.Errorf("domain fronting prep failed: %w (re-run after fixing, or uncheck Domain fronting)", err)
			}
		}
	}
	d, err := server.Setup(ctx, server.Options{
		ConfigPath:     config.Path(),
		StatePath:      state.Path(),
		CandidatesPath: candidatesPath(),
		Sandbox:        sandbox,
		RotateSub:      setupRotateSub && !sandbox,
		Confirm:        func(string) bool { return true },
		Log:            func(f string, a ...any) { emit(dimStyle.Render("• ") + fmt.Sprintf(f, a...)) },
	})
	if err != nil {
		return err
	}
	emit("")
	body := fmt.Sprintf("✓ %d endpoint(s) — menu 2 shows the clean link sheet", len(d.Endpoints))
	if setupRotateSub && !sandbox {
		body += "\n✓ subscription link ROTATED — the old URL now serves 404"
	}
	emit(okStyle.Render(body))
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
	d := st.Deploy
	if d == nil {
		return fmt.Errorf("no deployment found — run Server setup first")
	}

	// PURE link sheet: one URI per line, nothing else cluttering the view.
	// Copying never relies on mouse-dragging a wrapped line (terminal wrap
	// corrupts URIs): digits copy link N, 'a' copies everything, 'y' primary.
	clipTarget = ""
	clipList = clipList[:0]

	// Cloudflare-fronted subscribe URLs first — the primary ones.
	var subs []string
	if d.SubDomainMixedURL != "" {
		subs = append(subs, d.SubDomainMixedURL)
	}
	if d.SubMixedURL != "" {
		subs = append(subs, d.SubMixedURL)
	}
	if d.SubDomainURL != "" {
		subs = append(subs, d.SubDomainURL)
	}
	if d.SubDomainSingboxURL != "" {
		subs = append(subs, d.SubDomainSingboxURL)
	}
	if d.SubHTTPURL != "" {
		subs = append(subs, d.SubHTTPURL)
	}
	if d.SubURL != "" {
		subs = append(subs, d.SubURL)
	}
	// Endpoints, domain-fronted configs first, then direct-IP.
	var domainLinks, directLinks []string
	for _, e := range d.Endpoints {
		u := e.URI()
		if u == "" {
			continue
		}
		if e.Proto == "vless-ws-tls" || e.Proto == "vless-xhttp-tls" {
			domainLinks = append(domainLinks, u)
		} else {
			directLinks = append(directLinks, u)
		}
	}
	clipList = append(clipList, subs...)
	clipList = append(clipList, domainLinks...)
	clipList = append(clipList, directLinks...)
	clipTarget = clipList[0]

	emit(keyStyle.Render("① y = copy primary") + dimStyle.Render("  ·  a = copy all  ·  1-9 = copy that one  ·  drag unwrapped lines as-is"))
	emit("")
	for _, u := range clipList {
		emit(u)
	}
	return nil
}

// countLinks counts endpoints with a shareable URI.
func countLinks(eps []links.Endpoint) int {
	n := 0
	for _, e := range eps {
		if e.URI() != "" {
			n++
		}
	}
	return n
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
	if cfg == nil {
		return nil
	}
	st, _ := state.Load(state.Path())
	for _, c := range cores.Registry() {
		ver, ok := c.Installed(ctx, sys)
		if !ok {
			if !c.Enabled(cfg) {
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

	// Beyond the cores: nginx, the domain path, subscription liveness and the
	// certificates — the full picture, one screen.
	emit("")
	for _, c := range server.Doctor(ctx, cfg, st) {
		switch {
		case strings.HasPrefix(c.Name, "sub "), strings.HasPrefix(c.Name, "nginx"), strings.HasPrefix(c.Name, "domain "),
			strings.HasPrefix(c.Name, "cert "):
			mark := dimStyle.Render("PASS")
			switch c.Status {
			case server.Pass:
				mark = okStyle.Render("PASS")
			case server.Fail:
				mark = errStyle.Render("FAIL")
			case server.Warn:
				mark = warnStyle.Render("WARN")
			}
			emit(mark + " " + pad(c.Name, 24) + " " + c.Detail)
			if c.Fix != "" && c.Status != server.Pass {
				emit(dimStyle.Render("      fix: " + c.Fix))
			}
		}
	}
	if st != nil && len(st.Deploy.Endpoints) > 0 {
		emit("")
		emit(dimStyle.Render(fmt.Sprintf("%d endpoint(s) configured · run 'ngray doctor' for the full diagnosis", len(st.Deploy.Endpoints))))
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
	st, _ := state.Load(state.Path())
	for _, c := range server.Doctor(ctx, cfg, st) {
		mark := dimStyle.Render(string(c.Status))
		switch c.Status {
		case server.Pass:
			mark = okStyle.Render(string(c.Status))
		case server.Fail:
			mark = errStyle.Render(string(c.Status))
		}
		emit(mark + " " + pad(c.Name, 24) + " " + c.Detail)
		if c.Fix != "" && c.Status != server.Pass {
			emit(dimStyle.Render("    ↳ " + c.Fix))
		}
	}
	// Root + at least one live sub failure: apply the safe repairs (perms,
	// nginx restart) and show whether the re-probe turned green.
	if st != nil && os.Geteuid() == 0 {
		ip := st.IP
		if ip == "" {
			ip = "127.0.0.1"
		}
		needsFix := false
		for _, c := range server.ProbeSub(ctx, cfg, st, ip) {
			if c.Status == server.Fail {
				needsFix = true
			}
		}
		if needsFix {
			fixed := 0
			for _, c := range server.DoctorFix(ctx, cfg, st, func(f string, a ...any) {
				emit("  🛠 " + fmt.Sprintf(f, a...))
			}) {
				mark := errStyle.Render(string(c.Status))
				if c.Status == server.Pass {
					mark = okStyle.Render(string(c.Status) + " after fix")
					fixed++
				}
				emit(mark + " " + pad(c.Name, 18) + c.Detail)
			}
			emit("")
			if fixed > 0 {
				emit(okStyle.Render(fmt.Sprintf("✓ %d subscription check(s) repaired — re-import the links below the doctor report", fixed)))
			}
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

// runDomainCheck probes the CURRENT form domain without any CF credentials —
// DNS resolution, proxied-via-Cloudflare detection, edge status and origin TLS.
func (m Model) runDomainCheck() (tea.Model, tea.Cmd) {
	d := m.domain
	d.hint = validateDomainField(d)
	if strings.Contains(d.hint, "required") || strings.Contains(d.hint, "doesn't look like a domain") || strings.Contains(d.hint, "missing a TLD") {
		// Only hard errors block the probe; a Target hint is fine.
		m.domain = d
		return m, nil
	}
	dom := strings.TrimSuffix(strings.TrimSpace(strings.ToLower(d.domain)), ".")
	m.domain = d
	it := item{name: "Domain check", task: func(ctx context.Context, emit func(string)) error {
		return taskDomainCheck(ctx, emit, dom)
	}}
	return m.start(it)
}

func taskDomainCheck(ctx context.Context, emit func(string), dom string) error {
	ip := ""
	if st, err := state.Load(state.Path()); err == nil {
		ip = st.IP
	}
	if ip == "" {
		ip = system.New().PublicIP(ctx)
	}
	emit(dimStyle.Render("• ") + fmt.Sprintf("probing %s …", dom))
	report := domain.Check(ctx, dom, ip)
	// Verdict line: colored by outcome.
	emit(fmt.Sprintf("resolved: %s", strings.Join(resolve(report), ", ")))
	emit(dimStyle.Render(fmt.Sprintf("edge HTTP %d  ·  origin TLS %v  ·  server=%q",
		report.EdgeStatus, report.OriginTLSOK, report.ServerHeader)))
	for _, a := range report.Advice {
		emit("  ⤷ " + a)
	}
	// 502 on "/" is normal for a subscription-only setup: nothing is expected
	// to answer a web page at the apex unless a proxy target was configured.
	if report.EdgeStatus == 502 {
		if cfg, err := config.LoadOrDefault(config.Path()); err == nil {
			routed := false
			for _, pr := range cfg.ProxyRoutes {
				if pr.Domain == dom {
					routed = true
				}
			}
			if !routed {
				sub := dom
				if !strings.HasPrefix(sub, "www.") {
					sub = "www." + sub
				}
				emit("  ⤷ HTTP 502 on / is EXPECTED here — you have no reverse-proxy target for this domain; the subscription lives at https://" + sub + "/s/<token>, not on the root page")
			}
		}
	}
	emit("")
	emit(dimStyle.Render("  hint: ngray domain check " + dom + " — full report (add --ip YOUR.IP)"))
	return nil
}

// resolve never renders empty.
func resolve(r domain.Report) []string {
	if len(r.ResolvedIPs) > 0 {
		return r.ResolvedIPs
	}
	return []string{"(none)"}
}

// cfEnsure verifies the CF token, then (re)issues the Origin cert and upserts
// the DNS A record. Shared by the domain form and the setup wizard's
// domain-fronting toggle. Token candidate order: explicit override (the form
// value) → /etc/ngray/cf_token → env. Returns when no token is available
// (caller decides), so a missing token is not a fatal setup error.
func cfEnsure(ctx context.Context, emit func(string), domain string, wantDNS bool, tokenCandidate string) error {
	token := tokenCandidate
	token = cf.NormalizeToken(token)
	if token == "" {
		token = cf.NormalizeToken(cf.TokenFromEnvOrFile())
	}
	if token == "" {
		emit(warnStyle.Render("! ") + "no Cloudflare token — skipping cert/DNS (set it in the 'p  Domain / proxy' form first)")
		return nil
	}
	c := cf.New(token)
	emit(dimStyle.Render("• ") + "verifying Cloudflare token…")
	if err := c.VerifyToken(ctx); err != nil {
		return fmt.Errorf("Cloudflare rejected this credential: %w\n  received: %s\n  fix: re-enter fresh in the form (user token cfut_ — not cfat_, not Token ID), or test from a shell: NGRAY_CF_TOKEN='<token>' ngray domain verify",
			err, cf.Diagnostics(token))
	}
	emit(okStyle.Render("✓") + " token valid")
	// Only cache the token AFTER it passed verification — a wrong paste never
	// poisons /etc/ngray/cf_token and comes back prefilled next visit.
	if tokenCandidate != "" && os.Geteuid() == 0 {
		if err := writeAtomic("/etc/ngray/cf_token", []byte(tokenCandidate), 0o600); err == nil {
			emit(okStyle.Render("✓") + " cached CF token in /etc/ngray/cf_token")
		}
	}
	if certOK() {
		emit(dimStyle.Render("• ") + "origin cert already present — skipping issuance")
	} else {
		emit(dimStyle.Render("• ") + "requesting Cloudflare Origin certificate…")
		certPEM, keyPEM, err := c.IssueOriginCert(ctx, domain)
		if err != nil {
			return err
		}
		if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
			return fmt.Errorf("Cloudflare returned a mismatched cert/key: %w", err)
		}
		cdn := cfCertPaths()
		if err := writeAtomic(cdn.KeyFile, keyPEM, 0o600); err != nil {
			return err
		}
		if err := writeAtomic(cdn.CertFile, certPEM, 0o644); err != nil {
			return err
		}
		emit(okStyle.Render("✓") + " Origin certificate installed")
	}
	if !wantDNS {
		return nil
	}
	ip := ""
	if st, err := state.Load(state.Path()); err == nil {
		ip = st.IP
	}
	if ip == "" {
		ip = system.New().PublicIP(ctx)
	}
	if ip == "" {
		emit(dimStyle.Render("• ") + "could not detect server IP; skipping DNS record")
		return nil
	}
	if err := c.EnsureARecord(ctx, domain, ip, true); err != nil {
		emit(warnStyle.Render("! ") + "DNS record: " + err.Error())
		return nil
	}
	emit(okStyle.Render("✓") + " DNS A " + domain + " → " + ip + " (proxied)")
	return nil
}

// cfCertPaths returns the CDN cert/key paths from config (or defaults).
func cfCertPaths() config.CDN {
	if cfg, err := config.LoadOrDefault(config.Path()); err == nil && cfg.Cores.Xray.CDN != nil {
		return *cfg.Cores.Xray.CDN
	}
	return config.CDN{CertFile: "/etc/ngray/cf-origin.crt", KeyFile: "/etc/ngray/cf-origin.key"}
}

// certOK reports whether the currently configured origin cert+key both exist.
func certOK() bool {
	cdn := cfCertPaths()
	return cdn.CertFile != "" && cdn.KeyFile != "" &&
		fileExistsLoose(cdn.CertFile) && fileExistsLoose(cdn.KeyFile)
}

// normalizeTarget accepts bare hosts (no scheme/port): "127.0.0.1" becomes
// http://127.0.0.1:80. nginx reverse-proxies ONE upstream listener; "proxy on
// all ports" is not a thing on a single SNI listener — add service-specific
// targets instead. Returns "" for an empty target.
func normalizeTarget(t string) string {
	if t == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://"):
		return t
	default:
		return "http://" + t + ":80"
	}
}

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

	// Persist the inputs FIRST: cert/DNS may fail (bad token/quota), but the
	// domain, target and route must survive so nothing has to be retyped.
	cdn.Enabled = true
	cdn.Domain = domain
	target := normalizeTarget(strings.TrimSpace(d.target))
	if target != "" {
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
	} else {
		kept := cfg.ProxyRoutes[:0]
		for _, r := range cfg.ProxyRoutes {
			if r.Domain != domain {
				kept = append(kept, r)
			}
		}
		cfg.ProxyRoutes = kept
	}
	// Path-proxied local services: example.com/<PORT>/… reverses back into
	// 127.0.0.1:<PORT> — a censor can block the port, but not the 443 path.
	paths := nginx.NormalizePorts(parsePathPorts(d.paths), cfg.Nginx.Port, cfg.Subscription.HTTPSPort, 8444)
	if prev := cdn.PathPorts; !intsEqual(prev, paths) {
		if len(paths) > 0 {
			emit(okStyle.Render("✓") + " path proxy " + domain + "/" + fmt.Sprint(paths[0]) + "/… → 127.0.0.1:" + fmt.Sprint(paths[0]))
		}
		cdn.PathPorts = paths
	}

	// Make the ports survive a setup that re-reads config.yml: the sub servers
	// read the CDN list too (single source of truth).
	cfg.Cores.Xray.CDN = cdn
	// The fronting domain must never sit in a core's REALITY/XHTTP SNI list —
	// nginx routes one SNI to one backend and duplicates fail validation.
	if gone := cfg.StripCDNSNIs(domain); len(gone) > 0 {
		emit(warnStyle.Render("! ") + "removed " + strings.Join(gone, ", ") +
			" from the REALITY/XHTTP SNI list(s) — the fronting domain already routes via Cloudflare")
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := config.Save(config.Path(), cfg); err != nil {
		return err
	}
	emit(okStyle.Render("✓") + " saved to config.yml — inputs survive a failure")

	if d.cfToken != "" || cf.TokenFromEnvOrFile() != "" {
		if err := cfEnsure(ctx, emit, domain, d.dns, d.cfToken); err != nil {
			return err
		}
	} else {
		// No CF credentials at all: a self-signed origin cert keeps the CF
		// "Full" (non-strict) path working end-to-end. The DNS record must be
		// created manually in the dashboard (A → <server IP>, Proxied).
		emit(warnStyle.Render("! ") + "no CF token — writing a self-signed origin cert instead:")
		emit(dimStyle.Render("  · set Cloudflare SSL/TLS mode = Full (not Full strict)"))
		emit(dimStyle.Render("  · the A record must exist and be Proxied (orange cloud)"))
		sys := system.New()
		if err := sys.GenSelfSigned(domain, cdn.CertFile, cdn.KeyFile, domain); err != nil {
			return fmt.Errorf("self-signed origin cert: %w", err)
		}
		emit(okStyle.Render("✓") + " self-signed origin cert written → " + cdn.CertFile)
		emit(dimStyle.Render("  verify later: `ngray domain check " + domain + "`"))
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
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

// Cleanup action bit indices (checkbox order in the cleanup screen).
const (
	cuRestoreNginx = iota
	cuStopServices
	cuRemoveConfigs
	cuClearFirewall
	cuRemoveState
	cuRemoveBinaries
	cuRemoveSelf
	numCleanupActions
)

// cleanupForm is the checkbox list for the "Cleanup / uninstall" screen.
// Everything starts unchecked (safe default); the user opts each action in.
type cleanupForm struct {
	on    [numCleanupActions]bool
	focus int
}

func (m Model) viewCleanup() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Cleanup / uninstall") + "  " + dimStyle.Render("v"+version.Version) + "\n")
	b.WriteString(dimStyle.Render("roll back exactly what you tick — nothing else is touched") + "\n\n")

	labels := []string{
		"Restore nginx.conf    (pre-ngray backup + reload)",
		"Stop + disable cores  (xray, shoes, sing-box, hysteria, …)",
		"Remove ngray configs  (managed files under /etc, backed up first)",
		"Clear firewall chain  (input rules back to the host default)",
		"Remove ngray state    (config.yml, proxy-state.json, certs, token)",
		"Delete core binaries  (xray/shoes/sing-box/hysteria binaries)",
		"Delete ngray binary   (its ELF at its install path)",
	}
	for i, l := range labels {
		mark := mutedStyle.Render("○")
		style := textStyle
		if m.cleanup.on[i] {
			mark = okStyle.Render("●")
		}
		if m.cleanup.focus == i {
			b.WriteString(accentStyle.Render("  ❯ ") + accentStyle.Render(l) + " " + mark + "\n")
		} else {
			b.WriteString("    " + style.Render("  ") + l + " " + mark + "\n")
		}
	}

	b.WriteString("\n")
	detail := []string{
		"  nginx backup: /etc/ngray/backup/<latest>/nginx.conf is copied over the",
		"    managed config; without a backup this is a no-op.",
		"  Core binaries: shoes, xray, sing-box, hysteria from the release path.",
		"  No effect on other panels (x-ui, marzban, hiddify) — ngray never",
		"    touches what it did not create.",
	}
	b.WriteString(dimStyle.Render(strings.Join(detail, "\n")) + "\n\n")

	if m.cleanup.focus == numCleanupActions {
		b.WriteString(selStyle.Render("  ❯ Run selected actions") + "\n")
	} else {
		b.WriteString(accentStyle.Render("  › Run selected actions") + "\n")
	}
	b.WriteString("\n" + dimStyle.Render("  ↑↓/tab move  ·  space tick  ·  enter run  ·  esc back"))
	return formPanel.Render(b.String())
}

func (m Model) updateCleanup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	c := &m.cleanup
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.screen = screenMenu
		return m, nil
	case "up", "k", "shift+tab":
		if c.focus > 0 {
			c.focus--
		}
	case "down", "j", "tab":
		if c.focus <= numCleanupActions {
			c.focus++
		}
	case " ":
		if c.focus < numCleanupActions {
			c.on[c.focus] = !c.on[c.focus]
		}
	case "enter":
		if c.focus == numCleanupActions {
			return m.runCleanup()
		}
	}
	return m, nil
}

func (m Model) runCleanup() (tea.Model, tea.Cmd) {
	var anySelected bool
	for _, on := range m.cleanup.on {
		if on {
			anySelected = true
		}
	}
	if !anySelected {
		return m, nil
	}
	opts := server.CleanupOptions{
		RestoreNginx:   m.cleanup.on[cuRestoreNginx],
		StopServices:   m.cleanup.on[cuStopServices],
		RemoveConfigs:  m.cleanup.on[cuRemoveConfigs],
		ClearFirewall:  m.cleanup.on[cuClearFirewall],
		RemoveState:    m.cleanup.on[cuRemoveState],
		RemoveBinaries: m.cleanup.on[cuRemoveBinaries],
	}
	it := item{name: "Cleanup / uninstall", task: func(ctx context.Context, emit func(string)) error {
		err := server.Cleanup(ctx, server.ManageOptions{
			StatePath: state.Path(),
			Log: func(f string, a ...any) {
				emit(dimStyle.Render("• ") + fmt.Sprintf(f, a...))
			},
		}, opts)
		if err != nil {
			return err
		}
		if m.cleanup.on[cuRemoveSelf] {
			exe, _ := os.Executable()
			if exe != "" {
				if err := os.Remove(exe); err != nil {
					emit(warnStyle.Render("! ") + "could not delete the running ngray binary: " + err.Error())
				} else {
					emit(okStyle.Render("✓") + " deleted " + exe)
				}
			}
		}
		emit("")
		emit(okStyle.Render("✓ cleanup done — re-run `sudo ngray server setup` any time to rebuild"))
		return nil
	}}
	return m.start(it)
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

// intsEqual compares two int slices by value.
func intsEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
