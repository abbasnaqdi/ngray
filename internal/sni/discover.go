package sni

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// Found is a REALITY SNI candidate discovered near the server.
type Found struct {
	Domain    string
	IP        string
	LatencyMS int
	TLS13     bool
	H2        bool
}

// Discover scans the /24 of refIP plus blocksEachSide neighbor /24s on :443,
// does a no-SNI TLS handshake, extracts cert SAN/CN domains, and ranks them by
// latency. SNIs hosted next to the server make the most convincing REALITY
// destinations. Idea adapted from Reality-SNI-Finder, implemented natively.
func Discover(ctx context.Context, refIP string, blocksEachSide, workers int, onProgress func(done, total int)) []Found {
	if workers <= 0 {
		workers = 256
	}
	base, ok := ipToU32(refIP)
	if !ok {
		return nil
	}
	block := base &^ 0xFF // /24 network of refIP

	var targets []uint32
	for off := -blocksEachSide; off <= blocksEachSide; off++ {
		net24 := uint32(int64(block) + int64(off)*256)
		for h := 1; h < 255; h++ {
			targets = append(targets, net24+uint32(h))
		}
	}

	in := make(chan uint32)
	out := make(chan []Found)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ipn := range in {
				ip := u32ToIP(ipn)
				if f := probeCert(ctx, ip, base); len(f) > 0 {
					out <- f
				}
			}
		}()
	}
	go func() {
		var done int
		total := len(targets)
		for _, t := range targets {
			select {
			case <-ctx.Done():
				close(in)
				return
			case in <- t:
			}
			done++
			if onProgress != nil && done%256 == 0 {
				onProgress(done, total)
			}
		}
		close(in)
	}()
	go func() { wg.Wait(); close(out) }()

	best := map[string]Found{}
	for fs := range out {
		for _, f := range fs {
			if cur, ok := best[f.Domain]; !ok || f.LatencyMS < cur.LatencyMS {
				best[f.Domain] = f
			}
		}
	}

	var list []Found
	for _, f := range best {
		if f.TLS13 && f.H2 {
			list = append(list, f)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].LatencyMS < list[j].LatencyMS })
	return list
}

// probeCert opens a no-SNI TLS handshake and returns the cert's usable domains.
func probeCert(ctx context.Context, ip string, _ uint32) []Found {
	d := net.Dialer{Timeout: 1500 * time.Millisecond}
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, "443"))
	if err != nil {
		return nil
	}
	defer conn.Close()
	tconn := tls.Client(conn, &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"h2", "http/1.1"},
		MinVersion:         tls.VersionTLS12,
	})
	hctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := tconn.HandshakeContext(hctx); err != nil {
		return nil
	}
	st := tconn.ConnectionState()
	lat := int(time.Since(start).Milliseconds())
	tls13 := st.Version == tls.VersionTLS13
	h2 := st.NegotiatedProtocol == "h2"
	if len(st.PeerCertificates) == 0 {
		return nil
	}
	cert := st.PeerCertificates[0]
	domains := map[string]bool{}
	for _, n := range append(cert.DNSNames, cert.Subject.CommonName) {
		for _, d := range expandDomain(n) {
			domains[d] = true
		}
	}
	var out []Found
	for dom := range domains {
		out = append(out, Found{Domain: dom, IP: ip, LatencyMS: lat, TLS13: tls13, H2: h2})
	}
	return out
}

func expandDomain(d string) []string {
	d = strings.TrimSpace(strings.ToLower(d))
	if d == "" || !strings.Contains(d, ".") || strings.HasSuffix(d, ".local") {
		return nil
	}
	if strings.HasPrefix(d, "*.") {
		root := d[2:]
		return []string{root, "www." + root}
	}
	return []string{d}
}

func ipToU32(s string) (uint32, bool) {
	ip := net.ParseIP(s).To4()
	if ip == nil {
		return 0, false
	}
	return binary.BigEndian.Uint32(ip), true
}

func u32ToIP(n uint32) string {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], n)
	return net.IP(b[:]).String()
}
