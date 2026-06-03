// dns_validate.go — DNS resolver validator for DNS tunneling in filtered networks.
//
// ───────────────────────────────────────────────────────────────────
// WHY dig IS WRONG FOR THE TUNNEL PROBE
// ───────────────────────────────────────────────────────────────────
// In heavily filtered networks, DNS packets are often dropped
// selectively — not blocked 100%, but dropped 30–70% of the time.
// dig with +tries=1 sends exactly ONE UDP packet. If it's dropped,
// the probe fails. With THRESHOLD=2/3 outer attempts, a resolver
// with 66% packet loss will only pass ~36% of the time.
//
// MasterDnsVPN's ARQ retransmits automatically — it works fine
// through 60–80% packet loss because it keeps retrying. Our validator
// must match this tolerance: a resolver that responds to any 1 of 5
// probes is viable for MasterDnsVPN.
//
// Solution: raw UDP socket tunnel probe — send 5 packets, accept on
// first valid response. No subprocess, no ICMP dependency, precise
// control, exactly matches how MasterDnsVPN sends queries.
//
// ───────────────────────────────────────────────────────────────────
// WHY hping3 IS WRONG FOR REACHABILITY
// ───────────────────────────────────────────────────────────────────
// hping3 UDP mode: sends generic UDP, detects CLOSED ports via ICMP
// unreachable. In filtered networks ICMP is blocked → every host
// looks unreachable → false negatives for everything.
//
// hping3 TCP SYN to port 53: most resolvers only listen on UDP/53
// → SYN gets no reply even on working resolvers → systematic false
// negatives.
//
// A real DNS A query sent over UDP/53 and waiting for any DNS RCODE
// back is the correct "is there a DNS server here" test. It requires
// no root, no ICMP, produces no false negatives.
//
// ───────────────────────────────────────────────────────────────────
// WHY RANDOM SUBDOMAINS, NOT THE BASE TUNNEL DOMAIN
// ───────────────────────────────────────────────────────────────────
// Querying example.com directly tests its A record — ISPs cache and
// poison this to 10.10.34.35. Querying random.example.com requires
// NS delegation to the tunnel server. MasterDnsVPN sends queries in
// the form [base32-encoded-data].example.com — same mechanism.
//
// ───────────────────────────────────────────────────────────────────
// PIPELINE
// ───────────────────────────────────────────────────────────────────
//   Stage 1   Extract & deduplicate raw tokens from dirty source file
//             (or built-in Iran ranges when --scan-iran is used).
//             Write clean-source.txt.
//
//   Stage 2a  Parallel raw-socket DNS pre-filter on plain IPs.
//             DNS-active IPs written to unique-dns.txt.
//
//   Stage 2b  Expand CIDRs and ranges, parallel raw-socket DNS
//             pre-filter on expanded IPs. Append to unique-dns.txt.
//
//   Stage 3   Full validation of every IP in unique-dns.txt:
//     1. Tunnel probe   raw UDP, 5 probes for random.example.com,
//                       accept on FIRST response with NXDOMAIN/NOERROR + RA
//     2. google.com     dig +short — answer IPs must not be RFC1918
//     3. NXDOMAIN check dig +short random .com — any IP = hijacking
//     4. Upload MTU     raw UDP label-encoded subdomains
//     5. Download MTU   dig TXT google.com MSG SIZE rcvd
//     6. EDNS0          informational only → edns.txt
//
// Outputs:
//   clean-source.txt   deduplicated input tokens
//   unique-dns.txt     DNS-responsive candidates, sorted by address
//   valid-dns.txt      resolvers passing all checks, sorted by latency
//   edns.txt           subset of valid-dns with EDNS0 support

package dnsval

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ── configuration ──────────────────────────────────────────────────────────────

const (
	defaultSourceFile = "source.txt"
	cleanFile         = "clean-source.txt"
	uniqueDNSFile     = "unique-dns.txt"
	validDNSFile      = "valid-dns.txt"
	ednsFile          = "edns.txt"
	defaultDomain     = "example.com"

	defaultThreads = 100
	defaultTimeout = 4 // seconds, for dig probes
	slowMS         = 500

	prefilterTimeout = 2000 * time.Millisecond
	tunnelProbes     = 5
	tunnelProbeGap   = 150 * time.Millisecond

	mtuProbeTimeout = 2000 * time.Millisecond
	mtuProbes       = 3

	maxCIDRHosts = 1024
	maxTotalIPs  = 500_000
)

var uploadMTUSizes = []int{40, 70, 100, 120, 134}

// iranCIDRs is a curated list of Iranian ISP and datacenter address blocks
// known to carry recursive DNS resolvers. Used by --scan-iran.
var iranCIDRs = []string{
	// MCI (Hamrahe Avval / Iran Telecom)
	"5.22.0.0/17",
	"5.53.128.0/19",
	"31.2.64.0/18",
	"31.40.0.0/16",
	"37.254.0.0/16",
	"46.224.0.0/15",
	"77.75.64.0/18",
	"78.38.0.0/15",
	"80.71.0.0/17",
	"82.99.192.0/18",
	"85.15.0.0/16",
	"86.57.0.0/17",
	"91.98.0.0/15",
	"94.74.128.0/17",
	"95.38.0.0/16",
	"109.107.128.0/17",
	"176.65.192.0/18",
	"178.157.0.0/16",
	"185.18.192.0/22",
	"195.146.32.0/19",
	// Irancell (MTN)
	"5.160.0.0/14",
	"37.32.0.0/16",
	"37.255.0.0/16",
	"46.100.0.0/15",
	"93.126.0.0/16",
	"93.127.0.0/16",
	"188.158.0.0/15",
	"188.160.0.0/13",
	// TCI (Telecommunication Company of Iran)
	"2.176.0.0/12",
	"5.200.0.0/16",
	"37.156.0.0/15",
	"78.109.128.0/17",
	"83.147.192.0/18",
	"89.42.208.0/20",
	"94.182.0.0/15",
	"95.80.0.0/13",
	"109.122.192.0/18",
	"151.232.0.0/13",
	"176.99.0.0/17",
	"185.209.96.0/22",
	// Shatel
	"5.144.128.0/17",
	"31.14.80.0/20",
	"31.56.0.0/14",
	"62.60.160.0/19",
	"77.38.0.0/17",
	"178.251.128.0/18",
	"185.55.224.0/22",
	"185.147.160.0/22",
	// Rightel
	"46.34.96.0/19",
	"185.8.172.0/22",
	"185.108.0.0/22",
	// Pars Online
	"82.138.128.0/17",
	"87.107.0.0/16",
	"94.101.128.0/17",
	"185.96.0.0/22",
	// Asiatech
	"80.191.0.0/16",
	"81.28.0.0/16",
	"185.2.12.0/22",
	// Afranet
	"217.218.0.0/15",
	"217.172.0.0/14",
	// DATAK
	"85.133.0.0/16",
	"185.116.0.0/22",
	// ArvanCloud
	"185.143.232.0/22",
	"185.215.228.0/22",
	"194.5.175.0/24",
	// IranServer datacenter
	"5.61.24.0/21",
	"62.102.128.0/17",
	"185.130.0.0/16",
}

