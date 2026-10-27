// Package sni tests candidate domains for REALITY fitness: a usable SNI must
// terminate TLS 1.3, negotiate h2, and support the X25519 group.
package sni

import (
	"context"
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"sort"
	"time"
)

type Status string

const (
	StatusOK       Status = "OK"
	StatusNoTLS13  Status = "NO_TLS13"
	StatusNoH2     Status = "NO_H2"
	StatusNoX25519 Status = "NO_X25519"
	StatusBlocked  Status = "BLOCKED"
	StatusDNSFail  Status = "DNS_FAIL"
	StatusErr      Status = "ERR"
)

type Result struct {
	Host     string
	IP       string
	Status   Status
	MedianMS int
	JitterMS int
	HTTPCode int // HTTP/2 reachability of https://host/ (0 = unknown)
}

// Score ranks REALITY-fit SNIs: lower is better (latency + jitter penalty).
// Non-fit results sort last.
func (r Result) Score() int {
	if !r.Fit() {
		return 1 << 30
	}
	return r.MedianMS + r.JitterMS/2
}

// Fit reports whether the SNI passed every REALITY requirement.
func (r Result) Fit() bool { return r.Status == StatusOK }

type Tester struct {
	Samples int
	Timeout time.Duration
}

func NewTester() *Tester { return &Tester{Samples: 3, Timeout: 6 * time.Second} }

// probe runs a single cheap fitness check: DNS + one strict TLS 1.3/X25519
// handshake + h2 ALPN. It sets Status and IP (and MedianMS to the one handshake
// time) but skips latency sampling and the HTTP HEAD. Used for the first pass of
// Sweep over thousands of candidates; survivors are re-measured fully by Test.
func (t *Tester) probe(ctx context.Context, host string) Result {
	res := Result{Host: host}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil || len(ips) == 0 {
		res.Status = StatusDNSFail
		return res
	}
	res.IP = ips[0].String()
	st, d, err := t.handshake(ctx, host, true)
	if err != nil {
		res.Status = t.classify(ctx, host, err)
		return res
	}
	if st.NegotiatedProtocol != "h2" {
		res.Status = StatusNoH2
		return res
	}
	res.Status = StatusOK
	res.MedianMS = int(d.Milliseconds())
	return res
}

func (t *Tester) Test(ctx context.Context, host string) Result {
	res := Result{Host: host}

	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil || len(ips) == 0 {
		res.Status = StatusDNSFail
		return res
	}
	res.IP = ips[0].String()

	st, _, err := t.handshake(ctx, host, true)
	if err != nil {
		res.Status = t.classify(ctx, host, err)
		return res
	}
	if st.NegotiatedProtocol != "h2" {
		res.Status = StatusNoH2
		return res
	}

	var ms []int
	n := max(t.Samples, 1)
	for i := 0; i < n; i++ {
		_, d, err := t.handshake(ctx, host, true)
		if err != nil {
			res.Status = t.classify(ctx, host, err)
			return res
		}
		ms = append(ms, int(d.Milliseconds()))
	}
	sort.Ints(ms)
	res.Status = StatusOK
	res.MedianMS = ms[len(ms)/2]
	res.JitterMS = ms[len(ms)-1] - ms[0]
	res.HTTPCode = t.httpCode(ctx, host)
	return res
}

// httpCode does a best-effort HTTP/2 GET-over-Range to confirm the SNI is a
// live site a REALITY dest should mimic (an authentic 200/3xx looks more
// reasonable). GET instead of HEAD: buggy servers that attach a body to a HEAD
// response make the http2 transport log "protocol error: received DATA on a
// HEAD request" with the global logger. A fresh non-keepalive transport means
// no pooled connection can leak late frames back into the process.
func (t *Tester) httpCode(ctx context.Context, host string) int {
	cctx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, "https://"+host+"/", nil)
	if err != nil {
		return 0
	}
	req.Header.Set("Range", "bytes=0-64")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")
	cl := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
			DisableKeepAlives: true,
		},
	}
	resp, err := cl.Do(req)
	if err != nil {
		return 0
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 256))
	resp.Body.Close()
	return resp.StatusCode
}

// silenceHTTPLog diverts the stdlib log sink (used by the bundled http2 stack
// for "protocol error" notices from misbehaving servers) so sweeps never
// pollute CLI/TUI output. The returned func restores the previous writer.
func silenceHTTPLog() func() {
	prev := log.Writer()
	log.SetOutput(io.Discard)
	return func() { log.SetOutput(prev) }
}

// handshake performs a single TLS handshake; x25519 pins the group to X25519.
func (t *Tester) handshake(ctx context.Context, host string, x25519 bool) (*tls.ConnectionState, time.Duration, error) {
	cfg := &tls.Config{
		ServerName:         host,
		NextProtos:         []string{"h2", "http/1.1"},
		MinVersion:         tls.VersionTLS13,
		MaxVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true, // we inspect the handshake, not trust the cert
	}
	if x25519 {
		cfg.CurvePreferences = []tls.CurveID{tls.X25519}
	}

	d := net.Dialer{Timeout: t.Timeout}
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, "443"))
	if err != nil {
		return nil, 0, &dialError{err}
	}
	defer conn.Close()

	tconn := tls.Client(conn, cfg)
	hsCtx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()
	if err := tconn.HandshakeContext(hsCtx); err != nil {
		return nil, 0, err
	}
	st := tconn.ConnectionState()
	return &st, time.Since(start), nil
}

// classify distinguishes a TCP block from a TLS-1.3/X25519 capability gap.
func (t *Tester) classify(ctx context.Context, host string, hsErr error) Status {
	if _, ok := hsErr.(*dialError); ok {
		return StatusBlocked
	}
	// TLS handshake failed under the strict (TLS1.3 + X25519) profile. Retry
	// permissively to learn which requirement the server actually lacks.
	cfg := &tls.Config{ServerName: host, NextProtos: []string{"h2", "http/1.1"}, InsecureSkipVerify: true}
	d := net.Dialer{Timeout: t.Timeout}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, "443"))
	if err != nil {
		return StatusBlocked
	}
	defer conn.Close()
	tconn := tls.Client(conn, cfg)
	hsCtx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()
	if err := tconn.HandshakeContext(hsCtx); err != nil {
		return StatusErr
	}
	if tconn.ConnectionState().Version < tls.VersionTLS13 {
		return StatusNoTLS13
	}
	return StatusNoX25519
}

type dialError struct{ err error }

func (e *dialError) Error() string { return e.err.Error() }
func (e *dialError) Unwrap() error { return e.err }
