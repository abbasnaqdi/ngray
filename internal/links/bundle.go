package links

import (
	"encoding/base64"
	"encoding/json"
	"maps"
	"strings"
)

// Subscription returns the base64-encoded newline list of URIs that clients
// import as a single subscribe link. All endpoint types with a URI are included.
func Subscription(eps []Endpoint) string {
	var lines []string
	for _, e := range eps {
		if u := e.URI(); u != "" {
			lines = append(lines, u)
		}
	}
	return base64.StdEncoding.EncodeToString([]byte(strings.Join(lines, "\n")))
}

// Mixed returns a plain-TEXT subscription body for clients (e.g. nekoray) that
// read raw share links and/or sing-box JSON straight from the body: every
// share URI on its own line, then a blank line, then the complete sing-box
// JSON config. Nothing is base64-wrapped, so links stay visible and copyable.
func Mixed(eps []Endpoint) (string, error) {
	var lines []string
	for _, e := range eps {
		if u := e.URI(); u != "" {
			lines = append(lines, u)
		}
	}
	cfg, err := SingboxJSON(eps)
	if err != nil {
		return "", err
	}
	return strings.Join(lines, "\n") + "\n\n" + string(cfg) + "\n", nil
}

// SingboxJSON builds a sing-box client config with outbounds for every
// endpoint. ShadowTLS produces two chained outbounds (shadowtls + shadowsocks).
//
// When there is more than one endpoint it also appends two outbound groups:
//   - "auto"   (urltest): continuously health-checks every endpoint and routes
//     through the lowest-latency one that responds, so endpoints blocked on the
//     user's operator are skipped automatically.
//   - "blackout" (urltest): the subset of "auto" that can survive a national
//     protocol+SNI allowlist (Iran, Jan 2026) — TCP/443 REALITY and ShadowTLS
//     shapes that send a borrowed, allowlistable SNI. Excludes every UDP core
//     (hysteria2/tuic), the mieru side-port, and CF-WS (custom-domain
//     SNI). Added only when such an endpoint exists. The client default stays
//     "auto"; when everything else goes dark, switching "select" to "blackout"
//     health-checks only the shapes that can still cross.
//   - "select" (selector): lets the user pin a specific endpoint or group,
//     defaulting to "auto".
//
// This is what lets a client recover when only a subset of configs survive a
// given network: no manual trial-and-error, the working ones win on their own.
func SingboxJSON(eps []Endpoint) ([]byte, error) {
	var outs []map[string]any
	var tags, blackoutTags []string
	for _, e := range eps {
		o := singboxOutbounds(e)
		outs = append(outs, o...)
		// e.Tag is the dialable outbound for every proto (for shadowtls the inner
		// shadowsocks; its "-stl" detour is not health-checked directly).
		if len(o) > 0 {
			tags = append(tags, e.Tag)
			if blackoutSafe(e.Proto) {
				blackoutTags = append(blackoutTags, e.Tag)
			}
		}
	}

	if len(tags) > 1 {
		outs = append(outs, map[string]any{
			"type": "urltest", "tag": "auto",
			"outbounds": tags,
			"url":       "https://www.gstatic.com/generate_204",
			"interval":  "3m", "tolerance": 50,
		})
		sel := []string{"auto"}
		if len(blackoutTags) > 0 {
			outs = append(outs, map[string]any{
				"type": "urltest", "tag": "blackout",
				"outbounds": blackoutTags,
				"url":       "https://www.gstatic.com/generate_204",
				"interval":  "3m", "tolerance": 50,
			})
			sel = append(sel, "blackout")
		}
		outs = append(outs, map[string]any{
			"type": "selector", "tag": "select",
			"outbounds": append(sel, tags...),
			"default":   "auto",
		})
	}

	cfg := map[string]any{"outbounds": outs}
	return json.MarshalIndent(cfg, "", "  ")
}

// blackoutSafe reports whether an endpoint can survive a national protocol+SNI
// allowlist (only HTTPS/443 to allowlisted domains crosses). True for the
// TCP/443 REALITY and ShadowTLS shapes, which present a borrowed real SNI on the
// one allowlisted port. False for UDP cores (hysteria2, tuic, wireguard — all
// dropped), mieru (its own non-443 port is dropped), and CF-WS/xhttp-tls (they
// send a custom domain as SNI, which is not on the allowlist and gets RST).
func blackoutSafe(proto string) bool {
	switch proto {
	case "vless-vision", "vless-xhttp", "anytls", "shadowtls":
		return true
	}
	return false
}

