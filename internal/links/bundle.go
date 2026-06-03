package links

import (
	"encoding/base64"
	"encoding/json"
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

// SingboxJSON builds a sing-box client config with outbounds for every
// endpoint. ShadowTLS produces two chained outbounds (shadowtls + shadowsocks).
func SingboxJSON(eps []Endpoint) ([]byte, error) {
	var outs []map[string]any
	for _, e := range eps {
		outs = append(outs, singboxOutbounds(e)...)
	}
	cfg := map[string]any{"outbounds": outs}
	return json.MarshalIndent(cfg, "", "  ")
}

// singboxOutbounds returns one or two outbounds for the endpoint.
// ShadowTLS requires a shadowtls outer + shadowsocks inner; all others return one.
func singboxOutbounds(e Endpoint) []map[string]any {
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
			"tls": realityTLS,
			"transport": map[string]any{
				"type": "xhttp",
				"path": e.Path,
			},
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
		return []map[string]any{{
			"type": "hysteria2", "tag": e.Tag,
			"server": e.Server, "server_port": e.Port,
			"password": e.AuthPass,
			"obfs":     map[string]any{"type": e.Obfs, "password": e.ObfsPass},
			"tls":      hy2TLS,
		}}

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
		ssOut := map[string]any{
			"type": "shadowsocks", "tag": e.Tag,
			"server": "127.0.0.1", "server_port": 8388,
			"method": e.Obfs, "password": e.AuthPass,
			"detour": stlTag,
		}
		return []map[string]any{stlOut, ssOut}
	}
	return nil
}
