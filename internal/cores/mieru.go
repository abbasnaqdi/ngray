package cores

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/state"
	"github.com/abbasnaqdi/ng-ray/internal/system"
)

const (
	mieruDir  = "/etc/mieru"
	mieruBin  = "/usr/local/bin/mita"
	mieruConf = "/etc/mieru/server.json"
)

// Mieru is an independent TCP multiplexed tunnel that produces no recognisable
// protocol fingerprint — effective against ML-based DPI (China/Russia).
// The server binary is `mita`; clients use the `mieru` app (NekoBox/Exclave).
type Mieru struct{}

func (Mieru) Name() string                  { return "mieru" }
func (Mieru) Repo() string                  { return "enfein/mieru" }
func (Mieru) Service() string               { return "mieru-server" }
func (Mieru) Enabled(c *config.Config) bool { return c.Cores.Mieru.Enabled }

func (Mieru) Installed(ctx context.Context, sys *system.Env) (string, bool) {
	return detect(ctx, sys, "mita")
}

// Install downloads the mita binary from the latest GitHub release.
func (Mieru) Install(ctx context.Context, sys *system.Env) error {
	url, err := sys.AssetURL(ctx, "enfein/mieru", `mita.*linux.*amd64\.tar\.gz`)
	if err != nil {
		return err
	}
	tmp := filepath.Join(os.TempDir(), "mita.tar.gz")
	if err := sys.Download(ctx, url, tmp); err != nil {
		return err
	}
	if err := system.UntarFile(tmp, "mita", mieruBin); err != nil {
		return err
	}
	if err := os.MkdirAll(mieruDir, 0o755); err != nil {
		return err
	}
	if err := sys.WriteUnit("mieru-server.service",
		unit("mieru proxy server", mieruBin+" run")); err != nil {
		return err
	}
	return sys.DaemonReload(ctx)
}

func (Mieru) Build(p *Plan) (Result, error) {
	m := p.Cfg.Cores.Mieru
	pw := p.St.Secret("mieru|pw", func() state.Secret {
		return state.Secret{Password: state.NewPassword(20)}
	}).Password

	serverCfg := map[string]any{
		"portBindings": []map[string]any{{"port": m.Port, "protocol": "TCP"}},
		"users":        []map[string]any{{"name": m.User, "password": pw}},
		"loggingLevel": "INFO",
		"mtu":          1400,
	}
	out, err := json.MarshalIndent(serverCfg, "", "  ")
	if err != nil {
		return Result{}, err
	}
	if err := p.Sys.WriteFile(mieruConf, out, 0o600); err != nil {
		return Result{}, err
	}

	// Apply config and restart service (mita reads the file via its own config store).
	ctx := context.Background()
	if _, err := p.Sys.Run(ctx, mieruBin, "apply", "config", mieruConf); err != nil {
		return Result{}, fmt.Errorf("mita apply config: %w", err)
	}
	if err := p.Sys.EnableNow(ctx, "mieru-server"); err != nil {
		_ = p.Sys.Restart(ctx, "mieru-server")
	}

	// Client config JSON (imported by NekoBox / Exclave — no share-link URI).
	clientCfg := map[string]any{
		"activeProfile": "ngray",
		"profiles": []map[string]any{{
			"profileName": "ngray",
			"user":        map[string]any{"name": m.User, "password": pw},
			"servers": []map[string]any{{
				"ipAddress":    p.IP,
				"domainName":   "",
				"portBindings": []map[string]any{{"port": m.Port, "protocol": "TCP"}},
			}},
			"mtu": 1400,
		}},
	}
	clientJSON, err := json.MarshalIndent(clientCfg, "", "  ")
	if err != nil {
		return Result{}, err
	}
	return Result{Notes: map[string]string{
		"mieru-client-config": string(clientJSON),
	}}, nil
}