// ── ANSI colors ────────────────────────────────────────────────────────────────

var (
	colRST  = ""
	colBold = ""
	colDim  = ""
	colBGrn = ""
	colBYel = ""
	colBRed = ""
	colBCyn = ""
	colBBlu = ""
	colGrn  = ""
)

func initColors() {
	fi, err := os.Stdout.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return
	}
	colRST = "\033[0m"
	colBold = "\033[1m"
	colDim = "\033[2m"
	colBGrn = "\033[1;32m"
	colBYel = "\033[1;33m"
	colBRed = "\033[1;31m"
	colBCyn = "\033[1;36m"
	colBBlu = "\033[1;34m"
	colGrn = "\033[0;32m"
}

// ── box-drawing primitives ─────────────────────────────────────────────────────

const boxW = 58

func boxTop() string {
	return fmt.Sprintf("%s╭%s╮%s", colBBlu, strings.Repeat("─", boxW), colRST)
}
func boxBot() string {
	return fmt.Sprintf("%s╰%s╯%s", colBBlu, strings.Repeat("─", boxW), colRST)
}
func boxSep() string {
	return fmt.Sprintf("%s├%s┤%s", colBBlu, strings.Repeat("─", boxW), colRST)
}
func boxRow(label, val, vc string) string {
	// visible length = 2 (indent) + 14 (label) + len(val)
	pad := max(boxW-2-14-len(val), 0)
	return fmt.Sprintf("%s│%s  %s%-14s%s%s%s%s%s%s│%s",
		colBBlu, colRST,
		colDim, label, colRST,
		vc, val, colRST,
		strings.Repeat(" ", pad),
		colBBlu, colRST)
}
func boxTitle(title string) string {
	pad := max(boxW-4-len(title), 0)
	return fmt.Sprintf("%s│%s  %s◆  %s%s%s%s│%s",
		colBBlu, colRST,
		colBold, title, colRST,
		strings.Repeat(" ", pad),
		colBBlu, colRST)
}

// printHeader prints a minimal single-line titled box.
func printHeader(title string) {
	fmt.Printf("\n%s\n%s\n%s\n", boxTop(), boxTitle(title), boxBot())
}

// printBanner prints the startup config panel.
func printBanner(source, domain string, threads, timeout int, scanIran bool) {
	fmt.Println(boxTop())
	fmt.Println(boxTitle("DNS Resolver Validator"))
	fmt.Println(boxSep())
	fmt.Println(boxRow("domain", domain, colBCyn))
	if scanIran {
		fmt.Println(boxRow("source", fmt.Sprintf("built-in  (%d Iranian CIDRs)", len(iranCIDRs)), colBYel))
	} else {
		fmt.Println(boxRow("source", source, colBold))
	}
	fmt.Println(boxRow("threads", strconv.Itoa(threads), colBold))
	fmt.Println(boxRow("timeout",
		fmt.Sprintf("%ds dig  /  %.1fs prefilter", timeout, prefilterTimeout.Seconds()),
		colBold))
	fmt.Println(boxRow("tunnel",
		fmt.Sprintf("%d probes  gap %dms", tunnelProbes, int(tunnelProbeGap.Milliseconds())),
		colBold))
	fmt.Println(boxBot())
}

// printSummary prints the final results panel as a single bordered box.
func printSummary(
	ext extracted,
	dnsFromPlains, dnsFromRanges []string,
	allDNS []string,
	valid, ednsValid, poisoned, hijacked, slowOK []validResult,
) {
	dash := func(n int, c string) string {
		if n == 0 {
			return colDim + "0" + colRST
		}
		return c + strconv.Itoa(n) + colRST
	}
	fmt.Printf("\n%s\n%s\n%s\n", boxTop(), boxTitle("Results"), boxSep())
	fmt.Println(boxRow("valid", strconv.Itoa(len(valid)), colBGrn))
	fmt.Println(boxRow("  → file", validDNSFile, colDim))
	fmt.Println(boxRow("edns", strconv.Itoa(len(ednsValid)), colBCyn))
	fmt.Println(boxRow("  → file", ednsFile, colDim))
	fmt.Println(boxSep())
	fmt.Println(boxRow("dns-active", strconv.Itoa(len(allDNS)), colBold))
	fmt.Println(boxRow("  plains", strconv.Itoa(len(dnsFromPlains)), colDim))
	fmt.Println(boxRow("  expanded", strconv.Itoa(len(dnsFromRanges)), colDim))
	fmt.Println(boxRow("src IPs", strconv.Itoa(len(ext.plains)), colDim))
	fmt.Println(boxRow("src CIDRs", strconv.Itoa(len(ext.cidrs)), colDim))
	fmt.Println(boxRow("src ranges", strconv.Itoa(len(ext.ranges)), colDim))
	fmt.Println(boxSep())
	fmt.Println(boxRow("poisoned", dash(len(poisoned), colBRed), ""))
	fmt.Println(boxRow("hijacked", dash(len(hijacked), colBYel), ""))
	fmt.Println(boxRow(
		fmt.Sprintf("slow >%dms", slowMS),
		dash(len(slowOK), colBYel), ""))
	fmt.Println(boxBot())
}

