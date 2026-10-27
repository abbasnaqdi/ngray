package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/abbasnaqdi/ng-ray/internal/cdnscan"
	"github.com/abbasnaqdi/ng-ray/internal/cf"
	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/cores"
	"github.com/abbasnaqdi/ng-ray/internal/dnsval"
	"github.com/abbasnaqdi/ng-ray/internal/domain"
	"github.com/abbasnaqdi/ng-ray/internal/firewall"
	"github.com/abbasnaqdi/ng-ray/internal/server"
	"github.com/abbasnaqdi/ng-ray/internal/sni"
	"github.com/abbasnaqdi/ng-ray/internal/state"
	"github.com/abbasnaqdi/ng-ray/internal/system"
	"github.com/abbasnaqdi/ng-ray/internal/tui"
	"github.com/abbasnaqdi/ng-ray/internal/version"
)

const usage = `ng-ray — unified anti-DPI proxy toolkit

Usage:
  ngray                  launch the interactive TUI
  ngray server setup     provision/repair the server from config.yml
  ngray server status    show core versions, services and listening ports
  ngray client links     print share links and the subscribe URL
  ngray sni test [host…] test SNIs for REALITY fitness and latency
  ngray sni discover     find REALITY SNIs hosted near your server
  ngray domain check [d] check if a domain is a working CF-fronted path here
  ngray domain set d     enable Cloudflare fronting for a domain in config.yml
  ngray domain issue d   issue+install a Cloudflare Origin certificate
  ngray domain dns d     create/update the Cloudflare A record for a domain
  ngray domain cert c k  install a Cloudflare Origin cert+key for CF fronting
  ngray proxy add d      register a domain as a reverse proxy (-> target)
  ngray proxy list       list configured proxy routes
  ngray proxy remove d   remove a proxy route
  ngray scan             scan CDN edges for reachable IPs
  ngray dns validate     find DNS resolvers usable for DNS tunneling
  ngray dns doh [n]      benchmark public DoH resolvers
  ngray firewall apply   apply the safe, idempotent firewall ruleset
  ngray update [--self]  update cores (and optionally ngray itself)
  ngray doctor           diagnose cores, nginx, BBR, foreign panels
  ngray clean            back up + remove ngray-owned configs, stop services
  ngray remove           full uninstall (services, configs, firewall chain)
  ngray version          print build version

Global: --sandbox [--root dir]  test locally without touching the host
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		// Pre-TUI launch check: silently self-update and re-exec when a newer
		// release exists. Opt out with NGRAY_NO_SELFUPDATE=1.
		if os.Getenv("NGRAY_NO_SELFUPDATE") == "" {
			selfUpdateAndReexec()
		}
		return tui.Run()
	}
	switch args[0] {
	case "version", "-v", "--version":
		fmt.Println(version.String())
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	case "config":
		return cmdConfig(args[1:])
	case "server":
		return cmdServer(args[1:])
	case "client":
		return cmdClient(args[1:])
	case "sni":
		return cmdSNI(args[1:])
	case "firewall":
		return cmdFirewall(args[1:])
	case "update":
		return cmdUpdate(args[1:])
	case "scan":
		return cdnscan.Run()
	case "dns":
		return cmdDNS(args[1:])
	case "domain":
		return cmdDomain(args[1:])
	case "proxy":
		return cmdProxy(args[1:])
	case "regen":
		return cmdRegen()
	case "doctor":
		return cmdDoctor()
	case "clean":
		return cmdManage("clean")
	case "remove":
		return cmdManage("remove")
	default:
		fmt.Print(usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// cmdConfig validates and prints the resolved configuration.
func cmdConfig(args []string) error {
	c, err := config.LoadOrDefault(config.Path())
	if err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "init" {
		path := config.Path()
		if err := config.Save(path, c); err != nil {
			return err
		}
		fmt.Println("wrote", path)
		return nil
	}
	fmt.Printf("config valid: nginx:%d  subscription:%v  hysteria2:%v\n",
		c.Nginx.Port, c.Subscription.Enabled, c.Cores.Hysteria2.Enabled)
	return nil
}

// cmdDomainToken prints the exact Cloudflare token recipe and the dashboard
// URL; running it on a desktop also opens the panel in the default browser.
func cmdDomainToken() error {
	fmt.Println(`Create the token ngray needs (2 minutes):

  1. Open:  https://dash.cloudflare.com/profile/api-tokens
  2. "Create Token" → "Edit zone DNS" template → Continue.
  3. Add a second permission:   Zone · Origin CA · Edit
     (the template only includes Zone · DNS · Edit)
  4. Zone Resources: "All zones" — or a specific zone that owns your domain.
  5. Finally: "Continue to summary" → CREATE TOKEN.
  6. Copy the long secret shown ONCE (starts with a few letters and looks
     like 37+ random chars). It is NOT the "Token ID" next to it, NOT the
     Account ID, NOT the Global API Key.
  7. Paste it into the TUI:  Domain / proxy (p) → CF token → enter → paste.
     It verifies once, then is cached in /etc/ngray/cf_token (mode 0600).

