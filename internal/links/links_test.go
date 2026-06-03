package links

import (
	"encoding/base64"
	"encoding/json"
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

// TestSingboxJSONAutoFailover verifies the bundle gains urltest + selector
// groups (so clients auto-route around endpoints blocked on a given network)
// only when more than one endpoint is present, and that the shadowtls "-stl"
// detour is not listed as a directly health-checked member.
func TestSingboxJSONAutoFailover(t *testing.T) {
	eps := []Endpoint{
		{Tag: "a", Proto: "vless-vision", Server: "1.2.3.4", Port: 443, SNI: "x", UUID: "u1"},
		{Tag: "b", Proto: "shadowtls", Server: "1.2.3.4", Port: 2443, SNI: "x", Password: "p", AuthPass: "s", Obfs: "aes-128-gcm"},
	}
	b, err := SingboxJSON(eps)
	if err != nil {
		t.Fatalf("SingboxJSON: %v", err)
	}
	var doc struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}

	groups := map[string][]any{}
	for _, o := range doc.Outbounds {
		if tp, _ := o["type"].(string); tp == "urltest" || tp == "selector" {
			groups[tp], _ = o["outbounds"].([]any)
		}
	}
	if groups["urltest"] == nil || groups["selector"] == nil {
		t.Fatalf("missing failover groups: %s", b)
	}
	for _, m := range groups["urltest"] {
		if m == "b-stl" {
			t.Errorf("shadowtls detour b-stl must not be a urltest member")
		}
	}
	if len(groups["urltest"]) != 2 { // a + b (the dialable tags)
		t.Errorf("urltest should cover 2 endpoints, got %d", len(groups["urltest"]))
	}

	// A single endpoint must not produce groups (nothing to fail over to).
	one, _ := SingboxJSON(eps[:1])
	if strings.Contains(string(one), "\"urltest\"") {
		t.Errorf("single endpoint should not get a urltest group: %s", one)
	}
}