func logMsg(msg, color string) {
	fmt.Printf("  %s%s%s  %s%s%s\n",
		colDim, time.Now().Format("15:04:05"), colRST,
		color, msg, colRST)
}

func progressBar(done, total, width int) string {
	if total <= 0 {
		total = 1
	}
	fill := min(width*done/total, width)
	return fmt.Sprintf("%s%s%s%s",
		colBBlu, strings.Repeat("█", fill), strings.Repeat("░", width-fill), colRST)
}

// ── regex ──────────────────────────────────────────────────────────────────────

var (
	reCIDR    = regexp.MustCompile(`(?:\d{1,3}\.){3}\d{1,3}/\d{1,2}`)
	reRange   = regexp.MustCompile(`((?:\d{1,3}\.){3}\d{1,3})\s*-\s*((?:\d{1,3}\.){3}\d{1,3})`)
	reIP      = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	reStrict  = regexp.MustCompile(`\b(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(?:\.(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}\b`)
	reMsgSize = regexp.MustCompile(`MSG SIZE\s+rcvd:\s+(\d+)`)
	reFlags   = regexp.MustCompile(`;;\s*flags:\s*([^;]+)`)
	reAD      = regexp.MustCompile(`\bad\b`)
)

// ── IP utilities ───────────────────────────────────────────────────────────────

var privateNets []*net.IPNet

func init() {
	for _, cidr := range []string{
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
		"127.0.0.0/8", "169.254.0.0/16", "100.64.0.0/10",
		"0.0.0.0/8", "240.0.0.0/4", "224.0.0.0/4",
	} {
		_, n, _ := net.ParseCIDR(cidr)
		privateNets = append(privateNets, n)
	}
}

func ipToUint32(ip string) (uint32, bool) {
	p := net.ParseIP(ip)
	if p == nil {
		return 0, false
	}
	v4 := p.To4()
	if v4 == nil {
		return 0, false
	}
	return binary.BigEndian.Uint32(v4), true
}

func uint32ToIP(n uint32) string {
	b := [4]byte{}
	binary.BigEndian.PutUint32(b[:], n)
	return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3])
}

func isPrivate(ip string) bool {
	p := net.ParseIP(ip)
	if p == nil {
		return true
	}
	for _, n := range privateNets {
		if n.Contains(p) {
			return true
		}
	}
	return false
}

func isGlobal(ip string) bool {
	p := net.ParseIP(ip)
	if p == nil {
		return false
	}
	if p.IsLoopback() || p.IsLinkLocalUnicast() || p.IsMulticast() || p.IsUnspecified() {
		return false
	}
	return !isPrivate(ip)
}

func sortIPs(ips []string) []string {
	slices.SortFunc(ips, func(a, b string) int {
		ai, ok1 := ipToUint32(a)
		bi, ok2 := ipToUint32(b)
		if !ok1 || !ok2 {
			return strings.Compare(a, b)
		}
		switch {
		case ai < bi:
			return -1
		case ai > bi:
			return 1
		default:
			return 0
		}
	})
	return ips
}

func expandCIDRIntoPool(cidr string, pool map[string]struct{}, cap int) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return
	}
	ones, bits := network.Mask.Size()
	hostBits := bits - ones
	total := 1 << uint(hostBits)
	if total > maxCIDRHosts+2 {
		return
	}
	base, ok := ipToUint32(network.IP.String())
	if !ok {
		return
	}
	for i := range total - 2 {
		if len(pool) >= cap {
			return
		}
		if s := uint32ToIP(base + uint32(i+1)); isGlobal(s) {
			pool[s] = struct{}{}
		}
	}
}

func expandRangeIntoPool(a, b string, pool map[string]struct{}, cap int) {
	lo, ok1 := ipToUint32(a)
	hi, ok2 := ipToUint32(b)
	if !ok1 || !ok2 {
		return
	}
	if lo > hi {
		lo, hi = hi, lo
	}
	if hi-lo > maxCIDRHosts {
		return
	}
	for i := lo; i <= hi; i++ {
		if len(pool) >= cap {
			return
		}
		if s := uint32ToIP(i); isGlobal(s) {
			pool[s] = struct{}{}
		}
	}
}

// ── Stage 1: extract & deduplicate ────────────────────────────────────────────

type extracted struct {
	plains []string
	cidrs  []string
	ranges [][2]string
}

func extractClean(text string) extracted {
	printHeader("Stage 1 — Extract & Deduplicate")

	rawCIDRs := reCIDR.FindAllString(text, -1)
	rawRanges := reRange.FindAllStringSubmatch(text, -1)
	rawPlains := reIP.FindAllString(text, -1)

	fmt.Printf("  %sraw  %s%d%s%s CIDRs  %s%d%s%s ranges  %s%d%s%s IPs%s\n",
		colDim,
		colBold, len(rawCIDRs), colRST, colDim,
		colBold, len(rawRanges), colRST, colDim,
		colBold, len(rawPlains), colRST, colDim, colRST)

	seenCIDR := map[string]struct{}{}
	var cidrs []string
	for _, c := range rawCIDRs {
		_, network, err := net.ParseCIDR(c)
		if err != nil {
			continue
		}
		key := network.String()
		if _, exists := seenCIDR[key]; exists {
			continue
		}
		seenCIDR[key] = struct{}{}
		ones, bits := network.Mask.Size()
		if (1 << uint(bits-ones)) <= maxCIDRHosts+2 {
			cidrs = append(cidrs, key)
		}
	}

	type rKey struct{ a, b string }
	seenRange := map[rKey]struct{}{}
	var ranges [][2]string
	for _, m := range rawRanges {
		a, b := m[1], m[2]
		lo, ok1 := ipToUint32(a)
		hi, ok2 := ipToUint32(b)
		if !ok1 || !ok2 {
			continue
		}
		if lo > hi {
			a, b = b, a
			lo, hi = hi, lo
		}
		k := rKey{a, b}
		if _, exists := seenRange[k]; exists {
			continue
		}
		seenRange[k] = struct{}{}
		if hi-lo <= maxCIDRHosts {
			ranges = append(ranges, [2]string{a, b})
		}
	}

	seenIP := map[string]struct{}{}
	var plains []string
	for _, ip := range rawPlains {
		if _, exists := seenIP[ip]; exists {
			continue
		}
		seenIP[ip] = struct{}{}
		if isGlobal(ip) {
			plains = append(plains, ip)
		}
	}

	lines := make([]string, 0, len(cidrs)+len(ranges)+len(plains))
	lines = append(lines, cidrs...)
	for _, r := range ranges {
		lines = append(lines, r[0]+"-"+r[1])
	}
	lines = append(lines, plains...)
	writeLines(cleanFile, lines)

	logMsg(fmt.Sprintf("dedup  %s%d%s%s CIDRs  %d ranges  %d IPs  →  %s",
		colBGrn, len(cidrs), colRST, colGrn, len(ranges), len(plains), cleanFile), colGrn)

	return extracted{plains: plains, cidrs: cidrs, ranges: ranges}
}

