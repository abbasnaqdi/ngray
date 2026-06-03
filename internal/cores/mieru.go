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

// Install downloads the mita binary from the latest GitHub release and writes
// a systemd unit that passes the config file via MITA_CONFIG_JSON_FILE so
// mita run loads it directly — no gRPC apply-config step needed.
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
	if err := writeMieruUnit(sys); err != nil {
		return err
	}
	return sys.DaemonReload(ctx)
}

// mieruUnit is the systemd unit. RuntimeDirectory=mita creates /run/mita on
// service start (mita binds its RPC socket there); without it mita fails with
// "bind: no such file or directory". MITA_CONFIG_JSON_FILE makes mita run load
// our JSON directly — no gRPC apply-config step.
func mieruUnit() string {
	return fmt.Sprintf(`[Unit]
Description=mieru proxy server
After=network.target

[Service]
Type=simple
ExecStart=%s run
Environment=MITA_CONFIG_JSON_FILE=%s
RuntimeDirectory=mita
Restart=on-failure
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`, mieruBin, mieruConf)
}

func writeMieruUnit(sys *system.Env) error {
	return sys.WriteUnit("mieru-server.service", mieruUnit())
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
	raw, err := json.MarshalIndent(serverCfg, "", "  ")
	if err != nil {
		return Result{}, err
	}
	if err := p.Sys.WriteFile(mieruConf, raw, 0o600); err != nil {
		return Result{}, err
	}

	// Refresh the unit every deploy so unit-level fixes (e.g. RuntimeDirectory)
	// reach servers installed before the fix, then (re)start.
	ctx := context.Background()
	if err := writeMieruUnit(p.Sys); err == nil {
		_ = p.Sys.DaemonReload(ctx)
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
