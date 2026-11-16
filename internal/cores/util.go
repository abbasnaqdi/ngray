package cores

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/abbasnaqdi/ng-ray/internal/system"
)

func itoa(i int) string { return strconv.Itoa(i) }

// installDownloaded fetches url into a sibling temp file and atomically renames
// it over dest. The temp must live in dest's directory: /tmp is frequently a
// different filesystem, so renaming from there fails with EXDEV. The sibling
// rename also avoids ETXTBSY when dest is a running binary.
func installDownloaded(ctx context.Context, sys *system.Env, url, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".new"
	if err := sys.Download(ctx, url, tmp); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// detect reports whether cmd is installed. It prefers a parsed semver but
// falls back to "installed" when the binary is present yet its version output
// can't be parsed, so a working core is never mistaken for missing (which
// would trigger a needless reinstall over the running binary).
func detect(ctx context.Context, sys *system.Env, cmd string) (string, bool) {
	if v := sys.BinVersion(ctx, cmd); v != "" {
		return v, true
	}
	if sys.Which(cmd) {
		return "installed", true
	}
	return "", false
}

// unit renders a minimal hardened systemd service definition.
func unit(desc, execStart string) string {
	return fmt.Sprintf(`[Unit]
Description=%s
After=network.target

[Service]
Type=simple
ExecStart=%s
Restart=on-failure
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`, desc, execStart)
}
