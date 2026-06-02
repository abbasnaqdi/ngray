package cores

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/links"
	"github.com/abbasnaqdi/ng-ray/internal/nginx"
	"github.com/abbasnaqdi/ng-ray/internal/system"
)

const singboxConf = "/etc/sing-box/config.json"

type Singbox struct{}

func (Singbox) Name() string                  { return "sing-box" }
func (Singbox) Repo() string                  { return "SagerNet/sing-box" }
func (Singbox) Service() string               { return "sing-box" }
func (Singbox) Enabled(c *config.Config) bool { return len(c.Cores.Singbox.Reality) > 0 }

func (Singbox) Installed(ctx context.Context, sys *system.Env) (string, bool) {
	v := sys.BinVersion(ctx, "sing-box")
	return v, v != ""
}

func (Singbox) Install(ctx context.Context, sys *system.Env) error {
	_, err := sys.Run(ctx, "bash", "-c", "curl -fsSL https://sing-box.app/deb-install.sh | bash")
	return err
}

func (Singbox) Build(p *Plan) (Result, error) {
	priv, pub, err := realityKeys(p.St, "singbox")
	if err != nil {
		return Result{}, err
	}
	var res Result
	var inbounds []any
	for _, r := range p.Cfg.Cores.Singbox.Reality {
		kind := "vision"
		if r.Proto == "anytls" {
			kind = "anytls"
		}
		cid := id("singbox", kind, r.SNI)
		port, err := p.Alloc.Backend("singbox", cid)
		if err != nil {
			return Result{}, err
		}
		sid := shortID(p.St, cid)
		tls := map[string]any{
			"enabled": true, "server_name": r.SNI,
			"reality": map[string]any{
				"enabled":   true,
				"handshake": map[string]any{"server": r.SNI, "server_port": 443},
				"private_key": priv, "short_id": []string{sid},
			},
		}
		ep := links.Endpoint{Server: p.IP, Port: p.Cfg.Nginx.Port, SNI: r.SNI, PublicKey: pub, ShortID: sid}
		if kind == "anytls" {
			pw := password(p.St, cid)
			inbounds = append(inbounds, map[string]any{
				"type": "anytls", "listen": "127.0.0.1", "listen_port": port,
				"users": []map[string]any{{"password": pw}}, "tls": tls,
			})
			ep.Proto, ep.Password, ep.Tag = "anytls", pw, links.Tag("sb", "anytls", r.SNI)
		} else {
			uid := uuid(p.St, cid)
			inbounds = append(inbounds, map[string]any{
				"type": "vless", "listen": "127.0.0.1", "listen_port": port,
				"users": []map[string]any{{"uuid": uid, "flow": "xtls-rprx-vision"}}, "tls": tls,
			})
			ep.Proto, ep.UUID, ep.Tag = "vless-vision", uid, links.Tag("sb", "vision", r.SNI)
		}
		res.Routes = append(res.Routes, nginx.Route{SNI: r.SNI, Backend: port})
		res.Endpoints = append(res.Endpoints, ep)
	}

	cfg := map[string]any{"inbounds": inbounds, "outbounds": []map[string]any{{"type": "direct"}}}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(singboxConf), 0o755); err != nil {
		return Result{}, err
	}
	return res, os.WriteFile(singboxConf, out, 0o644)
}
