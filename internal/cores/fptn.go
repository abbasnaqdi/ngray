package cores

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/state"
	"github.com/abbasnaqdi/ng-ray/internal/system"
)

const fptnDir = "/etc/fptn"

// Fptn is an independent TUN VPN core; it emits a base64 connection token
// rather than a share link.
type Fptn struct{}

func (Fptn) Name() string                  { return "fptn" }
func (Fptn) Repo() string                  { return "fptn-project/fptn" }
func (Fptn) Service() string               { return "fptn-server" }
func (Fptn) Enabled(c *config.Config) bool { return c.Cores.Fptn.Enabled }

func (Fptn) Installed(ctx context.Context, sys *system.Env) (string, bool) {
	return detect(ctx, sys, "fptn-server")
}

func (Fptn) Install(ctx context.Context, sys *system.Env) error {
	ver, err := sys.LatestVersion(ctx, "fptn-project/fptn")
	if err != nil {
		return err
	}
	url := fmt.Sprintf("https://github.com/fptn-project/fptn/releases/download/v%s/fptn-server-%s-ubuntu%s-%s.deb",
		ver, ver, osReleaseID(), debArch(ctx, sys))
	tmp := filepath.Join(os.TempDir(), "fptn-server.deb")
	if err := sys.Download(ctx, url, tmp); err != nil {
		return err
	}
	if _, err := sys.Run(ctx, "dpkg", "-i", tmp); err != nil {
		// resolve dependencies and retry
		if e := sys.AptInstall(ctx, "-f"); e != nil {
			return err
		}
	}
	return os.MkdirAll(fptnDir, 0o755)
}

func (Fptn) Build(p *Plan) (Result, error) {
	f := p.Cfg.Cores.Fptn
	crt, key := fptnDir+"/server.crt", fptnDir+"/server.key"
	if err := p.Sys.GenSelfSigned(f.SNI, crt, key); err != nil {
		return Result{}, err
	}

	user := "vpnuser"
	pass := p.St.Secret("fptn|pw", func() state.Secret { return state.Secret{Password: state.NewPassword(16)} }).Password
	ctx := context.Background()
	_, _ = p.Sys.Run(ctx, "fptn-passwd", "--del-user", user, "--userfile", fptnDir+"/users.list")
	if _, err := p.Sys.Run(ctx, "bash", "-c",
		fmt.Sprintf("echo %q | fptn-passwd --add-user %s --bandwidth 100 --userfile %s/users.list", pass, user, fptnDir)); err != nil {
		return Result{}, err
	}

	iface := defaultIface(ctx, p.Sys)
	if err := p.Sys.WriteUnit("fptn-server.service", fptnUnit(iface, f.Port, f.SNI, p.IP)); err != nil {
		return Result{}, err
	}

	fp, err := certMD5(p.Sys, crt)
	if err != nil {
		return Result{}, err
	}
	token, err := fptnToken(p.IP, f.Port, user, pass, fp)
	if err != nil {
		return Result{}, err
	}
	return Result{Notes: map[string]string{"fptn-token": token}}, nil
}

func fptnToken(host string, port int, user, pass, md5fp string) (string, error) {
	tok := map[string]any{
		"version": 1, "service_name": "FptnServer", "username": user, "password": pass,
		"servers":               []map[string]any{{"name": "FptnServer", "host": host, "md5_fingerprint": md5fp, "port": port}},
		"censored_zone_servers": []any{},
	}
	b, err := json.Marshal(tok)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func fptnUnit(iface string, port int, sni, ip string) string {
	sniList := strings.Join([]string{sni, "rust-lang.org", "google.com", "gitlab.com",
		"vercel.com", "ubuntu.com", "python.org", "letsencrypt.org", "digikala.com",
		"cafebazaar.ir", "aparat.com", "chess.com"}, ",")
	exec := fmt.Sprintf("/usr/bin/fptn-server --server-crt %s/server.crt --server-key %s/server.key "+
		"--out-network-interface %s --server-port %d --userfile %s/users.list --mtu-size 1380 "+
		"--default-proxy-domain %s --allowed-sni-list %s --server-external-ips %s",
		fptnDir, fptnDir, iface, port, fptnDir, sni, sniList, ip)
	return unit("FPTN VPN Server", exec)
}

func certMD5(sys *system.Env, certPath string) (string, error) {
	b, err := sys.ReadFile(certPath)
	if err != nil {
		return "", err
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return "", fmt.Errorf("no PEM in %s", certPath)
	}
	sum := md5.Sum(blk.Bytes)
	var sb strings.Builder
	for _, x := range sum {
		fmt.Fprintf(&sb, "%02x", x)
	}
	return sb.String(), nil
}

func osReleaseID() string {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "24.04"
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "VERSION_ID=") {
			return strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), `"`)
		}
	}
	return "24.04"
}

func debArch(ctx context.Context, sys *system.Env) string {
	if out, err := sys.Run(ctx, "dpkg", "--print-architecture"); err == nil && out != "" {
		return out
	}
	return "amd64"
}

func defaultIface(ctx context.Context, sys *system.Env) string {
	out, err := sys.Run(ctx, "bash", "-c", "ip route get 8.8.8.8 | awk '{for(i=1;i<NF;i++) if($i==\"dev\") print $(i+1)}' | head -1")
	if err != nil || out == "" {
		return "eth0"
	}
	return out
}
