package firewall

import "testing"

func TestTrustedInterfaces(t *testing.T) {
	in := []string{"lo", "eth0", "tailscale0", "docker0", "br-1a2b3c4d5e6f", "wg0", "veth123", "virbr0"}
	got := trustedInterfaces(in)
	want := []string{"tailscale0", "docker0", "br-1a2b3c4d5e6f", "wg0", "virbr0"}
	if len(got) != len(want) {
		t.Fatalf("trusted = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("trusted = %v, want %v", got, want)
		}
	}
}
