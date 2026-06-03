// Package server orchestrates end-to-end provisioning: install cores, write
// their configs, build the nginx SNI mux + subscription, tune, firewall, and
// snapshot the deployment for client output.
package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/cores"
	"github.com/abbasnaqdi/ng-ray/internal/firewall"
	"github.com/abbasnaqdi/ng-ray/internal/links"
	"github.com/abbasnaqdi/ng-ray/internal/nginx"
	"github.com/abbasnaqdi/ng-ray/internal/ports"
	"github.com/abbasnaqdi/ng-ray/internal/sni"
	"github.com/abbasnaqdi/ng-ray/internal/state"
	"github.com/abbasnaqdi/ng-ray/internal/sub"
	"github.com/abbasnaqdi/ng-ray/internal/system"
)

type Options struct {
	ConfigPath     string
	StatePath      string
	CandidatesPath string                        // path to candidates.sni; auto-resolved if empty
	Sandbox        bool                          // redirect writes + skip host mutations
	Root           string                        // sandbox dir (default ./sandbox)
	Confirm        func(prompt string) bool      // install permission gate
	Log            func(format string, a ...any) // progress output
}

func (o Options) confirm(p string) bool {
	if o.Confirm == nil {
		return true
	}
	return o.Confirm(p)
}

func (o Options) log(f string, a ...any) {
	if o.Log != nil {
		o.Log(f, a...)
	}
}

// Setup provisions the server and returns the resulting deployment snapshot.
func Setup(ctx context.Context, o Options) (*state.Deploy, error) {
	sys := system.New()
	if o.Sandbox {
		root := o.Root
		if root == "" {
			root = "./sandbox"
		}
		sys = system.NewSandbox(root, o.Log)
		o.log("sandbox: writing under %s, host mutations skipped", root)
	}
	freshInstall := false
	if _, statErr := os.Stat(o.ConfigPath); os.IsNotExist(statErr) {
		freshInstall = true
	}

	cfg, err := config.LoadOrDefault(o.ConfigPath)
	if err != nil {
		return nil, err
	}

	if freshInstall && !sys.DryRun {
		candidatesPath := o.CandidatesPath
		if candidatesPath == "" {
			candidatesPath = candidatesNear(o.ConfigPath)
		}
		_ = autoPopulateSNIs(ctx, candidatesPath, o.ConfigPath, cfg, o.log)
	}
	st, err := state.Load(o.StatePath)
	if err != nil {
		return nil, err
	}

	ip := cfg.Server.IP
	if ip == "" || ip == "auto" {
		ip = sys.PublicIP(ctx)
		o.log("detected public IP: %s", ip)
	}
	st.IP, st.NginxPort = ip, cfg.Nginx.Port

	if err := ensureNginx(ctx, sys, o); err != nil {
		return nil, err
	}

	validateSNIs(ctx, cfg, o)

	plan := &cores.Plan{Cfg: cfg, St: st, Alloc: ports.NewAllocator(st), Sys: sys, IP: ip}
	var routes []nginx.Route
	var endpoints []links.Endpoint
	notes := map[string]string{}

	for _, core := range cores.Registry() {
		if !core.Enabled(cfg) {
			continue
		}
		if _, ok := core.Installed(ctx, sys); !ok {
			if sys.DryRun {
				o.log("would install %s (skipped in sandbox)", core.Name())
			} else if !o.confirm(fmt.Sprintf("install %s?", core.Name())) {
				o.log("skipping %s (declined)", core.Name())
				continue
			} else {
				o.log("installing %s…", core.Name())
				if err := core.Install(ctx, sys); err != nil {
					return nil, fmt.Errorf("install %s: %w", core.Name(), err)
				}
			}
		}
		res, err := core.Build(plan)
		if err != nil {
			return nil, fmt.Errorf("configure %s: %w", core.Name(), err)
		}
		routes = append(routes, res.Routes...)
		endpoints = append(endpoints, res.Endpoints...)
		for k, v := range res.Notes {
			notes[k] = v
		}
		// mieru/AmneziaWG/fptn deliver an importable config via Notes rather than
		// share-link endpoints; count those so the log doesn't read "0".
		if n := len(res.Endpoints); n > 0 {
			o.log("configured %s (%d endpoint(s))", core.Name(), n)
		} else {
			o.log("configured %s (%d client config(s) — see menu 2)", core.Name(), len(res.Notes))
		}
	}

	if len(cfg.Cores.Singbox.Reality) > 0 {
		notes["sing-box client config"] = "To print the client JSON config for sing-box, run: ngray client singbox"
	}
	ngCfg := nginx.Config{Port: cfg.Nginx.Port, Sink: ports.HTTPSink, Routes: routes}
	deploy := &state.Deploy{Endpoints: endpoints, Notes: notes}

	if cfg.Subscription.Enabled {
		token := st.EnsureSubToken()

		// Self-signed cert: regenerate if missing or if the server IP changed.
		if !sys.CertHasIPSAN(sub.CertFile, ip) {
			_ = sys.Remove(sub.CertFile)
			_ = sys.Remove(sub.KeyFile)
		}
		extraSANs := []string{ip}
		if cfg.Subscription.FrontSNI != "" {
			extraSANs = append(extraSANs, cfg.Subscription.FrontSNI)
		}
		if err := sys.GenSelfSigned(ip, sub.CertFile, sub.KeyFile, extraSANs...); err != nil {
			o.log("warning: subscription TLS cert: %v", err)
		}

		// Write base64 URI bundle (all endpoints, no emit filter).
		if err := sub.WriteBundle(sys, links.Subscription(endpoints)); err != nil {
			return nil, err
		}
		// Write sing-box JSON bundle (best-effort; non-fatal).
		if sjson, err := links.SingboxJSON(endpoints); err == nil {
			if err := sub.WriteSingboxBundle(sys, sjson); err != nil {
				o.log("warning: singbox bundle: %v", err)
			}
		}

		httpsPort := cfg.Subscription.HTTPSPort
		if httpsPort <= 0 {
			httpsPort = sub.DefaultHTTPSPort
		}

		ngCfg.Sub = &nginx.Subscription{
			Dir:         sub.Dir,
			BundleFile:  "bundle",
			SingboxFile: "singbox-bundle.json",
			CertFile:    sub.CertFile,
			KeyFile:     sub.KeyFile,
			HTTPSPort:   httpsPort,
		}
		deploy.SubURL = sub.HTTPSURL(ip, token, httpsPort)
		deploy.SubHTTPURL = sub.URL(ip, token)
		deploy.SubSingboxURL = sub.SingboxURL(ip, token, httpsPort)
		if fp, err := sys.CertFingerprint(sub.CertFile); err == nil {
			deploy.SubCertFingerprint = fp
		}
		o.log("subscription: HTTPS port %d, HTTP port %d", httpsPort, sub.HTTPPort)
	}

	if err := writeNginx(ctx, sys, ngCfg, o); err != nil {
		return nil, err
	}

	if cfg.Server.EnableBBR {
		if err := sys.EnableBBR(ctx); err != nil {
			o.log("warning: BBR tuning failed: %v", err)
		}
		st.BBR = true
	}

	restartServices(ctx, sys, cfg, o)

	if err := applyFirewall(ctx, sys, cfg, st, o); err != nil {
		o.log("warning: firewall: %v", err)
	}

	st.Deploy = deploy
	if err := st.Save(); err != nil {
		return nil, err
	}
	return deploy, nil
}

