package system

import (
	"context"
	"os"
	"path/filepath"
)

const unitDir = "/etc/systemd/system"

func (e *Env) WriteUnit(name, contents string) error {
	return os.WriteFile(filepath.Join(unitDir, name), []byte(contents), 0o644)
}

// WriteFile writes contents to path with standard 0644 permissions.
func WriteFile(path, contents string) error {
	return os.WriteFile(path, []byte(contents), 0o644)
}

func (e *Env) DaemonReload(ctx context.Context) error {
	_, err := e.Run(ctx, "systemctl", "daemon-reload")
	return err
}

func (e *Env) EnableNow(ctx context.Context, svc string) error {
	_, err := e.Run(ctx, "systemctl", "enable", "--now", svc)
	return err
}

func (e *Env) Restart(ctx context.Context, svc string) error {
	_, err := e.Run(ctx, "systemctl", "restart", svc)
	return err
}

func (e *Env) IsActive(ctx context.Context, svc string) bool {
	return e.OK(ctx, "systemctl", "is-active", "--quiet", svc)
}

// BinVersion runs `<cmd> version` (then `--version`) and extracts a semver.
func (e *Env) BinVersion(ctx context.Context, cmd string) string {
	if !e.Which(cmd) {
		return ""
	}
	for _, flag := range []string{"version", "--version"} {
		if out, err := e.Run(ctx, cmd, flag); err == nil {
			if v := ParseSemver(out); v != "" {
				return v
			}
		}
	}
	return ""
}
