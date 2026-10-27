// Package firewall builds a default-drop inbound ruleset without ever risking
// an SSH lockout: all rules live in a dedicated NGRAY chain that is populated
// fully before INPUT jumps to it, with lo/established/ssh accepted before DROP.
package firewall

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/abbasnaqdi/ng-ray/internal/system"
)

const chain = "NGRAY"

type Firewall struct {
	sys *system.Env
	bin string // iptables or ip6tables
}

func New(sys *system.Env) *Firewall { return &Firewall{sys: sys, bin: "iptables"} }

type UDPRedirect struct{ From, To int }

type Rules struct {
	SSHPort   int
	TCP       []int
	UDP       []int
	Redirects []UDPRedirect // PREROUTING REDIRECT: From port → To port (same host, UDP)
}

// Plan returns the human-readable rule list that Apply will install.
func (r Rules) Plan() []string {
	out := []string{"lo → ACCEPT", "ESTABLISHED,RELATED → ACCEPT", fmt.Sprintf("tcp/%d (ssh) → ACCEPT", r.sshPort())}
	for _, p := range r.TCP {
		out = append(out, fmt.Sprintf("tcp/%d → ACCEPT", p))
	}
	for _, p := range r.UDP {
		out = append(out, fmt.Sprintf("udp/%d → ACCEPT", p))
	}
	for _, rd := range r.Redirects {
		out = append(out, fmt.Sprintf("udp/%d → REDIRECT udp/%d", rd.From, rd.To))
	}
	return append(out, "trusted local interfaces (tailscale/docker/wg bridges) → ACCEPT", "everything else → DROP")
}

// hostInterfaces returns the network interface names of the host.
func (f *Firewall) hostInterfaces(ctx context.Context) []string {
	out, err := f.sys.Run(ctx, "sh", "-c", "for i in /sys/class/net/*; do basename \"$i\"; done 2>/dev/null")
	if err != nil {
		return nil
	}
	var ifs []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l != "" {
			ifs = append(ifs, l)
		}
	}
	return ifs
}

// interfacePrefixes are interface name prefixes whose INPUT traffic is host-local
// virtual networking: peers/containers reaching the host through a tunnel or a
// local bridge. Default-drop would otherwise silently break Tailscale inbound
// sessions (e.g. tailscale ssh), docker→host and plain WireGuard clients.
var interfacePrefixes = []string{"tailscale", "docker", "br-", "wg", "virbr", "cni", "cali", "flannel", "incus"}

// trustedInterfaces filters the host interfaces down to the ones ngray accepts.
func trustedInterfaces(ifaces []string) []string {
	var out []string
	for _, i := range ifaces {
		if i == "lo" {
			continue
		}
		for _, p := range interfacePrefixes {
			if strings.HasPrefix(i, p) {
				out = append(out, i)
				break
			}
		}
	}
	return out
}

// pickSSDPorts extracts the local TCP ports sshd/dropbear actually listen on
// from `ss -tlnp` — so a custom SSH port stays reachable and a lockout is
// impossible. Returns nil when detection fails (caller falls back to 22).
func (f *Firewall) sshPorts(ctx context.Context) []int {
	out, err := f.sys.Run(ctx, "sh", "-c", "ss -tlnp 2>/dev/null | grep -E 'sshd|dropbear' || true")
	if err != nil || out == "" {
		return nil
	}
	seen, ports := map[int]bool{}, []int{}
	for _, m := range regexp.MustCompile(`:(\d+)\s`).FindAllStringSubmatch(out, -1) {
		p, _ := strconv.Atoi(m[1])
		if p > 0 && p < 65536 && !seen[p] {
			seen[p] = true
			ports = append(ports, p)
		}
	}
	return ports
}

func (r Rules) sshPort() int {
	if r.SSHPort > 0 {
		return r.SSHPort
	}
	return 22
}

// Apply installs the ruleset on IPv4 and IPv6, then best-effort persists it.
func (f *Firewall) Apply(ctx context.Context, r Rules) error {
	if active, driver := f.foreignManager(ctx); active {
		// ufw / firewalld already OWN the operator's INPUT policy. ngray must
		// not fight them: add its ports through the active manager instead and
		// leave the default-deny policy exactly as the operator configured it.
		// Also dissolve a chain WE installed before (stale jump → dead rules).
		f.Clear(ctx)
		if err := f.allowForeign(ctx, r, driver); err != nil {
			return err
		}
		return nil
	}
	if r.SSHPort == 0 {
		// sshd may sit on a custom port; accept whatever it truly listens on,
		// plus the default, so a custom port can never lock the operator out.
		if detected := f.sshPorts(ctx); len(detected) > 0 {
			r.SSHPort = detected[0]
			r.TCP = dedupe(append(r.TCP, detected...))
		}
	}
	trusted := trustedInterfaces(f.hostInterfaces(ctx))
	for _, bin := range []string{"iptables", "ip6tables"} {
		f.bin = bin
		if err := f.applyOne(ctx, r, trusted); err != nil {
			return fmt.Errorf("%s: %w", bin, err)
		}
	}
	// UDP port redirects live in the NAT table's PREROUTING chain (IPv4 only).
	// Only add a rule that isn't already present, or every deploy appends a
	// duplicate.
	for _, rd := range r.Redirects {
		if f.sys.OK(ctx, "iptables", "-t", "nat", "-C", "PREROUTING",
			"-p", "udp", "--dport", strconv.Itoa(rd.From), "-j", "REDIRECT", "--to-port", strconv.Itoa(rd.To)) {
			continue
		}
		_, _ = f.sys.Run(ctx, "iptables", "-t", "nat", "-A", "PREROUTING",
			"-p", "udp", "--dport", strconv.Itoa(rd.From), "-j", "REDIRECT", "--to-port", strconv.Itoa(rd.To))
	}
	f.sys.OK(ctx, "netfilter-persistent", "save")
	return nil
}

