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
	xrayConf = "/usr/local/etc/xray/config.json"
	xrayBin  = "/usr/local/bin/xray"
)

type Xray struct{}

func (Xray) Name() string    { return "xray" }
func (Xray) Repo() string    { return "XTLS/Xray-core" }
func (Xray) Service() string { return "xray" }
func (Xray) Enabled(c *config.Config) bool {
	return len(c.Cores.Xray.Reality) > 0 || len(c.Cores.Xray.XHTTP) > 0
}

func (Xray) Installed(ctx context.Context, sys *system.Env) (string, bool) {
	return detect(ctx, sys, "xray")
}

// Install pulls the Xray-core release zip directly and unpacks the binary,
// avoiding the upstream install script's api.github.com calls that get
// rate-limited (HTTP 403) on shared or filtered networks.
func (Xray) Install(ctx context.Context, sys *system.Env) error {
	url, err := sys.AssetURL(ctx, "XTLS/Xray-core", `Xray-linux-64\.zip`)
	if err != nil {
		return err
	}
	tmp := filepath.Join(os.TempDir(), "xray.zip")
	if err := sys.Download(ctx, url, tmp); err != nil {
		return err
	}
	if err := system.UnzipFile(tmp, "xray", xrayBin); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(xrayConf), 0o755); err != nil {
		return err
	}
	if err := sys.WriteUnit("xray.service", unit("xray", xrayBin+" run -config "+xrayConf)); err != nil {
		return err
	}
	if err := sys.DaemonReload(ctx); err != nil {
		return err
	}
	return sys.EnableNow(ctx, "xray")
}

func (Xray) Build(p *Plan) (Result, error) {
	priv, pub, err := realityKeys(p.St, "xray")
	if err != nil {
		return Result{}, err
	}
	var res Result
	var inbounds []any

	reality := func(sni, sid string) map[string]any {
		return map[string]any{
			"show": false, "dest": sni + ":443", "xver": 0,
			"serverNames": []string{sni}, "privateKey": priv, "shortIds": []string{sid, ""},
		}
	}

	for _, r := range p.Cfg.Cores.Xray.Reality {
		cid := id("xray", "vision", r.SNI)
		port, err := p.Alloc.Backend("xray", cid)
		if err != nil {
			return Result{}, err
		}
		uid, sid := uuid(p.St, cid), shortID(p.St, cid)
		inbounds = append(inbounds, map[string]any{
			"listen": "127.0.0.1", "port": port, "protocol": "vless",
			"settings": map[string]any{
				"clients":    []map[string]any{{"id": uid, "flow": "xtls-rprx-vision"}},
				"decryption": "none",
			},
			"streamSettings": map[string]any{
				"network": "tcp", "security": "reality", "realitySettings": reality(r.SNI, sid),
			},
			"sniffing": map[string]any{"enabled": true, "destOverride": []string{"http", "tls"}},
		})
		res.Routes = append(res.Routes, nginx.Route{SNI: r.SNI, Backend: port})
		res.Endpoints = append(res.Endpoints, links.Endpoint{
			Tag: links.Tag("xray", "reality", r.SNI), Proto: "vless-vision",
			Server: p.IP, Port: p.Cfg.Nginx.Port, SNI: r.SNI, PublicKey: pub, ShortID: sid, UUID: uid,
		})
	}

	for _, x := range p.Cfg.Cores.Xray.XHTTP {
		cid := id("xray", "xhttp", x.SNI)
		port, err := p.Alloc.Backend("xray", cid)
		if err != nil {
			return Result{}, err
		}
		uid, sid := uuid(p.St, cid), shortID(p.St, cid)
		path := xhttpPath(p.St, cid)
		inbounds = append(inbounds, map[string]any{
			"listen": "127.0.0.1", "port": port, "protocol": "vless",
			"settings": map[string]any{
				"clients": []map[string]any{{"id": uid}}, "decryption": "none",
			},
			"streamSettings": map[string]any{
				"network": "xhttp", "security": "reality",
				"xhttpSettings":   map[string]any{"path": path, "mode": "auto"},
				"realitySettings": reality(x.SNI, sid),
			},
		})
		res.Routes = append(res.Routes, nginx.Route{SNI: x.SNI, Backend: port})
		res.Endpoints = append(res.Endpoints, links.Endpoint{
			Tag: links.Tag("xray", "xhttp", x.SNI), Proto: "vless-xhttp",
			Server: p.IP, Port: p.Cfg.Nginx.Port, SNI: x.SNI, PublicKey: pub, ShortID: sid, UUID: uid, Path: path,
		})
	}

	cfg := map[string]any{
		"log":       map[string]any{"loglevel": "warning"},
		"inbounds":  inbounds,
		"outbounds": []map[string]any{{"protocol": "freedom"}},
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return Result{}, err
	}
	return res, p.Sys.WriteFile(xrayConf, out, 0o644)
}

func xhttpPath(st *state.State, cid string) string {
	return "/" + st.Secret(cid+"|path", func() state.Secret { return state.Secret{Password: state.RandHex(6)} }).Password
}
