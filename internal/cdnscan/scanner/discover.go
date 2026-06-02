package scanner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type DiscoverStats struct {
	Scanned  atomic.Int64
	Found    atomic.Int64
	TotalIPs int64
}

// Discover is a convenience wrapper that uses a default (unbound) dialer.
func Discover(ctx context.Context, cidrs []string, workers int, timeout time.Duration, masscanRate int) (<-chan string, *DiscoverStats, error) {
	return DiscoverWithDialer(ctx, cidrs, workers, timeout, masscanRate, nil)
}

// DiscoverWithDialer scans CIDRs for hosts with port 443 open using the provided dialer.
// If dialer is nil, a plain default dialer is used.
// Uses masscan when available, otherwise falls back to internal TCP scanning.
func DiscoverWithDialer(ctx context.Context, cidrs []string, workers int, timeout time.Duration, masscanRate int, dialer *net.Dialer) (<-chan string, *DiscoverStats, error) {
	if dialer == nil {
		dialer = &net.Dialer{Timeout: timeout}
	} else {
		dialer.Timeout = timeout
	}

	var stats DiscoverStats
	for _, cidr := range cidrs {
		n, err := CountHosts(cidr)
		if err != nil {
			return nil, nil, err
		}
		stats.TotalIPs += n
	}

	ch := make(chan string, 1024)

	go func() {
		defer close(ch)
		// Note: masscan bypasses the dialer (raw sockets), so if a specific
		// interface is needed, prefer the internal TCP scanner.
		if dialer.LocalAddr == nil {
			if _, err := exec.LookPath("masscan"); err == nil {
				if runMasscan(ctx, cidrs, masscanRate, ch, &stats) {
					return
				}
			}
		}
		runTCP(ctx, cidrs, workers, dialer, ch, &stats)
	}()

	return ch, &stats, nil
}

func runMasscan(ctx context.Context, cidrs []string, rate int, out chan<- string, stats *DiscoverStats) bool {
	args := []string{
		"-p443",
		"--rate", fmt.Sprintf("%d", rate),
		"--open",
		"--output-format", "json",
		"--output-filename", "-",
	}
	args = append(args, cidrs...)

	cmd := exec.CommandContext(ctx, "masscan", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false
	}
	if err := cmd.Start(); err != nil {
		return false
	}

	type result struct {
		IP    string `json:"ip"`
		Ports []struct {
			Status string `json:"status"`
		} `json:"ports"`
	}

	found := false
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var r result
		if json.Unmarshal([]byte(line), &r) != nil {
			continue
		}
		stats.Scanned.Add(1)
		for _, p := range r.Ports {
			if p.Status == "open" {
				found = true
				stats.Found.Add(1)
				select {
				case out <- r.IP:
				case <-ctx.Done():
					cmd.Wait() //nolint:errcheck
					return true
				}
				break
			}
		}
	}
	cmd.Wait() //nolint:errcheck
	return found
}

func runTCP(ctx context.Context, cidrs []string, workers int, dialer *net.Dialer, out chan<- string, stats *DiscoverStats) {
	ipCh := make(chan string, workers*4)

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
					conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip, "443"))
					stats.Scanned.Add(1)
					if err == nil {
						conn.Close()
						stats.Found.Add(1)
						select {
						case out <- ip:
						case <-ctx.Done():
							return
						}
					}
				}
			}
		}()
	}

	go func() {
		defer close(ipCh)
		for _, cidr := range cidrs {
			seq, err := IterHosts(cidr)
			if err != nil {
				continue
			}
			for ip := range seq {
				select {
				case <-ctx.Done():
					return
				case ipCh <- ip:
				}
			}
		}
	}()

	wg.Wait()
}
