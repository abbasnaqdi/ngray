package server

import (
	"context"
	"sort"
	"time"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/sni"
)

// autoPopulateSNIs tests candidates and assigns the best REALITY-fit SNIs to
// every core and independent-TLS endpoint in cfg, then saves config.yml.
// Called only when no config.yml existed before setup (fresh install).
func autoPopulateSNIs(ctx context.Context, candidatesPath, configPath string, cfg *config.Config, log func(string, ...any)) error {
	return populateSNIs(ctx, candidatesPath, configPath, cfg, log, false)
}

// RegenerateSNIs re-runs SNI fitness testing and re-assigns all SNI fields in
// cfg unconditionally (used by the TUI "Regenerate SNIs" action and
// `ngray sni regen`).
func RegenerateSNIs(ctx context.Context, candidatesPath, configPath string, cfg *config.Config, log func(string, ...any)) error {
	return populateSNIs(ctx, candidatesPath, configPath, cfg, log, true)
}

func populateSNIs(ctx context.Context, candidatesPath, configPath string, cfg *config.Config, log func(string, ...any), force bool) error {
	prefHosts, extHosts, err := sni.LoadTiered(candidatesPath)
	hosts := append(append([]string{}, prefHosts...), extHosts...)
	if err != nil || len(hosts) == 0 {
		log("auto-SNI: no candidates found at %s — using built-in defaults", candidatesPath)
		return nil
	}
	preferred := make(map[string]bool, len(prefHosts))
	for _, h := range prefHosts {
		preferred[h] = true
	}
	log("auto-SNI: probing %d candidates (%d preferred + %d extended) for REALITY fitness…",
		len(hosts), len(prefHosts), len(extHosts))

	// Sweep is a fast probe-then-measure pass at high concurrency, so the budget
	// scales with the list: ~180s comfortably covers 1000+ candidates even on a
	// distant VPS with many high-latency or dead hosts.
	tctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()

	var passed, failed int
	results := sni.NewTester().Sweep(tctx, hosts, 256, func(r sni.Result) {
		if r.Fit() {
			passed++
			log("  ✓ %-36s %dms", r.Host, r.MedianMS)
		} else {
			failed++
		}
	})

	// Tiered: exhaust preferred (whitelist-friendly) hosts before extended ones,
	// so a lower-latency-but-unwhitelisted domain never displaces a proven one.
	ranked := sni.RankedTiered(results, preferred)
	if len(ranked) == 0 {
		log("auto-SNI: no SNIs passed fitness check — keeping current config")
		return nil
	}
	log("auto-SNI: %d passed, %d failed — assigning best SNIs (preferred tier first)", passed, failed)

	// Quality-first ordering: keep the full pool (volume is the ask) but lead
	// with the strongest destinations. Egregiously slow hosts (>2s) are dropped
	// as dead weight; the rest stay as anti-blocking fallback. Within each tier
	// we prefer SNIs serving a live page (HTTP 200/3xx — more convincing REALITY
	// camouflage), then lowest latency.
	live := func(code int) bool { return code == 200 || (code >= 300 && code < 400) }
	var trimmed []sni.Result
	for _, r := range ranked {
		if r.MedianMS <= 2000 {
			trimmed = append(trimmed, r)
		}
	}
	if len(trimmed) > 0 {
		ranked = trimmed
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if pi, pj := preferred[ranked[i].Host], preferred[ranked[j].Host]; pi != pj {
			return pi // proven (whitelist-friendly) tier stays first
		}
		if li, lj := live(ranked[i].HTTPCode), live(ranked[j].HTTPCode); li != lj {
			return li // a real live site is better camouflage
		}
		return ranked[i].Score() < ranked[j].Score()
	})

	pop := &sniBudget{ranked: ranked}

	// Maximize config volume: consume essentially the entire fit pool, split
	// across all three nginx-fronted REALITY cores plus a CDN-leaning XHTTP
	// slice, reserving one SNI for the shared independent-TLS endpoints. Counts
	// scale with supply, so a thin pool still yields a sane spread while a rich
	// pool yields hundreds of distinct REALITY configs. Each SNI is unique
	// (sniBudget never repeats a host) and latency-ranked, so the leading
	// configs are the fastest and the tail is bonus anti-blocking fallback.
	total := len(ranked)
	xhttpN := min(total/8, 24)
	rest := max(total-xhttpN-1, 0) // reserve one for the shared best SNI below
	xrayN := rest * 35 / 100
	shoesN := rest * 33 / 100
	singboxN := rest - xrayN - shoesN

	// xray: bulk vless+vision REALITY + a CDN-leaning XHTTP slice
	cfg.Cores.Xray.XHTTP = pop.xhttp(xhttpN)
	cfg.Cores.Xray.Reality = pop.reality(xrayN, "")

	// shoes: mix anytls (primary) and vless_vision (backup)
	cfg.Cores.Shoes.Reality = pop.mixedReality(shoesN, "anytls", "vless_vision", 3)

	// singbox: mix anytls (primary) and vless_vision (backup)
	cfg.Cores.Singbox.Reality = pop.mixedReality(singboxN, "anytls", "vless_vision", 3)

	// Independent-TLS cores and the subscription all share the same best SNI
	// for their self-signed cert (or masquerade target). Reuse first remaining.
	best := pop.next()
	if best == "" && len(ranked) > 0 {
		best = ranked[0].Host // fallback: reuse top result
	}
	if best != "" {
		cfg.Cores.Hysteria2.SNI = best
		if cfg.Cores.Shoes.TUIC != nil {
			cfg.Cores.Shoes.TUIC.SNI = best
		}
		if cfg.Cores.Shoes.Hysteria2 != nil {
			cfg.Cores.Shoes.Hysteria2.SNI = best
		}
		if cfg.Cores.Singbox.TUIC != nil {
			cfg.Cores.Singbox.TUIC.SNI = best
		}
		if cfg.Cores.Singbox.ShadowTLS != nil {
			cfg.Cores.Singbox.ShadowTLS.SNI = best
		}
		if cfg.Subscription.FrontSNI == "" || force {
			cfg.Subscription.FrontSNI = best
		}
	}

	if err := config.Save(configPath, cfg); err != nil {
		log("auto-SNI: could not save config.yml: %v", err)
		return nil
	}
	log("auto-SNI: config.yml written with %d SNIs across all cores",
		len(cfg.Cores.Xray.Reality)+len(cfg.Cores.Xray.XHTTP)+
			len(cfg.Cores.Shoes.Reality)+len(cfg.Cores.Singbox.Reality))
	return nil
}

