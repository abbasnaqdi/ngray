// Package domain health-checks a Cloudflare (or direct) domain against the
// server's origin IP before configs are generated, so the operator knows which
// censorship-circumvention scenarios the domain actually covers. It answers one
// question: "is this domain a usable, CF-fronted path to THIS server right now?"
package domain

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Verdict is the headline classification of a domain↔origin check.
type Verdict string

const (
	VerdictCFHealthy   Verdict = "CF_PROXIED_HEALTHY"    // proxied + origin serves TLS for the domain
	VerdictCFOriginBad Verdict = "CF_PROXIED_ORIGIN_BAD" // proxied but CF can't reach/handshake origin (52x)
	VerdictGreyDirect  Verdict = "DNS_ONLY_DIRECT"       // A record points straight at the origin (no CF shield)
	VerdictElsewhere   Verdict = "POINTS_ELSEWHERE"      // resolves, but not to this server and not via CF
	VerdictNoResolve   Verdict = "NOT_RESOLVING"         // no A record from public DNS
)

// Report is the full result of a check.
type Report struct {
	Domain       string
	ServerIP     string
	ResolvedIPs  []string
	ViaCloudflare bool
	EdgeStatus   int    // HTTP status seen at the CF edge (52x = origin problem)
	ServerHeader string // value of the HTTP Server header (e.g. "cloudflare")
	OriginTLSOK  bool   // direct origin:443 completed a TLS handshake for this SNI
	Verdict      Verdict
	Advice       []string
}

// cfRanges are Cloudflare's published anycast IPv4 networks; a resolved IP in
// one of these means the record is proxied even if headers are stripped.
var cfRanges = mustCIDRs(
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
	"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
	"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
)

// Check probes domain against serverIP and returns a scenario-aware report.
func Check(ctx context.Context, domain, serverIP string) Report {
	r := Report{Domain: domain, ServerIP: serverIP}
	r.ResolvedIPs = resolvePublic(ctx, domain)

	pointsHere := false
	for _, ip := range r.ResolvedIPs {
		if ip == serverIP {
			pointsHere = true
		}
		if inCF(ip) {
			r.ViaCloudflare = true
		}
	}

	// Edge probe: HEAD https://domain/ pinned to a resolved IP so we measure the
	// public path, not this host's (possibly hijacked) resolver.
	if len(r.ResolvedIPs) > 0 {
		r.EdgeStatus, r.ServerHeader = edgeProbe(ctx, domain, r.ResolvedIPs[0])
		if r.ServerHeader == "cloudflare" {
			r.ViaCloudflare = true
		}
	}

	// Origin probe: can we complete a TLS handshake straight to the origin for
	// this SNI? (If yes, CF Full/strict will too; if no, that's the 525 cause.)
	r.OriginTLSOK = originTLS(ctx, serverIP, domain)

	r.Verdict, r.Advice = classify(r, pointsHere)
	return r
}

// cfOriginError reports whether status is a Cloudflare origin-side failure.
func cfOriginError(status int) bool {
	switch status {
	case 521, 522, 523, 524, 525, 526:
		return true
	}
	return false
}

