package config

import "testing"

func TestDefaultValidatesAndHasUniqueSNIs(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
}

func TestDuplicateSNIRejected(t *testing.T) {
	c := Default()
	c.Cores.Singbox.Reality[0].SNI = "www.debian.org" // already used by shoes anytls
	if err := c.Validate(); err == nil {
		t.Fatal("expected duplicate-SNI error")
	}
}

func TestSubscriptionCollisionRejected(t *testing.T) {
	c := Default()
	c.Subscription.FrontSNI = "rust-lang.org" // a core route SNI
	if err := c.Validate(); err == nil {
		t.Fatal("expected subscription collision error")
	}
}