// extractFromIranCIDRs builds an extracted struct directly from the built-in
// Iranian CIDR list, bypassing file parsing entirely.
func extractFromIranCIDRs() extracted {
	printHeader("Stage 1 — Built-in Iran CIDRs")

	seenCIDR := map[string]struct{}{}
	var cidrs []string
	for _, c := range iranCIDRs {
		_, network, err := net.ParseCIDR(c)
		if err != nil {
			continue
		}
		key := network.String()
		if _, exists := seenCIDR[key]; exists {
			continue
		}
		seenCIDR[key] = struct{}{}
		cidrs = append(cidrs, key)
	}

	lines := make([]string, len(cidrs))
	copy(lines, cidrs)
	writeLines(cleanFile, lines)

	logMsg(fmt.Sprintf("%s%d%s%s Iranian CIDRs loaded  →  %s",
		colBGrn, len(cidrs), colRST, colGrn, cleanFile), colGrn)

	return extracted{cidrs: cidrs}
}

// ── Stage 2b: expand CIDRs/ranges ─────────────────────────────────────────────

func expandAll(cidrs []string, ranges [][2]string, exclude map[string]struct{}) []string {
	pool := make(map[string]struct{}, 8192)
	capped := false

	for _, c := range cidrs {
		if len(pool) >= maxTotalIPs {
			capped = true
			break
		}
		expandCIDRIntoPool(c, pool, maxTotalIPs)
	}
	if !capped {
		for _, r := range ranges {
			if len(pool) >= maxTotalIPs {
				capped = true
				break
			}
			expandRangeIntoPool(r[0], r[1], pool, maxTotalIPs)
		}
	}
	if capped {
		logMsg(fmt.Sprintf("⚠  expansion capped at %d IPs", maxTotalIPs), colBYel)
	}
	for ip := range exclude {
		delete(pool, ip)
	}
	result := make([]string, 0, len(pool))
	for ip := range pool {
		result = append(result, ip)
	}
	return sortIPs(result)
}

// ── raw DNS packet builder ─────────────────────────────────────────────────────

func buildQuery(domain string, qtype uint16) ([]byte, uint16) {
	qid := uint16(rand.Intn(65535) + 1)
	pkt := make([]byte, 0, 64)

	hdr := make([]byte, 12)
	binary.BigEndian.PutUint16(hdr[0:], qid)
	binary.BigEndian.PutUint16(hdr[2:], 0x0100) // RD=1
	binary.BigEndian.PutUint16(hdr[4:], 1)      // QDCOUNT=1
	pkt = append(pkt, hdr...)

	domain = strings.TrimSuffix(domain, ".")
	for label := range strings.SplitSeq(domain, ".") {
		b := []byte(label)
		pkt = append(pkt, byte(len(b)))
		pkt = append(pkt, b...)
	}
	pkt = append(pkt, 0x00)

	tail := [4]byte{}
	binary.BigEndian.PutUint16(tail[0:], qtype)
	binary.BigEndian.PutUint16(tail[2:], 1) // QCLASS=IN
	return append(pkt, tail[:]...), qid
}

func parseResponse(data []byte, expectedID uint16) (rcode int, ra bool, valid bool) {
	if len(data) < 12 {
		return 0, false, false
	}
	if binary.BigEndian.Uint16(data[0:2]) != expectedID {
		return 0, false, false
	}
	flags := binary.BigEndian.Uint16(data[2:4])
	if flags&0x8000 == 0 {
		return 0, false, false
	}
	return int(flags & 0x000F), flags&0x0080 != 0, true
}

// ── raw UDP DNS probe ──────────────────────────────────────────────────────────

type probeResult struct {
	rcode int // -1 = timeout / error
	ra    bool
	ms    float64
}

func rawProbe(ip, domain string, timeout time.Duration, qtype uint16) probeResult {
	pkt, qid := buildQuery(domain, qtype)
	t0 := time.Now()

	conn, err := net.DialTimeout("udp4", ip+":53", timeout)
	if err != nil {
		return probeResult{rcode: -1, ms: float64(timeout.Milliseconds())}
	}
	defer conn.Close()

	if err = conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return probeResult{rcode: -1, ms: float64(timeout.Milliseconds())}
	}
	if _, err = conn.Write(pkt); err != nil {
		return probeResult{rcode: -1, ms: float64(timeout.Milliseconds())}
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	ms := float64(time.Since(t0).Microseconds()) / 1000.0
	if err != nil {
		return probeResult{rcode: -1, ms: float64(timeout.Milliseconds())}
	}

	rcode, ra, valid := parseResponse(buf[:n], qid)
	if !valid {
		return probeResult{rcode: -1, ms: ms}
	}
	return probeResult{rcode: rcode, ra: ra, ms: ms}
}

