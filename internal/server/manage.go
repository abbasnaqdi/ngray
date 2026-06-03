package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/cores"
	"github.com/abbasnaqdi/ng-ray/internal/firewall"
	"github.com/abbasnaqdi/ng-ray/internal/nginx"
	"github.com/abbasnaqdi/ng-ray/internal/sub"
	"github.com/abbasnaqdi/ng-ray/internal/system"
)

// managedPaths are the files ngray owns and may back up + rebuild. Anything
// outside this list (x3-ui panels, custom services) is never touched.
var managedPaths = []string{
	nginx.ConfPath,
	"/etc/shoes/server.yaml",
	"/etc/sing-box/config.json",
	"/usr/local/etc/xray/config.json",
	"/etc/hysteria/config.yaml",
	sub.BundleFile,
	sub.SingboxFile,
	sub.CertFile,
	sub.KeyFile,
}

type Status string

const (
	Pass Status = "PASS"
	Warn Status = "WARN"
	Fail Status = "FAIL"
)

type Check struct {
	Name   string
	Status Status
	Detail string
	Fix    string
}

// Doctor diagnoses every enabled core and the shared plumbing, returning an
// actionable report (no changes made).
func Doctor(ctx context.Context, cfg *config.Config) []Check {
	sys := system.New()
	var out []Check

	// nginx + ownership
	if sys.Which("nginx") {
		ok := sys.OK(ctx, "nginx", "-t")
		owned := false
		if b, err := sys.ReadFile(nginx.ConfPath); err == nil {
			owned = strings.HasPrefix(string(b), nginx.Marker)
		}
		switch {
		case !ok:
			out = append(out, Check{"nginx config", Fail, "nginx -t failed", "ngray server setup"})
		case !owned:
			out = append(out, Check{"nginx config", Warn, "nginx.conf not ngray-managed", "ngray server setup (backs up + rebuilds)"})
		default:
			out = append(out, Check{"nginx config", Pass, "valid + ngray-managed", ""})
		}
	} else {
		out = append(out, Check{"nginx", Fail, "not installed", "ngray server setup"})
	}

	for _, core := range cores.Registry() {
		if !core.Enabled(cfg) {
			continue
		}
		ver, ok := core.Installed(ctx, sys)
		if !ok {
			out = append(out, Check{core.Name(), Fail, "not installed", "ngray server setup"})
			continue
		}
		if sys.IsActive(ctx, core.Service()) {
			out = append(out, Check{core.Name(), Pass, "active " + ver, ""})
		} else {
			out = append(out, Check{core.Name(), Fail, "installed " + ver + " but service inactive",
				"journalctl -u " + core.Service() + " -n50  ·  systemctl restart " + core.Service()})
		}
	}

	// BBR
	if cc, _ := sys.Run(ctx, "sysctl", "-n", "net.ipv4.tcp_congestion_control"); cc == "bbr" {
		out = append(out, Check{"bbr", Pass, "active", ""})
	} else if cfg.Server.EnableBBR {
		out = append(out, Check{"bbr", Warn, "not active (" + cc + ")", "ngray server setup"})
	}

	for _, f := range DetectForeign(ctx, sys) {
		out = append(out, Check{"foreign", Warn, f, "left untouched by ngray"})
	}
	return out
}

// DetectForeign reports other proxy panels present so the operator knows ngray
// will not modify them.
func DetectForeign(ctx context.Context, sys *system.Env) []string {
	var found []string
	for _, svc := range []string{"x-ui", "3x-ui", "x3-ui", "hiddify", "marzban"} {
		if sys.IsActive(ctx, svc) {
			found = append(found, svc+" service active")
		}
	}
	return found
}

type ManageOptions struct {
	StatePath string
	Confirm   func(string) bool
	Log       func(string, ...any)
}

func (o ManageOptions) log(f string, a ...any) {
	if o.Log != nil {
		o.Log(f, a...)
	}
}

// Clean backs up and removes ngray-owned config files and stops its services,
// leaving core binaries and foreign panels intact.
func Clean(ctx context.Context, o ManageOptions) error {
	sys := system.New()
	backup := fmt.Sprintf("/etc/ngray/backup/%s", time.Now().Format("20060102-150405"))

	for _, core := range cores.Registry() {
		_, _ = sys.Run(ctx, "systemctl", "stop", core.Service())
	}
	for _, p := range managedPaths {
		if !sys.Exists(p) {
			continue
		}
		if err := sys.CopyFile(p, backup+p); err == nil {
			o.log("backed up %s", p)
		}
		_ = sys.Remove(p)
	}
	o.log("cleaned ngray configs (backup: %s)", backup)
	return nil
}

// Remove fully uninstalls ngray's footprint: stop+disable services, remove its
// configs and firewall chain. Core binaries are removed only on confirm.
func Remove(ctx context.Context, o ManageOptions) error {
	sys := system.New()
	if err := Clean(ctx, o); err != nil {
		return err
	}
	for _, core := range cores.Registry() {
		_, _ = sys.Run(ctx, "systemctl", "disable", core.Service())
		_ = sys.Remove("/etc/systemd/system/" + core.Service() + ".service")
	}
	_, _ = sys.Run(ctx, "systemctl", "daemon-reload")
	firewall.New(sys).Clear(ctx)
	o.log("removed ngray services and firewall chain")
	if o.Confirm != nil && o.Confirm("also remove core binaries (shoes/xray/sing-box/hysteria)?") {
		for _, p := range []string{"/usr/local/bin/shoes", "/usr/local/bin/xray", "/usr/bin/sing-box", "/usr/local/bin/hysteria"} {
			_ = sys.Remove(p)
		}
		o.log("removed core binaries")
	}
	return nil
}
