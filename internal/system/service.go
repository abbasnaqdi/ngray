package system

import (
	"context"
	"os/exec"
	"path/filepath"
)

const unitDir = "/etc/systemd/system"

func (e *Env) WriteUnit(name, contents string) error {
	return e.WriteString(filepath.Join(unitDir, name), contents, 0o644)
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

// BinVersion runs common version flags and extracts a semver.
// It ignores the exit code and checks both stdout and stderr because tools
// vary: some exit non-zero for `version`, some write to stderr.
func (e *Env) BinVersion(ctx context.Context, cmd string) string {
	if !e.Which(cmd) {
		return ""
	}
	for _, flag := range []string{"--version", "version", "-v", "-V"} {
		c := exec.CommandContext(ctx, cmd, flag)
		out, _ := c.CombinedOutput() // combined: captures both stdout and stderr
		if v := ParseSemver(string(out)); v != "" {
			return v
		}
	}
	return ""
}