func isTunnelRcode(rcode int) bool { return rcode == 0 || rcode == 3 }

// ── pre-filter ─────────────────────────────────────────────────────────────────

func isDNSAlive(ip string) bool {
	return rawProbe(ip, "google.com", prefilterTimeout, 1).rcode != -1
}

func prefilter(ips []string, threads int, label string) []string {
	printHeader(label)
	fmt.Printf("  %s%d candidates  threads:%d  timeout:%.1fs%s\n\n",
		colDim, len(ips), threads, prefilterTimeout.Seconds(), colRST)

	var (
		alive []string
		mu    sync.Mutex
		total = len(ips)
		t0    = time.Now()
	)
	var done int64

	showProgress := func() {
		d := atomic.LoadInt64(&done)
		mu.Lock()
		aliveN := len(alive)
		mu.Unlock()
		elapsed := time.Since(t0).Seconds()
		rate := 0.0
		if elapsed >= 0.001 {
			rate = float64(d) / elapsed
		}
		fmt.Printf("\r  %s  %s%d/%d%s  %s✔ %d%s  %s%.0f/s  %.0f%%%s",
			progressBar(int(d), total, 30),
			colDim, d, total, colRST,
			colBGrn, aliveN, colRST,
			colDim, rate, float64(d)/float64(max(total, 1))*100, colRST)
	}

	sem := make(chan struct{}, threads)
	var wg sync.WaitGroup
	for _, ip := range ips {
		wg.Add(1)
		sem <- struct{}{}
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()
			isAlive := isDNSAlive(ip)
			mu.Lock()
			atomic.AddInt64(&done, 1)
			if isAlive {
				alive = append(alive, ip)
			}
			mu.Unlock()
			showProgress()
		}(ip)
	}
	wg.Wait()
	fmt.Println()

	sortIPs(alive)
	logMsg(fmt.Sprintf("%s%d%s%s active  /  %d probed  (%d dropped)",
		colBGrn, len(alive), colRST, colGrn, len(ips), len(ips)-len(alive)), colGrn)
	return alive
}

// ── random string helpers ──────────────────────────────────────────────────────

const (
	lowerAlpha = "abcdefghijklmnopqrstuvwxyz"
	lowerAlnum = "abcdefghijklmnopqrstuvwxyz0123456789"
)

func randStr(n int) string {
	b := make([]byte, n)
	for i := range n {
		b[i] = lowerAlpha[rand.Intn(len(lowerAlpha))]
	}
	return string(b)
}

func randAlnum(n int) string {
	b := make([]byte, n)
	for i := range n {
		b[i] = lowerAlnum[rand.Intn(len(lowerAlnum))]
	}
	return string(b)
}

// ── tunnel probe ──────────────────────────────────────────────────────────────

func probeTunnel(ip, domain string) (bool, float64) {
	target := randStr(10) + "." + domain
	probeTimeout := tunnelProbeGap + 500*time.Millisecond
	for range tunnelProbes {
		r := rawProbe(ip, target, probeTimeout, 1)
		if r.rcode != -1 && r.ra && isTunnelRcode(r.rcode) {
			return true, r.ms
		}
		time.Sleep(tunnelProbeGap)
	}
	return false, 0
}

// ── dig wrapper ────────────────────────────────────────────────────────────────

type digResult struct {
	code int
	out  string
	ms   float64
}

func runDig(ip, domain string, timeout int, extra ...string) digResult {
	args := make([]string, 0, 4+len(extra))
	args = append(args, "@"+ip, domain, "+time=2", "+tries=2")
	args = append(args, extra...)

	t0 := time.Now()
	out, err := exec.Command("dig", args...).Output()
	ms := float64(time.Since(t0).Microseconds()) / 1000.0

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return digResult{
				code: exitErr.ExitCode(),
				out:  string(out) + string(exitErr.Stderr),
				ms:   ms,
			}
		}
		return digResult{code: -1, ms: float64(timeout) * 1000}
	}
	return digResult{code: 0, out: string(out), ms: ms}
}

func answerIPs(shortOut string) []string { return reStrict.FindAllString(shortOut, -1) }

func extractFlags(digOut string) string {
	if m := reFlags.FindStringSubmatch(digOut); m != nil {
		return m[1]
	}
	return ""
}

// ── secondary probes ───────────────────────────────────────────────────────────

func probeGoogle(ip string, timeout int) string {
	r := runDig(ip, "google.com", timeout, "+short")
	if r.code == -1 || strings.TrimSpace(r.out) == "" {
		return ""
	}
	for _, a := range answerIPs(r.out) {
		if isPrivate(a) {
			return "poison:google→" + a
		}
	}
	return ""
}

func probeNXDomain(ip string, timeout int) bool {
	r := runDig(ip, randAlnum(20)+".com", timeout, "+short")
	if r.code == -1 {
		return true
	}
	return len(answerIPs(r.out)) == 0
}

func probeUploadMTU(ip, domain string) int {
	maxOK := 0
	for _, size := range uploadMTUSizes {
		payload := randAlnum(size)
		var labels []string
		for i := 0; i < len(payload); i += 63 {
			labels = append(labels, payload[i:min(i+63, len(payload))])
		}
		fqdn := strings.Join(labels, ".") + "." + domain
		accepted := false
		for range mtuProbes {
			r := rawProbe(ip, fqdn, mtuProbeTimeout, 1)
			if r.rcode != -1 && r.ra && isTunnelRcode(r.rcode) {
				accepted = true
				break
			}
		}
		if accepted {
			maxOK = size
		} else {
			break
		}
	}
	return maxOK
}

