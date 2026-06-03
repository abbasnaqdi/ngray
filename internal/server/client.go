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
	"vless-vision": "vless",
	"vless-xhttp":  "vless",
	"anytls":       "anytls",
	"hysteria2":    "hysteria2",
	"tuic":         "tuic",
	"shadowtls":    "shadowtls",
}

// noteKey maps a Note key to the emit keyword that enables it.
var noteKey = map[string]string{
	"fptn-token":              "fptn",
	"mieru-client-config":     "mieru",
	"amneziawg-client-config": "amneziawg",
}

type emitSet map[string]bool

func buildEmitSet(cfg *config.Config) emitSet {
	s := make(emitSet)
	if cfg == nil {
		// no config — show everything
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

// RenderClient writes share links, QR codes and notes filtered by cfg.Client.Emit.
func RenderClient(w io.Writer, st *state.State, cfg *config.Config, qr bool) error {
	if st.Deploy == nil || (len(st.Deploy.Endpoints) == 0 && len(st.Deploy.Notes) == 0) {
		return fmt.Errorf("no deployment found — run `ngray server setup` first")
	}
	d := st.Deploy
	emit := buildEmitSet(cfg)
	showQR := qr && emit.allows("qr")

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
		if showQR {
			fmt.Fprintln(w, links.QR(uri))
		}
	}

	if emit.allows("singbox_json") {
		if b, err := links.SingboxJSON(d.Endpoints); err == nil {
			fmt.Fprintf(w, "\n── sing-box client JSON ──\n%s\n", b)
		}
	}

	// Notes: fptn token, mieru config, AmneziaWG conf
	for k, v := range d.Notes {
		nk, ok := noteKey[k]
		if ok && !emit.allows(nk) {
			continue
		}
		fmt.Fprintf(w, "\n── %s ──\n%s\n", k, v)
	}

	if d.SubURL != "" && emit.allows("subscription") {
		fmt.Fprintf(w, "\nSubscribe (add once — auto-updates all configs):\n  %s\n", d.SubURL)
		if showQR {
			fmt.Fprintln(w, links.QR(d.SubURL))
		}
	}
	return nil
}

// SingboxJSON returns the sing-box client config for the last deployment.
func SingboxJSON(st *state.State) ([]byte, error) {
	if st.Deploy == nil {
		return nil, fmt.Errorf("no deployment found")
	}
	return links.SingboxJSON(st.Deploy.Endpoints)
}
