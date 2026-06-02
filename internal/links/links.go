// Package links turns live server endpoints into client share URIs, a sing-box
// JSON bundle, a base64 subscription and terminal QR codes.
package links

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Endpoint describes one reachable proxy configuration.
type Endpoint struct {
	Tag    string
	Proto  string // vless-vision | vless-xhttp | anytls | hysteria2
	Server string
	Port   int

	SNI       string
	PublicKey string // REALITY public key
	ShortID   string

	UUID     string // vless
	Password string // anytls

	AuthPass string // hysteria2
	Obfs     string // hysteria2 obfs type
	ObfsPass string

	Path string // xhttp
}

// URI renders the client share link for an endpoint.
func (e Endpoint) URI() string {
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
	case "anytls":
		q.Set("security", "reality")
		q.Set("sni", e.SNI)
		q.Set("fp", "chrome")
		q.Set("pbk", e.PublicKey)
		q.Set("sid", e.ShortID)
		q.Set("insecure", "1")
		return fmt.Sprintf("anytls://%s@%s?%s#%s", e.Password, host, q.Encode(), frag(e.Tag))
	case "hysteria2":
		q.Set("obfs", e.Obfs)
		q.Set("obfs-password", e.ObfsPass)
		q.Set("sni", e.SNI)
		q.Set("insecure", "1")
		return fmt.Sprintf("hysteria2://%s@%s?%s#%s", e.AuthPass, host, q.Encode(), frag(e.Tag))
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
