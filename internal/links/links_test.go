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

// TestCloudflareFrontedEndpoints verifies the CF-fronted WS/XHTTP endpoints
// render real-TLS URIs (no reality, no insecure) pointed at the domain, and
// appear as ws/xhttp sing-box outbounds.
func TestCloudflareFrontedEndpoints(t *testing.T) {
	ws := Endpoint{Tag: "cf-ws-example", Proto: "vless-ws-tls", Server: "example.com", Port: 443, SNI: "example.com", UUID: "uid", Path: "/abc123"}
	u := ws.URI()
	for _, want := range []string{"vless://uid@example.com:443", "security=tls", "type=ws", "host=example.com", "path=%2Fabc123"} {
		if !strings.Contains(u, want) {
			t.Errorf("ws uri %q missing %q", u, want)
		}
	}
	if strings.Contains(u, "reality") || strings.Contains(u, "insecure") {
		t.Errorf("cf ws uri must not use reality/insecure: %q", u)
	}

	xh := Endpoint{Tag: "cf-xhttp-www", Proto: "vless-xhttp-tls", Server: "www.example.com", Port: 443, SNI: "www.example.com", UUID: "uid2", Path: "/xh1"}
	if xu := xh.URI(); !strings.Contains(xu, "type=xhttp") || !strings.Contains(xu, "mode=auto") {
		t.Errorf("xhttp uri wrong: %q", xu)
	}

	b, err := SingboxJSON([]Endpoint{ws, xh})
	if err != nil {
		t.Fatalf("SingboxJSON: %v", err)
	}
	s := string(b)
	for _, want := range []string{`"type": "ws"`, `"type": "xhttp"`, "example.com"} {
		if !strings.Contains(s, want) {
			t.Errorf("singbox json missing %q", want)
		}
	}
	if strings.Contains(s, `"reality"`) {
		t.Errorf("cf outbounds must not contain reality: %s", s)
	}
}

// TestRawOutbounds verifies mieru/amneziawg (Raw) endpoints are emitted verbatim
// into the sing-box bundle with their tag set, and excluded from the base64 list.
func TestRawOutbounds(t *testing.T) {
	mieru := Endpoint{Tag: "mieru-user", Proto: "mieru", Raw: map[string]any{
		"type": "mieru", "server": "1.2.3.4", "server_port": 14000, "username": "user", "password": "pw",
	}}
	awg := Endpoint{Tag: "amneziawg", Proto: "amneziawg", Raw: map[string]any{
		"type": "wireguard", "server": "1.2.3.4", "server_port": 51820, "jc": 5,
	}}
	if mieru.URI() != "" {
		t.Errorf("Raw endpoint must have no URI, got %q", mieru.URI())
	}
	b, err := SingboxJSON([]Endpoint{mieru, awg})
	if err != nil {
		t.Fatalf("SingboxJSON: %v", err)
	}
	s := string(b)
	for _, want := range []string{`"type": "mieru"`, `"tag": "mieru-user"`, `"type": "wireguard"`, `"tag": "amneziawg"`, `"username": "user"`} {
		if !strings.Contains(s, want) {
			t.Errorf("singbox bundle missing %q", want)
		}
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
		{Tag: "c", Proto: "hysteria2", Server: "1.2.3.4", Port: 443, SNI: "x", AuthPass: "h", Obfs: "salamander", ObfsPass: "o"},
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
			tag, _ := o["tag"].(string)
			groups[tag], _ = o["outbounds"].([]any)
		}
	}
	if groups["auto"] == nil || groups["select"] == nil {
		t.Fatalf("missing failover groups: %s", b)
	}
	for _, m := range groups["auto"] {
		if m == "b-stl" {
			t.Errorf("shadowtls detour b-stl must not be a urltest member")
		}
	}
	if len(groups["auto"]) != 3 { // a + b + c (the dialable tags)
		t.Errorf("auto should cover 3 endpoints, got %d", len(groups["auto"]))
	}
	// a (vision) and b (shadowtls) are TCP REALITY/ShadowTLS → blackout-safe;
	// c (hysteria2, UDP) must be excluded from the blackout group.
	if len(groups["blackout"]) != 2 {
		t.Errorf("blackout group should cover 2 TCP-safe endpoints, got %d", len(groups["blackout"]))
	}
	for _, m := range groups["blackout"] {
		if m == "c" {
			t.Errorf("hysteria2 (UDP) must not be in the blackout group")
		}
	}

	// A single endpoint must not produce groups (nothing to fail over to).
	one, _ := SingboxJSON(eps[:1])
	if strings.Contains(string(one), "\"urltest\"") {
		t.Errorf("single endpoint should not get a urltest group: %s", one)
	}
}