func probeDownloadMTU(ip string, timeout int) int {
	r := runDig(ip, "google.com", timeout, "TXT", "+bufsize=4096", "+ignore")
	if r.code == -1 || strings.Contains(r.out, "FORMERR") {
		return 0
	}
	if m := reMsgSize.FindStringSubmatch(r.out); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

func probeEDNS(ip string, timeout int) (bool, bool) {
	r := runDig(ip, "dns.google", timeout, "+bufsize=4096", "+dnssec")
	if r.code == -1 {
		return false, false
	}
	return strings.Contains(r.out, "EDNS:"), reAD.MatchString(extractFlags(r.out))
}

// ── per-IP full validation ─────────────────────────────────────────────────────

type validResult struct {
	ip     string
	valid  bool
	avgMS  float64
	edns   bool
	dnssec bool
	ulMTU  int
	dlMTU  int
	flags  []string
}

func validateIP(ip, domain string, timeout int) validResult {
	r := validResult{ip: ip}

	passed, ms := probeTunnel(ip, domain)
	if !passed {
		return r
	}
	r.avgMS = ms

	var (
		googleErr string
		nxdOK     bool
		dlMTU     int
		wg        sync.WaitGroup
	)
	wg.Add(3)
	go func() { defer wg.Done(); googleErr = probeGoogle(ip, timeout) }()
	go func() { defer wg.Done(); nxdOK = probeNXDomain(ip, timeout) }()
	go func() { defer wg.Done(); dlMTU = probeDownloadMTU(ip, timeout) }()
	wg.Wait()

	if googleErr != "" {
		r.flags = append(r.flags, googleErr)
		return r
	}
	if !nxdOK {
		r.flags = append(r.flags, "nxdomain-hijack")
		return r
	}
	r.dlMTU = dlMTU

	var (
		ulMTU  int
		edns   bool
		dnssec bool
	)
	wg.Add(2)
	go func() { defer wg.Done(); ulMTU = probeUploadMTU(ip, domain) }()
	go func() { defer wg.Done(); edns, dnssec = probeEDNS(ip, timeout) }()
	wg.Wait()

	r.ulMTU = ulMTU
	r.edns = edns
	r.dnssec = dnssec
	if r.avgMS > float64(slowMS) {
		r.flags = append(r.flags, "slow")
	}
	r.valid = true
	return r
}

// ── validation progress ────────────────────────────────────────────────────────

type valProgress struct {
	total  int
	done   atomic.Int64
	ok     atomic.Int64
	poison atomic.Int64
	hijack atomic.Int64
	t0     time.Time
}

func newValProgress(total int) *valProgress {
	return &valProgress{total: total, t0: time.Now()}
}

func (p *valProgress) update(r validResult) {
	p.done.Add(1)
	if r.valid {
		p.ok.Add(1)
	}
	if slices.ContainsFunc(r.flags, func(f string) bool { return strings.HasPrefix(f, "poison") }) {
		p.poison.Add(1)
	}
	if slices.Contains(r.flags, "nxdomain-hijack") {
		p.hijack.Add(1)
	}

	d := p.done.Load()
	ok := p.ok.Load()
	po := p.poison.Load()
	hi := p.hijack.Load()

	elapsed := time.Since(p.t0).Seconds()
	rate := 0.0
	if elapsed >= 0.001 {
		rate = float64(d) / elapsed
	}

	fmt.Printf("\r  %s  %s%d/%d%s  %s✔ %d%s  %s✘ %d%s  %s⚑ %d%s  %s%.0f/s  %.0f%%%s",
		progressBar(int(d), p.total, 30),
		colDim, d, p.total, colRST,
		colBGrn, ok, colRST,
		colBRed, po, colRST,
		colBYel, hi, colRST,
		colDim, rate, float64(d)/float64(max(p.total, 1))*100, colRST)

	if d == int64(p.total) {
		fmt.Println()
	}
}

// ── Stage 3 ────────────────────────────────────────────────────────────────────

func runValidation(ips []string, domain string, threads, timeout int) []validResult {
	printHeader("Stage 3 — Validate")
	fmt.Printf("  %s%d resolvers  %d threads  %ds timeout%s\n\n",
		colDim, len(ips), threads, timeout, colRST)

	shuffled := slices.Clone(ips)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	prog := newValProgress(len(shuffled))
	results := make([]validResult, 0, len(shuffled))
	var mu sync.Mutex

	sem := make(chan struct{}, threads)
	var wg sync.WaitGroup

	for _, ip := range shuffled {
		wg.Add(1)
		sem <- struct{}{}
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()
			r := validateIP(ip, domain, timeout)
			mu.Lock()
			results = append(results, r)
			mu.Unlock()
			prog.update(r)
		}(ip)
	}
	wg.Wait()
	return results
}

// ── interactive post-run REPL ──────────────────────────────────────────────────

