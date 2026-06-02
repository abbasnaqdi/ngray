package ports

import (
	"testing"

	"github.com/abbasnaqdi/ng-ray/internal/state"
)

func TestBackendAllocatesAndReuses(t *testing.T) {
	st := &state.State{Ports: map[string]int{}}
	a := NewAllocator(st)

	p1, err := a.Backend("shoes", "shoes:vless:rust")
	if err != nil {
		t.Fatal(err)
	}
	if p1 < 12000 || p1 > 12999 {
		t.Fatalf("port %d outside shoes range", p1)
	}

	p2, _ := a.Backend("shoes", "shoes:anytls:debian")
	if p2 == p1 {
		t.Fatalf("distinct ids must get distinct ports, both %d", p1)
	}

	// Same id reuses the persisted assignment.
	if again, _ := NewAllocator(st).Backend("shoes", "shoes:vless:rust"); again != p1 {
		t.Fatalf("reuse: got %d, want %d", again, p1)
	}

	if _, err := a.Backend("bogus", "x"); err == nil {
		t.Fatal("unknown core should error")
	}
}
