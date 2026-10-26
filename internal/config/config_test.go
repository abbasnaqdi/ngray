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
	c.Subscription.FrontSNI = "www.rust-lang.org" // a core route SNI
	if err := c.Validate(); err == nil {
		t.Fatal("expected subscription collision error")
	}
}

func TestProxyRouteValid(t *testing.T) {
	c := Default()
	c.ProxyRoutes = []ProxyRoute{{Domain: "app.example.com", Target: "http://127.0.0.1:9000",
		CertFile: "/etc/ngray/app.crt", KeyFile: "/etc/ngray/app.key"}}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid proxy route rejected: %v", err)
	}
}

func TestProxyRouteRejectsBadDomain(t *testing.T) {
	for _, dom := range []string{"bad domain", "a/b", "x;y", "../etc"} {
		c := Default()
		c.ProxyRoutes = []ProxyRoute{{Domain: dom, Target: "http://127.0.0.1:9000"}}
		if err := c.Validate(); err == nil {
			t.Errorf("expected domain %q to be rejected", dom)
		}
	}
}

func TestProxyRouteRejectsBadTarget(t *testing.T) {
	for _, tgt := range []string{"ftp://x", "http://x;}", "http://a b", ""} {
		c := Default()
		c.ProxyRoutes = []ProxyRoute{{Domain: "app.example.com", Target: tgt}}
		if err := c.Validate(); err == nil {
			t.Errorf("expected target %q to be rejected", tgt)
		}
	}
}

func TestProxyRouteCertKeyPairing(t *testing.T) {
	c := Default()
	c.ProxyRoutes = []ProxyRoute{{Domain: "app.example.com", Target: "http://127.0.0.1:9000",
		CertFile: "/etc/ngray/app.crt"}}
	if err := c.Validate(); err == nil {
		t.Fatal("expected cert-without-key to be rejected")
	}
}

func TestProxyRouteDuplicateRejected(t *testing.T) {
	c := Default()
	c.ProxyRoutes = []ProxyRoute{
		{Domain: "app.example.com", Target: "http://127.0.0.1:9000"},
		{Domain: "app.example.com", Target: "http://127.0.0.1:9001"},
	}
	if err := c.Validate(); err == nil {
		t.Fatal("expected duplicate proxy domain to be rejected")
	}
}
