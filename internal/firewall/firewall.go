// Package firewall builds a default-drop inbound ruleset without ever risking
// an SSH lockout: all rules live in a dedicated NGRAY chain that is populated
// fully before INPUT jumps to it, with lo/established/ssh accepted before DROP.
package firewall

import (
	"context"
	"fmt"
	"strconv"

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
	return append(out, "everything else → DROP")
}

func (r Rules) sshPort() int {
	if r.SSHPort > 0 {
		return r.SSHPort
	}
	return 22
}

// Apply installs the ruleset on IPv4 and IPv6, then best-effort persists it.
func (f *Firewall) Apply(ctx context.Context, r Rules) error {
	for _, bin := range []string{"iptables", "ip6tables"} {
		f.bin = bin
		if err := f.applyOne(ctx, r); err != nil {
			return fmt.Errorf("%s: %w", bin, err)
		}
	}
	// UDP port redirects live in the NAT table's PREROUTING chain (IPv4 only).
	for _, rd := range r.Redirects {
		f.sys.OK(ctx, "iptables", "-t", "nat", "-C", "PREROUTING",
			"-p", "udp", "--dport", strconv.Itoa(rd.From), "-j", "REDIRECT", "--to-port", strconv.Itoa(rd.To))
		_, _ = f.sys.Run(ctx, "iptables", "-t", "nat", "-A", "PREROUTING",
			"-p", "udp", "--dport", strconv.Itoa(rd.From), "-j", "REDIRECT", "--to-port", strconv.Itoa(rd.To))
	}
	f.sys.OK(ctx, "netfilter-persistent", "save")
	return nil
}

func (f *Firewall) applyOne(ctx context.Context, r Rules) error {
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
	// Ensure INPUT jumps to our chain exactly once, at the top.
	if !f.sys.OK(ctx, f.bin, "-C", "INPUT", "-j", chain) {
		if _, err := f.sys.Run(ctx, f.bin, "-I", "INPUT", "1", "-j", chain); err != nil {
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