// sniBudget hands out ranked SNIs without repeating any host.
type sniBudget struct {
	ranked []sni.Result
	used   map[string]bool
}

func (b *sniBudget) next() string {
	if b.used == nil {
		b.used = map[string]bool{}
	}
	for len(b.ranked) > 0 {
		h := b.ranked[0].Host
		b.ranked = b.ranked[1:]
		if !b.used[h] {
			b.used[h] = true
			return h
		}
	}
	return ""
}

func (b *sniBudget) reality(n int, proto string) []config.Reality {
	var out []config.Reality
	for i := 0; i < n; i++ {
		h := b.next()
		if h == "" {
			break
		}
		out = append(out, config.Reality{SNI: h, Proto: proto})
	}
	return out
}

func (b *sniBudget) xhttp(n int) []config.XHTTP {
	var out []config.XHTTP
	for i := 0; i < n; i++ {
		h := b.next()
		if h == "" {
			break
		}
		out = append(out, config.XHTTP{SNI: h})
	}
	return out
}

// mixedReality assigns n SNIs cycling through protos, with the secondary proto
// appearing every stride-th position.
func (b *sniBudget) mixedReality(n int, primary, secondary string, stride int) []config.Reality {
	var out []config.Reality
	for i := 0; i < n; i++ {
		h := b.next()
		if h == "" {
			break
		}
		proto := primary
		if stride > 0 && (i+1)%stride == 0 {
			proto = secondary
		}
		out = append(out, config.Reality{SNI: h, Proto: proto})
	}
	return out
}
