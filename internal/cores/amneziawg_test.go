package cores

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/state"
	"github.com/abbasnaqdi/ng-ray/internal/system"
)

// parseAWGConf extracts the obfuscation key=value pairs from an [Interface]
// section so server and client confs can be compared field by field.
func parseAWGConf(conf string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(conf, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), " = ")
		if ok && !strings.Contains(k, "Key") && k != "Endpoint" {
			out[k] = v
		}
	}
	return out
}

// TestAWGV2Conf validates the opt-in v2 parameter set: S3/S4 present, magic
// headers are disjoint ranges, and server and client confs agree field by
// field (v2 went wrong historically exactly when those drifted apart).
func TestAWGV2Conf(t *testing.T) {
	for range 20 { // randomized params — exercise several draws
		root := t.TempDir()
		sys := system.NewSandbox(root, nil)
		st, err := state.Load(filepath.Join(root, "state.json"))
		if err != nil {
			t.Fatalf("state.Load: %v", err)
		}
		cfg := config.Default()
		cfg.Cores.AmneziaWG = config.AmneziaWGCore{Enabled: true, Port: 51820, V2: true}

		p := newPlan(cfg, st, sys)
		res, err := (AmneziaWG{}).Build(&p)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		server, err := sys.ReadFile(awgDir + "/wg0.conf")
		if err != nil {
			t.Fatalf("read wg0.conf: %v", err)
		}
		srv := parseAWGConf(string(server))
		cli := parseAWGConf(res.Notes["amneziawg-client-config"])

		for _, k := range []string{"Jc", "Jmin", "Jmax", "S1", "S2", "S3", "S4", "H1", "H2", "H3", "H4"} {
			if srv[k] != cli[k] {
				t.Errorf("v2 mismatch on %s: server %q ≠ client %q", k, srv[k], cli[k])
			}
		}
		if srv["S3"] == "" || srv["S4"] == "" {
			t.Errorf("v2 conf missing S3/S4: %q %q", srv["S3"], srv["S4"])
		}

		hn := map[string]struct{ lo, hi uint32 }{"H1": {}, "H2": {}, "H3": {}, "H4": {}}
		for k := range hn {
			var lo, hi uint32
			if _, err := fmt.Sscanf(srv[k], "%d-%d", &lo, &hi); err != nil {
				t.Errorf("header %s = %q is not a lo-hi range", k, srv[k])
				continue
			}
			if lo < 5 || hi < lo {
				t.Errorf("header %s range %d-%d violates lo ≥ 5 / hi ≥ lo", k, lo, hi)
			}
			hn[k] = struct{ lo, hi uint32 }{lo, hi}
		}
		names := []string{"H1", "H2", "H3", "H4"}
		for i := 0; i < len(names); i++ {
			for j := i + 1; j < len(names); j++ {
				if hn[names[i]].hi >= hn[names[j]].lo && hn[names[j]].hi >= hn[names[i]].lo {
					t.Errorf("header ranges %q [%d-%d] and %q [%d-%d] overlap",
						names[i], hn[names[i]].lo, hn[names[i]].hi,
						names[j], hn[names[j]].lo, hn[names[j]].hi)
				}
			}
		}
		// 1.x params stay untouched in state.
		if sec := st.Secrets["awg|obfs"].Password; sec != "" {
			t.Errorf("v2 build leaked into the 1.x secret: %q", sec)
		}
	}
}

// TestAWGV1Conf guards the default path: 1.x tools reject unknown keys, so S3
// and S4 must be entirely absent and headers single-valued.
func TestAWGV1Conf(t *testing.T) {
	root := t.TempDir()
	sys := system.NewSandbox(root, nil)
	st, err := state.Load(filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	cfg := config.Default()
	cfg.Cores.AmneziaWG = config.AmneziaWGCore{Enabled: true, Port: 51820}

	p := newPlan(cfg, st, sys)
	res, err := (AmneziaWG{}).Build(&p)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, name := range []string{awgDir + "/wg0.conf"} {
		conf, err := sys.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		s := string(conf)
		if strings.Contains(s, "S3 =") || strings.Contains(s, "S4 =") {
			t.Errorf("v1 conf contains S3/S4; amneziawg-tools 1.x rejects unknown keys:\n%s", s)
		}
		// Plenty of "-" chars exist (iptables flags, addr prefixes) — check
		// header forms only.
		for _, line := range strings.Split(s, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "H") && strings.Contains(line, " = ") {
				if strings.Contains(strings.TrimSpace(line), "-") {
					t.Errorf("v1 header is a range: %s", line)
				}
			}
		}
	}
	// Params must be stable across rebuilds (persisted in state).
	p2 := newPlan(cfg, st, sys)
	if _, err := (AmneziaWG{}).Build(&p2); err != nil {
		t.Fatalf("second Build: %v", err)
	}
	first := parseAWGConf(res.Notes["amneziawg-client-config"])
	confB, _ := sys.ReadFile(awgDir + "/wg0.conf")
	second := parseAWGConf(string(confB))
	for _, k := range []string{"Jc", "Jmin", "Jmax", "S1", "S2", "H1", "H2", "H3", "H4"} {
		if first[k] != second[k] {
			t.Errorf("rebuild changed %s: %q → %q", k, first[k], second[k])
		}
	}
}