// validateSNIs runs the SNI tester over every REALITY SNI before configs are
// generated, so the operator sees which destinations are REALITY-fit. It warns
// rather than skips: server-side reachability is not an operator's whitelist.
func validateSNIs(ctx context.Context, cfg *config.Config, o Options) {
	seen := map[string]bool{}
	var hosts []string
	for _, s := range cfg.RouteSNIs() {
		if !seen[s] {
			seen[s] = true
			hosts = append(hosts, s)
		}
	}
	if len(hosts) == 0 {
		return
	}
	o.log("validating %d REALITY SNI(s) via the sni tester…", len(hosts))
	sni.NewTester().TestAll(ctx, hosts, 12, func(r sni.Result) {
		if r.Fit() {
			o.log("  ✓ %s  ok  (%dms)", r.Host, r.MedianMS)
		} else {
			o.log("  ! %s  %s — keeping (operator whitelist may differ)", r.Host, r.Status)
		}
	})
}

// candidatesNear resolves snis/candidates.sni relative to configPath (which
// lives next to the binary), falling back to a relative path for dev runs.
func candidatesNear(configPath string) string {
	rel := filepath.Join(filepath.Dir(configPath), "snis", "candidates.sni")
	if _, err := os.Stat(rel); err == nil {
		return rel
	}
	return filepath.Join("snis", "candidates.sni")
}

