// Package cores installs and configures each proxy core and reports the nginx
// routes and client endpoints it exposes. xray/shoes/sing-box sit behind
// nginx:443; hysteria2 is an independent core with its own listener.
package cores

import (
	"context"
	"fmt"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/links"
	"github.com/abbasnaqdi/ng-ray/internal/nginx"
	"github.com/abbasnaqdi/ng-ray/internal/ports"
	"github.com/abbasnaqdi/ng-ray/internal/state"
	"github.com/abbasnaqdi/ng-ray/internal/system"
)

// Plan carries the shared context a core needs to build its configuration.
type Plan struct {
	Cfg   *config.Config
	St    *state.State
	Alloc *ports.Allocator
	Sys   *system.Env
	IP    string
}

// Result is what a core contributes to the deployment.
type Result struct {
	Routes    []nginx.Route     // SNI -> backend (empty for independent cores)
	CDNWS     []nginx.WSRoute   // loopback WS backends exposed under the CDN domain
	Endpoints []links.Endpoint  // client share configs
	Notes     map[string]string // out-of-band info (e.g. mieru client JSON)
}

type Core interface {
	Name() string
	Repo() string    // owner/name on GitHub, for version + update checks
	Service() string // systemd unit
	Enabled(*config.Config) bool
	Installed(ctx context.Context, sys *system.Env) (string, bool)
	Install(ctx context.Context, sys *system.Env) error
	Build(p *Plan) (Result, error)
}

// Registry is the ordered set of supported cores.
func Registry() []Core {
	return []Core{Xray{}, Shoes{}, Singbox{}, Hysteria{}, Mieru{}}
}

// realityKeys returns (or generates and persists) the REALITY keypair for a core.
func realityKeys(st *state.State, key string) (priv, pub string, err error) {
	if kp, ok := st.Keys[key]; ok {
		return kp.Private, kp.Public, nil
	}
	priv, pub, err = system.GenReality()
	if err != nil {
		return "", "", err
	}
	st.Keys[key] = state.Keypair{Private: priv, Public: pub}
	return priv, pub, nil
}

func id(core, kind, sni string) string { return fmt.Sprintf("%s:%s:%s", core, kind, sni) }