func classify(r Report, pointsHere bool) (Verdict, []string) {
	switch {
	case len(r.ResolvedIPs) == 0:
		return VerdictNoResolve, []string{
			"Public DNS returns no A record for the domain — it is not usable yet.",
			"Check the Cloudflare DNS tab; allow a few minutes after adding the record.",
		}

	case r.ViaCloudflare && cfOriginError(r.EdgeStatus):
		adv := []string{
			fmt.Sprintf("Cloudflare reaches the edge but FAILS to your origin (HTTP %d) — the domain is NOT a working tunnel yet.", r.EdgeStatus),
		}
		if r.EdgeStatus == 525 || r.EdgeStatus == 526 {
			adv = append(adv,
				"525/526 = origin TLS handshake/cert. The origin must terminate TLS for this domain on a CF-proxied port.",
				"Fix: install a Cloudflare Origin Certificate on the server, serve it for the domain, and set SSL mode = Full (strict).",
			)
		} else {
			adv = append(adv,
				"52x = origin unreachable from Cloudflare. Ensure the origin listens on a CF-proxied port (443/2053/2083/2087/2096/8443) and the firewall allows Cloudflare.",
			)
		}
		adv = append(adv, "Until fixed: rely on the DIRECT-IP configs (REALITY / Hysteria2 / TUIC / ShadowTLS / Mieru / AmneziaWG).")
		return VerdictCFOriginBad, adv

	case r.ViaCloudflare:
		adv := []string{
			"Domain is proxied through Cloudflare and the origin responds — a CF-fronted path is viable.",
			"Use CF-fronted (VLESS + WS/XHTTP + TLS) configs as the fallback for when the direct IP is blocked.",
			"Keep direct-IP configs too: they are faster and survive a Cloudflare/SNI block.",
		}
		if !r.OriginTLSOK {
			adv = append(adv, "Note: a direct TLS handshake to the origin for this SNI failed — CF likely uses a non-strict SSL mode; consider Full (strict) with an Origin Certificate.")
		}
		return VerdictCFHealthy, adv

	case pointsHere:
		return VerdictGreyDirect, []string{
			"Domain is DNS-only (grey cloud): it resolves straight to your server IP, so it gives NO Cloudflare protection.",
			"If the IP is blocked, the domain is blocked with it. Enable the orange cloud (Proxied) for real resilience.",
			"As-is, the domain is only an alias for the IP — direct-IP configs already cover this.",
		}

	default:
		return VerdictElsewhere, []string{
			fmt.Sprintf("Domain resolves to %v, which is neither this server (%s) nor Cloudflare.", r.ResolvedIPs, r.ServerIP),
			"Point the A record at this server (Proxied) before generating domain configs.",
		}
	}
}

// resolvePublic looks up A records via public resolvers first (1.1.1.1, 8.8.8.8)
// to bypass a hijacked/poisoned local resolver, falling back to the system one.
func resolvePublic(ctx context.Context, domain string) []string {
	for _, ns := range []string{"1.1.1.1:53", "8.8.8.8:53"} {
		res := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "udp", ns)
			},
		}
		if ips, err := res.LookupIP(ctx, "ip4", domain); err == nil && len(ips) > 0 {
			return ipStrings(ips)
		}
	}
	ips, _ := net.DefaultResolver.LookupIP(ctx, "ip4", domain)
	return ipStrings(ips)
}

// edgeProbe sends HEAD https://domain/ but dials the given resolved IP, so the
// SNI/Host is the domain while the connection targets the public path.
func edgeProbe(ctx context.Context, domain, ip string) (status int, server string) {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip, "443"))
		},
		TLSClientConfig:   &tls.Config{ServerName: domain, InsecureSkipVerify: true},
		DisableKeepAlives: true,
	}
	client := &http.Client{Transport: tr, Timeout: 12 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, "https://"+domain+"/", nil)
	if err != nil {
		return 0, ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, ""
	}
	defer resp.Body.Close()
	return resp.StatusCode, normalizeServer(resp.Header.Get("Server"))
}

// originTLS reports whether the origin completes a TLS handshake for this SNI.
func originTLS(ctx context.Context, serverIP, sni string) bool {
	d := net.Dialer{Timeout: 6 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(serverIP, "443"))
	if err != nil {
		return false
	}
	defer conn.Close()
	tconn := tls.Client(conn, &tls.Config{ServerName: sni, InsecureSkipVerify: true})
	hctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	return tconn.HandshakeContext(hctx) == nil
}

func normalizeServer(s string) string {
	if len(s) >= 10 && s[:10] == "cloudflare" {
		return "cloudflare"
	}
	return s
}

func inCF(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, n := range cfRanges {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func ipStrings(ips []net.IP) []string {
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out
}

func mustCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}
