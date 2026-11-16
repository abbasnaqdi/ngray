package links

import (
	"encoding/base64"
	"encoding/hex"
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

// TestPinsAreHex guards the client compatibility bug: CertFingerprint is
// base64, but TUIC and Xray-compatible Hysteria2 clients require pinSHA256 to
// be 64 lowercase hex characters.
func TestPinsAreHex(t *testing.T) {
	raw := []byte("0123456789abcdef0123456789abcdef") // 32 bytes
	fp := base64.StdEncoding.EncodeToString(raw)
	want := hex.EncodeToString(raw)

	tuic := Endpoint{
		Tag: "sb-tuic", Proto: "tuic", Server: "1.2.3.4", Port: 443,
		SNI: "www.example.com", UUID: "uid", TUICPass: "pw", Fingerprint: fp,
	}
	if u := tuic.URI(); !strings.Contains(u, "pinSHA256="+want) || strings.Contains(u, "allow_insecure") {
		t.Errorf("tuic pin not hex: %q", u)
	}

	hy := Endpoint{
		Tag: "hy2", Proto: "hysteria2", Server: "1.2.3.4", Port: 443,
		SNI: "www.example.com", AuthPass: "pw", Fingerprint: fp,
	}
	if u := hy.URI(); !strings.Contains(u, "pinSHA256="+want) || strings.Contains(u, "insecure=1") {
		t.Errorf("hysteria2 pin not hex: %q", u)
	}

	// Already-hex and colon-separated openssl fingerprints must also work.
	bare := Endpoint{Tag: "bare", Proto: "tuic", Server: "1.2.3.4", Port: 443, SNI: "x", UUID: "u", TUICPass: "p", Fingerprint: strings.ToUpper(want)}
	if u := bare.URI(); !strings.Contains(u, "pinSHA256="+want) {
		t.Errorf("bare hex pin not normalised: %q", u)
	}
}

// ShadowTLS has no standard share URI. Emitting ss:// with plugin=shadow-tls
// makes NekoBox/v2rayN reject the whole subscription, so only the sing-box
// JSON chain may carry it.
func TestShadowTLSIsJSONOnly(t *testing.T) {
	e := Endpoint{
		Tag: "sb-shadowtls", Proto: "shadowtls", Server: "1.2.3.4", Port: 2443,
		SNI: "www.example.com", Password: "outer", AuthPass: "inner", Obfs: "aes-128-gcm",
	}
	if u := e.URI(); u != "" {
		t.Fatalf("shadowtls URI must be empty, got %q", u)
	}
	b, err := SingboxJSON([]Endpoint{e})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"type": "shadowtls"`, `"type": "shadowsocks"`, `"detour": "sb-shadowtls-stl"`} {
		if !strings.Contains(s, want) {
			t.Errorf("sing-box bundle missing %q: %s", want, s)
		}
	}
}

func TestAnyTLSWarmPool(t *testing.T) {
	e := Endpoint{
		Tag: "anytls", Proto: "anytls", Server: "1.2.3.4", Port: 443,
		SNI: "www.example.com", Password: "pw",
	}
	b, err := SingboxJSON([]Endpoint{e})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"min_idle_session": 4`) {
		t.Errorf("anytls outbound missing warm session pool: %s", b)
	}
}

// TestCloudflareFrontedEndpoints verifies the CF-fronted WS/XHTTP endpoints
// render real-TLS URIs (no reality, no insecure) pointed at the domain. WS is
// a sing-box transport; XHTTP is Xray-only and must not leak into the JSON.
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
	for _, want := range []string{`"type": "ws"`, "example.com"} {
		if !strings.Contains(s, want) {
			t.Errorf("singbox json missing %q", want)
		}
	}
	if strings.Contains(s, `"type": "xhttp"`) {
		t.Errorf("xhttp must not appear in sing-box JSON: %s", s)
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
	rawEP := Endpoint{Tag: "wg", Proto: "wireguard", Raw: map[string]any{
		"type": "wireguard", "server": "1.2.3.4", "server_port": 51820, "jc": 5,
	}}
	if mieru.URI() != "" {
		t.Errorf("Raw endpoint must have no URI, got %q", mieru.URI())
	}
	b, err := SingboxJSON([]Endpoint{mieru, rawEP})
	if err != nil {
		t.Fatalf("SingboxJSON: %v", err)
	}
	s := string(b)
	for _, want := range []string{`"type": "mieru"`, `"tag": "mieru-user"`, `"type": "wireguard"`, `"tag": "wg"`, `"username": "user"`} {
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

// The mixed text subscription must be ONE plain-text body: raw links (no
// base64) followed by the complete sing-box JSON — for text-subscription
// clients like nekoray.
func TestMixed(t *testing.T) {
	eps := []Endpoint{
		{Tag: "cf-ws-test", Proto: "vless-ws-tls", Server: "www.example.org", Port: 443, SNI: "www.example.org", UUID: "u1", Path: "/t/a/b"},
		{Tag: "xray-reality-rustlang", Proto: "vless-vision", Server: "1.2.3.4", Port: 443, SNI: "www.rust-lang.org", PublicKey: "k", ShortID: "s", UUID: "u2"},
		{Tag: "xray-xhttp-cf", Proto: "vless-xhttp", Server: "1.2.3.4", Port: 443, SNI: "www.cloudflare.com", PublicKey: "k", ShortID: "s", UUID: "u3", Path: "/x"},
	}
	out, err := Mixed(eps)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "ZXhhbXBsZQ") { // looks base64
		t.Fatal("mixed body must not be base64-wrapped")
	}
	if !strings.Contains(out, "vless://u2@1.2.3.4") || !strings.Contains(out, "vless://u1@www.example.org") {
		t.Fatalf("links lost:\n%s", out[:min(len(out), 300)])
	}
	// Sing-box has no xhttp transport: the mixed body is for sing-box clients,
	// so the XHTTP link and JSON outbound must both be absent.
	if strings.Contains(out, "u3@") || strings.Contains(out, `"type": "xhttp"`) {
		t.Fatalf("xhttp leaked into mixed sing-box body:\n%s", out)
	}
	// The JSON tail must parse as sing-box outbounds (mieru included verbatim).
	i := strings.Index(out, "\n\n")
	var cfg struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal([]byte(out[i+2:]), &cfg); err != nil {
		t.Fatalf("json tail: %v", err)
	}
	if len(cfg.Outbounds) < 2 {
		t.Fatalf("want ≥2 outbounds incl. auto group, got %d", len(cfg.Outbounds))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
