package cores

import (
	"context"
	"fmt"
	"strconv"

	"github.com/abbasnaqdi/ng-ray/internal/system"
)

func itoa(i int) string { return strconv.Itoa(i) }

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
