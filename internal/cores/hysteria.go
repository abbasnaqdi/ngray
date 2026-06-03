package cores

import (
	"context"
	"fmt"
	"os"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/links"
	"github.com/abbasnaqdi/ng-ray/internal/state"
	"github.com/abbasnaqdi/ng-ray/internal/system"
	"gopkg.in/yaml.v3"
)

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

func (Hysteria) Install(ctx context.Context, sys *system.Env) error {
	if _, err := sys.Run(ctx, "bash", "-c", "curl -fsSL https://get.hy2.sh/ | bash"); err != nil {
		return err
	}
	return os.MkdirAll(hyDir, 0o755)
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
		"obfs":   map[string]any{"type": h.Obfs, h.Obfs: map[string]any{"password": obfs}},
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
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return Result{}, err
	}
	if err := p.Sys.WriteFile(hyDir+"/config.yaml", out, 0o644); err != nil {
		return Result{}, err
	}
	eps := []links.Endpoint{{
		Tag: links.Tag("hy2", "", h.SNI), Proto: "hysteria2",
		Server: p.IP, Port: h.Port, SNI: h.SNI, Obfs: h.Obfs, ObfsPass: obfs, AuthPass: auth,
	}}
	// Alt ports share the same server config; the firewall redirects them to h.Port.
	for _, ap := range h.AltPorts {
		eps = append(eps, links.Endpoint{
			Tag: fmt.Sprintf("hy2-alt%d", ap), Proto: "hysteria2",
			Server: p.IP, Port: ap, SNI: h.SNI, Obfs: h.Obfs, ObfsPass: obfs, AuthPass: auth,
		})
	}
	return Result{Endpoints: eps}, nil
}