Permissions the token needs:
  · Zone · DNS          Edit    → create/update the proxied A record
  · Zone · Origin CA    Edit    → request the Origin CA certificate

Then:
  p  Domain / proxy  → Apply          (cert + DNS + config)
  ngray domain check <domain>                          (no token; probes DNS/edge/origin)`)
	url := "https://dash.cloudflare.com/profile/api-tokens"
	if _, err := exec.LookPath("xdg-open"); err == nil {
		_ = exec.Command("xdg-open", url).Start()
		fmt.Println("\nopening the panel in your browser:", url)
	} else {
		fmt.Println("\nopen this URL in a browser:", url)
	}
	return nil
}

// cmdSNI tests SNI candidates for REALITY fitness.
func cmdSNI(args []string) error {
	if len(args) > 0 && args[0] == "discover" {
		return cmdSNIDiscover(args[1:])
	}
	if len(args) == 0 || args[0] != "test" {
		return fmt.Errorf("usage: ngray sni test [host…] | ngray sni discover [--ref IP] [--blocks N]")
	}
	hosts := args[1:]
	if len(hosts) == 0 {
		var err error
		if hosts, err = sni.LoadCandidates(candidatesPath()); err != nil {
			return fmt.Errorf("no hosts given and %w", err)
		}
	}

	t := sni.NewTester()
	fmt.Printf("%-38s %-10s %s\n", "DOMAIN", "STATUS", "DETAILS")
	// Sweep scales to thousands of candidates: cheap probe across all, full
	// latency/HTTP measurement only on the hosts that pass.
	results := t.Sweep(context.Background(), hosts, 256, func(r sni.Result) {
		switch r.Status {
		case sni.StatusOK:
			fmt.Printf("%-38s %-10s ms=%-4d (±%-3d) http=%-3d ip=%s\n", r.Host, r.Status, r.MedianMS, r.JitterMS, r.HTTPCode, r.IP)
		default:
			fmt.Printf("%-38s %-10s ip=%s\n", r.Host, r.Status, r.IP)
		}
	})

	ranked := sni.Ranked(results)
	fmt.Printf("\nbest picks for REALITY (%d fit):\n", len(ranked))
	for i, r := range ranked {
		if i >= 10 {
			break
		}
		fmt.Printf("  %2d. %-32s %dms (±%d) http=%d\n", i+1, r.Host, r.MedianMS, r.JitterMS, r.HTTPCode)
	}
	return nil
}

// cmdServer provisions or repairs the server.
func cmdServer(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ngray server setup|status")
	}
	switch args[0] {
	case "setup":
		sandbox := contains(args, "--sandbox")
		root := flagValue(args, "--root")
		if root != "" {
			sandbox = true
		}
		if !sandbox && os.Geteuid() != 0 {
			return fmt.Errorf("server setup must run as root (or pass --sandbox for local testing)")
		}
		d, err := server.Setup(context.Background(), server.Options{
			ConfigPath: config.Path(),
			StatePath:  state.Path(),
			Sandbox:    sandbox,
			Root:       root,
			Confirm:    askYesNo,
			Log:        func(f string, a ...any) { fmt.Printf("  "+f+"\n", a...) },
		})
		if err != nil {
			return err
		}
		fmt.Printf("\ndone — %d endpoint(s)\n", len(d.Endpoints))
		st, err := state.Load(state.Path())
		if err != nil {
			return fmt.Errorf("load state: %w", err)
		}
		// Print only the subscribe URLs here; the full per-config dump is one
		// command away (`ngray client links`) and just clutters setup output.
		return server.RenderSubscribe(os.Stdout, st)
	case "status":
		return cmdStatus()
	default:
		return fmt.Errorf("usage: ngray server setup|status")
	}
}

// cmdClient renders share links and the subscribe URL.
func cmdClient(args []string) error {
	st, err := state.Load(state.Path())
	if err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "singbox" {
		b, err := server.SingboxJSON(st)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	cfg, _ := config.LoadOrDefault(config.Path())
	return server.RenderClient(os.Stdout, st, cfg)
}

// cmdUpdate updates cores, or ngray itself with --self.
func cmdUpdate(args []string) error {
	ctx := context.Background()
	log := func(f string, a ...any) { fmt.Printf("  "+f+"\n", a...) }
	if contains(args, "--self") {
		return server.SelfUpdate(ctx, log, true)
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("update must run as root")
	}
	return server.UpdateCores(ctx, askYesNo, log)
}

// selfUpdateAndReexec runs the bounded launch-time update; when the binary on
// disk is now newer, exec() replaces this process with it (same argv/environ)
// so the launched TUI IS the new version. Windows cannot exec over its own
// running image — there the new binary is picked up at the next start.
func selfUpdateAndReexec() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	finished := make(chan bool, 1)
	go func() {
		finished <- server.AutoUpdateOnLaunch(context.Background(), func(f string, a ...any) {
			fmt.Printf("  "+f+"\n", a...)
		})
	}()
	var updated bool
	select {
	case updated = <-finished:
	case <-time.After(90 * time.Second):
		return // release check hung; run what we have
	}
	if !updated {
		return
	}
	if runtime.GOOS != "windows" {
		syscall.Exec(exe, os.Args, os.Environ()) //nolint // only reachable once replaced
	}
}

// cmdDNS runs the DNS tunneling resolver validator or the DoH benchmark.
func cmdDNS(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ngray dns validate [flags] | ngray dns doh [phases]")
	}
	switch args[0] {
	case "validate":
		return dnsval.Run(args[1:])
	case "doh":
		phases := 10
		if len(args) > 1 {
			if n, err := strconv.Atoi(args[1]); err == nil {
				phases = n
			}
		}
		return dnsval.DoH(phases)
	default:
		return fmt.Errorf("usage: ngray dns validate|doh")
	}
}

// cmdDomain checks whether a domain is a working Cloudflare-fronted path to this
// server and prints scenario-aware advice ("be smart") before configs are made.
func cmdDomain(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "cert":
			return cmdDomainCert(args[1:])
		case "issue":
			return cmdDomainIssue(args[1:])
		case "dns":
			return cmdDomainDNS(args[1:])
		case "set":
			return cmdDomainSet(args[1:])
		case "token":
			return cmdDomainToken()
		}
	}
	if len(args) == 0 || args[0] != "check" {
		return fmt.Errorf("usage: ngray domain check <domain> [--ip IP]\n" +
			"       ngray domain token    step-by-step Cloudflare token guide\n" + "       ngray domain set <domain>          enable CDN fronting for a domain\n" +
			"       ngray domain issue <domain>        issue a Cloudflare Origin cert\n" +
			"       ngray domain dns <domain> [--ip IP] [--proxied=false]  upsert the A record\n" +
			"       ngray domain cert <cert.pem> <key.pem>")
	}
	rest := args[1:]
	var dom string
	for _, a := range rest {
		if !strings.HasPrefix(a, "-") {
			dom = a
			break
		}
	}
	if dom == "" {
		return fmt.Errorf("no domain given: ngray domain check <domain> [--ip IP]")
	}
	ip := flagValue(rest, "--ip")
	if ip == "" {
		if st, err := state.Load(state.Path()); err == nil {
			ip = st.IP
		}
	}
	if ip == "" {
		ip = system.New().PublicIP(context.Background())
	}
	if ip == "" {
		return fmt.Errorf("could not determine server IP; pass --ip")
	}

	fmt.Printf("checking %s against origin %s …\n\n", dom, ip)
	rep := domain.Check(context.Background(), dom, ip)
	fmt.Printf("  domain        %s\n", rep.Domain)
	fmt.Printf("  origin IP     %s\n", rep.ServerIP)
	fmt.Printf("  resolves to   %s\n", dash(strings.Join(rep.ResolvedIPs, ", ")))
	fmt.Printf("  cloudflare    %v\n", rep.ViaCloudflare)
	if rep.EdgeStatus > 0 {
		fmt.Printf("  edge status   HTTP %d (server: %s)\n", rep.EdgeStatus, dash(rep.ServerHeader))
	}
	fmt.Printf("  origin TLS    %v\n", rep.OriginTLSOK)
	if tok := cf.TokenFromEnvOrFile(); tok != "" {
		if err := cf.New(tok).VerifyToken(context.Background()); err == nil {
			fmt.Printf("  cf token      valid\n")
		} else {
			fmt.Printf("  cf token      invalid (%v)\n", err)
		}
	} else {
		fmt.Printf("  cf token      not configured (set NGRAY_CF_TOKEN or /etc/ngray/cf_token)\n")
	}
	fmt.Printf("\n  verdict: %s\n", rep.Verdict)
	for _, a := range rep.Advice {
		fmt.Printf("    • %s\n", a)
	}
	return nil
}

// cmdDomainCert validates a Cloudflare Origin Certificate + key pair and
// installs them at the CDN cert paths so the WS/XHTTP TLS inbounds can serve
// the domain (fixing CF error 525). Run as root (writes under /etc/ngray).
func cmdDomainCert(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: ngray domain cert <cert.pem> <key.pem>")
	}
	if _, err := tls.LoadX509KeyPair(args[0], args[1]); err != nil {
		return fmt.Errorf("cert/key invalid or do not match: %w", err)
	}
	// Cert paths come from the default CDN config; override from on-disk config
	// only if it loads cleanly (never depend on it — this command must work even
	// when config.yml is absent or invalid).
	cdn := config.Default().Cores.Xray.CDN
	if cfg, err := config.LoadOrDefault(config.Path()); err == nil && cfg.Cores.Xray.CDN != nil {
		cdn = cfg.Cores.Xray.CDN
	}
	if cdn == nil || cdn.CertFile == "" || cdn.KeyFile == "" {
		return fmt.Errorf("no cores.xray.cdn cert paths configured")
	}
	crt, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	key, err := os.ReadFile(args[1])
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cdn.CertFile), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(cdn.CertFile, crt, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(cdn.KeyFile, key, 0o600); err != nil {
		return err
	}
	fmt.Printf("installed origin cert → %s\ninstalled origin key  → %s\n", cdn.CertFile, cdn.KeyFile)
	fmt.Println("next: set Cloudflare SSL/TLS mode to Full (strict), then run: ngray server setup")
	return nil
}

// cfClient builds a Cloudflare client from env/0600 file, or errors clearly.
func cfClient() (*cf.Client, error) {
	tok := cf.TokenFromEnvOrFile()
	if tok == "" {
		return nil, fmt.Errorf("no Cloudflare token: set NGRAY_CF_TOKEN or write /etc/ngray/cf_token (0600)")
	}
	return cf.New(tok), nil
}

// cmdDomainSet enables CDN fronting for a domain in config.yml.
func cmdDomainSet(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: ngray domain set <domain>")
	}
	dom := args[0]
	cfg, err := config.LoadOrDefault(config.Path())
	if err != nil {
		return err
	}
	if cfg.Cores.Xray.CDN == nil {
		cfg.Cores.Xray.CDN = &config.CDN{CertFile: "/etc/ngray/cf-origin.crt", KeyFile: "/etc/ngray/cf-origin.key", WS: true}
	}
	cfg.Cores.Xray.CDN.Enabled = true
	cfg.Cores.Xray.CDN.Domain = dom
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := config.Save(config.Path(), cfg); err != nil {
		return err
	}
	fmt.Printf("CDN fronting enabled for %s\n", dom)
	fmt.Println("next: ngray domain issue <domain>, ngray domain dns <domain>, then ngray server setup")
	return nil
}

// cmdDomainIssue issues and installs a Cloudflare Origin certificate.
func cmdDomainIssue(args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: ngray domain issue <domain>")
	}
	dom := strings.TrimSuffix(strings.ToLower(args[0]), ".")
	c, err := cfClient()
	if err != nil {
		return err
	}
	ctx := context.Background()
	if err := c.VerifyToken(ctx); err != nil {
		return fmt.Errorf("cloudflare token rejected: %w", err)
	}
	fmt.Printf("requesting Cloudflare Origin certificate for %s …\n", dom)
	certPEM, keyPEM, err := c.IssueOriginCert(ctx, dom)
	if err != nil {
		return err
	}
	// Validate the pair before touching the live files.
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		return fmt.Errorf("cloudflare returned a mismatched cert/key: %w", err)
	}
	cdn := config.Default().Cores.Xray.CDN
	if cfg, err := config.LoadOrDefault(config.Path()); err == nil && cfg.Cores.Xray.CDN != nil {
		cdn = cfg.Cores.Xray.CDN
	}
	if cdn == nil || cdn.CertFile == "" || cdn.KeyFile == "" {
		return fmt.Errorf("no cdn cert paths configured")
	}
	if err := writeFileAtomic(cdn.KeyFile, keyPEM, 0o600); err != nil {
		return err
	}
	if err := writeFileAtomic(cdn.CertFile, certPEM, 0o644); err != nil {
		return err
	}
	fmt.Printf("issued → %s\n        → %s\n", cdn.CertFile, cdn.KeyFile)
	fmt.Println("set Cloudflare SSL/TLS mode to Full (strict), then run: ngray server setup")
	return nil
}

// writeFileAtomic writes data via a temp file + rename so a crash never leaves a
// truncated cert/key, and tightens the mode even for a pre-existing file.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
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

// cmdDomainDNS creates or updates the Cloudflare A record for a domain.
func cmdDomainDNS(args []string) error {
	ip := flagValue(args, "--ip")
	// The domain is the first positional left after removing flag pairs.
	var dom string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if a == "--ip" || a == "--proxied" {
				i++ // skip the value
			}
			continue
		}
		dom = strings.TrimSuffix(strings.ToLower(a), ".")
		break
	}
	if dom == "" {
		return fmt.Errorf("usage: ngray domain dns <domain> [--ip IP] [--proxied=false]")
	}
	if ip == "" {
		if st, err := state.Load(state.Path()); err == nil {
			ip = st.IP
		}
	}
	if ip == "" {
		ip = system.New().PublicIP(context.Background())
	}
	if ip == "" {
		return fmt.Errorf("could not determine server IP; pass --ip")
	}
	if net.ParseIP(ip) == nil || strings.Contains(ip, ":") {
		return fmt.Errorf("invalid IPv4 address %q", ip)
	}
	proxied := flagValue(args, "--proxied") != "false"
	c, err := cfClient()
	if err != nil {
		return err
	}
	if err := c.EnsureARecord(context.Background(), dom, ip, proxied); err != nil {
		return err
	}
	fmt.Printf("A %s → %s (proxied=%v)\n", dom, ip, proxied)
	return nil
}

// cmdProxy registers a domain as a reverse proxy in front of an arbitrary service.
func cmdProxy(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ngray proxy add <domain> --target <url> | list | remove <domain>")
	}
	cfg, err := config.LoadOrDefault(config.Path())
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		if len(cfg.ProxyRoutes) == 0 {
			fmt.Println("no proxy routes configured")
			return nil
		}
		for _, r := range cfg.ProxyRoutes {
			fmt.Printf("%s  ->  %s  (%s)\n", r.Domain, r.Target, r.CertFile)
		}
		return nil
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: ngray proxy remove <domain>")
		}
		dom := args[1]
		var kept []config.ProxyRoute
		for _, r := range cfg.ProxyRoutes {
			if r.Domain != dom {
				kept = append(kept, r)
			}
		}
		cfg.ProxyRoutes = kept
		if err := config.Save(config.Path(), cfg); err != nil {
			return err
		}
		fmt.Printf("removed %s; run `ngray server setup` to apply\n", dom)
		return nil
	case "add":
		// parse domain + --target
		if len(args) < 2 {
			return fmt.Errorf("usage: ngray proxy add <domain> --target <url> [--cert FILE --key FILE]")
		}
		dom := args[1]
		target := ""
		certFile, keyFile := "", ""
		for i := 2; i < len(args); i++ {
			switch args[i] {
			case "--target":
				if i+1 >= len(args) {
					return fmt.Errorf("missing value for --target")
				}
				target = args[i+1]
				i++
			case "--cert":
				if i+1 >= len(args) {
					return fmt.Errorf("missing value for --cert")
				}
				certFile = args[i+1]
				i++
			case "--key":
				if i+1 >= len(args) {
					return fmt.Errorf("missing value for --key")
				}
				keyFile = args[i+1]
				i++
			default:
				return fmt.Errorf("unknown flag %q", args[i])
			}
		}
		if target == "" {
			return fmt.Errorf("proxy add requires --target <http(s)://host:port>")
		}
		if (certFile == "") != (keyFile == "") {
			return fmt.Errorf("--cert and --key must be given together")
		}
		// strict target validation: parse as URL, require http/https scheme with host,
		// and reject any control/special characters that could break nginx config.
		u, err := url.Parse(target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("invalid target %q: must be http:// or https:// URL with a host", target)
		}
		if u.User != nil {
			return fmt.Errorf("target must not contain userinfo")
		}
		if strings.ContainsAny(target, " \t\r\n;{}\"'\\$") {
			return fmt.Errorf("target contains invalid characters")
		}
		target = u.String()
		// cert resolution: if not provided, try CF origin cert paths
		if certFile == "" {
			certFile = "/etc/ngray/cf-origin-" + dom + ".crt"
			keyFile = "/etc/ngray/cf-origin-" + dom + ".key"
		}
		// remove existing route for this domain
		var kept []config.ProxyRoute
		for _, r := range cfg.ProxyRoutes {
			if r.Domain != dom {
				kept = append(kept, r)
			}
		}
		cfg.ProxyRoutes = append(kept, config.ProxyRoute{
			Domain:   dom,
			Target:   target,
			CertFile: certFile,
			KeyFile:  keyFile,
		})
		if err := cfg.Validate(); err != nil {
			return err
		}
		if err := config.Save(config.Path(), cfg); err != nil {
			return err
		}
		fmt.Printf("registered proxy: %s -> %s\n", dom, target)
		fmt.Println("run `ngray server setup` to apply")
		return nil
	default:
		return fmt.Errorf("usage: ngray proxy add <domain> --target <url> | list | remove <domain>")
	}
}

// cmdDoctor diagnoses cores, nginx, BBR and foreign panels.
func cmdDoctor() error {
	cfg, err := config.LoadOrDefault(config.Path())
	if err != nil {
		return err
	}
	checks := server.Doctor(context.Background(), cfg)
	color := map[server.Status]string{server.Pass: "\033[32m", server.Warn: "\033[33m", server.Fail: "\033[31m"}
	for _, c := range checks {
		fmt.Printf("  %s%-5s\033[0m %-14s %s\n", color[c.Status], c.Status, c.Name, c.Detail)
		if c.Fix != "" && c.Status != server.Pass {
			fmt.Printf("        \033[2mfix: %s\033[0m\n", c.Fix)
		}
	}
	return nil
}

// cmdManage runs clean or remove (root required).
func cmdManage(action string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("%s must run as root", action)
	}
	if !askYesNo(fmt.Sprintf("%s ngray's configs and services?", action)) {
		return nil
	}
	opts := server.ManageOptions{
		StatePath: state.Path(),
		Confirm:   askYesNo,
		Log:       func(f string, a ...any) { fmt.Printf("  "+f+"\n", a...) },
	}
	if action == "remove" {
		return server.Remove(context.Background(), opts)
	}
	return server.Clean(context.Background(), opts)
}

// cmdRegen clears generated credentials/keys/ports so the next setup regenerates them.
func cmdRegen() error {
	if !askYesNo("regenerate ALL keys, credentials and the subscribe token?") {
		return nil
	}
	st, err := state.Load(state.Path())
	if err != nil {
		return err
	}
	st.Keys, st.Secrets, st.Ports, st.SubToken, st.Deploy = nil, nil, nil, "", nil
	if err := st.Save(); err != nil {
		return err
	}
	fmt.Println("cleared — run `ngray server setup` to regenerate and apply")
	return nil
}

// cmdStatus reports each core's installed version, service state and latest release.
func cmdStatus() error {
	ctx := context.Background()
	sys := system.New()
	fmt.Printf("%-10s %-10s %-8s %-10s\n", "CORE", "INSTALLED", "ACTIVE", "LATEST")
	for _, core := range cores.Registry() {
		ver, ok := core.Installed(ctx, sys)
		if !ok {
			ver = "-"
		}
		active := "no"
		if sys.IsActive(ctx, core.Service()) {
			active = "yes"
		}
		latest, _ := sys.LatestVersion(ctx, core.Repo())
		mark := ""
		if ok && latest != "" && ver != latest {
			mark = "  ⬆ update"
		}
		fmt.Printf("%-10s %-10s %-8s %-10s%s\n", core.Name(), ver, active, dash(latest), mark)
	}
	return nil
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func askYesNo(prompt string) bool {
	fmt.Printf("%s [Y/n] ", prompt)
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return true
	}
	ans := strings.ToLower(strings.TrimSpace(sc.Text()))
	return ans == "" || ans == "y" || ans == "yes"
}

// cmdFirewall derives the public-port ruleset from config and applies it.
func cmdFirewall(args []string) error {
	if len(args) == 0 || args[0] != "apply" {
		return fmt.Errorf("usage: ngray firewall apply [--yes]")
	}
	c, err := config.LoadOrDefault(config.Path())
	if err != nil {
		return err
	}
	st, _ := state.Load(state.Path())
	rules := rulesFromConfig(c, st)

	fmt.Println("Firewall plan (default-drop inbound):")
	for _, line := range rules.Plan() {
		fmt.Println("  •", line)
	}
	if !contains(args, "--yes") {
		fmt.Println("\nre-run with --yes to apply")
		return nil
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("must run as root to apply firewall")
	}
	if err := firewall.New(system.New()).Apply(context.Background(), rules); err != nil {
		return err
	}
	fmt.Println("firewall applied")
	return nil
}

func rulesFromConfig(c *config.Config, st *state.State) firewall.Rules {
	r := firewall.Rules{TCP: []int{c.Nginx.Port}}
	if st != nil {
		for _, p := range st.Ports {
			r.TCP = append(r.TCP, p)
		}
	}
	if c.Cores.Hysteria2.Enabled {
		r.UDP = append(r.UDP, c.Cores.Hysteria2.Port)
	}
	return r
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// flagValue returns the value after `name` in args, or "".
func flagValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(a, name+"="); ok {
			return v
		}
	}
	return ""
}

// cmdSNIDiscover scans /24 blocks near the server for REALITY-fit SNIs.
func cmdSNIDiscover(args []string) error {
	ref := flagValue(args, "--ref")
	if ref == "" {
		ref = system.New().PublicIP(context.Background())
	}
	if ref == "" {
		return fmt.Errorf("could not detect server IP; pass --ref IP")
	}
	blocks := 4
	if v := flagValue(args, "--blocks"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			blocks = n
		}
	}
	fmt.Printf("discovering REALITY SNIs near %s (±%d /24 blocks)…\n", ref, blocks)
	found := sni.Discover(context.Background(), ref, blocks, 256, func(done, total int) {
		fmt.Printf("\r  scanned %d/%d", done, total)
	})
	fmt.Printf("\r%-40s\n", " ")
	fmt.Printf("found %d REALITY-fit domains (TLS1.3 + h2):\n", len(found))
	for i, f := range found {
		if i >= 30 {
			break
		}
		fmt.Printf("  %-36s %dms  %s\n", f.Domain, f.LatencyMS, f.IP)
	}
	return nil
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
