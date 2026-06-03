package nginx

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	c := Config{
		Port: 443, Sink: 8080,
		Routes: []Route{{SNI: "pypi.org", Backend: 11003}, {SNI: "www.debian.org", Backend: 12001}},
		Sub:    &Subscription{FrontSNI: "www.kernel.org", ServerIP: "1.2.3.4", Token: "deadbeef", HTTPSPort: 8443, CertPath: "/c", KeyPath: "/k", BundleFile: "/b"},
	}
	out := c.Render()
	for _, want := range []string{
		"ssl_preread on;",
		"pypi.org",
		"127.0.0.1:11003;",
		`""                               127.0.0.1:8443;`,
		`1.2.3.4                          127.0.0.1:8443;`,
		"www.kernel.org",
		"127.0.0.1:8443;",
		"default                          127.0.0.1:8080;",
		"location = /s/deadbeef",
		"listen 443 reuseport;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered nginx.conf missing %q\n---\n%s", want, out)
		}
	}
}
