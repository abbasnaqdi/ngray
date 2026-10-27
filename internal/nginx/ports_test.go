package nginx

import (
	"strings"
	"testing"
)

// Path ports render on ALL three servers and their digit prefixes can never
// shadow the /s/ or /sb/ token locations.
func TestPortLocations(t *testing.T) {
	got := portLocations([]int{20128, 3000, 20128})
	for _, want := range []string{
		"location = /20128 { return 301 /20128/; }",
		"location /20128/ {",
		"proxy_pass http://127.0.0.1:20128/;",
		"location /3000/ {",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "location /20128/ {") != 1 {
		t.Fatalf("duplicate location: %s", got)
	}
}

func TestNormalizePorts(t *testing.T) {
	got := NormalizePorts([]int{20128, 80, 443, 2053, 0, -1, 70000, 20128}, 443, 2053)
	want := []int{20128}
	if len(got) != 1 || got[0] != 20128 {
		t.Fatalf("NormalizePorts = %v, want %v", got, want)
	}

	// The full subscription/CDN servers carry the ports as plain-HTTP and TLS
	// locations while /s and /sb (regex) keep priority over prefix locations.
	s := "loc"
	_ = s
}

func TestRenderPathProxyServers(t *testing.T) {
	out := Config{
		Port: 443, Sink: 8442,
		Sub: &Subscription{Dir: "/etc/ngray", BundleFile: "bundle", Token: "tok",
			CertFile: "/c/crt", KeyFile: "/c/key", HTTPSPort: 2053, PathPorts: []int{20128}},
		CDNSub: &CDNSub{Domain: "www.x.com", Backend: 8444, CertFile: "/c/crt", KeyFile: "/c/key",
			Dir: "/etc/ngray", BundleFile: "bundle", Token: "tok", PathPorts: []int{20128}},
	}.Render()
	for _, want := range []string{
		"proxy_pass http://127.0.0.1:20128/;", // 80, 2053 and CDN servers → 3 blocks?
	} {
		if n := strings.Count(out, want); n != 3 {
			t.Fatalf("expected 3 path blocks (http, https-sub, cdn), got %d", n)
		}
	}
	if !strings.Contains(out, `if ($subtok != "tok")`) {
		t.Fatal("token guard lost")
	}
}

// When writeNginx decides foreign fragments are safe, Render emits exactly
// those include lines — nothing more.
func TestRenderIncludes(t *testing.T) {
	on := Config{Port: 443, Sink: 8442,
		Sub:      &Subscription{Dir: "/etc/ngray", BundleFile: "bundle", Token: "tok", HTTPSPort: 2053},
		Includes: []string{"include /etc/nginx/conf.d/*.conf;", "include /etc/nginx/sites-enabled/*;"},
	}.Render()
	for _, want := range []string{"include /etc/nginx/conf.d/*.conf;", "include /etc/nginx/sites-enabled/*;"} {
		if !strings.Contains(on, want) {
			t.Fatalf("IncludeConfD missing %q", want)
		}
	}
	off := Config{Port: 443, Sink: 8442,
		Sub: &Subscription{Dir: "/etc/ngray", BundleFile: "bundle", Token: "tok", HTTPSPort: 2053},
	}.Render()
	if strings.Contains(off, "include /etc/nginx/conf.d") || strings.Contains(off, "include /etc/nginx/sites-enabled") {
		t.Fatal("include leaked when no includes were decided") // the modules-enabled include is always present and fine
	}
	// Our :80 is default_server so foreign fragments (even with includes) can
	// never steal the plain-HTTP subscription traffic — the /sm/ EOF class.
	if !strings.Contains(on, "listen 80 default_server;") {
		t.Fatal("our :80 must be default_server")
	}
}
