package server

import (
	"context"
	"time"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/sni"
)

// autoPopulateSNIs tests candidates and assigns the best REALITY-fit SNIs to
// every core in cfg, then saves config.yml so the user has a permanent record.
// Called only when no config.yml existed before setup.
func autoPopulateSNIs(ctx context.Context, candidatesPath, configPath string, cfg *config.Config, log func(string, ...any)) error {
	hosts, err := sni.LoadCandidates(candidatesPath)
	if err != nil || len(hosts) == 0 {
		log("auto-SNI: no candidates found at %s — using built-in defaults", candidatesPath)
		return nil
	}
	log("auto-SNI: testing %d candidates to find best REALITY-fit SNIs for this VPS…", len(hosts))

	tctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	var passed, failed int
	results := sni.NewTester().TestAll(tctx, hosts, 32, func(r sni.Result) {
		if r.Fit() {
			passed++
			log("  ✓ %s (%dms)", r.Host, r.MedianMS)
		} else {
			failed++
		}
	})

	ranked := sni.Ranked(results)
	if len(ranked) == 0 {
		log("auto-SNI: no SNIs passed fitness check — keeping defaults")
		return nil
	}
	log("auto-SNI: %d passed, %d failed — assigning best to cores", passed, failed)

	// Assign in order, never repeating a SNI.
	assign := func(n int, proto string) []config.Reality {
		var out []config.Reality
		for len(ranked) > 0 && len(out) < n {
			out = append(out, config.Reality{SNI: ranked[0].Host, Proto: proto})
			ranked = ranked[1:]
		}
		return out
	}
	assignXHTTP := func(n int) []config.XHTTP {
		var out []config.XHTTP
		for len(ranked) > 0 && len(out) < n {
			out = append(out, config.XHTTP{SNI: ranked[0].Host})
			ranked = ranked[1:]
		}
		return out
	}

	// xray: 4 REALITY + 3 XHTTP
	cfg.Cores.Xray.Reality = assign(4, "")
	cfg.Cores.Xray.XHTTP = assignXHTTP(3)

	// shoes: alternating anytls / vless_vision
	var shoesReality []config.Reality
	for i := 0; len(ranked) > 0 && i < 5; i++ {
		proto := "anytls"
		if i%3 == 2 {
			proto = "vless_vision"
		}
		shoesReality = append(shoesReality, config.Reality{SNI: ranked[0].Host, Proto: proto})
		ranked = ranked[1:]
	}
	cfg.Cores.Shoes.Reality = shoesReality

	// singbox: alternating anytls / vless_vision
	var sbReality []config.Reality
	for i := 0; len(ranked) > 0 && i < 5; i++ {
		proto := "anytls"
		if i%3 == 2 {
			proto = "vless_vision"
		}
		sbReality = append(sbReality, config.Reality{SNI: ranked[0].Host, Proto: proto})
		ranked = ranked[1:]
	}
	cfg.Cores.Singbox.Reality = sbReality

	// hysteria2 + TUIC + ShadowTLS: reuse first fit SNI (independent cores)
	if len(ranked) > 0 {
		best := ranked[0].Host
		cfg.Cores.Hysteria2.SNI = best
		if cfg.Cores.Singbox.TUIC != nil {
			cfg.Cores.Singbox.TUIC.SNI = best
		}
		if cfg.Cores.Singbox.ShadowTLS != nil {
			cfg.Cores.Singbox.ShadowTLS.SNI = best
		}
	}

	if err := config.Save(configPath, cfg); err != nil {
		log("auto-SNI: could not save config.yml: %v", err)
		return nil // non-fatal; continue with the in-memory config
	}
	log("auto-SNI: config.yml written with %d SNIs",
		len(cfg.Cores.Xray.Reality)+len(cfg.Cores.Xray.XHTTP)+
			len(cfg.Cores.Shoes.Reality)+len(cfg.Cores.Singbox.Reality))
	return nil
}
