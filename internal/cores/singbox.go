package cores

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/links"
	"github.com/abbasnaqdi/ng-ray/internal/nginx"
	"github.com/abbasnaqdi/ng-ray/internal/state"
	"github.com/abbasnaqdi/ng-ray/internal/system"
)

const (
	singboxConf    = "/etc/sing-box/config.json"
	singboxTUICCrt = "/etc/sing-box/tuic.crt"
	singboxTUICKey = "/etc/sing-box/tuic.key"
)

type Singbox struct{}

func (Singbox) Name() string    { return "sing-box" }
func (Singbox) Repo() string    { return "SagerNet/sing-box" }
func (Singbox) Service() string { return "sing-box" }
func (Singbox) Enabled(c *config.Config) bool {
	return len(c.Cores.Singbox.Reality) > 0 ||
		(c.Cores.Singbox.TUIC != nil && c.Cores.Singbox.TUIC.Enabled) ||
		(c.Cores.Singbox.ShadowTLS != nil && c.Cores.Singbox.ShadowTLS.Enabled)
}

func (Singbox) Installed(ctx context.Context, sys *system.Env) (string, bool) {
	return detect(ctx, sys, "sing-box")
}

// Install downloads the latest sing-box deb directly from GitHub releases,
// avoiding the upstream deb-install.sh script which hits rate-limited API calls.
func (Singbox) Install(ctx context.Context, sys *system.Env) error {
	url, err := sys.AssetURL(ctx, "SagerNet/sing-box", `sing-box_.*_linux_amd64\.deb`)
	if err != nil {
		return err
	}
	tmp := filepath.Join(os.TempDir(), "sing-box.deb")
	if err := sys.Download(ctx, url, tmp); err != nil {
		return err
	}
	_, err = sys.Run(ctx, "dpkg", "-i", tmp)
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
				"enabled":     true,
				"handshake":   map[string]any{"server": r.SNI, "server_port": 443},
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

	if t := p.Cfg.Cores.Singbox.TUIC; t != nil && t.Enabled {
		if err := p.Sys.GenSelfSigned(t.SNI, singboxTUICCrt, singboxTUICKey); err != nil {
			return Result{}, err
		}
		uid := uuid(p.St, "singbox|tuic")
		pw := p.St.Secret("singbox|tuic|pw", func() state.Secret {
			return state.Secret{Password: state.NewPassword(20)}
		}).Password
		inbounds = append(inbounds, map[string]any{
			"type": "tuic", "listen": "0.0.0.0", "listen_port": t.Port,
			"users":              []map[string]any{{"uuid": uid, "password": pw}},
			"congestion_control": "bbr",
			"tls": map[string]any{
				"enabled": true, "server_name": t.SNI, "alpn": []string{"h3"},
				"certificate_path": singboxTUICCrt, "key_path": singboxTUICKey,
			},
		})
		res.Endpoints = append(res.Endpoints, links.Endpoint{
			Tag: links.Tag("sb", "tuic", t.SNI), Proto: "tuic",
			Server: p.IP, Port: t.Port, SNI: t.SNI, UUID: uid, TUICPass: pw,
		})
	}

	if stl := p.Cfg.Cores.Singbox.ShadowTLS; stl != nil && stl.Enabled {
		cid := id("singbox", "shadowtls", stl.SNI)
		ssPort, err := p.Alloc.Backend("singbox", cid+"|ss")
		if err != nil {
			return Result{}, err
		}
		stlPW := p.St.Secret(cid+"|stl", func() state.Secret {
			return state.Secret{Password: state.RandHex(16)}
		}).Password
		ssPW := p.St.Secret(cid+"|ss", func() state.Secret {
			return state.Secret{Password: state.NewPassword(20)}
		}).Password
		inbounds = append(inbounds,
			map[string]any{
				"type": "shadowtls", "listen": "0.0.0.0", "listen_port": stl.Port,
				"version": 3, "password": stlPW,
				"handshake": map[string]any{"server": stl.SNI, "server_port": 443},
				"detour":    "ss-stl-inner",
			},
			map[string]any{
				"type": "shadowsocks", "tag": "ss-stl-inner",
				"listen": "127.0.0.1", "listen_port": ssPort,
				"method": "aes-128-gcm", "password": ssPW,
			},
		)
		res.Endpoints = append(res.Endpoints, links.Endpoint{
			Tag:    links.Tag("sb", "shadowtls", stl.SNI),
			Proto:  "shadowtls",
			Server: p.IP, Port: stl.Port, SNI: stl.SNI,
			Password: stlPW,         // shadow-tls outer password
			AuthPass: ssPW,          // shadowsocks inner password
			Obfs:     "aes-128-gcm", // shadowsocks method (reused as cipher field)
		})
	}

	cfg := map[string]any{"inbounds": inbounds, "outbounds": []map[string]any{{"type": "direct"}}}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return Result{}, err
	}
	return res, p.Sys.WriteFile(singboxConf, out, 0o644)
}
