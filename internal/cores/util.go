package cores

import (
	"fmt"
	"strconv"
)

func itoa(i int) string { return strconv.Itoa(i) }

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
