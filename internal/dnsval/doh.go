package dnsval

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

type dohProvider struct{ name, url string }

var dohProviders = []dohProvider{
	{"Cloudflare", "https://1.1.1.1/dns-query"},
	{"Google", "https://8.8.8.8/dns-query"},
	{"Quad9", "https://9.9.9.9/dns-query"},
	{"Mullvad", "https://194.242.2.2/dns-query"},
	{"AdGuard", "https://dns.adguard-dns.com/dns-query"},
	{"DNS.SB", "https://doh.dns.sb/dns-query"},
	{"NextDNS", "https://dns.nextdns.io"},
	{"ControlD", "https://dns.controld.com/p0"},
	{"LibreDNS", "https://doh.libredns.gr/dns-query"},
	{"Switch-CH", "https://dns.switch.ch/dns-query"},
	{"FDN-FR", "https://doh.fdn.fr/dns-query"},
	{"OpenDNS", "https://doh.opendns.com/dns-query"},
}

type dohStat struct {
	name    string
	avgMS   float64
	okCount int
}

// DoH benchmarks public DoH resolvers over phases and prints a latency ranking.
// Use it to pick a reachable, fast DoH endpoint for the client side.
func DoH(phases int) error {
	if phases <= 0 {
		phases = 10
	}
	client := &http.Client{Timeout: 3 * time.Second}
	sum := make(map[string]float64)
	cnt := make(map[string]int)
	var mu sync.Mutex

	fmt.Printf("DoH benchmark — %d phases × %d providers\n\n", phases, len(dohProviders))
	for p := 1; p <= phases; p++ {
		var wg sync.WaitGroup
		for _, prov := range dohProviders {
			wg.Add(1)
			go func(pr dohProvider) {
				defer wg.Done()
				if ms, ok := probeDoH(client, pr.url); ok {
					mu.Lock()
					sum[pr.name] += ms
					cnt[pr.name]++
					mu.Unlock()
				}
			}(prov)
		}
		wg.Wait()
		fmt.Printf("\rphase %d/%d", p, phases)
	}
	fmt.Print("\n\n")

	var stats []dohStat
	for _, pr := range dohProviders {
		s := dohStat{name: pr.name, okCount: cnt[pr.name]}
		if s.okCount > 0 {
			s.avgMS = sum[pr.name] / float64(s.okCount)
		}
		stats = append(stats, s)
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].okCount == 0 {
			return false
		}
		if stats[j].okCount == 0 {
			return true
		}
		return stats[i].avgMS < stats[j].avgMS
	})

	fmt.Printf("%-12s %-10s %s\n", "PROVIDER", "AVG", "SUCCESS")
	for _, s := range stats {
		if s.okCount == 0 {
			fmt.Printf("%-12s %-10s %d/%d\n", s.name, "FAILED", 0, phases)
			continue
		}
		fmt.Printf("%-12s %-10s %d/%d\n", s.name, fmt.Sprintf("%.0fms", s.avgMS), s.okCount, phases)
	}
	return nil
}

func probeDoH(client *http.Client, url string) (float64, bool) {
	sub := make([]byte, 3)
	rand.Read(sub)
	q := url + "?name=" + hex.EncodeToString(sub) + ".google.com"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, q, nil)
	if err != nil {
		return 0, false
	}
	req.Header.Set("Accept", "application/dns-json")
	t0 := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, false
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, false
	}
	return float64(time.Since(t0).Milliseconds()), true
}