// foreignManager reports an active firewall manager (ufw or firewalld) whose
// rules take precedence on INPUT.
func (f *Firewall) foreignManager(ctx context.Context) (bool, string) {
	if f.sys.DryRun {
		return false, "" // detection command is skipped in dry-run; assume none
	}
	if out, err := f.sys.Run(ctx, "ufw", "status"); err == nil && strings.Contains(out, "Status: active") {
		return true, "ufw"
	}
	if f.sys.OK(ctx, "firewall-cmd", "--state") {
		return true, "firewalld"
	}
	return false, ""
}

// allowForeign adds ngray's ports to the operator's firewall manager.
func (f *Firewall) allowForeign(ctx context.Context, r Rules, driver string) error {
	if driver == "ufw" {
		for _, p := range dedupe(append([]int{r.sshPort()}, r.TCP...)) {
			_, _ = f.sys.Run(ctx, "ufw", "allow", strconv.Itoa(p)+"/tcp")
		}
		for _, p := range r.UDP {
			_, _ = f.sys.Run(ctx, "ufw", "allow", strconv.Itoa(p)+"/udp")
		}
		return nil
	}
	for _, p := range dedupe(append([]int{r.sshPort()}, r.TCP...)) {
		_, _ = f.sys.Run(ctx, "firewall-cmd", "--permanent", "--add-port", strconv.Itoa(p)+"/tcp")
	}
	for _, p := range r.UDP {
		_, _ = f.sys.Run(ctx, "firewall-cmd", "--permanent", "--add-port", strconv.Itoa(p)+"/udp")
	}
	_, err := f.sys.Run(ctx, "firewall-cmd", "--reload")
	return err
}

func (f *Firewall) applyOne(ctx context.Context, r Rules, trusted []string) error {
	// Create our chain if missing; flushing our own chain can't drop live INPUT.
	if !f.sys.OK(ctx, f.bin, "-L", chain) {
		if _, err := f.sys.Run(ctx, f.bin, "-N", chain); err != nil {
			return err
		}
	}
	if _, err := f.sys.Run(ctx, f.bin, "-F", chain); err != nil {
		return err
	}

	add := func(args ...string) error {
		_, err := f.sys.Run(ctx, f.bin, append([]string{"-A", chain}, args...)...)
		return err
	}
	if err := add("-i", "lo", "-j", "ACCEPT"); err != nil {
		return err
	}
	// Tailscale / docker / WireGuard bridges: traffic into these interfaces is
	// host-local virtual networking other tools own; leave it alone.
	for _, i := range trusted {
		if err := add("-i", i, "-j", "ACCEPT"); err != nil {
			return err
		}
	}
	if err := add("-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"); err != nil {
		return err
	}
	if err := add("-p", "tcp", "--dport", strconv.Itoa(r.sshPort()), "-j", "ACCEPT"); err != nil {
		return err
	}
	for _, p := range r.TCP {
		if err := add("-p", "tcp", "--dport", strconv.Itoa(p), "-j", "ACCEPT"); err != nil {
			return err
		}
	}
	for _, p := range r.UDP {
		if err := add("-p", "udp", "--dport", strconv.Itoa(p), "-j", "ACCEPT"); err != nil {
			return err
		}
	}
	if err := add("-j", "DROP"); err != nil {
		return err
	}
	// Jump from INPUT into our chain exactly once — APPENDED, at the end, so
	// any rules another tool owns (ufw, firewalld-persisted rules, docker's
	// hosted-port accepts) are evaluated first and keep working. Only traffic
	// they all ignore reaches our default-drop.
	if !f.sys.OK(ctx, f.bin, "-C", "INPUT", "-j", chain) {
		if _, err := f.sys.Run(ctx, f.bin, "-A", "INPUT", "-j", chain); err != nil {
			return err
		}
	}
	return nil
}

// Clear removes ngray's firewall chain on both IP families, leaving the rest of
// INPUT intact. Used by `ngray remove`.
func (f *Firewall) Clear(ctx context.Context) {
	for _, bin := range []string{"iptables", "ip6tables"} {
		f.sys.OK(ctx, bin, "-D", "INPUT", "-j", chain)
		f.sys.OK(ctx, bin, "-F", chain)
		f.sys.OK(ctx, bin, "-X", chain)
	}
	f.sys.OK(ctx, "netfilter-persistent", "save")
}

// dedupe removes duplicate ports while preserving order.
func dedupe(in []int) []int {
	seen := map[int]bool{}
	out := make([]int, 0, len(in))
	for _, p := range in {
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}