func ensureNginx(ctx context.Context, sys *system.Env, o Options) error {
	if sys.DryRun {
		o.log("would ensure nginx + libnginx-mod-stream")
		return nil
	}
	if sys.Which("nginx") {
		return sys.AptInstall(ctx, "libnginx-mod-stream")
	}
	if !o.confirm("install nginx?") {
		return fmt.Errorf("nginx required")
	}
	o.log("installing nginx…")
	if err := sys.AptUpdate(ctx); err != nil {
		return err
	}
	if err := sys.AptInstall(ctx, "nginx", "libnginx-mod-stream"); err != nil {
		return err
	}
	return sys.EnableNow(ctx, "nginx")
}

func writeNginx(ctx context.Context, sys *system.Env, c nginx.Config, o Options) error {
	// Back up a pre-existing non-ngray nginx.conf before replacing it; never
	// touch it silently. ngray-owned configs (with the marker) are rebuilt in place.
	if b, err := sys.ReadFile(nginx.ConfPath); err == nil && !strings.HasPrefix(string(b), nginx.Marker) {
		dst := fmt.Sprintf("/etc/ngray/backup/%s/nginx.conf", time.Now().Format("20060102-150405"))
		if err := sys.WriteFile(dst, b, 0o644); err == nil {
			o.log("backed up existing nginx.conf → %s", dst)
		}
	}
	if err := sys.WriteString(nginx.ConfPath, c.Render(), 0o644); err != nil {
		return err
	}
	if out, err := sys.Run(ctx, "nginx", "-t"); err != nil {
		return fmt.Errorf("nginx -t failed: %s", out)
	}
	o.log("nginx.conf written and validated")
	if err := sys.Restart(ctx, "nginx"); err != nil {
		if b, rerr := sys.ReadFile("/var/log/nginx/error.log"); rerr == nil {
			lines := strings.Split(strings.TrimSpace(string(b)), "\n")
			if n := len(lines); n > 10 {
				lines = lines[n-10:]
			}
			return fmt.Errorf("%w\nnginx error log:\n%s", err, strings.Join(lines, "\n"))
		}
		return err
	}
	return nil
}

func restartServices(ctx context.Context, sys *system.Env, cfg *config.Config, o Options) {
	for _, core := range cores.Registry() {
		if !core.Enabled(cfg) {
			continue
		}
		// Enable for boot, then ALWAYS restart to load the freshly written
		// config. `enable --now` is a no-op on an already-running service, so
		// relying on it alone leaves running cores on their previous config.
		_ = sys.EnableNow(ctx, core.Service())
		if err := sys.Restart(ctx, core.Service()); err != nil {
			o.log("warning: restart %s: %v", core.Service(), err)
		} else {
			o.log("service %s restarted", core.Service())
		}
	}
}

func applyFirewall(ctx context.Context, sys *system.Env, cfg *config.Config, st *state.State, o Options) error {
	r := firewall.Rules{TCP: []int{cfg.Nginx.Port}}
	if cfg.Subscription.Enabled {
		httpsPort := cfg.Subscription.HTTPSPort
		if httpsPort <= 0 {
			httpsPort = sub.DefaultHTTPSPort
		}
		r.TCP = append(r.TCP, sub.HTTPPort, httpsPort)
	}
	for _, p := range st.Ports {
		r.TCP = append(r.TCP, p)
	}
	if cfg.Cores.Hysteria2.Enabled {
		h := cfg.Cores.Hysteria2
		r.UDP = append(r.UDP, h.Port)
		for _, ap := range h.AltPorts {
			r.UDP = append(r.UDP, ap)
			r.Redirects = append(r.Redirects, firewall.UDPRedirect{From: ap, To: h.Port})
		}
	}
	if t := cfg.Cores.Singbox.TUIC; t != nil && t.Enabled {
		r.UDP = append(r.UDP, t.Port)
	}
	if stl := cfg.Cores.Singbox.ShadowTLS; stl != nil && stl.Enabled {
		r.TCP = append(r.TCP, stl.Port)
	}
	if t := cfg.Cores.Shoes.TUIC; t != nil && t.Enabled {
		r.UDP = append(r.UDP, t.Port)
	}
	if h := cfg.Cores.Shoes.Hysteria2; h != nil && h.Enabled {
		r.UDP = append(r.UDP, h.Port)
	}
	if cfg.Cores.Mieru.Enabled {
		r.TCP = append(r.TCP, cfg.Cores.Mieru.Port)
	}
	if cfg.Cores.AmneziaWG.Enabled {
		r.UDP = append(r.UDP, cfg.Cores.AmneziaWG.Port)
	}
	if cfg.Cores.Fptn.Enabled {
		r.TCP = append(r.TCP, cfg.Cores.Fptn.Port)
	}
	o.log("applying firewall: tcp%v udp%v", r.TCP, r.UDP)
	return firewall.New(sys).Apply(ctx, r)
}
