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
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
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

// TokenFromEnvOrFile resolves a token from NGRAY_CF_TOKEN / CF_API_TOKEN, then
// /etc/ngray/cf_token. Returns "" when none is configured.
func TokenFromEnvOrFile() string {
	for _, k := range []string{"NGRAY_CF_TOKEN", "CF_API_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	if b, err := os.ReadFile("/etc/ngray/cf_token"); err == nil {
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

// VerifyToken checks the token and returns the account/zone scope on success.
func (c *Client) VerifyToken(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodGet, "/user/tokens/verify", nil)
	return err
}

// zoneID finds the zone that owns domain by walking up its labels
// (app.example.com → example.com → com).
func (c *Client) zoneID(ctx context.Context, domain string) (string, error) {
	labels := strings.Split(domain, ".")
	for i := 0; i < len(labels)-1; i++ {
		candidate := strings.Join(labels[i:], ".")
		raw, err := c.do(ctx, http.MethodGet, "/zones?name="+candidate, nil)
		if err != nil {
			continue
		}
		var zones []struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &zones) == nil && len(zones) > 0 {
			return zones[0].ID, nil
		}
	}
	return "", fmt.Errorf("no Cloudflare zone found for %s", domain)
}

// EnsureARecord creates or updates a proxied A record for domain → ip.
func (c *Client) EnsureARecord(ctx context.Context, domain, ip string, proxied bool) error {
	zone, err := c.zoneID(ctx, domain)
	if err != nil {
		return err
	}
	raw, err := c.do(ctx, http.MethodGet, "/zones/"+zone+"/dns_records?name="+domain, nil)
	if err != nil {
		return err
	}
	var recs []struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &recs)
	body := map[string]any{
		"type": "A", "name": domain, "content": ip, "ttl": 1, "proxied": proxied,
	}
	if len(recs) > 0 {
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
	if !strings.HasPrefix(domain, "www.") {
		hosts = append(hosts, "www."+domain)
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader,
		&x509.CertificateRequest{Subject: pkix.Name{CommonName: domain}, DNSNames: hosts}, priv)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	body := map[string]any{
		"hostnames":          hosts,
		"requested_validity": 5475,
		"request_type":       "origin-ecc",
		"csr":                base64.StdEncoding.EncodeToString(csr),
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
