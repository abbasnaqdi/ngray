package verifier

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abbasnaqdi/ng-ray/internal/cdnscan/cdn"
)

type Result struct {
	IP       string
	CDN      string
	Clean    bool
	Latency  time.Duration
	Status   string
}

type Stats struct {
	Verified atomic.Int64
	Clean    atomic.Int64
	Total    int64
}

// Verify runs multi-stage CDN fronting verification on each IP for the given provider.
// The dialer controls which network interface is used (nil = system default).
func Verify(ctx context.Context, ips []string, provider *cdn.Provider, workers int, timeout time.Duration, dialer *net.Dialer) (<-chan Result, *Stats) {
	if dialer == nil {
		dialer = &net.Dialer{Timeout: timeout}
	} else {
		// Ensure timeout is set
		dialer.Timeout = timeout
	}

	stats := &Stats{Total: int64(len(ips))}
	ch := make(chan Result, workers)

	ipCh := make(chan string, workers)
	go func() {
		defer close(ipCh)
		for _, ip := range ips {
			select {
			case <-ctx.Done():
				return
			case ipCh <- ip:
			}
		}
	}()

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case ip, ok := <-ipCh:
					if !ok {
						return
					}
					res := check(ctx, ip, provider, timeout, dialer)
					stats.Verified.Add(1)
					if res.Clean {
						stats.Clean.Add(1)
					}
					select {
					case ch <- res:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	return ch, stats
}

func check(ctx context.Context, ip string, provider *cdn.Provider, timeout time.Duration, dialer *net.Dialer) Result {
	res := Result{IP: ip, CDN: provider.Name}

	tlsCfg := &tls.Config{
		ServerName:         provider.FrontDomain,
		InsecureSkipVerify: true, //nolint:gosec — intentional for CDN edge probing
		NextProtos:         []string{"h2", "http/1.1"},
	}

	// Stage 1: TLS handshake to confirm TLS works on this IP.
	start := time.Now()
	raw, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip, "443"))
	if err != nil {
		res.Status = "TCP failed"
		return res
	}
	tlsConn := tls.Client(raw, tlsCfg)
	tlsConn.SetDeadline(time.Now().Add(timeout)) //nolint:errcheck
	if err := tlsConn.Handshake(); err != nil {
		raw.Close()
		res.Status = "TLS failed"
		return res
	}
	tlsConn.Close()
	res.Latency = time.Since(start)

	// Stage 2: HTTP HEAD with Host spoofing.
	// All TCP connections are forced to ip:443 regardless of URL hostname.
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip, "443"))
		},
		TLSClientConfig:   tlsCfg,
		DisableKeepAlives: true,
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequestWithContext(ctx, "HEAD", "https://"+provider.FrontDomain+"/", nil)
	if err != nil {
		res.Status = "request error"
		return res
	}

	resp, err := client.Do(req)
	if err != nil {
		res.Status = "HTTP failed"
		return res
	}
	defer resp.Body.Close()

	if provider.IsEdge(resp) {
		res.Clean = true
		res.Status = "clean"
		return res
	}

	res.Status = fmt.Sprintf("not %s (%d)", provider.Name, resp.StatusCode)
	return res
}
