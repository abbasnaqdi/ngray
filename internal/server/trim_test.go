package server

import (
	"github.com/abbasnaqdi/ng-ray/internal/sni"
	"os"
	"testing"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/links"
)

// Pre-cap installs carried hundreds of REALITY routes; every setup must
// converge to the same readable budget.
func TestTrimLegacySNIs(t *testing.T) {
	cfg := config.Default()
	budget, xcap := sniLimits(cfg)
	for i := 0; i < budget+34; i++ {
		cfg.Cores.Xray.Reality = append(cfg.Cores.Xray.Reality, config.Reality{SNI: "a.rr.com"})
	}
	cfg.Cores.Xray.XHTTP = make([]config.XHTTP, xcap+12)
	cfg.Cores.Shoes.Reality = make([]config.Reality, 30)
	want := (len(cfg.Cores.Xray.Reality) - budget) + (len(cfg.Cores.Xray.XHTTP) - xcap)
	if n := 30 - budget; n > 0 {
		want += n
	}
	dropped := trimLegacySNIs(cfg)
	if dropped != want {
		t.Fatalf("dropped = %d, want %d", dropped, want)
	}
	if len(cfg.Cores.Xray.Reality) != budget || len(cfg.Cores.Xray.XHTTP) != xcap {
		t.Fatalf("caps not applied: xray=%d xhttp=%d (budget=%d xcap=%d)",
			len(cfg.Cores.Xray.Reality), len(cfg.Cores.Xray.XHTTP), budget, xcap)
	}
	if len(cfg.Cores.Shoes.Reality) > budget {
		t.Fatalf("shoes = %d > %d", len(cfg.Cores.Shoes.Reality), budget)
	}
	if dropped2 := trimLegacySNIs(cfg); dropped2 != 0 {
		t.Fatalf("second trim removed %d more", dropped2)
	}
}

// The operator can raise the volume without a rebuild via the config knob.
func TestSniBudgetKnob(t *testing.T) {
	cfg := config.Default()
	cfg.Server.SNIMaxPerCore = 120
	b, x := sniLimits(cfg)
	if b != 120 || x != 40 {
		t.Fatalf("knob ignored: %d/%d", b, x)
	}
	cfg.Server.SNIMaxPerCore = 0
	b, x = sniLimits(cfg)
	if b != 96 {
		t.Fatalf("default budget = %d, want 96", b)
	}
}

func TestSwapFilteredSNIs(t *testing.T) {
	dir := t.TempDir()
	pool := "clean.sites.org\nalso-clean.net\n"
	for _, extra := range []string{"third.ok.org"} {
		pool += extra + "\n"
	}
	path := dir + "/candidates.sni"
	if err := osWrite(path, []byte(pool)); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Cores.Xray.Reality = []config.Reality{{SNI: "www.github.com"}, {SNI: "fine.dz.com"}}
	n, err := swapFilteredSNIs(nil, cfg, path)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("swapped %d, want 1", n)
	}
	if sni.IranFiltered(cfg.Cores.Xray.Reality[0].SNI) {
		t.Fatalf("replacement still filtered: %s", cfg.Cores.Xray.Reality[0].SNI)
	}
	if cfg.Cores.Xray.Reality[1].SNI != "fine.dz.com" {
		t.Fatalf("untouched entry changed: %s", cfg.Cores.Xray.Reality[1].SNI)
	}
}

func osWrite(path string, data []byte) error { return os.WriteFile(path, data, 0o600) }

// An auto re-fit MERGES the operator's proven SNIs with fresh discoveries:
// already-configured hosts that pass fitness pin to the front, duplicates die.
func TestMergeRankedPinsProven(t *testing.T) {
	cfg := config.Default()
	cfg.Cores.Xray.Reality = []config.Reality{
		{SNI: "proven-one.org"}, {SNI: "proven-two.org"}, {SNI: "www.github.com"},
	}
	cfg.Cores.Shoes.Reality = nil
	cfg.Cores.Singbox.Reality = nil
	cfg.Cores.Xray.XHTTP = nil
	ranked := []sni.Result{
		{Host: "fresh-a.org", Status: sni.StatusOK, MedianMS: 30},
		{Host: "proven-two.org", Status: sni.StatusOK, MedianMS: 45},
		{Host: "proven-one.org", Status: sni.StatusOK, MedianMS: 40},
		{Host: "www.github.com", Status: sni.StatusOK, MedianMS: 25}, // fit but IRAN-filtered
	}
	var logLines int
	out := mergeRanked(ranked, cfg, func(string, ...any) { logLines++ })
	if got := out[0].Host + " " + out[1].Host; got != "proven-one.org proven-two.org" {
		t.Fatalf("pinned order wrong: %s", got)
	}
	for i, r := range out {
		if r.Host == "www.github.com" {
			t.Fatalf("Iran-filtered rank survived at %d", i)
		}
	}
	if out[len(out)-1].Host != "fresh-a.org" {
		t.Fatalf("fresh discoveries dropped: %v", out)
	}
}

// Direct-domain clones mirror every non-WS endpoint with a domain address.
func TestDomainClones(t *testing.T) {
	eps := []links.Endpoint{
		{Tag: "shoes-anytls-x", Proto: "anytls", Server: "1.2.3.4", Port: 443, SNI: "camo.org", Password: "pw"},
		{Tag: "cf-ws-y", Proto: "vless-ws-tls", Server: "www.d.org", Port: 443, SNI: "www.d.org",
			UUID: "u", Path: "/t/a/b"},
		{Tag: "m", Proto: "mieru", Raw: map[string]any{"server": "1.2.3.4"}},
		{Tag: "hy2-x", Proto: "hysteria2", Server: "1.2.3.4", Port: 443},
	}
	out := domainClones(eps, "direct.d.org")
	if len(out) != 2 {
		t.Fatalf("clones = %d, want 2 (anytls + hy2)", len(out))
	}
	if out[0].Server != "direct.d.org" || out[0].Tag != "shoes-anytls-x-domain" || out[0].Password != "pw" {
		t.Fatalf("clone integrity: %+v", out[0])
	}
	if out[1].Tag != "hy2-x-domain" {
		t.Fatalf("hy2 clone: %+v", out[1])
	}
}
