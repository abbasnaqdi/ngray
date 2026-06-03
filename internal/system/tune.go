package system

import (
	"bufio"
	"context"
	"fmt"
	"strings"
)

const sysctlFile = "/etc/sysctl.d/99-ngray.conf"

var tuning = map[string]string{
	// Congestion control — BBR with fq qdisc
	"net.core.default_qdisc":          "fq",
	"net.ipv4.tcp_congestion_control": "bbr",
	// Network buffers — large enough for fast QUIC and bulk transfers
	"net.core.rmem_max":     "67108864",
	"net.core.wmem_max":     "67108864",
	"net.core.rmem_default": "4194304",
	"net.core.wmem_default": "4194304",
	// Accept queue depth — prevents drop under burst connections
	"net.core.somaxconn":          "65535",
	"net.core.netdev_max_backlog": "65535",
	// TCP latency — reduce idle slow-start, enable Fast Open
	"net.ipv4.tcp_slow_start_after_idle": "0",
	"net.ipv4.tcp_fastopen":              "3",
	"net.ipv4.tcp_notsent_lowat":         "16384",
	// MTU probing — adapt to path MTU automatically
	"net.ipv4.tcp_mtu_probing": "1",
}

// EnableBBR persists fq+bbr and network buffer tuning, then reloads sysctl.
func (e *Env) EnableBBR(ctx context.Context) error {
	var b strings.Builder
	b.WriteString("# managed by ng-ray\n")
	for k, v := range tuning {
		fmt.Fprintf(&b, "%s = %s\n", k, v)
	}
	if err := e.WriteString(sysctlFile, b.String(), 0o644); err != nil {
		return err
	}
	_, err := e.Run(ctx, "sysctl", "--system")
	return err
}

// EnableIPForward turns on IPv4 forwarding (needed by the fptn TUN core).
func (e *Env) EnableIPForward(ctx context.Context) error {
	if _, err := e.Run(ctx, "sysctl", "-w", "net.ipv4.ip_forward=1"); err != nil {
		return err
	}
	return e.appendUnique("/etc/sysctl.conf", "net.ipv4.ip_forward=1")
}

func (e *Env) appendUnique(path, line string) error {
	if data, err := e.ReadFile(path); err == nil {
		sc := bufio.NewScanner(strings.NewReader(string(data)))
		for sc.Scan() {
			if strings.TrimSpace(sc.Text()) == line {
				return nil
			}
		}
	}
	existing, _ := e.ReadFile(path)
	return e.WriteFile(path, append(existing, []byte(line+"\n")...), 0o644)
}
