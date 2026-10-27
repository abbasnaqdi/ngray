package system

import (
	"bufio"
	"context"
	"fmt"
	"strings"
)

const sysctlFile = "/etc/sysctl.d/99-ngray.conf"

var tuning = map[string]string{
	// Congestion control — BBR + fq is the single biggest win for lossy links.
	// BBR does not back off on packet loss the way CUBIC does.
	"net.core.default_qdisc":          "fq",
	"net.ipv4.tcp_congestion_control": "bbr",

	// Network buffers — large enough for QUIC throughput on lossy high-latency links.
	"net.core.rmem_max":     "67108864",
	"net.core.wmem_max":     "67108864",
	"net.core.rmem_default": "8388608",
	"net.core.wmem_default": "8388608",

	// Accept queue depth — prevents connection drops during bursts.
	"net.core.somaxconn":          "65535",
	"net.core.netdev_max_backlog": "65535",

	// TCP keepalive — detect dead connections quickly and recover.
	// On lossy MTN/Rightel, idle connections die silently; fast keepalive
	// lets the client reconnect before it notices a timeout.
	"net.ipv4.tcp_keepalive_time":   "60",
	"net.ipv4.tcp_keepalive_intvl":  "10",
	"net.ipv4.tcp_keepalive_probes": "3",

	// MTU probing — auto-adapt to path MTU; prevents fragmentation-induced loss.
	"net.ipv4.tcp_mtu_probing": "1",

	// No slow-start penalty after idle — stream resumes at full rate.
	"net.ipv4.tcp_slow_start_after_idle": "0",

	// Reduce write-buffer latency for interactive proxy traffic.
	"net.ipv4.tcp_notsent_lowat": "16384",
}

// EnableBBR persists the tuning table and reloads sysctl.
// NOTE: tcp_fastopen is intentionally excluded — it causes middlebox drops
// and DPI fingerprinting on Iranian/Chinese carrier networks.
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

// EnableIPForward turns on IPv4 forwarding (needed by TUN-style cores).
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
