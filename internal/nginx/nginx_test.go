package nginx

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	c := Config{
		Port:   443,
		Sink:   8080,
		Routes: []Route{{SNI: "pypi.org", Backend: 11003}, {SNI: "www.debian.org", Backend: 12001}},
		Sub:    &Subscription{BundleFile: "/etc/ngray/bundle"},
	}
	out := c.Render()
	for _, want := range []string{
		"ssl_preread on;",
		"pypi.org",
		"127.0.0.1:11003;",
		"default                          127.0.0.1:8080;",
		"listen 443 reuseport;",
		"listen 80;",
		"location ~ ^/s/",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered nginx.conf missing %q\n---\n%s", want, out)
		}
	}
}
