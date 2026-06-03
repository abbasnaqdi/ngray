// Package config defines the declarative source of truth (config.yml) that
// drives server provisioning and client output.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const FileName = "config.yml"

type Config struct {
	Server       Server       `yaml:"server"`
	Nginx        Nginx        `yaml:"nginx"`
	Cores        Cores        `yaml:"cores"`
	Subscription Subscription `yaml:"subscription"`
	Client       Client       `yaml:"client"`
}

type Server struct {
	IP        string `yaml:"ip"` // "auto" to detect, or an explicit address
	EnableBBR bool   `yaml:"enable_bbr"`
}

type Nginx struct {
	Port int `yaml:"port"`
}

type Cores struct {
	Xray      Xray          `yaml:"xray"`
	Shoes     Shoes         `yaml:"shoes"`
	Singbox   Singbox       `yaml:"singbox"`
	Hysteria2 Hysteria2     `yaml:"hysteria2"`
	Fptn      Fptn          `yaml:"fptn"`
	Mieru     MieruCore     `yaml:"mieru"`
	AmneziaWG AmneziaWGCore `yaml:"amneziawg"`
}

// MieruCore is an independent TCP/UDP multiplexed tunnel with no recognisable
// protocol pattern. Best against ML-based detection (China/Russia).
type MieruCore struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"` // public TCP
	User    string `yaml:"user"`
}

// AmneziaWGCore is obfuscated WireGuard (Russia-grade). Adds random junk
// headers and salt to every UDP packet so WG traffic is unidentifiable.
type AmneziaWGCore struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"` // public UDP
}

// Reality is one REALITY config bound to a single whitelisted SNI.
type Reality struct {
	SNI   string `yaml:"sni"`
	Flow  string `yaml:"flow,omitempty"`  // xray: "vision"
	Proto string `yaml:"proto,omitempty"` // shoes/singbox: "vless_vision" | "anytls"
}

type XHTTP struct {
	SNI string `yaml:"sni"`
}

type Xray struct {
	Reality []Reality `yaml:"reality"`
	XHTTP   []XHTTP   `yaml:"xhttp"`
	CDN     *CDN      `yaml:"cdn,omitempty"`
}

// CDN serves Cloudflare-frontable inbounds (VLESS over WebSocket and/or XHTTP,
// with real TLS) so the domain is a working fallback path when the direct
// server IP is blocked. The origin terminates TLS with a Cloudflare Origin
// Certificate (set the CF SSL/TLS mode to Full (strict)). WS is served on
// Domain and XHTTP on AltDomain (defaults to "www."+Domain), so both share
// port 443 through distinct SNI routes in the existing ssl_preread mux. The
// inbounds are skipped at build time if the cert/key files are absent.
type CDN struct {
	Enabled   bool   `yaml:"enabled"`
	Domain    string `yaml:"domain"`               // WS SNI, e.g. example.com
	AltDomain string `yaml:"alt_domain,omitempty"` // XHTTP SNI; default "www."+Domain
	WS        bool   `yaml:"ws"`
	XHTTP     bool   `yaml:"xhttp"`
	CertFile  string `yaml:"cert_file"`
	KeyFile   string `yaml:"key_file"`
}

// XHTTPDomain returns the SNI used for the XHTTP inbound.
func (c *CDN) XHTTPDomain() string {
	if c.AltDomain != "" {
		return c.AltDomain
	}
	if c.Domain != "" {
		return "www." + c.Domain
	}
	return ""
}

type Shoes struct {
	Reality   []Reality  `yaml:"reality"`
	TUIC      *ShoesTUIC `yaml:"tuic,omitempty"`
	Hysteria2 *ShoesHy2  `yaml:"hysteria2,omitempty"`
}

// ShoesTUIC is a TUIC v5 inbound served by the shoes binary (Rust implementation).
type ShoesTUIC struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	SNI     string `yaml:"sni"`
}

// ShoesHy2 is a Hysteria2 inbound served by the shoes binary (Rust implementation).
type ShoesHy2 struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	SNI     string `yaml:"sni"`
	Obfs    string `yaml:"obfs"`
}

type Singbox struct {
	Reality   []Reality         `yaml:"reality"`
	TUIC      *SingboxTUIC      `yaml:"tuic,omitempty"`
	ShadowTLS *SingboxShadowTLS `yaml:"shadowtls,omitempty"`
}

// SingboxTUIC configures a TUIC v5 inbound inside sing-box (independent UDP port).
type SingboxTUIC struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	SNI     string `yaml:"sni"`
}

// SingboxShadowTLS configures a ShadowTLS v3 inbound wrapping Shadowsocks.
// Mimics a real TLS handshake to SNI; tunnels traffic through inner Shadowsocks.
type SingboxShadowTLS struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"` // public TCP port (own listener, NOT behind nginx)
	SNI     string `yaml:"sni"`  // real TLS handshake target
}

type Hysteria2 struct {
	Enabled  bool   `yaml:"enabled"`
	Port     int    `yaml:"port"`
	AltPorts []int  `yaml:"alt_ports,omitempty"` // extra UDP ports; firewall redirects them to Port
	SNI      string `yaml:"sni"`
	Obfs     string `yaml:"obfs"`
}

type Fptn struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	SNI     string `yaml:"sni"`
	User    string `yaml:"user"`
}

type Subscription struct {
	Enabled   bool   `yaml:"enabled"`
	FrontSNI  string `yaml:"front_sni,omitempty"`
	HTTPSPort int    `yaml:"https_port,omitempty"` // HTTPS sub port (default 2053)
}

type Client struct {
	Emit []string `yaml:"emit"`
}

