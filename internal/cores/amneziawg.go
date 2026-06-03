package cores

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/big"
	"os"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/state"
	"github.com/abbasnaqdi/ng-ray/internal/system"
)

const awgDir = "/etc/amnezia/amneziawg"

// AmneziaWG wraps WireGuard packets in random junk headers and salt, making
// WireGuard traffic unidentifiable to DPI (especially effective in Russia).
// Uses awg-quick as the interface manager (amneziawg-tools package).
type AmneziaWG struct{}

func (AmneziaWG) Name() string                  { return "amneziawg" }
func (AmneziaWG) Repo() string                  { return "amnezia-vpn/amneziawg-tools" }
func (AmneziaWG) Service() string               { return "awg-quick@wg0" }
func (AmneziaWG) Enabled(c *config.Config) bool { return c.Cores.AmneziaWG.Enabled }

func (AmneziaWG) Installed(_ context.Context, sys *system.Env) (string, bool) {
	if sys.Which("awg") {
		return "installed", true
	}
	return "", false
}

// Install adds the Amnezia PPA and installs amneziawg + wireguard-tools.
func (AmneziaWG) Install(ctx context.Context, sys *system.Env) error {
	if err := sys.AptInstall(ctx, "software-properties-common"); err != nil {
		return err
	}
	if _, err := sys.Run(ctx, "add-apt-repository", "-y", "ppa:amnezia-vpn/amneziawg"); err != nil {
		return err
	}
	if err := sys.AptUpdate(ctx); err != nil {
		return err
	}
	if err := sys.AptInstall(ctx, "amneziawg", "wireguard-tools"); err != nil {
		return err
	}
	return os.MkdirAll(awgDir, 0o700)
}

func (AmneziaWG) Build(p *Plan) (Result, error) {
	a := p.Cfg.Cores.AmneziaWG

	// Server keypair
	srvPriv, srvPub, err := serverWGKeys(p.St)
	if err != nil {
		return Result{}, err
	}
	// Client keypair (generated once, persisted)
	cliPriv, cliPub, err := clientWGKeys(p.St)
	if err != nil {
		return Result{}, err
	}
	// Obfuscation params (stable across runs)
	ob := awgObfs(p.St)

	serverConf := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = 10.66.66.1/24
ListenPort = %d
Jc = %d
Jmin = %d
Jmax = %d
S1 = %d
S2 = %d
H1 = %d
H2 = %d
H3 = %d
H4 = %d
PostUp   = iptables -A FORWARD -i wg0 -j ACCEPT; iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE
PostDown = iptables -D FORWARD -i wg0 -j ACCEPT; iptables -t nat -D POSTROUTING -o eth0 -j MASQUERADE

[Peer]
PublicKey = %s
AllowedIPs = 10.66.66.2/32
`,
		srvPriv, a.Port,
		ob.Jc, ob.Jmin, ob.Jmax, ob.S1, ob.S2,
		ob.H1, ob.H2, ob.H3, ob.H4,
		cliPub,
	)

	clientConf := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = 10.66.66.2/24
DNS = 1.1.1.1, 8.8.8.8
Jc = %d
Jmin = %d
Jmax = %d
S1 = %d
S2 = %d
H1 = %d
H2 = %d
H3 = %d
H4 = %d

[Peer]
PublicKey = %s
Endpoint = %s:%d
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25
`,
		cliPriv,
		ob.Jc, ob.Jmin, ob.Jmax, ob.S1, ob.S2,
		ob.H1, ob.H2, ob.H3, ob.H4,
		srvPub, p.IP, a.Port,
	)

	if err := p.Sys.WriteFile(awgDir+"/wg0.conf", []byte(serverConf), 0o600); err != nil {
		return Result{}, err
	}
	ctx := context.Background()
	if err := p.Sys.EnableNow(ctx, "awg-quick@wg0"); err != nil {
		_ = p.Sys.Restart(ctx, "awg-quick@wg0")
	}

	return Result{Notes: map[string]string{
		"amneziawg-client-config": clientConf,
	}}, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func serverWGKeys(st *state.State) (priv, pub string, err error) {
	if kp, ok := st.Keys["awg|server"]; ok {
		return kp.Private, kp.Public, nil
	}
	priv, pub, err = system.GenWGKeyPair()
	if err != nil {
		return
	}
	st.Keys["awg|server"] = state.Keypair{Private: priv, Public: pub}
	return
}

func clientWGKeys(st *state.State) (priv, pub string, err error) {
	if kp, ok := st.Keys["awg|client"]; ok {
		return kp.Private, kp.Public, nil
	}
	priv, pub, err = system.GenWGKeyPair()
	if err != nil {
		return
	}
	st.Keys["awg|client"] = state.Keypair{Private: priv, Public: pub}
	return
}

type awgParams struct {
	Jc, Jmin, Jmax, S1, S2 int
	H1, H2, H3, H4         uint32
}

func awgObfs(st *state.State) awgParams {
	// Persist params in state so client and server always agree.
	sec := st.Secret("awg|obfs", func() state.Secret {
		return state.Secret{Password: randAWGParams()}
	})
	var p awgParams
	fmt.Sscanf(sec.Password,
		"%d %d %d %d %d %d %d %d %d",
		&p.Jc, &p.Jmin, &p.Jmax, &p.S1, &p.S2,
		&p.H1, &p.H2, &p.H3, &p.H4)
	return p
}

func randAWGParams() string {
	jc := randInt(1, 128)
	jmin := randInt(15, 1280)
	jmax := jmin + randInt(1, 1280-jmin+1)
	s1 := randInt(15, 150)
	s2 := randInt(15, 150)
	h1 := randU32()
	h2 := randU32()
	h3 := randU32()
	h4 := randU32()
	return fmt.Sprintf("%d %d %d %d %d %d %d %d %d", jc, jmin, jmax, s1, s2, h1, h2, h3, h4)
}

func randInt(lo, hi int) int {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(hi-lo+1)))
	return lo + int(n.Int64())
}

func randU32() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return binary.BigEndian.Uint32(b[:])
}