func interactiveREPL(valid []validResult, domain string) {
	if len(valid) == 0 {
		return
	}

	help := func() {
		fmt.Printf("\n%s\n%s\n%s\n", boxTop(), boxTitle("Interactive Mode"), boxSep())
		fmt.Println(boxRow("list [n]", "show top n resolvers (default 10)", colBold))
		fmt.Println(boxRow("show <ip>", "full details for one resolver", colBold))
		fmt.Println(boxRow("copy", "print IPs one per line (for pipe)", colBold))
		fmt.Println(boxRow("save <file>", "write IPs to file", colBold))
		fmt.Println(boxRow("test <ip>", "quick tunnel probe right now", colBold))
		fmt.Println(boxRow("q / quit", "exit", colBold))
		fmt.Println(boxBot())
	}

	printList := func(n int) {
		n = min(n, len(valid))
		fmt.Printf("\n  %s%-18s  %5s  %3s  %4s  edns  sec  flags%s\n",
			colDim, "IP", "ms", "ul", "dl", colRST)
		fmt.Printf("  %s%s%s\n", colDim,
			strings.Repeat("─", 58), colRST)
		for i, r := range valid[:n] {
			ul := "─"
			if r.ulMTU > 0 {
				ul = strconv.Itoa(r.ulMTU)
			}
			dl := "─"
			if r.dlMTU > 0 {
				dl = strconv.Itoa(r.dlMTU)
			}
			ednsS := colDim + "─" + colRST
			if r.edns {
				ednsS = colBGrn + "✔" + colRST
			}
			secS := colDim + "─" + colRST
			if r.dnssec {
				secS = colBGrn + "✔" + colRST
			}
			var fparts []string
			for _, f := range r.flags {
				if f == "slow" {
					fparts = append(fparts, colBYel+f+colRST)
				}
			}
			fs := colDim + "─" + colRST
			if len(fparts) > 0 {
				fs = strings.Join(fparts, " ")
			}
			ipCol := colBGrn
			if slices.Contains(r.flags, "slow") {
				ipCol = colBYel
			}
			fmt.Printf("  %s%2d%s  %s%-18s%s  %s%5.0f%s  %3s  %4s  %s    %s    %s\n",
				colDim, i+1, colRST,
				ipCol, r.ip, colRST,
				colBold, r.avgMS, colRST,
				ul, dl, ednsS, secS, fs)
		}
		fmt.Println()
	}

	printDetail := func(ip string) {
		r, ok := func() (validResult, bool) {
			for _, v := range valid {
				if v.ip == ip {
					return v, true
				}
			}
			return validResult{}, false
		}()
		if !ok {
			fmt.Printf("  %s%s not in valid list%s\n", colBRed, ip, colRST)
			return
		}
		fmt.Printf("\n%s\n%s\n%s\n", boxTop(), boxTitle("Resolver  "+r.ip), boxSep())
		fmt.Println(boxRow("latency", fmt.Sprintf("%.0f ms", r.avgMS), colBold))
		fmt.Println(boxRow("upload MTU", strconv.Itoa(r.ulMTU)+" bytes", colBold))
		fmt.Println(boxRow("download MTU", strconv.Itoa(r.dlMTU)+" bytes", colBold))
		ednsStr := colDim + "no" + colRST
		if r.edns {
			ednsStr = colBGrn + "yes" + colRST
		}
		secStr := colDim + "no" + colRST
		if r.dnssec {
			secStr = colBGrn + "yes" + colRST
		}
		fmt.Println(boxRow("EDNS0", ednsStr, ""))
		fmt.Println(boxRow("DNSSEC", secStr, ""))
		fstr := "none"
		if len(r.flags) > 0 {
			fstr = strings.Join(r.flags, ", ")
		}
		fmt.Println(boxRow("flags", fstr, colBYel))
		fmt.Println(boxRow("test cmd",
			fmt.Sprintf("dig @%s %s.%s", r.ip, randStr(6), domain), colDim))
		fmt.Println(boxBot())
	}

	runTest := func(ip string, timeout int) {
		fmt.Printf("  %sprobing %s ...%s\n", colDim, ip, colRST)
		passed, ms := probeTunnel(ip, domain)
		if passed {
			logMsg(fmt.Sprintf("%s%s%s  tunnel  %s%.0f ms%s", colBGrn, ip, colRST, colBold, ms, colRST), colBGrn)
		} else {
			logMsg(fmt.Sprintf("%s%s%s  no response", colBRed, ip, colRST), colBRed)
		}
	}

	help()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("%s›%s ", colBBlu, colRST)
		if !scanner.Scan() {
			break
		}
		parts := strings.Fields(scanner.Text())
		if len(parts) == 0 {
			continue
		}
		switch parts[0] {
		case "list", "l":
			n := 10
			if len(parts) > 1 {
				if v, err := strconv.Atoi(parts[1]); err == nil && v > 0 {
					n = v
				}
			}
			printList(n)

		case "show", "s":
			if len(parts) < 2 {
				fmt.Printf("  %susage: show <ip>%s\n", colBYel, colRST)
				continue
			}
			printDetail(parts[1])

		case "copy", "c":
			for _, r := range valid {
				fmt.Println(r.ip)
			}

		case "save":
			if len(parts) < 2 {
				fmt.Printf("  %susage: save <filename>%s\n", colBYel, colRST)
				continue
			}
			ips := make([]string, len(valid))
			for i, r := range valid {
				ips[i] = r.ip
			}
			writeLines(parts[1], ips)
			logMsg(fmt.Sprintf("saved %d IPs  →  %s", len(ips), parts[1]), colGrn)

		case "test", "t":
			if len(parts) < 2 {
				fmt.Printf("  %susage: test <ip>%s\n", colBYel, colRST)
				continue
			}
			runTest(parts[1], defaultTimeout)

		case "help", "h", "?":
			help()

		case "q", "quit", "exit":
			fmt.Println()
			return

		default:
			fmt.Printf("  %sunknown command — type help%s\n", colBYel, colRST)
		}
	}
}

// ── utilities ──────────────────────────────────────────────────────────────────

func writeLines(path string, lines []string) {
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		logMsg(fmt.Sprintf("failed to write %s: %v", path, err), colBRed)
	}
}

func mapKeys(m map[string]struct{}) []string {
	s := make([]string, 0, len(m))
	for k := range m {
		s = append(s, k)
	}
	return s
}

// ── main ───────────────────────────────────────────────────────────────────────

