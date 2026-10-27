package cores

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/links"
	"github.com/abbasnaqdi/ng-ray/internal/state"
	"github.com/abbasnaqdi/ng-ray/internal/system"
	"gopkg.in/yaml.v3"
)

const hyBin = "/usr/local/bin/hysteria"

const hyDir = "/etc/hysteria"

// Hysteria is an independent QUIC/UDP core (not behind nginx).
type Hysteria struct{}

func (Hysteria) Name() string                  { return "hysteria" }
func (Hysteria) Repo() string                  { return "apernet/hysteria" }
func (Hysteria) Service() string               { return "hysteria-server" }
func (Hysteria) Enabled(c *config.Config) bool { return c.Cores.Hysteria2.Enabled }

func (Hysteria) Installed(ctx context.Context, sys *system.Env) (string, bool) {
	return detect(ctx, sys, "hysteria")
}

// Install downloads the hysteria2 binary directly from GitHub releases,
// bypassing get.hy2.sh which makes rate-limited api.github.com calls.
func (Hysteria) Install(ctx context.Context, sys *system.Env) error {
	url, err := sys.AssetURL(ctx, "apernet/hysteria", `hysteria-linux-amd64$`)
	if err != nil {
		return err
	}
	tmp := filepath.Join(os.TempDir(), "hysteria-linux-amd64")
	if err := sys.Download(ctx, url, tmp); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	// Atomic rename avoids ETXTBSY when replacing a running binary.
	if err := os.Rename(tmp, hyBin); err != nil {
		return err
	}
	if err := os.MkdirAll(hyDir, 0o755); err != nil {
		return err
	}
	if err := sys.WriteUnit("hysteria-server.service",
		unit("hysteria2", hyBin+" server -c "+hyDir+"/config.yaml")); err != nil {
		return err
	}
	if err := sys.DaemonReload(ctx); err != nil {
		return err
	}
	return sys.EnableNow(ctx, "hysteria-server")
}

func (Hysteria) Build(p *Plan) (Result, error) {
	h := p.Cfg.Cores.Hysteria2
	if err := p.Sys.GenSelfSigned(h.SNI, hyDir+"/server.crt", hyDir+"/server.key"); err != nil {
		return Result{}, err
	}
	auth := p.St.Secret("hysteria|auth", func() state.Secret { return state.Secret{Password: state.NewPassword(20)} }).Password
	obfs := p.St.Secret("hysteria|obfs", func() state.Secret { return state.Secret{Password: state.NewPassword(20)} }).Password

	cfg := map[string]any{
		"listen": ":" + itoa(h.Port),
		"tls":    map[string]any{"cert": hyDir + "/server.crt", "key": hyDir + "/server.key"},
		"auth":   map[string]any{"type": "password", "password": auth},
		"quic": map[string]any{
			"initStreamReceiveWindow": 26843545, "maxStreamReceiveWindow": 26843545,
			"initConnReceiveWindow": 67108864, "maxConnReceiveWindow": 67108864,
			"maxIdleTimeout": "60s", "keepAlivePeriod": "10s", "disablePathMTUDiscovery": false,
		},
		"ignoreClientBandwidth": true,
		"masquerade": map[string]any{
			"type": "proxy", "proxy": map[string]any{"url": "https://" + h.SNI, "rewriteHost": true},
		},
	}
	// obfs is optional: an empty Obfs disables it entirely (the server and
	// client both drop the block). With salamander/gecko the type name is the
	// obfs subkey, per hysteria's config syntax.
	if h.Obfs != "" {
		cfg["obfs"] = map[string]any{"type": h.Obfs, h.Obfs: map[string]any{"password": obfs}}
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return Result{}, err
	}
	if err := p.Sys.WriteFile(hyDir+"/config.yaml", out, 0o644); err != nil {
		return Result{}, err
	}
	fp, _ := p.Sys.CertFingerprint(hyDir + "/server.crt")
	certPEM := p.Sys.CertPEM(hyDir + "/server.crt")

	base := links.Endpoint{
		Proto: "hysteria2", Server: p.IP, SNI: h.SNI,
		Obfs: h.Obfs, ObfsPass: obfs, AuthPass: auth,
		Fingerprint: fp, CertPEM: certPEM,
	}
	eps := []links.Endpoint{func() links.Endpoint { e := base; e.Tag = links.Tag("hy2", "", h.SNI); e.Port = h.Port; return e }()}
	// Alt ports share the same credentials; firewall redirects them to h.Port.
	for _, ap := range h.AltPorts {
		e := base
		e.Tag = fmt.Sprintf("hy2-alt%d", ap)
		e.Port = ap
		eps = append(eps, e)
	}
	return Result{Endpoints: eps}, nil
}
