// Package state persists generated credentials, keys, port assignments and the
// subscription token across runs so re-provisioning is idempotent.
package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/abbasnaqdi/ng-ray/internal/links"
)

const FileName = "proxy-state.json"

type Keypair struct {
	Private string `json:"private"`
	Public  string `json:"public"`
}

type Secret struct {
	UUID     string `json:"uuid,omitempty"`
	Password string `json:"password,omitempty"`
	ShortID  string `json:"short_id,omitempty"`
}

type State struct {
	path string `json:"-"`

	IP           string             `json:"ip,omitempty"`
	NginxPort    int                `json:"nginx_port,omitempty"`
	BBR          bool               `json:"bbr,omitempty"`
	LastSNIProbe time.Time          `json:"last_sni_probe,omitempty"`
	SubToken     string             `json:"sub_token,omitempty"`
	Keys         map[string]Keypair `json:"keys,omitempty"`
	Secrets      map[string]Secret  `json:"secrets,omitempty"`
	Ports        map[string]int     `json:"ports,omitempty"`
	Deploy       *Deploy            `json:"deploy,omitempty"`
}

// Deploy is the snapshot of the last successful provisioning, so client output
// can be rendered without re-running the build.
type Deploy struct {
	Endpoints          []links.Endpoint  `json:"endpoints,omitempty"`
	Notes              map[string]string `json:"notes,omitempty"`
	SubURL             string            `json:"sub_url,omitempty"`              // HTTPS primary (IP:port, self-signed)
	SubHTTPURL         string            `json:"sub_http_url,omitempty"`         // HTTP fallback
	SubSingboxURL      string            `json:"sub_singbox_url,omitempty"`      // sing-box JSON (IP:port)
	SubMixedURL        string            `json:"sub_mixed_url,omitempty"`        // plain text: raw links + JSON (IP:port)
	SubCertFingerprint string            `json:"sub_cert_fingerprint,omitempty"` // SHA256 of sub TLS cert

	// Cloudflare-fronted subscription on the domain (no port, valid CF cert).
	SubDomainURL        string `json:"sub_domain_url,omitempty"`         // base64 list
	SubDomainSingboxURL string `json:"sub_domain_singbox_url,omitempty"` // sing-box JSON
	SubDomainMixedURL   string `json:"sub_domain_mixed_url,omitempty"`   // plain text: raw links + JSON
}

func Path() string {
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), FileName)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if _, err := os.Stat("/etc/ngray"); err == nil {
		return filepath.Join("/etc/ngray", FileName)
	}
	return FileName
}

func Load(path string) (*State, error) {
	s := &State{
		path:    path,
		Keys:    map[string]Keypair{},
		Secrets: map[string]Secret{},
		Ports:   map[string]int{},
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, err
	}
	s.path = path
	if s.Keys == nil {
		s.Keys = map[string]Keypair{}
	}
	if s.Secrets == nil {
		s.Secrets = map[string]Secret{}
	}
	if s.Ports == nil {
		s.Ports = map[string]int{}
	}
	return s, nil
}

func (s *State) Save() error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// Atomic write: temp file + rename so a crash never leaves truncated JSON.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Secret returns the secret for id, generating and persisting it on first use.
func (s *State) Secret(id string, gen func() Secret) Secret {
	if v, ok := s.Secrets[id]; ok {
		return v
	}
	v := gen()
	s.Secrets[id] = v
	return v
}

// EnsureSubToken returns a stable random subscription path token.
func (s *State) EnsureSubToken() string {
	if s.SubToken == "" {
		s.SubToken = RandHex(16)
	}
	return s.SubToken
}

// RotateSubToken replaces the subscription token and returns it. Paired with
// nginx's token guard, this makes every previously distributed link fail with
// 404 the instant the new config is live.
func (s *State) RotateSubToken() string {
	s.SubToken = RandHex(16)
	return s.SubToken
}

// NewUUID returns an RFC 4122 version-4 UUID.
func NewUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func NewShortID() string { return RandHex(8) }

func NewPassword(n int) string {
	const ab = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	limit := 256 - (256 % len(ab)) // reject values that would bias the modulo
	b := make([]byte, n)
	buf := make([]byte, 1)
	for i := 0; i < n; {
		_, _ = rand.Read(buf)
		if int(buf[0]) < limit {
			b[i] = ab[int(buf[0])%len(ab)]
			i++
		}
	}
	return string(b)
}

func RandHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
