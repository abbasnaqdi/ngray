package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/abbasnaqdi/ng-ray/internal/cdnscan"
	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/cores"
	"github.com/abbasnaqdi/ng-ray/internal/dnsval"
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
		path := config.FileName
		if err := config.Save(path, c); err != nil {
			return err
		}
		fmt.Println("wrote", path)
		return nil
	}
	fmt.Printf("config valid: nginx:%d  subscription:%v  hysteria2:%v  fptn:%v\n",
		c.Nginx.Port, c.Subscription.Enabled, c.Cores.Hysteria2.Enabled, c.Cores.Fptn.Enabled)
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
		cfg, _ := config.LoadOrDefault(config.Path())
		return server.RenderClient(os.Stdout, st, cfg)
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
		return server.SelfUpdate(ctx, log)
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("update must run as root")
	}
	return server.UpdateCores(ctx, askYesNo, log)
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
	if c.Cores.Fptn.Enabled {
		r.TCP = append(r.TCP, c.Cores.Fptn.Port)
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
	return rel
}
