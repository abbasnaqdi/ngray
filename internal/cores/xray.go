package cores

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	x := c.Cores.Xray
	return len(x.Reality) > 0 || len(x.XHTTP) > 0 || (x.CDN != nil && x.CDN.Enabled && x.CDN.Domain != "")
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

	// Cloudflare-frontable inbounds: VLESS over WS and/or XHTTP with real TLS
	// (Origin Certificate), served behind the same nginx ssl_preread mux via
	// distinct SNIs. The client connects to the domain (Cloudflare edge), not
	// the server IP, so these survive a direct-IP block. Skipped if the cert is
	// not yet installed.
	if cdn := p.Cfg.Cores.Xray.CDN; cdn != nil && cdn.Enabled && cdn.Domain != "" {
		if fileExists(cdn.CertFile) && fileExists(cdn.KeyFile) {
			tlsSettings := func(sni string, alpn []string) map[string]any {
				return map[string]any{
					"serverName":   sni,
					"alpn":         alpn,
					"certificates": []map[string]any{{"certificateFile": cdn.CertFile, "keyFile": cdn.KeyFile}},
				}
			}
			vlessClient := func(uid string) map[string]any {
				return map[string]any{"clients": []map[string]any{{"id": uid}}, "decryption": "none"}
			}
			if cdn.WS {
				cid := id("xray", "cdnws", cdn.Domain)
				port, err := p.Alloc.Backend("xray", cid)
				if err != nil {
					return Result{}, err
				}
				uid, path := uuid(p.St, cid), xhttpPath(p.St, cid)
				inbounds = append(inbounds, map[string]any{
					"listen": "127.0.0.1", "port": port, "protocol": "vless",
					"settings": vlessClient(uid),
					"streamSettings": map[string]any{
						"network": "ws", "security": "tls",
						// WS runs over HTTP/1.1 only — offering h2 in ALPN makes TLS
						// negotiate HTTP/2 and the WebSocket upgrade fails with 400.
						"tlsSettings": tlsSettings(cdn.Domain, []string{"http/1.1"}),
						"wsSettings":  map[string]any{"path": path, "host": cdn.Domain},
					},
				})
				res.Routes = append(res.Routes, nginx.Route{SNI: cdn.Domain, Backend: port})
				res.Endpoints = append(res.Endpoints, links.Endpoint{
					Tag: links.Tag("cf", "ws", cdn.Domain), Proto: "vless-ws-tls",
					Server: cdn.Domain, Port: 443, SNI: cdn.Domain, UUID: uid, Path: path,
				})
			}
			if cdn.XHTTP {
				alt := cdn.XHTTPDomain()
				cid := id("xray", "cdnxh", alt)
				port, err := p.Alloc.Backend("xray", cid)
				if err != nil {
					return Result{}, err
				}
				uid, path := uuid(p.St, cid), xhttpPath(p.St, cid)
				inbounds = append(inbounds, map[string]any{
					"listen": "127.0.0.1", "port": port, "protocol": "vless",
					"settings": vlessClient(uid),
					"streamSettings": map[string]any{
						"network": "xhttp", "security": "tls",
						"tlsSettings":   tlsSettings(alt, []string{"h2", "http/1.1"}),
						"xhttpSettings": map[string]any{"path": path, "mode": "auto", "host": alt},
					},
				})
				res.Routes = append(res.Routes, nginx.Route{SNI: alt, Backend: port})
				res.Endpoints = append(res.Endpoints, links.Endpoint{
					Tag: links.Tag("cf", "xhttp", alt), Proto: "vless-xhttp-tls",
					Server: alt, Port: 443, SNI: alt, UUID: uid, Path: path,
				})
			}
		} else {
			res.Notes = map[string]string{"cloudflare": fmt.Sprintf(
				"CDN (Cloudflare) inbounds skipped: cert/key not found at %s and %s. Install the Cloudflare Origin Certificate there and re-run setup.",
				cdn.CertFile, cdn.KeyFile)}
		}
	}

	// Domain+IP pairing: every route SNI also gets a CF-fronted WS TWIN —
	// a client config over https://DOMAIN/… with a fresh UUID, served by the
	// www-domain TLS server (XHTTPDomain) as a plain-ws loopback backend. When
	// the censor blocks the VPS IP or starts RST-flooding a REALITY SNI (Iran
	// 2026 behaviour), the twin still carries the same SNI's traffic.
	if cdn := p.Cfg.Cores.Xray.CDN; cdn != nil && cdn.Enabled && cdn.WS && cdn.Domain != "" &&
		fileExists(cdn.CertFile) && fileExists(cdn.KeyFile) {
		alt := cdn.XHTTPDomain()
		seenSNI := map[string]bool{}
		for _, sni := range p.Cfg.RouteSNIs() {
			if sni == "" || seenSNI[sni] {
				continue
			}
			seenSNI[sni] = true
			cid := id("xray", "twin", sni)
			port, err := p.Alloc.Backend("xray", cid)
			if err != nil {
				return Result{}, err
			}
			uid, path := uuid(p.St, cid), twinPath(p.St, sni)
			inbounds = append(inbounds, map[string]any{
				"listen": "127.0.0.1", "port": port, "protocol": "vless",
				"settings": map[string]any{"clients": []map[string]any{{"id": uid}}, "decryption": "none"},
				"streamSettings": map[string]any{
					"network": "ws", "security": "none",
					"wsSettings": map[string]any{"path": path, "host": alt},
				},
				"sniffing": map[string]any{"enabled": true, "destOverride": []string{"http", "tls"}},
			})
			res.CDNWS = append(res.CDNWS, nginx.WSRoute{Path: path, Backend: port})
			res.Endpoints = append(res.Endpoints, links.Endpoint{
				Tag: links.Tag("cf", "ws", sni), Proto: "vless-ws-tls",
				Server: alt, Port: 443, SNI: alt, UUID: uid, Path: path,
			})
		}
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

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func xhttpPath(st *state.State, cid string) string {
	return "/" + st.Secret(cid+"|path", func() state.Secret { return state.Secret{Password: state.RandHex(6)} }).Password
}

// twinPath derives a stable WS path for a route-SNI twin: /t/<sub-token>/<sni
// fingerprint>. Tying the prefix to the subscription token means a token
// rotation (or link-rotation) also kills every previously exported twin link.
func twinPath(st *state.State, sni string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSuffix(sni, "."))))
	return "/t/" + st.EnsureSubToken() + "/" + hex.EncodeToString(sum[:])[:12]
}
