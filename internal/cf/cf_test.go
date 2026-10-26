package cf

import (
	"context"
	"encoding/json"
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
	// no file at /etc/ngray/cf_token in the test env
	if got := TokenFromEnvOrFile(); got != "" {
		t.Fatalf("expected empty token, got %q", got)
	}
	_ = os.Getenv("HOME")
	_ = filepath.Separator
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
		if body.CSR == "" {
			t.Error("missing csr")
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
