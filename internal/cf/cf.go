// Package cf is a tiny Cloudflare API client used to issue Origin CA
// certificates and manage the DNS record that points a domain at this server.
// It never stores or logs the token; callers pass it in from env or a 0600 file.
package cf

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultBase = "https://api.cloudflare.com/client/v4"

// Client talks to the Cloudflare API v4.
type Client struct {
	Token string
	HTTP  *http.Client
	Base  string
}

func New(token string) *Client {
	return &Client{Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}, Base: defaultBase}
}

// TokenFileOverride lets tests (and unusual layouts) point at another file.
// Set NGRAY_CF_TOKEN_FILE to override the default /etc/ngray/cf_token.
func TokenFileOverride() string {
	if p := strings.TrimSpace(os.Getenv("NGRAY_CF_TOKEN_FILE")); p != "" {
		return p
	}
	return "/etc/ngray/cf_token"
}

// TokenFromEnvOrFile resolves a token from NGRAY_CF_TOKEN / CF_API_TOKEN, then
// the token file (default /etc/ngray/cf_token). Returns "" when none is set.
func TokenFromEnvOrFile() string {
	for _, k := range []string{"NGRAY_CF_TOKEN", "CF_API_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	if b, err := os.ReadFile(TokenFileOverride()); err == nil {
		return strings.TrimSpace(string(b))
	}
	return ""
}

type apiResp struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result json.RawMessage `json:"result"`
}

func (c *Client) do(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var ar apiResp
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&ar); err != nil {
		return nil, fmt.Errorf("cloudflare: bad response (HTTP %d)", resp.StatusCode)
	}
	if !ar.Success {
		msg := "unknown error"
		if len(ar.Errors) > 0 {
			msg = ar.Errors[0].Message
		}
		return nil, fmt.Errorf("cloudflare %s %s: %s", method, path, msg)
	}
	return ar.Result, nil
}

// NormalizeToken strips the usual copy artifacts around a Cloudflare secret:
// surrounding quotes, a "Bearer " prefix and stray whitespace/newlines.
func NormalizeToken(t string) string {
	t = strings.TrimSpace(t)
	t = strings.Trim(t, "\"'`")
	t = strings.TrimSpace(t)
	if l := strings.ToLower(t); strings.HasPrefix(l, "bearer ") {
		t = strings.TrimSpace(t[7:])
	}
	return t
}

// Diagnostics renders the shape of a token WITHOUT revealing it — the only
// thing exposed is length + first/last characters, enough to spot a truncated
// or wrong-type paste.
func Diagnostics(t string) string {
	r := []rune(NormalizeToken(t))
	switch {
	case len(r) == 0:
		return "(empty)"
	case len(r) <= 8:
		return fmt.Sprintf("len=%d (suspiciously short — an API Token is ~40 chars)", len(r))
	}
	return fmt.Sprintf("len=%d head=%-4s tail=%-4s", len(r), string(r[:4]), string(r[len(r)-4:]))
}

// VerifyToken checks the token with the /user/tokens/verify endpoint and adds
// the two common misconfiguration hints when the failure looks like a wrong
// credential type (the #1 paste error is the Global API Key or an Origin CA
// key instead of an API Token).
func (c *Client) VerifyToken(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodGet, "/user/tokens/verify", nil)
	if err == nil {
		return nil
	}
	e := err.Error()
	switch {
	case strings.Contains(e, "9109") || strings.Contains(e, "not a valid token") ||
		strings.Contains(e, "Authentication error") || strings.Contains(e, "400 Bad Request"):
		return fmt.Errorf("%w\n  hint: use an API Token, not the Global API Key or a password:\n  dash.cloudflare.com → My Profile → API Tokens → Create Token\n  permissions: Zone · DNS · Edit   (+ Zone · Origin CA · Edit not needed)",
			err)
	}
	return err
}

// zoneID finds the zone that owns domain by walking up its labels
// (app.example.com → example.com). It never queries a bare TLD.
func (c *Client) zoneID(ctx context.Context, domain string) (string, error) {
	labels := strings.Split(domain, ".")
	var lastErr error
	for i := 0; i < len(labels)-1; i++ {
		candidate := strings.Join(labels[i:], ".")
		raw, err := c.do(ctx, http.MethodGet, "/zones?name="+url.QueryEscape(candidate)+"&status=active&per_page=50", nil)
		if err != nil {
			// Keep the first real API error (auth, network) so the caller sees
			// the true cause instead of "zone not found".
			if lastErr == nil {
				lastErr = err
			}
			continue
		}
		var zones []struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &zones) == nil && len(zones) > 0 {
			return zones[0].ID, nil
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("no Cloudflare zone found for %s", domain)
}

// EnsureARecord creates or updates a proxied A record for domain → ip.
func (c *Client) EnsureARecord(ctx context.Context, domain, ip string, proxied bool) error {
	zone, err := c.zoneID(ctx, domain)
	if err != nil {
		return err
	}
	raw, err := c.do(ctx, http.MethodGet,
		"/zones/"+zone+"/dns_records?name="+url.QueryEscape(domain)+"&type=A&per_page=100", nil)
	if err != nil {
		return err
	}
	var recs []struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &recs); err != nil {
		return fmt.Errorf("cloudflare dns list: %w", err)
	}
	body := map[string]any{
		"type": "A", "name": domain, "content": ip, "ttl": 1, "proxied": proxied,
	}
	if len(recs) > 0 {
		if recs[0].Content == ip {
			return nil // already correct
		}
		_, err = c.do(ctx, http.MethodPatch, "/zones/"+zone+"/dns_records/"+recs[0].ID, body)
	} else {
		_, err = c.do(ctx, http.MethodPost, "/zones/"+zone+"/dns_records", body)
	}
	return err
}

// IssueOriginCert generates an ECDSA key + CSR and asks Cloudflare for a
// 15-year Origin CA certificate covering domain (+ its www. sibling). Returns
// PEM cert and key.
func (c *Client) IssueOriginCert(ctx context.Context, domain string) (certPEM, keyPEM []byte, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	hosts := []string{domain}
	// Add the www. sibling only for a plain apex/A-label domain; a wildcard or
	// an existing www. would otherwise produce an invalid hostname.
	if !strings.HasPrefix(domain, "www.") && !strings.Contains(domain, "*") {
		hosts = append(hosts, "www."+domain)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader,
		&x509.CertificateRequest{Subject: pkix.Name{CommonName: domain}, DNSNames: hosts}, priv)
	if err != nil {
		return nil, nil, err
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	body := map[string]any{
		"hostnames":          hosts,
		"requested_validity": 5475,
		"request_type":       "origin-ecc",
		"csr":                string(csrPEM),
	}
	raw, err := c.do(ctx, http.MethodPost, "/certificates", body)
	if err != nil {
		return nil, nil, err
	}
	var res struct {
		Certificate string `json:"certificate"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, nil, err
	}
	if res.Certificate == "" {
		return nil, nil, fmt.Errorf("cloudflare returned an empty certificate")
	}
	return []byte(res.Certificate), keyPEM, nil
}
