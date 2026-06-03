package cores

import (
	"context"
	"os"
	"path/filepath"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/links"
	"github.com/abbasnaqdi/ng-ray/internal/nginx"
	"github.com/abbasnaqdi/ng-ray/internal/state"
	"github.com/abbasnaqdi/ng-ray/internal/system"
	"gopkg.in/yaml.v3"
)

const shoesDir = "/etc/shoes"

type Shoes struct{}

func (Shoes) Name() string                  { return "shoes" }
func (Shoes) Repo() string                  { return "cfal/shoes" }
func (Shoes) Service() string               { return "shoes" }
func (Shoes) Enabled(c *config.Config) bool { return len(c.Cores.Shoes.Reality) > 0 }

func (Shoes) Installed(ctx context.Context, sys *system.Env) (string, bool) {
	return detect(ctx, sys, "shoes")
}

func (Shoes) Install(ctx context.Context, sys *system.Env) error {
	url, err := sys.AssetURL(ctx, "cfal/shoes", `x86_64-unknown-linux-musl\.tar\.gz`)
	if err != nil {
		return err
	}
	tmp := filepath.Join(os.TempDir(), "shoes.tar.gz")
	if err := sys.Download(ctx, url, tmp); err != nil {
		return err
	}
	if err := system.UntarFile(tmp, "shoes", "/usr/local/bin/shoes"); err != nil {
		return err
	}
	if err := os.MkdirAll(shoesDir, 0o755); err != nil {
		return err
	}
	if err := sys.WriteUnit("shoes.service", unit("shoes", "/usr/local/bin/shoes "+shoesDir+"/server.yaml")); err != nil {
		return err
	}
	if err := sys.DaemonReload(ctx); err != nil {
		return err
	}
	return sys.EnableNow(ctx, "shoes")
}

func (Shoes) Build(p *Plan) (Result, error) {
	priv, pub, err := realityKeys(p.St, "shoes")
	if err != nil {
		return Result{}, err
	}
	var res Result
	var docs []any
	for _, r := range p.Cfg.Cores.Shoes.Reality {
		kind := "vision"
		if r.Proto == "anytls" {
			kind = "anytls"
		}
		cid := id("shoes", kind, r.SNI)
		port, err := p.Alloc.Backend("shoes", cid)
		if err != nil {
			return Result{}, err
		}
		sid := shortID(p.St, cid)
		reality := map[string]any{
			"private_key": priv,
			"short_ids":   []string{sid, ""},
			"dest":        r.SNI + ":443",
		}
		ep := links.Endpoint{
			Server: p.IP, Port: p.Cfg.Nginx.Port, SNI: r.SNI, PublicKey: pub, ShortID: sid,
		}
		epDirect := links.Endpoint{
			Server: p.IP, Port: port, SNI: r.SNI, PublicKey: pub, ShortID: sid,
		}
		if kind == "anytls" {
			pw := password(p.St, cid)
			reality["protocol"] = map[string]any{
				"type": "anytls", "udp_enabled": true,
				"users": []map[string]any{{"name": "user", "password": pw}},
			}
			ep.Proto, ep.Password, ep.Tag = "anytls", pw, links.Tag("shoes", "anytls", r.SNI)
			epDirect.Proto, epDirect.Password, epDirect.Tag = "anytls", pw, links.Tag("shoes", "anytls-direct", r.SNI)
		} else {
			uid := uuid(p.St, cid)
			reality["vision"] = true
			reality["protocol"] = map[string]any{"type": "vless", "user_id": uid, "udp_enabled": true}
			ep.Proto, ep.UUID, ep.Tag = "vless-vision", uid, links.Tag("shoes", "vision", r.SNI)
			epDirect.Proto, epDirect.UUID, epDirect.Tag = "vless-vision", uid, links.Tag("shoes", "vision-direct", r.SNI)
		}
		docs = append(docs, map[string]any{
			"address":  "0.0.0.0:" + itoa(port),
			"protocol": map[string]any{"type": "tls", "reality_targets": map[string]any{r.SNI: reality}},
		})
		res.Routes = append(res.Routes, nginx.Route{SNI: r.SNI, Backend: port})
		res.Endpoints = append(res.Endpoints, ep, epDirect)
	}
	out, err := yaml.Marshal(docs)
	if err != nil {
		return Result{}, err
	}
	return res, p.Sys.WriteFile(filepath.Join(shoesDir, "server.yaml"), out, 0o644)
}

// shared credential helpers (stable per config id) -------------------------

func shortID(st *state.State, cid string) string {
	return st.Secret(cid+"|sid", func() state.Secret { return state.Secret{ShortID: state.NewShortID()} }).ShortID
}
func uuid(st *state.State, cid string) string {
	return st.Secret(cid+"|uid", func() state.Secret { return state.Secret{UUID: state.NewUUID()} }).UUID
}
func password(st *state.State, cid string) string {
	return st.Secret(cid+"|pw", func() state.Secret { return state.Secret{Password: state.NewPassword(20)} }).Password
}