// singboxOutbounds returns one or two outbounds for the endpoint.
// ShadowTLS requires a shadowtls outer + shadowsocks inner; all others return one.
func singboxOutbounds(e Endpoint) []map[string]any {
	// Pre-rendered outbound (mieru): emit verbatim with the tag set.
	if e.Raw != nil {
		o := make(map[string]any, len(e.Raw)+1)
		maps.Copy(o, e.Raw)
		o["tag"] = e.Tag
		return []map[string]any{o}
	}

	realityTLS := map[string]any{
		"enabled":     true,
		"server_name": e.SNI,
		"utls":        map[string]any{"enabled": true, "fingerprint": "chrome"},
		"reality":     map[string]any{"enabled": true, "public_key": e.PublicKey, "short_id": e.ShortID},
	}

	switch e.Proto {
	case "vless-vision":
		return []map[string]any{{
			"type": "vless", "tag": e.Tag,
			"server": e.Server, "server_port": e.Port,
			"uuid": e.UUID, "flow": "xtls-rprx-vision",
			"tls": realityTLS,
		}}

	case "vless-xhttp":
		return []map[string]any{{
			"type": "vless", "tag": e.Tag,
			"server": e.Server, "server_port": e.Port,
			"uuid": e.UUID,
			"tls":  realityTLS,
			"transport": map[string]any{
				"type": "xhttp",
				"path": e.Path,
			},
		}}

	case "vless-ws-tls":
		// Cloudflare-fronted: real TLS to the domain (CF edge), WS transport.
		return []map[string]any{{
			"type": "vless", "tag": e.Tag,
			"server": e.Server, "server_port": e.Port,
			"uuid": e.UUID,
			"tls": map[string]any{
				"enabled": true, "server_name": e.SNI,
				"utls": map[string]any{"enabled": true, "fingerprint": "chrome"},
			},
			"transport": map[string]any{
				"type": "ws", "path": e.Path,
				"headers": map[string]any{"Host": e.SNI},
			},
		}}

	case "vless-xhttp-tls":
		return []map[string]any{{
			"type": "vless", "tag": e.Tag,
			"server": e.Server, "server_port": e.Port,
			"uuid": e.UUID,
			"tls": map[string]any{
				"enabled": true, "server_name": e.SNI,
				"utls": map[string]any{"enabled": true, "fingerprint": "chrome"},
			},
			"transport": map[string]any{"type": "xhttp", "path": e.Path},
		}}

	case "anytls":
		return []map[string]any{{
			"type": "anytls", "tag": e.Tag,
			"server": e.Server, "server_port": e.Port,
			"password": e.Password,
			"tls":      realityTLS,
		}}

	case "hysteria2":
		hy2TLS := map[string]any{"enabled": true, "server_name": e.SNI}
		if e.CertPEM != "" {
			hy2TLS["certificate"] = e.CertPEM
		} else {
			hy2TLS["insecure"] = true
		}
		epOut := map[string]any{
			"type": "hysteria2", "tag": e.Tag,
			"server": e.Server, "server_port": e.Port,
			"password": e.AuthPass,
			"tls":      hy2TLS,
		}
		if e.Obfs != "" {
			epOut["obfs"] = map[string]any{"type": e.Obfs, "password": e.ObfsPass}
		}
		return []map[string]any{epOut}

	case "tuic":
		tuicTLS := map[string]any{
			"enabled": true, "server_name": e.SNI, "alpn": []string{"h3"},
		}
		if e.CertPEM != "" {
			tuicTLS["certificate"] = e.CertPEM
		} else {
			tuicTLS["insecure"] = true
		}
		return []map[string]any{{
			"type": "tuic", "tag": e.Tag,
			"server": e.Server, "server_port": e.Port,
			"uuid": e.UUID, "password": e.TUICPass,
			"congestion_control": "bbr",
			"tls":                tuicTLS,
		}}

	case "shadowtls":
		// ShadowTLS v3 requires two chained outbounds in sing-box:
		//   1. shadowtls outbound (outer TLS layer)
		//   2. shadowsocks outbound (inner payload) that detours through shadowtls
		stlTag := e.Tag + "-stl"
		stlOut := map[string]any{
			"type": "shadowtls", "tag": stlTag,
			"server": e.Server, "server_port": e.Port,
			"version":  3,
			"password": e.Password,
			"tls":      map[string]any{"enabled": true, "server_name": e.SNI},
		}
		// server/server_port are nominal: sing-box dials through the detour, so
		// the actual connection rides the shadowtls outer layer. Carry the real
		// endpoint anyway so the config reads correctly and survives validation.
		ssOut := map[string]any{
			"type": "shadowsocks", "tag": e.Tag,
			"server": e.Server, "server_port": e.Port,
			"method": e.Obfs, "password": e.AuthPass,
			"detour": stlTag,
		}
		return []map[string]any{stlOut, ssOut}
	}
	return nil
}