// Run executes the DNS resolver validator with the given args.
func Run(args []string) error {
	initColors()

	fs := flag.NewFlagSet("dns validate", flag.ContinueOnError)
	sourceFlag := fs.String("source", defaultSourceFile, "")
	domainFlag := fs.String("domain", defaultDomain, "")
	threadsFlag := fs.Int("threads", defaultThreads, "")
	timeoutFlag := fs.Int("timeout", defaultTimeout, "")
	scanIranFlag := fs.Bool("scan-iran", false, "")
	noREPLFlag := fs.Bool("no-repl", false, "")

	fs.Usage = func() {
		fmt.Println(boxTop())
		fmt.Println(boxTitle("dns_validate — usage"))
		fmt.Println(boxSep())
		fmt.Println(boxRow("-source file", "input file (default: source.txt)", colBold))
		fmt.Println(boxRow("-domain d", "tunnel domain  (default: example.com)", colBold))
		fmt.Println(boxRow("-threads n", fmt.Sprintf("goroutines    (default: %d)", defaultThreads), colBold))
		fmt.Println(boxRow("-timeout s", fmt.Sprintf("dig timeout   (default: %ds)", defaultTimeout), colBold))
		fmt.Println(boxRow("-scan-iran", "scan built-in Iranian ISP CIDRs", colBYel))
		fmt.Println(boxRow("-no-repl", "skip interactive mode after scan", colBold))
		fmt.Println(boxSep())
		fmt.Println(boxRow("source format", "IPs, CIDRs, ranges (a.b.c.d-e.f.g.h)", colDim))
		fmt.Println(boxRow("outputs", "valid-dns.txt  edns.txt  unique-dns.txt", colDim))
		fmt.Println(boxSep())
		fmt.Println(boxRow("examples", "", colDim))
		fmt.Printf("%s│%s  %s%s%s%s│%s\n",
			colBBlu, colRST, colDim,
			"  dns_validate -scan-iran -threads 150    ",
			strings.Repeat(" ", 14),
			colBBlu, colRST)
		fmt.Printf("%s│%s  %s%s%s%s│%s\n",
			colBBlu, colRST, colDim,
			"  dns_validate -source my.txt -domain x.ir",
			strings.Repeat(" ", 13),
			colBBlu, colRST)
		fmt.Printf("%s│%s  %s%s%s%s│%s\n",
			colBBlu, colRST, colDim,
			"  dns_validate -scan-iran -no-repl | wc -l",
			strings.Repeat(" ", 13),
			colBBlu, colRST)
		fmt.Println(boxBot())
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	printBanner(*sourceFlag, *domainFlag, *threadsFlag, *timeoutFlag, *scanIranFlag)

	// ── Stage 1 ────────────────────────────────────────────────────────────
	var ext extracted
	if *scanIranFlag {
		ext = extractFromIranCIDRs()
	} else {
		data, err := os.ReadFile(*sourceFlag)
		if err != nil {
			return fmt.Errorf("source file not found: %s", *sourceFlag)
		}
		ext = extractClean(string(data))
	}
	if len(ext.plains) == 0 && len(ext.cidrs) == 0 && len(ext.ranges) == 0 {
		return fmt.Errorf("no valid IPs, CIDRs, or ranges found")
	}

	// ── Stage 2a ───────────────────────────────────────────────────────────
	var dnsFromPlains []string
	if len(ext.plains) > 0 {
		dnsFromPlains = prefilter(ext.plains, *threadsFlag, "Stage 2a — Pre-Filter IPs")
	} else {
		logMsg("no plain IPs — skipping 2a", colDim)
	}

	// ── Stage 2b ───────────────────────────────────────────────────────────
	var dnsFromRanges []string
	if len(ext.cidrs) > 0 || len(ext.ranges) > 0 {
		printHeader("Stage 2b — Expand CIDRs / Ranges")
		excludeSet := make(map[string]struct{}, len(ext.plains))
		for _, ip := range ext.plains {
			excludeSet[ip] = struct{}{}
		}
		expanded := expandAll(ext.cidrs, ext.ranges, excludeSet)
		if len(expanded) > 0 {
			logMsg(fmt.Sprintf("expanded  %s%d%s IPs  (%d CIDRs  %d ranges)",
				colBold, len(expanded), colRST, len(ext.cidrs), len(ext.ranges)), "")
			dnsFromRanges = prefilter(expanded, *threadsFlag, "Stage 2b — Pre-Filter Expanded")
		} else {
			logMsg("no new IPs after deduplication", colDim)
		}
	} else {
		logMsg("no CIDRs or ranges — skipping 2b", colDim)
	}

	// ── Consolidate ────────────────────────────────────────────────────────
	allSet := make(map[string]struct{}, len(dnsFromPlains)+len(dnsFromRanges))
	for _, ip := range dnsFromPlains {
		allSet[ip] = struct{}{}
	}
	for _, ip := range dnsFromRanges {
		allSet[ip] = struct{}{}
	}
	allDNS := sortIPs(mapKeys(allSet))
	writeLines(uniqueDNSFile, allDNS)
	logMsg(fmt.Sprintf("saved %s%d%s%s DNS-active  →  %s",
		colBGrn, len(allDNS), colRST, colGrn, uniqueDNSFile), colGrn)

	if len(allDNS) == 0 {
		return fmt.Errorf("no DNS-responsive IPs found")
	}

	// ── Stage 3 ────────────────────────────────────────────────────────────
	results := runValidation(allDNS, *domainFlag, *threadsFlag, *timeoutFlag)

	var valid, ednsValid, poisoned, hijacked, slowOK []validResult
	for _, r := range results {
		if r.valid {
			valid = append(valid, r)
		}
		if slices.ContainsFunc(r.flags, func(f string) bool { return strings.HasPrefix(f, "poison") }) {
			poisoned = append(poisoned, r)
		}
		if slices.Contains(r.flags, "nxdomain-hijack") {
			hijacked = append(hijacked, r)
		}
	}

	slices.SortFunc(valid, func(a, b validResult) int {
		switch {
		case a.avgMS < b.avgMS:
			return -1
		case a.avgMS > b.avgMS:
			return 1
		default:
			return 0
		}
	})

	for _, r := range valid {
		if r.edns {
			ednsValid = append(ednsValid, r)
		}
		if slices.Contains(r.flags, "slow") {
			slowOK = append(slowOK, r)
		}
	}

	validIPs := make([]string, len(valid))
	for i, r := range valid {
		validIPs[i] = r.ip
	}
	ednsIPs := make([]string, len(ednsValid))
	for i, r := range ednsValid {
		ednsIPs[i] = r.ip
	}
	writeLines(validDNSFile, validIPs)
	writeLines(ednsFile, ednsIPs)

	// ── Summary ────────────────────────────────────────────────────────────
	printSummary(ext, dnsFromPlains, dnsFromRanges, allDNS,
		valid, ednsValid, poisoned, hijacked, slowOK)

	if len(valid) == 0 {
		logMsg("no valid resolvers found", colBRed)
		logMsg(fmt.Sprintf("test:  dig @<ip> %s.%s", randStr(6), *domainFlag), colDim)
		return nil
	}

	// ── Interactive REPL ───────────────────────────────────────────────────
	if !*noREPLFlag {
		interactiveREPL(valid, *domainFlag)
	}
	return nil
}
