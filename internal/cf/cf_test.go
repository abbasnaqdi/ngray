package cf

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTokenFromEnvOrFile(t *testing.T) {
	t.Setenv("NGRAY_CF_TOKEN", "env-token")
	if got := TokenFromEnvOrFile(); got != "env-token" {
		t.Fatalf("env token = %q", got)
	}
	t.Setenv("NGRAY_CF_TOKEN", "")
	t.Setenv("CF_API_TOKEN", "")
	// point the file lookup at a guaranteed-missing path
	t.Setenv("NGRAY_CF_TOKEN_FILE", filepath.Join(t.TempDir(), "missing"))
	if got := TokenFromEnvOrFile(); got != "" {
		t.Fatalf("expected empty token, got %q", got)
	}
	// and at a real file
	p := filepath.Join(t.TempDir(), "cf_token")
	if err := os.WriteFile(p, []byte("file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NGRAY_CF_TOKEN_FILE", p)
	if got := TokenFromEnvOrFile(); got != "file-token" {
		t.Fatalf("file token = %q", got)
	}
}

func newMock(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := New("test-token")
	c.Base = srv.URL
	return c
}

func okBody(result any) []byte {
	b, _ := json.Marshal(map[string]any{"success": true, "result": result})
	return b
}

func TestEnsureARecordCreates(t *testing.T) {
	var created bool
	c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/zones/") && r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/dns_records"):
			w.Write(okBody([]any{}))
		case strings.HasPrefix(r.URL.Path, "/zones/") && r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/dns_records"):
			created = true
			w.Write(okBody(map[string]any{"id": "rec1"}))
		case strings.Contains(r.URL.Path, "/zones"):
			w.Write(okBody([]map[string]any{{"id": "zone1"}}))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	if err := c.EnsureARecord(context.Background(), "app.example.com", "1.2.3.4", true); err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected a POST to create the DNS record")
	}
}

func TestIssueOriginCert(t *testing.T) {
	c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/certificates" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			CSR       string   `json:"csr"`
			Hostnames []string `json:"hostnames"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !strings.HasPrefix(body.CSR, "-----BEGIN CERTIFICATE REQUEST-----") {
			t.Errorf("csr must be PEM, got %q", body.CSR)
		}
		if len(body.Hostnames) != 2 {
			t.Errorf("expected domain + www, got %v", body.Hostnames)
		}
		w.Write(okBody(map[string]any{"certificate": "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----"}))
	})
	cert, key, err := c.IssueOriginCert(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cert), "BEGIN CERTIFICATE") {
		t.Fatal("bad cert")
	}
	if !strings.Contains(string(key), "PRIVATE KEY") {
		t.Fatal("bad key")
	}
}

func TestZoneNotFound(t *testing.T) {
	c := newMock(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write(okBody([]any{}))
	})
	if _, err := c.zoneID(context.Background(), "a.b.c"); err == nil {
		t.Fatal("expected no-zone error")
	}
}

func TestNormalizeToken(t *testing.T) {
	for in, want := range map[string]string{
		"  clean40token  ": "clean40token",
		`"quoted"`:         "quoted",
		"Bearer abc":       "abc",
		"bearer\tABC ":     "ABC",
		"cfut_a b c":       "cfut_abc",  // chat-wrapped copy
		"cfut_ab\u200fcd":  "cfut_abcd", // RTL mark stripped
		"'\u200b.cfut_x'":  "cfut_x",    // zero-width + noise
		"cfut_abcdefghijklmnopqrstuvwxyz0123456789": "cfut_abcdefghijklmnopqrstuvwxyz0123456789",
	} {
		if got := NormalizeToken(in); got != want {
			t.Errorf("NormalizeToken(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTokenKind(t *testing.T) {
	for in, want := range map[string]string{
		"cfut_" + strings.Repeat("a", 53-5):        "user API token (cfut_ format)",
		"cfat_abcdefghijklmnop":                    "ACCOUNT-owned token (cfat_ format)",
		"cfk_abcdef":                               "Global API Key (cfk_ format)",
		strings.Repeat("0123456789abcdef", 3)[:37]: "Global API Key (legacy 37-char hex)",
		strings.Repeat("abcdefghij", 4):            "legacy API token (unprefixed)",
		"v1.0-something":                           "Origin CA key (v1.0- format)",
		"Bearer cfat_x":                            "ACCOUNT-owned token (cfat_ format)",
	} {
		if got := TokenKind(in); got != want {
			t.Errorf("TokenKind(%q…) = %q, want %q", in[:min(len(in), 8)], got, want)
		}
	}
}

func TestVerifyTokenRejectsWrongKind(t *testing.T) {
	// An account-owned token must fail fast WITHOUT any network call.
	called := false
	c := New("cfat_" + strings.Repeat("x", 48))
	c.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success":true}`))}, nil
	})}
	err := c.VerifyToken(context.Background())
	if err == nil {
		t.Fatal("expected rejection of cfat_ token")
	}
	if called {
		t.Fatal("cfat_ must be rejected without a network call")
	}
	if !strings.Contains(err.Error(), "ACCOUNT-owned") || !strings.Contains(err.Error(), "cfut_") {
		t.Fatalf("guidance missing: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
