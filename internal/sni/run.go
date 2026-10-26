package sni

import (
	"context"
	"slices"
	"sync"
)

// Ranked returns the REALITY-fit results best-first (lowest latency + jitter).
func Ranked(results []Result) []Result {
	fit := make([]Result, 0, len(results))
	for _, r := range results {
		if r.Fit() {
			fit = append(fit, r)
		}
	}
	slices.SortFunc(fit, func(a, b Result) int { return a.Score() - b.Score() })
	return fit
}

// RankedTiered ranks fit results so every host in preferred comes before any
// host that is not, with latency ordering applied within each tier. This lets
// auto-SNI exhaust the proven (operator-whitelist-friendly) tier before falling
// back to the extended pool, even when an extended host has lower latency.
func RankedTiered(results []Result, preferred map[string]bool) []Result {
	fit := Ranked(results) // latency-sorted
	var pref, ext []Result
	for _, r := range fit {
		if preferred[r.Host] {
			pref = append(pref, r)
		} else {
			ext = append(ext, r)
		}
	}
	return append(pref, ext...)
}

// mapHosts runs fn over hosts on a pool of workers, invoking onResult (if set)
// as each finishes, and returns results in the original host order.
func (t *Tester) mapHosts(ctx context.Context, hosts []string, workers int, fn func(context.Context, string) Result, onResult func(Result)) []Result {
	if workers <= 0 {
		workers = 256
	}
	type job struct {
		host string
		idx  int
	}
	in := make(chan job)
	results := make([]Result, len(hosts))

	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range in {
				r := fn(ctx, j.host)
				mu.Lock()
				results[j.idx] = r
				mu.Unlock()
				// Callbacks run outside the lock so a slow sink cannot
				// serialize all workers.
				if onResult != nil {
					onResult(r)
				}
			}
		}()
	}
	for i, h := range hosts {
		select {
		case <-ctx.Done():
			close(in)
			wg.Wait()
			return results
		case in <- job{host: h, idx: i}:
		}
	}
	close(in)
	wg.Wait()
	return results
}

// TestAll tests hosts concurrently with the full per-host measurement
// (fitness + latency samples + HTTP), invoking onResult as each finishes.
func (t *Tester) TestAll(ctx context.Context, hosts []string, workers int, onResult func(Result)) []Result {
	return t.mapHosts(ctx, hosts, workers, t.Test, onResult)
}

// Sweep scales fitness testing to thousands of candidates. It runs a single
// cheap probe over every host at high concurrency, then re-measures only the
// hosts that passed (latency samples + HTTP HEAD). Non-fit hosts cost one
// handshake; the expensive sampling is spent only on survivors, so a 1000+
// candidate list finishes in well under a minute.
//
// onResult fires once per host: immediately for hosts that fail the probe, and
// after full measurement for hosts that pass.
func (t *Tester) Sweep(ctx context.Context, hosts []string, workers int, onResult func(Result)) []Result {
	probed := t.mapHosts(ctx, hosts, workers, t.probe, func(r Result) {
		if !r.Fit() && onResult != nil {
			onResult(r)
		}
	})

	var fit []string
	for _, r := range probed {
		if r.Fit() {
			fit = append(fit, r.Host)
		}
	}
	measured := t.mapHosts(ctx, fit, workers, t.Test, onResult)

	byHost := make(map[string]Result, len(hosts))
	for _, r := range probed {
		byHost[r.Host] = r
	}
	for _, r := range measured {
		byHost[r.Host] = r
	}
	out := make([]Result, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, byHost[h])
	}
	return out
}
