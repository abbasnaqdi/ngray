// Package links turns live server endpoints into client share URIs, a sing-box
// JSON bundle, and a base64 subscription.
package links

import (
	"encoding/base64"
	"encoding/hex"
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
	// Fingerprint is base64(SHA256(DER)); URI() normalises it to the 64-char
	// hex form that TUIC and Xray-compatible Hysteria2 clients require.
	// CertPEM is the raw PEM — embedded in sing-box JSON outbound.
	Fingerprint string
	CertPEM     string

	// Raw is a fully pre-rendered sing-box outbound (minus "tag"), used by cores
	// whose protocol has no compact share URI (mieru). When set,
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
		if e.Obfs != "" {
			q.Set("obfs", e.Obfs)
			q.Set("obfs-password", e.ObfsPass)
		}
		q.Set("sni", e.SNI)
		if pin := pinHex(e.Fingerprint); pin != "" {
			q.Set("pinSHA256", pin)
		} else {
			q.Set("insecure", "1")
		}
		return fmt.Sprintf("hysteria2://%s@%s?%s#%s", e.AuthPass, host, q.Encode(), frag(e.Tag))
	case "tuic":
		q.Set("congestion_control", "bbr")
		q.Set("alpn", "h3")
		q.Set("sni", e.SNI)
		if pin := pinHex(e.Fingerprint); pin != "" {
			q.Set("pinSHA256", pin)
		} else {
			q.Set("allow_insecure", "1")
		}
		return fmt.Sprintf("tuic://%s:%s@%s?%s#%s", e.UUID, e.TUICPass, host, q.Encode(), frag(e.Tag))
	case "shadowtls":
		// ShadowTLS is not a SIP003 plugin: NekoBox/v2rayN reject
		// `plugin=shadow-tls` and fail the whole subscription import. This
		// shape is distributed only through the chained sing-box JSON below.
		return ""
	}
	return ""
}

// pinHex renders a certificate SHA-256 in the 64-character lowercase hex form
// expected by TUIC and Xray-compatible Hysteria2 clients. Fingerprint may be
// base64 (our CertFingerprint helper), bare hex, or colon-separated openssl
// output; an unrecognised value returns "" so the caller can fall back to
// insecure=1.
func pinHex(fp string) string {
	fp = strings.TrimSpace(fp)
	if fp == "" {
		return ""
	}
	bare := strings.ReplaceAll(fp, ":", "")
	if len(bare) == 64 {
		if _, err := hex.DecodeString(bare); err == nil {
			return strings.ToLower(bare)
		}
	}
	b, err := base64.StdEncoding.DecodeString(fp)
	if err != nil || len(b) != 32 {
		return ""
	}
	return hex.EncodeToString(b)
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
