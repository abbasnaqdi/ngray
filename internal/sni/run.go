package sni

import (
	"context"
	"sync"
)

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
