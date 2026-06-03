package cores

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/ports"
	"github.com/abbasnaqdi/ng-ray/internal/state"
	"github.com/abbasnaqdi/ng-ray/internal/system"
)

// TestSingboxShadowTLSUsesUsers guards against the regression where the
// ShadowTLS v3 inbound emitted a singular "password" field. sing-box only
// accepts "password" for v2; with "version": 3 it rejects the entire config
// at startup, taking every sing-box inbound (anytls, vless, tuic) offline.
func TestSingboxShadowTLSUsesUsers(t *testing.T) {
	root := t.TempDir()
	sys := system.NewSandbox(root, nil)

	st, err := state.Load(filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}

	cfg := config.Default()
	// Isolate to ShadowTLS so the assertion targets exactly that inbound.
	cfg.Cores.Singbox = config.Singbox{
		ShadowTLS: &config.SingboxShadowTLS{Enabled: true, Port: 2443, SNI: "www.rust-lang.org"},
	}

	p := newPlan(cfg, st, sys)
	if _, err := (Singbox{}).Build(&p); err != nil {
		t.Fatalf("Singbox.Build: %v", err)
	}

	b, err := sys.ReadFile(singboxConf)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	var doc struct {
		Inbounds []map[string]any `json:"inbounds"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("parse generated config: %v", err)
	}

	var found bool
	for _, in := range doc.Inbounds {
		if in["type"] != "shadowtls" {
			continue
		}
		found = true
		if _, hasPassword := in["password"]; hasPassword {
			t.Error("shadowtls inbound has a top-level \"password\"; v3 requires \"users\"")
		}
		users, ok := in["users"].([]any)
		if !ok || len(users) == 0 {
			t.Errorf("shadowtls inbound missing non-empty \"users\" array, got %#v", in["users"])
		}
	}
	if !found {
		t.Fatal("no shadowtls inbound generated")
	}
}

func newPlan(cfg *config.Config, st *state.State, sys *system.Env) Plan {
	return Plan{Cfg: cfg, St: st, Alloc: ports.NewAllocator(st), Sys: sys, IP: "203.0.113.10"}
}