func Default() *Config {
	return &Config{
		Server: Server{IP: "auto", EnableBBR: true},
		Nginx:  Nginx{Port: 443},
		Cores: Cores{
			Xray: Xray{
				Reality: []Reality{
					{SNI: "www.rust-lang.org", Flow: "vision"},
				},
				XHTTP: []XHTTP{{SNI: "www.python.org"}},
				CDN: &CDN{
					Enabled: true, Domain: "example.com", WS: true, XHTTP: true,
					CertFile: "/etc/ngray/cf-origin.crt", KeyFile: "/etc/ngray/cf-origin.key",
				},
			},
			Shoes: Shoes{
				Reality: []Reality{
					{SNI: "www.debian.org", Proto: "anytls"},
				},
				TUIC:      &ShoesTUIC{Enabled: true, Port: 36712, SNI: "www.rust-lang.org"},
				Hysteria2: &ShoesHy2{Enabled: true, Port: 36713, SNI: "www.rust-lang.org", Obfs: "salamander"},
			},
			Singbox: Singbox{
				Reality: []Reality{
					{SNI: "docs.python.org", Proto: "anytls"},
					{SNI: "www.nginx.org", Proto: "vless_vision"},
				},
				TUIC:      &SingboxTUIC{Enabled: true, Port: 36711, SNI: "www.rust-lang.org"},
				ShadowTLS: &SingboxShadowTLS{Enabled: true, Port: 2443, SNI: "www.rust-lang.org"},
			},
			Hysteria2: Hysteria2{Enabled: true, Port: 443, AltPorts: []int{8443, 2087, 2096, 2052, 2082}, SNI: "www.rust-lang.org", Obfs: "salamander"},
			Fptn:      Fptn{Enabled: false, Port: 2096, SNI: "www.rust-lang.org", User: "ngray"},
			Mieru:     MieruCore{Enabled: true, Port: 14000, User: "ngray"},
			AmneziaWG: AmneziaWGCore{Enabled: true, Port: 51820},
		},
		Subscription: Subscription{Enabled: true, FrontSNI: "www.kernel.org", HTTPSPort: 2053},
		Client:       Client{Emit: []string{"vless", "anytls", "hysteria2", "tuic", "shadowtls", "singbox_json", "subscription", "mieru", "amneziawg", "fptn"}},
	}
}

// Path resolves config.yml next to the executable, falling back to the CWD.
func Path() string {
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), FileName)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return FileName
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := Default()
	if err := yaml.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// LoadOrDefault returns the on-disk config, or the built-in default when absent.
func LoadOrDefault(path string) (*Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return Default(), nil
	}
	return Load(path)
}

func Save(path string, c *Config) error {
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func (c *Config) Validate() error {
	if c.Nginx.Port <= 0 || c.Nginx.Port > 65535 {
		return fmt.Errorf("nginx.port %d out of range", c.Nginx.Port)
	}
	// FrontSNI is optional — used as a DNS SAN in the self-signed cert when set.
	for _, r := range append(append([]Reality{}, c.Cores.Shoes.Reality...), c.Cores.Singbox.Reality...) {
		if r.Proto != "" && r.Proto != "vless_vision" && r.Proto != "anytls" {
			return fmt.Errorf("invalid reality proto %q for sni %s", r.Proto, r.SNI)
		}
	}
	// SNI values are written verbatim into the nginx config; reject chars that
	// would break the map block or allow config injection.
	allSNIs := append(c.RouteSNIs(), c.CDNDomains()...)
	allSNIs = append(allSNIs, c.Subscription.FrontSNI)
	for _, s := range allSNIs {
		if s == "" {
			continue
		}
		if strings.ContainsAny(s, " \t\n\r;{}\"'\\") {
			return fmt.Errorf("SNI %q contains characters not valid in an nginx map key", s)
		}
	}

	// Each SNI may map to only one backend under nginx ssl_preread.
	seen := map[string]bool{}
	for _, s := range append(c.RouteSNIs(), c.CDNDomains()...) {
		if seen[s] {
			return fmt.Errorf("SNI %q is used by more than one core; each REALITY SNI must be unique", s)
		}
		seen[s] = true
	}
	if c.Subscription.Enabled {
		if seen[c.Subscription.FrontSNI] {
			return fmt.Errorf("subscription.front_sni %q collides with a core route SNI; use a dedicated SNI", c.Subscription.FrontSNI)
		}
	}
	return nil
}

// CDNDomains returns the Cloudflare-fronted SNIs (WS + XHTTP) that nginx routes
// to the CDN backends. Kept separate from RouteSNIs so they are uniqueness- and
// nginx-key-checked but not REALITY-fitness-tested (they front through CF).
func (c *Config) CDNDomains() []string {
	cdn := c.Cores.Xray.CDN
	if cdn == nil || !cdn.Enabled || cdn.Domain == "" {
		return nil
	}
	var out []string
	if cdn.WS {
		out = append(out, cdn.Domain)
	}
	if cdn.XHTTP {
		out = append(out, cdn.XHTTPDomain())
	}
	return out
}

// RouteSNIs returns every SNI that nginx multiplexes to a TCP core backend.
func (c *Config) RouteSNIs() []string {
	var out []string
	for _, r := range c.Cores.Xray.Reality {
		out = append(out, r.SNI)
	}
	for _, x := range c.Cores.Xray.XHTTP {
		out = append(out, x.SNI)
	}
	for _, r := range c.Cores.Shoes.Reality {
		out = append(out, r.SNI)
	}
	for _, r := range c.Cores.Singbox.Reality {
		out = append(out, r.SNI)
	}
	return out
}
