package links

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/mdp/qrterminal/v3"
)

// Subscription returns the base64-encoded newline list of URIs that clients
// import as a single subscribe link.
func Subscription(eps []Endpoint) string {
	var lines []string
	for _, e := range eps {
		if u := e.URI(); u != "" {
			lines = append(lines, u)
		}
	}
	return base64.StdEncoding.EncodeToString([]byte(strings.Join(lines, "\n")))
}

// QR renders a compact terminal QR code for a single URI.
func QR(uri string) string {
	var b bytes.Buffer
	qrterminal.GenerateWithConfig(uri, qrterminal.Config{
		Writer:    &b,
		Level:     qrterminal.L,
		BlackChar: qrterminal.BLACK,
		WhiteChar: qrterminal.WHITE,
		QuietZone: 1,
	})
	return b.String()
}

// SingboxJSON builds a sing-box client config with one outbound per endpoint.
func SingboxJSON(eps []Endpoint) ([]byte, error) {
	var outs []map[string]any
	for _, e := range eps {
		if o := singboxOutbound(e); o != nil {
			outs = append(outs, o)
		}
	}
	cfg := map[string]any{"outbounds": outs}
	return json.MarshalIndent(cfg, "", "  ")
}

func singboxOutbound(e Endpoint) map[string]any {
	tls := map[string]any{
		"enabled":     true,
		"server_name": e.SNI,
		"utls":        map[string]any{"enabled": true, "fingerprint": "chrome"},
		"reality":     map[string]any{"enabled": true, "public_key": e.PublicKey, "short_id": e.ShortID},
	}
	switch e.Proto {
	case "vless-vision":
		return map[string]any{
			"type": "vless", "tag": e.Tag, "server": e.Server, "server_port": e.Port,
			"uuid": e.UUID, "flow": "xtls-rprx-vision", "tls": tls,
		}
	case "anytls":
		return map[string]any{
			"type": "anytls", "tag": e.Tag, "server": e.Server, "server_port": e.Port,
			"password": e.Password, "tls": tls,
		}
	case "hysteria2":
		return map[string]any{
			"type": "hysteria2", "tag": e.Tag, "server": e.Server, "server_port": e.Port,
			"password": e.AuthPass,
			"obfs":     map[string]any{"type": e.Obfs, "password": e.ObfsPass},
			"tls":      map[string]any{"enabled": true, "server_name": e.SNI, "insecure": true},
		}
	case "tuic":
		return map[string]any{
			"type": "tuic", "tag": e.Tag, "server": e.Server, "server_port": e.Port,
			"uuid": e.UUID, "password": e.TUICPass,
			"congestion_control": "bbr",
			"tls": map[string]any{
				"enabled":     true,
				"server_name": e.SNI,
				"insecure":    true,
				"alpn":        []string{"h3"},
			},
		}
	case "shadowtls":
		// ShadowTLS requires two chained outbounds: outer shadowtls + inner shadowsocks.
		// Return the shadowsocks outbound (which detours through shadowtls).
		// A full JSON export should include both; single-outbound format uses shadowsocks as root.
		return map[string]any{
			"type": "shadowsocks", "tag": e.Tag, "server": "127.0.0.1", "server_port": 1,
			"method": e.Obfs, "password": e.AuthPass,
			"detour": e.Tag + "-stl",
			"_shadowtls_outbound": map[string]any{
				"type": "shadowtls", "tag": e.Tag + "-stl",
				"server": e.Server, "server_port": e.Port,
				"version": 3, "password": e.Password,
				"tls": map[string]any{"enabled": true, "server_name": e.SNI},
			},
		}
	}
	return nil
}
