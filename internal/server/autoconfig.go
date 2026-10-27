package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/sni"
)

// autoPopulateSNIs tests candidates and assigns the best REALITY-fit SNIs to
// every core and independent-TLS endpoint in cfg, then saves config.yml.
// Called only when no config.yml existed before setup (fresh install).
func autoPopulateSNIs(ctx context.Context, candidatesPath, configPath string, cfg *config.Config, log func(string, ...any)) (ran bool) {
	populateSNIs(ctx, candidatesPath, configPath, cfg, log, false)
	return true
}

// RegenerateSNIs re-runs SNI fitness testing and re-assigns all SNI fields in
// cfg unconditionally (used by the TUI "Regenerate SNIs" action and
// `ngray sni regen`).
func RegenerateSNIs(ctx context.Context, candidatesPath, configPath string, cfg *config.Config, log func(string, ...any)) error {
	return populateSNIs(ctx, candidatesPath, configPath, cfg, log, true)
}

// maxPerCore is the DEFAULT REALITY budget per core. The active budget comes
// from config server.sni_max_per_core (see sniBudget), so operators can raise
// the endpoint volume without a rebuild.
const maxPerCore = 160

// maxXHTTP is the default XHTTP cap; the active cap scales with the budget.
func xHTTPCap(budget int) int {
	if n := budget / 3; n > 8 {
		return n
	}
	return 8
}

// sniBudget returns the operator's REALITY budget per core (config
// server.sni_max_per_core, default 40). The XHTTP cap scales one third of it.
func sniLimits(cfg *config.Config) (int, int) {
	b := cfg.Server.SNIMaxPerCore
	if b <= 0 {
		b = maxPerCore
	}
	return b, xHTTPCap(b)
}

// trimLegacySNIs caps config lists at the active budget so installs created
// before the volume policy converge to the same readable size on every setup.
// The FIRST entries are kept — legacy lists were ranked at creation time.
// Returns how many entries were dropped.
func trimLegacySNIs(cfg *config.Config) int {
	budget, xcap := sniLimits(cfg)
	dropped := 0
	if n := len(cfg.Cores.Xray.Reality) - budget; n > 0 {
		dropped += n
		cfg.Cores.Xray.Reality = cfg.Cores.Xray.Reality[:budget]
	}
	if n := len(cfg.Cores.Shoes.Reality) - budget; n > 0 {
		dropped += n
		cfg.Cores.Shoes.Reality = cfg.Cores.Shoes.Reality[:budget]
	}
	if n := len(cfg.Cores.Singbox.Reality) - budget; n > 0 {
		dropped += n
		cfg.Cores.Singbox.Reality = cfg.Cores.Singbox.Reality[:budget]
	}
	if n := len(cfg.Cores.Xray.XHTTP) - xcap; n > 0 {
		dropped += n
		cfg.Cores.Xray.XHTTP = cfg.Cores.Xray.XHTTP[:xcap]
	}
	return dropped
}

