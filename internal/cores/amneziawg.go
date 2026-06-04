package cores

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"path/filepath"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/links"
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

// Install installs AmneziaWG without a PPA.
//
//   - Userspace tools (awg, awg-quick): ubuntu zip from amnezia-vpn/amneziawg-tools
//   - Kernel module: built via DKMS from the amnezia-vpn/amneziawg source archive
func (AmneziaWG) Install(ctx context.Context, sys *system.Env) error {
	kver, _ := sys.Run(ctx, "uname", "-r")
	if err := sys.AptInstall(ctx, "dkms", "build-essential", "linux-headers-"+kver, "wireguard-tools"); err != nil {
		return err
	}
	if err := awgInstallTools(ctx, sys); err != nil {
		return err
	}
	if err := awgInstallKernel(ctx, sys); err != nil {
		return err
	}
	if err := os.MkdirAll(awgDir, 0o700); err != nil {
		return err
	}
	if err := awgWriteUnit(sys); err != nil {
		return err
	}
	return sys.DaemonReload(ctx)
}

// awgWriteUnit installs the awg-quick@.service systemd template. The
// amneziawg-tools zip ships only the awg/awg-quick binaries (no unit), so
// without this `systemctl start awg-quick@wg0` fails with "unit not found".
// awg-quick reads /etc/amnezia/amneziawg/<iface>.conf.
func awgWriteUnit(sys *system.Env) error {
	return sys.WriteUnit("awg-quick@.service", `[Unit]
Description=AmneziaWG via awg-quick(8) for %I
After=network-online.target nss-lookup.target
Wants=network-online.target nss-lookup.target
Documentation=man:awg-quick(8)

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/bin/awg-quick up %i
ExecStop=/usr/local/bin/awg-quick down %i
Environment=WG_ENDPOINT_RESOLUTION_RETRIES=infinity
WorkingDirectory=/etc/amnezia/amneziawg

[Install]
WantedBy=multi-user.target
`)
}

// awgInstallTools extracts awg and awg-quick from the ubuntu zip release.
func awgInstallTools(ctx context.Context, sys *system.Env) error {
	url, err := sys.AssetURL(ctx, "amnezia-vpn/amneziawg-tools", `ubuntu.*amneziawg-tools\.zip`)
	if err != nil {
		return fmt.Errorf("find amneziawg-tools release: %w", err)
	}
	tmp := filepath.Join(os.TempDir(), "amneziawg-tools.zip")
	if err := sys.Download(ctx, url, tmp); err != nil {
		return err
	}
	for _, bin := range []string{"awg", "awg-quick"} {
		if err := system.UnzipFile(tmp, bin, "/usr/local/bin/"+bin); err != nil {
			return fmt.Errorf("extract %s: %w", bin, err)
		}
	}
	return nil
}

