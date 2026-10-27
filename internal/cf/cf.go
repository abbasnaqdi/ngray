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
	"unicode"
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
// surrounding quotes, a "Bearer " prefix, and — anywhere in the string — every
// space/invisible mark. Chats wrap long secrets and RTL text inserts
// zero-width characters, and both turn a valid token into "Invalid API
// Token". Cloudflare credentials contain only [A-Za-z0-9_-], so anything
// outside that alphabet is copy noise.
func NormalizeToken(t string) string {
	t = strings.TrimSpace(t)
	t = strings.Trim(t, "\"'`")
	var b strings.Builder
	for _, r := range t {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	t = b.String()
	// "Bearer " died in the whitespace purge above, so strip the bare word.
	if strings.HasPrefix(strings.ToLower(t), "bearer") && len(t) > 6 {
		t = t[6:]
	}
	return t
}

// TokenKind classifies a Cloudflare credential by its scannable prefix
// (cfut_ user tokens and cfat_ account-owned tokens were introduced when
// Cloudflare moved to prefixed formats; legacy tokens are unprefixed).
func TokenKind(t string) string {
	if strings.HasPrefix(strings.TrimSpace(t), "v1.0-") {
		return "Origin CA key (v1.0- format)" // dots/slashes die in NormalizeToken
	}
	t = NormalizeToken(t)
	switch {
	case strings.HasPrefix(t, "cfut_"):
		return "user API token (cfut_ format)"
	case strings.HasPrefix(t, "cfat_"):
		return "ACCOUNT-owned token (cfat_ format)"
	case strings.HasPrefix(t, "cfk_"):
		return "Global API Key (cfk_ format)"
	case strings.HasPrefix(t, "v1.0-"):
		return "Origin CA key (v1.0- format)"
	case len(t) == 37 && isHex(t):
		return "Global API Key (legacy 37-char hex)"
	}
	return "legacy API token (unprefixed)"
}

func isHex(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

// Diagnostics renders the shape of a token WITHOUT revealing it — kind,
// length and first/last characters, plus a note when copy noise was stripped.
func Diagnostics(t string) string {
	if strings.TrimSpace(t) == "" {
		return "(empty)"
	}
	r := []rune(NormalizeToken(t))
	kind := TokenKind(t)
	switch {
	case len(r) == 0:
		return "(empty after cleaning) — " + kind
	case len(r) <= 8:
		return fmt.Sprintf("%s · len=%d (suspiciously short)", kind, len(r))
	}
	out := fmt.Sprintf("%s · len=%d head=%-4s tail=%-4s", kind, len(r), string(r[:4]), string(r[len(r)-4:]))
	if len([]rune(t)) != len(r) {
		out += fmt.Sprintf(" (%d chars of copy noise were stripped — double-check the source)", len([]rune(t))-len(r))
	}
	return out
}

// VerifyToken checks the token with the /user/tokens/verify endpoint, failing
// fast with actionable advice when the credential is a known wrong TYPE
// (account-owned token, Global/API key, Origin CA key), and adding the common
// misconfiguration hints when Cloudflare itself rejects the credential.
func (c *Client) VerifyToken(ctx context.Context) error {
	kind := TokenKind(c.Token)
	switch {
	case strings.Contains(kind, "ACCOUNT-owned"):
		return fmt.Errorf( // account tokens cannot use the user-verify endpoint and
			// typically lack zone permissions — send the operator to the right page.
			"this is an ACCOUNT-owned token (created under Manage Account → Account API Tokens)\n" +
				"  ngray needs a USER API Token instead — on Cloudflare:\n" +
				"dash.cloudflare.com/profile/api-tokens → Create Token → Edit zone DNS\n" +
				"  the correct token's value starts with cfut_")
	case strings.Contains(kind, "Global API Key"):
		return fmt.Errorf("this is the Global API Key — it is not supported (full-access, wrong header)\n" +
			"  create a scoped USER API Token instead:\n" +
			"dash.cloudflare.com/profile/api-tokens → Create Token → Edit zone DNS")
	case strings.Contains(kind, "Origin CA key"):
		return fmt.Errorf("this is an Origin CA Key — not an API token usable for DNS/config\n" +
			"  create a USER API Token instead:\n" +
			"dash.cloudflare.com/profile/api-tokens → Create Token → Edit zone DNS")
	}
	_, err := c.do(ctx, http.MethodGet, "/user/tokens/verify", nil)
	if err == nil {
		return nil
	}
	e := err.Error()
	switch {
	case strings.Contains(e, "9109") || strings.Contains(e, "not a valid token") ||
		strings.Contains(e, "Authentication error") || strings.Contains(e, "Invalid API Token") ||
		strings.Contains(e, "400 Bad Request"):
		return fmt.Errorf("%w\n  hint: create a USER API Token (not Global API Key, not Token ID, not password):\n"+
			"dash.cloudflare.com/profile/api-tokens → Create Token → Create Custom Token\n"+
			"  permissions: Zone · DNS · Edit  +  Zone · SSL and Certificates · Edit\n"+
			"  (SSL and Certificates Edit is what issues the Origin CA certificate)\n"+
			"  zone resources: the zone that owns your domain; copy the long secret\n"+
			"  shown once — a user token starts with cfut_ or is a 40-char string", err)
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
