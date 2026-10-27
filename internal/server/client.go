package server

import (
	"fmt"
	"io"
	"strings"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/links"
	"github.com/abbasnaqdi/ng-ray/internal/state"
)

// protoKey maps an endpoint Proto to the emit keyword that enables it.
var protoKey = map[string]string{
	"vless-vision":    "vless",
	"vless-xhttp":     "vless",
	"vless-ws-tls":    "vless",
	"vless-xhttp-tls": "vless",
	"anytls":          "anytls",
	"hysteria2":       "hysteria2",
	"tuic":            "tuic",
	"shadowtls":       "shadowtls",
}

// noteKey maps a Note key to the emit keyword that enables it.
var noteKey = map[string]string{
	"mieru-client-config": "mieru",
}

type emitSet map[string]bool

func buildEmitSet(cfg *config.Config) emitSet {
	s := make(emitSet)
	if cfg == nil {
		s["all"] = true
		return s
	}
	for _, e := range cfg.Client.Emit {
		s[strings.ToLower(e)] = true
	}
	return s
}

func (s emitSet) allows(key string) bool {
	return s["all"] || s[key]
}

// RenderClient writes share links and notes filtered by cfg.Client.Emit.
func RenderClient(w io.Writer, st *state.State, cfg *config.Config) error {
	if st.Deploy == nil || (len(st.Deploy.Endpoints) == 0 && len(st.Deploy.Notes) == 0) {
		return fmt.Errorf("no deployment found — run `ngray server setup` first")
	}
	d := st.Deploy
	emit := buildEmitSet(cfg)

	for _, e := range d.Endpoints {
		key, ok := protoKey[e.Proto]
		if !ok || !emit.allows(key) {
			continue
		}
		uri := e.URI()
		if uri == "" {
			continue
		}
		fmt.Fprintf(w, "\n%-36s %s\n", e.Tag, uri)
	}

	// Notes first (extra Notes cores payload): these are short,
	// importable configs and must not be buried under the large sing-box JSON
	// dump that follows — otherwise mieru/extra Notes cores look "missing" in the list.
	for k, v := range d.Notes {
		nk, ok := noteKey[k]
		if ok && !emit.allows(nk) {
			continue
		}
		fmt.Fprintf(w, "\n── %s ──\n%s\n", k, v)
	}

	if emit.allows("singbox_json") {
		if b, err := links.SingboxJSON(d.Endpoints); err == nil {
			fmt.Fprintf(w, "\n── sing-box client JSON (or just import the sbJSON subscribe URL) ──\n%s\n", b)
		}
	}

	if emit.allows("subscription") {
		writeSubscribe(w, d)
	}
	return nil
}

// RenderSubscribe writes only the subscribe URLs — the concise summary shown
// after `ngray server setup`. The full per-config share links and the sing-box
// JSON dump stay behind `ngray client links`, so setup ends clean.
func RenderSubscribe(w io.Writer, st *state.State) error {
	if st.Deploy == nil || (st.Deploy.SubURL == "" && st.Deploy.SubDomainURL == "") {
		return fmt.Errorf("no subscription found — run `ngray server setup` first")
	}
	writeSubscribe(w, st.Deploy)
	fmt.Fprintf(w, "\nAll individual links + sing-box JSON:  ngray client links\n")
	return nil
}

// writeSubscribe prints the subscribe block: primary = direct-IP mixed link
// (0.9.6: everything client-facing lives on the IP; the CF domain ones are
// optional extras printed last). Shared by RenderClient and RenderSubscribe.
func writeSubscribe(w io.Writer, d *state.Deploy) {
	if d.SubURL == "" && d.SubHTTPURL == "" && d.SubMixedURL == "" {
		return
	}
	fmt.Fprintf(w, "\nSubscribe (add once — auto-updates all configs):\n")

	fmt.Fprintf(w, "\n  Primary — direct IP (mixed link = everything in one text file):\n")
	if d.SubMixedURL != "" {
		fmt.Fprintf(w, "    mixed   %s\n", d.SubMixedURL)
		fmt.Fprintf(w, "            → nekoray & most text clients: raw links + sing-box JSON, nothing encoded\n")
	}
	if d.SubHTTPURL != "" {
		fmt.Fprintf(w, "    HTTP    %s  (plain — always reachable, no TLS warning)\n", d.SubHTTPURL)
	}
	if d.SubURL != "" {
		fmt.Fprintf(w, "    HTTPS   %s  (self-signed → enable allow-insecure)\n", d.SubURL)
	}
	if d.SubSingboxURL != "" {
		fmt.Fprintf(w, "    sbJSON  %s\n", d.SubSingboxURL)
	}
	if d.SubCertFingerprint != "" {
		fmt.Fprintf(w, "    cert    SHA256:%s  (proxy clients pin this)\n", d.SubCertFingerprint)
	}

	// Optional extras: the same three formats over the Cloudflare domain.
	if d.SubDomainURL != "" || d.SubDomainMixedURL != "" || d.SubDomainSingboxURL != "" {
		fmt.Fprintf(w, "\n  Optional — via the Cloudflare domain (no port, trusted cert; only if your client handles it well):\n")
		if d.SubDomainMixedURL != "" {
			fmt.Fprintf(w, "    mixed   %s\n", d.SubDomainMixedURL)
		}
		if d.SubDomainSingboxURL != "" {
			fmt.Fprintf(w, "    sbJSON  %s\n", d.SubDomainSingboxURL)
		}
		if d.SubDomainURL != "" {
			fmt.Fprintf(w, "    base64  %s\n", d.SubDomainURL)
		}
	}
}

// SingboxJSON returns the sing-box client config for the last deployment.
func SingboxJSON(st *state.State) ([]byte, error) {
	if st.Deploy == nil {
		return nil, fmt.Errorf("no deployment found")
	}
	return links.SingboxJSON(st.Deploy.Endpoints)
}