// awgInstallKernel builds the amneziawg kernel module via DKMS from source.
// amnezia-vpn/amneziawg has no binary releases; we fetch the source archive
// and drive DKMS to compile it against the running kernel.
func awgInstallKernel(ctx context.Context, sys *system.Env) error {
	const (
		srcURL  = "https://github.com/amnezia-vpn/amneziawg-linux-kernel-module/archive/refs/heads/master.tar.gz"
		modVer  = "ngray1"
		dkmsDir = "/usr/src/amneziawg-" + modVer
	)
	tmp := filepath.Join(os.TempDir(), "amneziawg-src.tar.gz")
	if err := sys.Download(ctx, srcURL, tmp); err != nil {
		return fmt.Errorf("download amneziawg source: %w", err)
	}
	if err := os.MkdirAll(dkmsDir, 0o755); err != nil {
		return err
	}
	// GitHub archives have a top-level directory prefix; strip it.
	if out, err := sys.Run(ctx, "tar", "-xzf", tmp, "--strip-components=1", "-C", dkmsDir); err != nil {
		return fmt.Errorf("extract amneziawg source: %w — %s", err, out)
	}

	// Write dkms.conf. The module source lives in the src/ sub-directory.
	dkmsConf := `PACKAGE_NAME="amneziawg"
PACKAGE_VERSION="` + modVer + `"
BUILT_MODULE_NAME[0]="amneziawg"
BUILT_MODULE_LOCATION[0]="src"
DEST_MODULE_LOCATION[0]="/kernel/net/wireguard/"
AUTOINSTALL="yes"
MAKE[0]="make -C src KERNELDIR=/lib/modules/${kernelver}/build"
CLEAN="make -C src KERNELDIR=/lib/modules/${kernelver}/build clean"
`
	if err := os.WriteFile(filepath.Join(dkmsDir, "dkms.conf"), []byte(dkmsConf), 0o644); err != nil {
		return err
	}

	kver, _ := sys.Run(ctx, "uname", "-r")
	_, _ = sys.Run(ctx, "dkms", "add", "amneziawg/"+modVer) // idempotent
	if out, err := sys.Run(ctx, "dkms", "build", "amneziawg/"+modVer, "-k", kver); err != nil {
		return fmt.Errorf("dkms build amneziawg: %w — %s", err, out)
	}
	if out, err := sys.Run(ctx, "dkms", "install", "amneziawg/"+modVer, "-k", kver); err != nil {
		return fmt.Errorf("dkms install amneziawg: %w — %s", err, out)
	}
	return nil
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
	ctx := context.Background()
	iface := defaultIface(ctx, p.Sys)

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
PostUp   = iptables -A FORWARD -i wg0 -j ACCEPT; iptables -t nat -A POSTROUTING -o %s -j MASQUERADE
PostDown = iptables -D FORWARD -i wg0 -j ACCEPT; iptables -t nat -D POSTROUTING -o %s -j MASQUERADE

[Peer]
PublicKey = %s
AllowedIPs = 10.66.66.2/32
`,
		srvPriv, a.Port,
		ob.Jc, ob.Jmin, ob.Jmax, ob.S1, ob.S2,
		ob.H1, ob.H2, ob.H3, ob.H4,
		iface, iface,
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
	// Refresh the unit every deploy so servers installed before the template fix
	// gain awg-quick@.service.
	if err := awgWriteUnit(p.Sys); err == nil {
		_ = p.Sys.DaemonReload(ctx)
	}

	// The interface needs the amneziawg kernel module. On servers where the awg
	// binary pre-existed, Install (which DKMS-builds the module) was skipped, so
	// the module can be absent. Don't pretend the interface is up — surface a
	// clear repair pointer instead of looping awg-quick on a missing device.
	if !p.Sys.OK(ctx, "modprobe", "amneziawg") {
		return Result{Notes: map[string]string{
			"amneziawg": "kernel module not built for this kernel — run `bash scripts/awg-build.sh` (needs matching linux-headers). Interface not started.",
		}}, nil
	}
	if err := p.Sys.EnableNow(ctx, "awg-quick@wg0"); err != nil {
		_ = p.Sys.Restart(ctx, "awg-quick@wg0")
	}

	// sing-box wireguard outbound with AmneziaWG junk params (NekoBox/husi
	// import it via the JSON bundle). Field names mirror the wg0.conf params.
	ep := links.Endpoint{
		Tag:   "amneziawg",
		Proto: "amneziawg",
		Raw: map[string]any{
			"type":        "wireguard",
			"address":     []string{"10.66.66.2/32"},
			"private_key": cliPriv,
			"peers": []map[string]any{{
				"address":                       p.IP,
				"port":                          a.Port,
				"public_key":                    srvPub,
				"allowed_ips":                   []string{"0.0.0.0/0", "::/0"},
				"persistent_keepalive_interval": 25,
			}},
			"mtu": 1420,
			"jc":  ob.Jc, "jmin": ob.Jmin, "jmax": ob.Jmax,
			"s1": ob.S1, "s2": ob.S2,
			"h1": ob.H1, "h2": ob.H2, "h3": ob.H3, "h4": ob.H4,
		},
	}
	return Result{
		Endpoints: []links.Endpoint{ep},
		Notes:     map[string]string{"amneziawg-client-config": clientConf},
	}, nil
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
