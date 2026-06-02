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

	IP        string             `json:"ip,omitempty"`
	NginxPort int                `json:"nginx_port,omitempty"`
	BBR       bool               `json:"bbr,omitempty"`
	SubToken  string             `json:"sub_token,omitempty"`
	Keys      map[string]Keypair `json:"keys,omitempty"`
	Secrets   map[string]Secret  `json:"secrets,omitempty"`
	Ports     map[string]int     `json:"ports,omitempty"`
	Deploy    *Deploy            `json:"deploy,omitempty"`
}

// Deploy is the snapshot of the last successful provisioning, so client output
// can be rendered without re-running the build.
type Deploy struct {
	Endpoints []links.Endpoint  `json:"endpoints,omitempty"`
	Notes     map[string]string `json:"notes,omitempty"`
	SubURL    string            `json:"sub_url,omitempty"`
}

func Path() string {
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), FileName)
		if _, err := os.Stat(p); err == nil {
			return p
		}
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
	return os.WriteFile(s.path, b, 0o600)
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
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = ab[int(b[i])%len(ab)]
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
