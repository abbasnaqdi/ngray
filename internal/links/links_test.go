package links

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestTag(t *testing.T) {
	cases := []struct{ core, kind, sni, want string }{
		{"shoes", "vision", "www.rust-lang.org", "shoes-vision-rustlang"},
		{"shoes", "anytls", "www.debian.org", "shoes-anytls-debian"},
		{"xray", "xhttp", "pypi.org", "xray-xhttp-pypi"},
		{"hy2", "", "rust-lang.org", "hy2-rustlang"},
	}
	for _, c := range cases {
		if got := Tag(c.core, c.kind, c.sni); got != c.want {
			t.Errorf("Tag(%q,%q,%q)=%q want %q", c.core, c.kind, c.sni, got, c.want)
		}
	}
}

func TestVlessURIAndSubscription(t *testing.T) {
	e := Endpoint{
		Tag: "shoes-vision-rustlang", Proto: "vless-vision", Server: "1.2.3.4", Port: 443,
		SNI: "www.rust-lang.org", PublicKey: "PUB", ShortID: "ab12", UUID: "uid",
	}
	u := e.URI()
	for _, want := range []string{"vless://uid@1.2.3.4:443", "security=reality", "flow=xtls-rprx-vision", "#shoes-vision-rustlang"} {
		if !strings.Contains(u, want) {
			t.Errorf("uri %q missing %q", u, want)
		}
	}
	sub := Subscription([]Endpoint{e})
	raw, err := base64.StdEncoding.DecodeString(sub)
	if err != nil || !strings.Contains(string(raw), "vless://uid@") {
		t.Errorf("subscription decode failed: %v %q", err, raw)
	}
}
