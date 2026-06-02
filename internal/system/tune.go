package system

import (
	"bufio"
	"context"
	"fmt"
	"os"
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
	if err := os.WriteFile(sysctlFile, []byte(b.String()), 0o644); err != nil {
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
	return appendUnique("/etc/sysctl.conf", "net.ipv4.ip_forward=1")
}

func appendUnique(path, line string) error {
	if data, err := os.ReadFile(path); err == nil {
		sc := bufio.NewScanner(strings.NewReader(string(data)))
		for sc.Scan() {
			if strings.TrimSpace(sc.Text()) == line {
				return nil
			}
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, line)
	return err
}
