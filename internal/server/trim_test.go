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
	for i := 0; i < 34; i++ {
		cfg.Cores.Xray.Reality = append(cfg.Cores.Xray.Reality, config.Reality{SNI: "a.rr.com"})
	}
	cfg.Cores.Xray.XHTTP = make([]config.XHTTP, 12)
	cfg.Cores.Shoes.Reality = make([]config.Reality, 30)
	dropped := trimLegacySNIs(cfg)
	if dropped != 21 { // default already carried one xray entry => 35 total
		t.Fatalf("dropped = %d", dropped)
	}
	if len(cfg.Cores.Xray.Reality) != maxPerCore || len(cfg.Cores.Shoes.Reality) != maxPerCore ||
		len(cfg.Cores.Xray.XHTTP) != maxXHTTP {
		t.Fatalf("caps not applied: xray=%d shoes=%d xhttp=%d",
			len(cfg.Cores.Xray.Reality), len(cfg.Cores.Shoes.Reality), len(cfg.Cores.Xray.XHTTP))
	}
	if dropped2 := trimLegacySNIs(cfg); dropped2 != 0 {
		t.Fatalf("second trim removed %d more", dropped2)
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