// scanIranFiltered lists every configured core SNI that borrows an
// organisation known to be filtered from Iranian networks. Server-side tests
// pass for them, so only this policy saves the user an instant RST.
func scanIranFiltered(cfg *config.Config) []string {
	seen := map[string]bool{}
	var out []string
	for _, h := range cfg.RouteSNIs() {
		if sni.IranFiltered(h) && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

// swapFilteredSNIs replaces each Iran-filtered SNI with the next unused local
// candidate (preferred tier first) — no sweep, instant. Returns the count.
func swapFilteredSNIs(ctx context.Context, cfg *config.Config, candidatesPath string) (int, error) {
	pref, ext, err := sni.LoadTiered(candidatesPath)
	if err != nil && len(pref) == 0 && len(ext) == 0 {
		return 0, fmt.Errorf("load SNI candidates: %w", err)
	}
	banned := map[string]bool{}
	if cdn := cfg.Cores.Xray.CDN; cdn != nil {
		for _, d := range cfg.CDNDomains() {
			banned[strings.ToLower(d)] = true
		}
	}
	for _, h := range cfg.RouteSNIs() {
		banned[strings.ToLower(h)] = true
	}
	pool := make([]string, 0, len(pref)+len(ext))
	pool = append(pool, pref...)
	pool = append(pool, ext...)
	nextFree := func() string {
		for _, h := range pool {
			lh := strings.ToLower(h)
			if banned[lh] || sni.IranFiltered(lh) {
				continue
			}
			banned[lh] = true
			return h
		}
		return ""
	}
	swapped := 0
	for i := range cfg.Cores.Xray.Reality {
		if sni.IranFiltered(cfg.Cores.Xray.Reality[i].SNI) {
			if repl := nextFree(); repl != "" {
				cfg.Cores.Xray.Reality[i].SNI = repl
				swapped++
			}
		}
	}
	for i := range cfg.Cores.Shoes.Reality {
		if sni.IranFiltered(cfg.Cores.Shoes.Reality[i].SNI) {
			if repl := nextFree(); repl != "" {
				cfg.Cores.Shoes.Reality[i].SNI = repl
				swapped++
			}
		}
	}
	for i := range cfg.Cores.Singbox.Reality {
		if sni.IranFiltered(cfg.Cores.Singbox.Reality[i].SNI) {
			if repl := nextFree(); repl != "" {
				cfg.Cores.Singbox.Reality[i].SNI = repl
				swapped++
			}
		}
	}
	for i := range cfg.Cores.Xray.XHTTP {
		if sni.IranFiltered(cfg.Cores.Xray.XHTTP[i].SNI) {
			if repl := nextFree(); repl != "" {
				cfg.Cores.Xray.XHTTP[i].SNI = repl
				swapped++
			}
		}
	}
	return swapped, nil
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
	ranked = mergeRanked(ranked, cfg, log)
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

	// Volume policy: diversity matters, hundreds of routes do not. Keep a
	// bounded pool per core — enough distinct REALITY destinations to rotate
	// against blocks, few enough that nginx/firewall/clients stay readable.
	// Each SNI is unique (sniBudget never repeats a host) and latency-ranked.
	budget, xcap := sniLimits(cfg)
	total := len(ranked)
	xhttpN := min(min(total/8, xcap), xcap)
	rest := max(total-xhttpN-1, 0) // reserve one for the shared best SNI below
	xrayN := min(rest*35/100, budget)
	shoesN := min(rest*33/100, budget)
	singboxN := min(rest-xrayN-shoesN, budget)
	if assigned := xhttpN + xrayN + shoesN + singboxN + 2; assigned < total {
		log("auto-SNI: %d hosts fit — keeping the best %d (cap %d per core; set to rotate every few weeks)", total, assigned, budget)
	}

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
		// front_sni MUST be a dedicated SNI that is not one of the nginx route
		// SNIs — nginx maps each route SNI to exactly one backend, so a collision
		// makes config.yml fail validation on reload (setup uses the in-memory
		// cfg, so it deploys, but every later reload errors). Always pull a fresh
		// budget SNI (never a route SNI); fall back to best only if exhausted.
		if fs := pop.next(); fs != "" {
			cfg.Subscription.FrontSNI = fs
		} else {
			cfg.Subscription.FrontSNI = best
		}
	}
	_ = force

	// Catch a generated config that would fail on reload (setup runs off the
	// in-memory cfg, so an invalid file would otherwise go unnoticed until a
	// later command reloads it).
	if err := cfg.Validate(); err != nil {
		log("auto-SNI: WARNING generated config is invalid: %v", err)
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

// mergeRanked adds the operator's OWNED, PROVEN SNIs (already in config.yml,
// passed fitness in this sweep) to the front of the fresh ranking. An auto
// re-fit therefore MERGES proven data with new discoveries — the previous
// working set is never silently replaced, only Iran-filtered hosts drop out
// (handled by the swap pass before this), and the newest discoveries join.
// Duplicate and unfit hosts are dropped; the merged list stays ranked.
func mergeRanked(ranked []sni.Result, cfg *config.Config, log func(string, ...any)) []sni.Result {
	byHost := map[string]sni.Result{}
	for _, r := range ranked {
		byHost[strings.ToLower(r.Host)] = r
	}
	var pinned []sni.Result
	pinnedSet := map[string]bool{}
	missing := 0
	for _, h := range cfg.RouteSNIs() {
		h = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(h), "."))
		if h == "" || pinnedSet[h] {
			continue
		}
		r, ok := byHost[h]
		if !ok {
			missing++ // SNI not in the pool file at all (custom entry): keep the ranking untouched for it
			continue
		}
		if r.Fit() && r.MedianMS <= 2000 && !sni.IranFiltered(h) {
			pinned = append(pinned, r)
			pinnedSet[h] = true
		}
	}
	if len(pinned) == 0 {
		// Still strip new-pool entries that borrow Iran-filtered brands.
		out := make([]sni.Result, 0, len(ranked))
		for _, r := range ranked {
			if !sni.IranFiltered(r.Host) {
				out = append(out, r)
			}
		}
		return out
	}
	out := make([]sni.Result, 0, len(ranked)+len(pinned))
	out = append(out, pinned...)
	for _, r := range ranked {
		lh := strings.ToLower(r.Host)
		if pinnedSet[lh] || sni.IranFiltered(lh) {
			continue
		}
		out = append(out, r)
	}
	log("auto-SNI: %d proven existing SNI(s) pinned first (%d config SNIs were not in the pool)", len(pinned), missing)
	return out
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
