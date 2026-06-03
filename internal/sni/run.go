package sni

import (
	"context"
	"sort"
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
	sort.Slice(fit, func(i, j int) bool { return fit[i].Score() < fit[j].Score() })
	return fit
}

// TestAll tests hosts concurrently, invoking onResult (if set) as each finishes.
func (t *Tester) TestAll(ctx context.Context, hosts []string, workers int, onResult func(Result)) []Result {
	if workers <= 0 {
		workers = 16
	}
	in := make(chan string)
	results := make([]Result, len(hosts))
	idx := map[string]int{}
	for i, h := range hosts {
		idx[h] = i
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for h := range in {
				r := t.Test(ctx, h)
				mu.Lock()
				results[idx[h]] = r
				if onResult != nil {
					onResult(r)
				}
				mu.Unlock()
			}
		}()
	}
	for _, h := range hosts {
		select {
		case <-ctx.Done():
			close(in)
			wg.Wait()
			return results
		case in <- h:
		}
	}
	close(in)
	wg.Wait()
	return results
}
