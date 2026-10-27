package server

import (
	"github.com/abbasnaqdi/ng-ray/internal/sni"
	"os"
	"testing"

	"github.com/abbasnaqdi/ng-ray/internal/config"
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
	if b != 40 {
		t.Fatalf("default budget = %d, want 40", b)
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
