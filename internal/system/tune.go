package system

import (
	"bufio"
	"context"
	"fmt"
	"strings"
)

const sysctlFile = "/etc/sysctl.d/99-ngray.conf"

var tuning = map[string]string{
	"net.core.default_qdisc":          "fq",
	"net.ipv4.tcp_congestion_control": "bbr",
	"net.core.rmem_max":               "16777216",
	"net.core.wmem_max":               "16777216",
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
