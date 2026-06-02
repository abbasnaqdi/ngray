// Package config defines the declarative source of truth (config.yml) that
// drives server provisioning and client output.
package config

import (
	"fmt"
	"os"
	"path/filepath"

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
	Xray      Xray      `yaml:"xray"`
	Shoes     Shoes     `yaml:"shoes"`
	Singbox   Singbox   `yaml:"singbox"`
	Hysteria2 Hysteria2 `yaml:"hysteria2"`
	Fptn      Fptn      `yaml:"fptn"`
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
}

type Shoes struct {
	Reality []Reality `yaml:"reality"`
}

type Singbox struct {
	Reality []Reality `yaml:"reality"`
}

type Hysteria2 struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	SNI     string `yaml:"sni"`
	Obfs    string `yaml:"obfs"`
}

type Fptn struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	SNI     string `yaml:"sni"`
}

type Subscription struct {
	Enabled  bool   `yaml:"enabled"`
	FrontSNI string `yaml:"front_sni"`
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
					{SNI: "rust-lang.org", Flow: "vision"},
					{SNI: "python.org", Flow: "vision"},
				},
				XHTTP: []XHTTP{{SNI: "pypi.org"}},
			},
			Shoes: Shoes{
				Reality: []Reality{
					{SNI: "www.rust-lang.org", Proto: "vless_vision"},
					{SNI: "www.debian.org", Proto: "anytls"},
				},
			},
			Singbox: Singbox{
				Reality: []Reality{
					{SNI: "www.debian.org", Proto: "anytls"},
				},
			},
			Hysteria2: Hysteria2{Enabled: true, Port: 443, SNI: "rust-lang.org", Obfs: "salamander"},
			Fptn:      Fptn{Enabled: false, Port: 2096, SNI: "rust-lang.org"},
		},
		Subscription: Subscription{Enabled: true, FrontSNI: "www.kernel.org"},
		Client:       Client{Emit: []string{"vless", "anytls", "hysteria2", "singbox_json", "subscription", "qr"}},
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
	if c.Subscription.Enabled && c.Subscription.FrontSNI == "" {
		return fmt.Errorf("subscription.front_sni required when subscription enabled")
	}
	for _, r := range append(append([]Reality{}, c.Cores.Shoes.Reality...), c.Cores.Singbox.Reality...) {
		if r.Proto != "" && r.Proto != "vless_vision" && r.Proto != "anytls" {
			return fmt.Errorf("invalid reality proto %q for sni %s", r.Proto, r.SNI)
		}
	}
	if c.Subscription.Enabled {
		for _, s := range c.RouteSNIs() {
			if s == c.Subscription.FrontSNI {
				return fmt.Errorf("subscription.front_sni %q collides with a core route SNI; use a dedicated SNI", s)
			}
		}
	}
	return nil
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
