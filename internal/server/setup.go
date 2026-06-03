// Package server orchestrates end-to-end provisioning: install cores, write
// their configs, build the nginx SNI mux + subscription, tune, firewall, and
// snapshot the deployment for client output.
package server

import (
	"context"
	"fmt"

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
	ConfigPath string
	StatePath  string
	Sandbox    bool                          // redirect writes + skip host mutations
	Root       string                        // sandbox dir (default ./sandbox)
	Confirm    func(prompt string) bool      // install permission gate
	Log        func(format string, a ...any) // progress output
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
	cfg, err := config.LoadOrDefault(o.ConfigPath)
	if err != nil {
		return nil, err
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
		o.log("configured %s (%d endpoint(s))", core.Name(), len(res.Endpoints))
	}

	ngCfg := nginx.Config{Port: cfg.Nginx.Port, Sink: ports.HTTPSink, Routes: routes}
	deploy := &state.Deploy{Endpoints: endpoints, Notes: notes}

	if cfg.Subscription.Enabled {
		token := st.EnsureSubToken()
		if err := sys.GenSelfSigned(cfg.Subscription.FrontSNI, sub.CertPath, sub.KeyPath); err != nil {
			return nil, err
		}
		if err := sub.WriteBundle(sys, links.Subscription(endpoints)); err != nil {
			return nil, err
		}
		ngCfg.Sub = &nginx.Subscription{
			FrontSNI: cfg.Subscription.FrontSNI, Token: token, HTTPSPort: sub.HTTPSPort,
			CertPath: sub.CertPath, KeyPath: sub.KeyPath, BundleFile: sub.BundleFile,
		}
		deploy.SubURL = sub.URL(cfg.Subscription.FrontSNI, token)
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

	if err := applyFirewall(ctx, sys, cfg, o); err != nil {
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
	if err := sys.WriteString(nginx.ConfPath, c.Render(), 0o644); err != nil {
		return err
	}
	if out, err := sys.Run(ctx, "nginx", "-t"); err != nil {
		return fmt.Errorf("nginx -t failed: %s", out)
	}
	o.log("nginx.conf written and validated")
	return sys.Restart(ctx, "nginx")
}

func restartServices(ctx context.Context, sys *system.Env, cfg *config.Config, o Options) {
	for _, core := range cores.Registry() {
		if !core.Enabled(cfg) {
			continue
		}
		if err := sys.EnableNow(ctx, core.Service()); err != nil {
			_ = sys.Restart(ctx, core.Service())
		}
		o.log("service %s up", core.Service())
	}
}

func applyFirewall(ctx context.Context, sys *system.Env, cfg *config.Config, o Options) error {
	r := firewall.Rules{TCP: []int{cfg.Nginx.Port}}
	if cfg.Cores.Hysteria2.Enabled {
		r.UDP = append(r.UDP, cfg.Cores.Hysteria2.Port)
	}
	if cfg.Cores.Fptn.Enabled {
		r.TCP = append(r.TCP, cfg.Cores.Fptn.Port)
	}
	o.log("applying firewall: tcp%v udp%v", r.TCP, r.UDP)
	return firewall.New(sys).Apply(ctx, r)
}
