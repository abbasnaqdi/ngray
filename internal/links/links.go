// Package links turns live server endpoints into client share URIs, a sing-box
// JSON bundle, and a base64 subscription.
package links

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Endpoint describes one reachable proxy configuration.
type Endpoint struct {
	Tag    string
	Proto  string // vless-vision | vless-xhttp | anytls | hysteria2 | tuic | shadowtls
	Server string
	Port   int

	SNI       string
	PublicKey string // REALITY public key
	ShortID   string

	UUID     string // vless
	Password string // anytls / shadowtls outer password

	AuthPass string // hysteria2 / tuic auth
	Obfs     string // hysteria2 obfs type
	ObfsPass string
	TUICPass string // tuic password

	Path string // xhttp

	// Self-signed cert fields (used instead of insecure=1/allow_insecure=1).
	// Fingerprint is base64(SHA256(DER)) — used in URI pinSHA256 parameter.
	// CertPEM is the raw PEM — embedded in sing-box JSON outbound.
	Fingerprint string
	CertPEM     string

	// Raw is a fully pre-rendered sing-box outbound (minus "tag"), used by cores
	// whose protocol has no compact share URI (mieru, amneziawg). When set,
	// SingboxJSON emits it verbatim and URI() returns "" (no base64-bundle line).
	Raw map[string]any
}

// URI renders the client share link for an endpoint. Raw (sing-box-only)
// endpoints have no compact URI and are excluded from the base64 bundle.
func (e Endpoint) URI() string {
	if e.Raw != nil {
		return ""
	}
	host := net.JoinHostPort(e.Server, strconv.Itoa(e.Port))
	q := url.Values{}
	switch e.Proto {
	case "vless-vision":
		q.Set("security", "reality")
		q.Set("sni", e.SNI)
		q.Set("fp", "chrome")
		q.Set("pbk", e.PublicKey)
		q.Set("sid", e.ShortID)
		q.Set("flow", "xtls-rprx-vision")
		q.Set("type", "tcp")
		return fmt.Sprintf("vless://%s@%s?%s#%s", e.UUID, host, q.Encode(), frag(e.Tag))
	case "vless-xhttp":
		q.Set("security", "reality")
		q.Set("sni", e.SNI)
		q.Set("fp", "chrome")
		q.Set("pbk", e.PublicKey)
		q.Set("sid", e.ShortID)
		q.Set("type", "xhttp")
		q.Set("path", e.Path)
		q.Set("mode", "auto")
		return fmt.Sprintf("vless://%s@%s?%s#%s", e.UUID, host, q.Encode(), frag(e.Tag))
	case "vless-ws-tls":
		// Cloudflare-fronted: client connects to the domain (CF edge) on 443
		// with real TLS; CF proxies to the origin. No insecure flag (real cert).
		q.Set("encryption", "none")
		q.Set("security", "tls")
		q.Set("sni", e.SNI)
		q.Set("fp", "chrome")
		q.Set("type", "ws")
		q.Set("host", e.SNI)
		q.Set("path", e.Path)
		return fmt.Sprintf("vless://%s@%s?%s#%s", e.UUID, host, q.Encode(), frag(e.Tag))
	case "vless-xhttp-tls":
		q.Set("encryption", "none")
		q.Set("security", "tls")
		q.Set("sni", e.SNI)
		q.Set("fp", "chrome")
		q.Set("type", "xhttp")
		q.Set("host", e.SNI)
		q.Set("path", e.Path)
		q.Set("mode", "auto")
		return fmt.Sprintf("vless://%s@%s?%s#%s", e.UUID, host, q.Encode(), frag(e.Tag))
	case "anytls":
		q.Set("security", "reality")
		q.Set("sni", e.SNI)
		q.Set("fp", "chrome")
		q.Set("pbk", e.PublicKey)
		q.Set("sid", e.ShortID)
		// No insecure flag: REALITY authenticates via keypair, not TLS chain.
		return fmt.Sprintf("anytls://%s@%s?%s#%s", e.Password, host, q.Encode(), frag(e.Tag))
	case "hysteria2":
		q.Set("obfs", e.Obfs)
		q.Set("obfs-password", e.ObfsPass)
		q.Set("sni", e.SNI)
		if e.Fingerprint != "" {
			q.Set("pinSHA256", e.Fingerprint)
		} else {
			q.Set("insecure", "1")
		}
		return fmt.Sprintf("hysteria2://%s@%s?%s#%s", e.AuthPass, host, q.Encode(), frag(e.Tag))
	case "tuic":
		q.Set("congestion_control", "bbr")
		q.Set("alpn", "h3")
		q.Set("sni", e.SNI)
		if e.Fingerprint != "" {
			q.Set("pinSHA256", e.Fingerprint)
		} else {
			q.Set("allow_insecure", "1")
		}
		return fmt.Sprintf("tuic://%s:%s@%s?%s#%s", e.UUID, e.TUICPass, host, q.Encode(), frag(e.Tag))
	case "shadowtls":
		// SIP002 Shadowsocks URI with the shadow-tls plugin. The plugin options
		// are a single semicolon-separated string under one "plugin" parameter
		// (name;opt=val;…) — emitting separate shadow-tls-* query params is
		// non-standard and SIP002 clients ignore it.
		// AuthPass = inner Shadowsocks password; Obfs = SS method; Password = ShadowTLS password.
		userInfo := base64.StdEncoding.EncodeToString([]byte(e.Obfs + ":" + e.AuthPass))
		q.Set("plugin", "shadow-tls;version=3;host="+e.SNI+";password="+e.Password)
		return fmt.Sprintf("ss://%s@%s?%s#%s", userInfo, host, q.Encode(), frag(e.Tag))
	}
	return ""
}

func frag(tag string) string { return url.PathEscape(tag) }

// Tag builds a short, distinct config name: <core>-<kind>-<sni-stem>.
func Tag(core, kind, sni string) string {
	stem := strings.TrimPrefix(sni, "www.")
	if i := strings.IndexByte(stem, '.'); i > 0 {
		stem = stem[:i]
	}
	stem = strings.ReplaceAll(stem, "-", "")
	if kind == "" {
		return fmt.Sprintf("%s-%s", core, stem)
	}
	return fmt.Sprintf("%s-%s-%s", core, kind, stem)
}
