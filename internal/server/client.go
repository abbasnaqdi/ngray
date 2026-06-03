package server

import (
	"fmt"
	"io"

	"github.com/abbasnaqdi/ng-ray/internal/links"
	"github.com/abbasnaqdi/ng-ray/internal/state"
)

// RenderClient writes share links, optional QR codes, the sing-box JSON and the
// fixed subscribe URL from the last deployment snapshot.
func RenderClient(w io.Writer, st *state.State, qr bool) error {
	if st.Deploy == nil || len(st.Deploy.Endpoints) == 0 {
		return fmt.Errorf("no deployment found — run `ngray server setup` first")
	}
	d := st.Deploy
	for _, e := range d.Endpoints {
		fmt.Fprintf(w, "\n%s\n  %s\n", e.Tag, e.URI())
		if qr {
			fmt.Fprintln(w, links.QR(e.URI()))
		}
	}
	for k, v := range d.Notes {
		fmt.Fprintf(w, "\n%s:\n%s\n", k, v)
	}
	if d.SubURL != "" {
		fmt.Fprintf(w, "\nSubscribe link (add once in v2rayN/nekoray/husi — auto-updates):\n  %s\n", d.SubURL)
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
