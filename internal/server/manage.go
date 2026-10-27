package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/abbasnaqdi/ng-ray/internal/config"
	"github.com/abbasnaqdi/ng-ray/internal/cores"
	"github.com/abbasnaqdi/ng-ray/internal/domain"
	"github.com/abbasnaqdi/ng-ray/internal/firewall"
	"github.com/abbasnaqdi/ng-ray/internal/nginx"
	"github.com/abbasnaqdi/ng-ray/internal/state"
	"github.com/abbasnaqdi/ng-ray/internal/sub"
	"github.com/abbasnaqdi/ng-ray/internal/system"
)

// managedPaths are the files ngray owns and may back up + rebuild. Anything
// outside this list (x3-ui panels, custom services) is never touched.
var managedPaths = []string{
	nginx.ConfPath,
	"/etc/shoes/server.yaml",
	"/etc/sing-box/config.json",
	"/usr/local/etc/xray/config.json",
	"/etc/hysteria/config.yaml",
	sub.BundleFile,
	sub.SingboxFile,
	sub.CertFile,
	sub.KeyFile,
}

type Status string

const (
	Pass Status = "PASS"
	Warn Status = "WARN"
	Fail Status = "FAIL"
)

type Check struct {
	Name   string
	Status Status
	Detail string
	Fix    string
}

// subProbeURLs describes every subscription transport that must answer 200
// (server HTTP:80, self-signed HTTPS, and the CF-fronted domain when set).
func subProbeURLs(cfg *config.Config, st *state.State, ip string) []struct {
	name string
	url  string
	tls  bool
} {
	token := st.EnsureSubToken()
	httpsPort := cfg.Subscription.HTTPSPort
	if httpsPort <= 0 {
		httpsPort = sub.DefaultHTTPSPort
	}
	var out []struct {
		name string
		url  string
		tls  bool
	}
	if cfg.Subscription.Enabled {
		// Direct-IP URLs are probed over loopback — the same nginx vhosts,
		// immune to hairpin-NAT and local firewall quirks. The Cloudflare
		// fronted URL is probed over the real edge path.
		out = append(out, struct {
			name string
			url  string
			tls  bool
		}{"sub HTTP :80 (loopback)", fmt.Sprintf("http://127.0.0.1/s/%s", token), false})
		out = append(out, struct {
			name string
			url  string
			tls  bool
		}{fmt.Sprintf("sub HTTPS :%d (loopback)", httpsPort),
			fmt.Sprintf("https://127.0.0.1:%d/s/%s", httpsPort, token), true})
		// The mixed text route (/sm/) guards its own location + file.
		out = append(out, struct {
			name string
			url  string
			tls  bool
		}{"sub mixed :80 (loopback)", fmt.Sprintf("http://127.0.0.1/sm/%s", token), false})
		if cdn := cfg.Cores.Xray.CDN; cdn != nil && cdn.Enabled && cdn.Domain != "" {
			out = append(out, struct {
				name string
				url  string
				tls  bool
			}{"sub via Cloudflare", fmt.Sprintf("https://%s/s/%s", cdn.XHTTPDomain(), token), false})
		}
	}
	return out
}

// ProbeSub fetches every subscription URL and checks the HTTP status is 200,
// probing all transports CONCURRENTLY (the slow one no longer delays the rest).
// Statuses other than 200 carry a targeted fix (404 = nginx worker cannot read
// the bundle; connection refused = stale nginx config).
func ProbeSub(ctx context.Context, cfg *config.Config, st *state.State, ip string) []Check {
	urls := subProbeURLs(cfg, st, ip)
	out := make([]Check, len(urls))
	var wg sync.WaitGroup
	client := &http.Client{Timeout: 6 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // self-signed fallback
	}}
	check := func(ctx context.Context, p struct {
		name string
		url  string
		tls  bool
	}) Check {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, nil)
		if err != nil {
			return Check{"sub " + p.name, Warn, "unbuildable URL", ""}
		}
		req.Header.Set("User-Agent", "ngray-selfcheck/1")
		resp, err := client.Do(req)
		if err != nil {
			return Check{"sub " + p.name, Fail, "unreachable: " + errShort(err),
				"ngray server setup (rebuilds nginx.conf + restarts nginx)"}
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
		resp.Body.Close()
		switch {
		case resp.StatusCode == 200:
			return Check{"sub " + p.name, Pass, "200 ✓", ""}
		case resp.StatusCode == 404:
			return Check{"sub " + p.name, Fail, "404 — the bundle file is not being served",
				"chmod 755 /etc/ngray  ·  nginx -t && systemctl restart nginx  ·  rerun server setup"}
		default:
			return Check{"sub " + p.name, Warn,
				fmt.Sprintf("HTTP %d — unexpected", resp.StatusCode), ""}
		}
	}
	for i, p := range urls {
		wg.Add(1)
		go func(i int, p struct {
			name string
			url  string
			tls  bool
		}) {
			defer wg.Done()
			out[i] = check(ctx, p)
		}(i, p)
	}
	wg.Wait()
	return out
}

func errShort(err error) string {
	s := err.Error()
	if i := strings.Index(s, ": "); i >= 0 && strings.Count(s, ": ") > 1 {
		s = s[strings.LastIndex(s, ": ")+2:]
	}
	return s
}

// wsPathProbe performs a REAL client handshake against the nginx TLS server:
// dials 127.0.0.1:muxPort with the CDN SNI (the same path CF takes), sends an
// HTTP/1.1 WebSocket Upgrade for one twin path, and expects HTTP 101. This
// catches the "domain configs don't even ping" class: a broken location, a
// missing Upgrade header, or a dead twin backend surface right here.
func wsPathProbe(ctx context.Context, log func(string, ...any), muxPort int, sni, path string) {
	addr := fmt.Sprintf("127.0.0.1:%d", muxPort)
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		log("  ✗ ws twin probe %s: cannot connect: %v", sni, err)
		return
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(6 * time.Second))
	tlsConn := tls.Client(conn, &tls.Config{ServerName: sni, InsecureSkipVerify: true}) // origin cert is self-issued; CF already validated it
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		log("  ✗ ws twin probe %s: TLS handshake: %v", sni, err)
		return
	}
	req := "GET " + path + " HTTP/1.1\r\nHost: " + sni + "\r\nUpgrade: websocket\r\n" +
		"Connection: Upgrade\r\nSec-WebSocket-Key: c3dhbnRoZWxhc3RiYXJzCQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := tlsConn.Write([]byte(req)); err != nil {
		log("  ✗ ws twin probe %s: write: %v", sni, err)
		return
	}
	resp := make([]byte, 128)
	n, err := tlsConn.Read(resp)
	if err != nil || n == 0 {
		log("  ✗ ws twin probe %s: no response — the nginx location or Upgrade header is broken", sni)
		return
	}
	line := strings.Fields(string(resp[:n]))
	if len(line) >= 2 && line[1] == "101" {
		log("  ✓ ws twin probe %s: 101 Switching Protocols", sni)
		return
	}
	log("  ! ws twin probe %s: %s — rerun setup / check the xray twin inbound", sni, strings.Join(line[:min(len(line), 3)], " "))
}

// min returns the smaller of two ints (Go <1.21 guard not needed but cheap).
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// CheckSub is the stringy twin of ProbeSub used inline during server setup.
// It also runs the live ws-twin handshake when the mixed bundle carries a
// /t/-routed link, so a broken domain path surfaces in the same run.
func CheckSub(ctx context.Context, log func(string, ...any), token string, httpsPort int) {
	client := &http.Client{Timeout: 6 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}
	check := func(name, url string) bool {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			log("subscription self-check %s: %v", name, err)
			return false
		}
		resp, err := client.Do(req)
		if err != nil {
			log("subscription self-check %s: UNREACHABLE (%v) — fix: nginx -t && systemctl restart nginx", name, errShort(err))
			return false
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
		resp.Body.Close()
		if resp.StatusCode != 200 {
			log("subscription self-check %s: HTTP %d — fix: chmod 755 /etc/ngray && systemctl restart nginx", name, resp.StatusCode)
			return false
		}
		log("subscription self-check %s: 200 ✓", name)
		return true
	}
	check("HTTP :80", fmt.Sprintf("http://127.0.0.1/s/%s", token))
	check("mixed :80", fmt.Sprintf("http://127.0.0.1/sm/%s", token))
	check(fmt.Sprintf("HTTPS :%d", httpsPort), fmt.Sprintf("https://127.0.0.1:%d/s/%s", httpsPort, token))
	// Live ws-twin handshake over the real 443-mux path (what CF→origin does).
	if dt := deployTwinProbe(); dt.sni != "" {
		wsPathProbe(ctx, log, dt.port, dt.sni, dt.path)
	}
}

// deployTwinProbe yields the FIRST ws-twin route of the current state, for the
// loopback handshake probe (empty when no deployment or no twins).
func deployTwinProbe() (p struct {
	port int
	sni  string
	path string
}) {
	st, err := state.Load(state.Path())
	if err != nil || st.Deploy == nil {
		return p
	}
	for _, e := range st.Deploy.Endpoints {
		if e.Proto == "vless-ws-tls" && strings.HasPrefix(e.Path, "/t/") {
			return struct {
				port int
				sni  string
				path string
			}{e.Port, e.SNI, e.Path}
		}
	}
	return p
}

// certExpiryCheck reports days remaining on a PEM certificate file.
func certExpiryCheck(cfg *config.Config) []Check {
	var out []Check
	type pair struct {
		name string
		path string
	}
	var pairs []pair
	if cdn := cfg.Cores.Xray.CDN; cdn != nil && cdn.Enabled && cdn.Domain != "" {
		pairs = append(pairs, pair{"cert origin (CF)", cdn.CertFile})
	}
	pairs = append(pairs, pair{"cert sub (self-signed)", sub.CertFile})
	for _, p := range pairs {
		b, err := os.ReadFile(p.path)
		if err != nil {
			out = append(out, Check{p.name, Warn, "no cert file at " + p.path, ""})
			continue
		}
		blk, _ := pem.Decode(b)
		if blk == nil {
			out = append(out, Check{p.name, Warn, "unreadable PEM", ""})
			continue
		}
		crt, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			out = append(out, Check{p.name, Warn, "unparseable certificate", ""})
			continue
		}
		days := int(time.Until(crt.NotAfter).Hours() / 24)
		st := Pass
		if days < 30 {
			st = Warn
		}
		out = append(out, Check{p.name, st,
			fmt.Sprintf("expires in %d days (%s)", days, crt.NotAfter.Format("2006-01-02")), ""})
	}
	return out
}

// Doctor diagnoses every enabled core and the shared plumbing, returning an
// actionable report (no auto-repair here; see DoctorFix).
func Doctor(ctx context.Context, cfg *config.Config, st *state.State) []Check {
	sys := system.New()
	var out []Check

	// nginx + ownership
	if sys.Which("nginx") {
		ok := sys.OK(ctx, "nginx", "-t")
		owned := false
		if b, err := sys.ReadFile(nginx.ConfPath); err == nil {
			owned = strings.HasPrefix(string(b), nginx.Marker)
		}
		active := sys.IsActive(ctx, "nginx")
		switch {
		case !ok:
			out = append(out, Check{"nginx config", Fail, "nginx -t failed", "ngray server setup"})
		case !owned:
			out = append(out, Check{"nginx config", Warn, "nginx.conf not ngray-managed", "ngray server setup (backs up + rebuilds)"})
		default:
			detail := "valid + ngray-managed"
			if !active {
				detail += " · service NOT active"
			}
			cst := Pass
			if !active {
				cst = Fail
			}
			out = append(out, Check{"nginx", cst, detail, "systemctl restart nginx"})
		}
	} else {
		out = append(out, Check{"nginx", Fail, "not installed", "ngray server setup"})
	}

	// The three slow groups run CONCURRENTLY: core versions (exec per core),
	// live subscription probes (HTTP round trips), and the domain check
	// (DNS + edge + origin TLS). Output order stays deterministic.
	coreChecks := make([]Check, len(cores.Registry()))
	probeChecks := []Check{}
	domainChecks := []Check{}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		wg2 := sync.WaitGroup{}
		for i, core := range cores.Registry() {
			if !core.Enabled(cfg) {
				continue
			}
			wg2.Add(1)
			go func(i int, core cores.Core) {
				defer wg2.Done()
				coreChecks[i] = coreCheck(ctx, sys, core, cfg)
			}(i, core)
		}
		wg2.Wait()
	}()
	go func() {
		defer wg.Done()
		if st == nil {
			return
		}
		if sys.Exists(sub.BundleFile) {
			probeChecks = append(probeChecks, Check{"subscription bundle", Pass, "file present", ""})
		} else {
			probeChecks = append(probeChecks, Check{"subscription bundle", Fail, sub.BundleFile + " missing", "ngray server setup"})
		}
		ip := st.IP
		if ip == "" {
			ip = "127.0.0.1"
		}
		probeChecks = append(probeChecks, ProbeSub(ctx, cfg, st, ip)...)
	}()
	go func() {
		defer wg.Done()
		domainChecks = domainCheckChecks(ctx, cfg, st)
	}()
	wg.Wait()
	for _, c := range coreChecks {
		if c.Name != "" {
			out = append(out, c)
		}
	}
	out = append(out, probeChecks...)
	out = append(out, domainChecks...)

	out = append(out, certExpiryCheck(cfg)...)

	// BBR
	if cc, _ := sys.Run(ctx, "sysctl", "-n", "net.ipv4.tcp_congestion_control"); cc == "bbr" {
		out = append(out, Check{"bbr", Pass, "active", ""})
	} else if cfg.Server.EnableBBR {
		out = append(out, Check{"bbr", Warn, "not active (" + cc + ")", "ngray server setup"})
	}

	for _, f := range DetectForeign(ctx, sys) {
		out = append(out, Check{"foreign", Warn, f, "left untouched by ngray"})
	}
	return out
}

// DoctorFix applies the safe repairs doctor usually identifies (bundle
// unreadable by the nginx worker, config not reloaded) and re-probes.
func DoctorFix(ctx context.Context, cfg *config.Config, st *state.State, logf func(string, ...any)) []Check {
	sys := system.New()
	if logf == nil {
		logf = func(string, ...any) {}
	}
	dir := filepath.Dir(sub.BundleFile)
	if info, err := os.Stat(dir); err == nil && info.IsDir() && info.Mode().Perm()&0o055 != 0o055 {
		if err := os.Chmod(dir, 0o755); err == nil {
			logf("fixed: chmod 755 %s (nginx worker can read the bundle now)", dir)
		}
	}
	if sys.Which("nginx") {
		_, _ = sys.Run(ctx, "systemctl", "restart", "nginx")
	}
	ip := ""
	if st != nil {
		ip = st.IP
	}
	if ip == "" {
		ip = "127.0.0.1"
	}
	return ProbeSub(ctx, cfg, st, ip)
}

// DetectForeign reports other proxy panels present so the operator knows ngray
// will not modify them.
func DetectForeign(ctx context.Context, sys *system.Env) []string {
	var found []string
	for _, svc := range []string{"x-ui", "3x-ui", "x3-ui", "hiddify", "marzban"} {
		if sys.IsActive(ctx, svc) {
			found = append(found, svc+" service active")
		}
	}
	return found
}

type ManageOptions struct {
	StatePath string
	Confirm   func(string) bool
	Log       func(string, ...any)
}

func (o ManageOptions) log(f string, a ...any) {
	if o.Log != nil {
		o.Log(f, a...)
	}
}

// Clean backs up and removes ngray-owned config files and stops its services,
// leaving core binaries and foreign panels intact.
func Clean(ctx context.Context, o ManageOptions) error {
	sys := system.New()
	backup := fmt.Sprintf("/etc/ngray/backup/%s", time.Now().Format("20060102-150405"))

	for _, core := range cores.Registry() {
		_, _ = sys.Run(ctx, "systemctl", "stop", core.Service())
	}
	for _, p := range managedPaths {
		if !sys.Exists(p) {
			continue
		}
		if err := sys.CopyFile(p, backup+p); err == nil {
			o.log("backed up %s", p)
		}
		_ = sys.Remove(p)
	}
	o.log("cleaned ngray configs (backup: %s)", backup)
	return nil
}

// Remove fully uninstalls ngray's footprint: stop+disable services, remove its
// configs and firewall chain. Core binaries are removed only on confirm.
func Remove(ctx context.Context, o ManageOptions) error {
	sys := system.New()
	if err := Clean(ctx, o); err != nil {
		return err
	}
	for _, core := range cores.Registry() {
		_, _ = sys.Run(ctx, "systemctl", "disable", core.Service())
		_ = sys.Remove("/etc/systemd/system/" + core.Service() + ".service")
	}
	_, _ = sys.Run(ctx, "systemctl", "daemon-reload")
	firewall.New(sys).Clear(ctx)
	o.log("removed ngray services and firewall chain")
	if o.Confirm != nil && o.Confirm("also remove core binaries (shoes/xray/sing-box/hysteria)?") {
		for _, p := range []string{"/usr/local/bin/shoes", "/usr/local/bin/xray", "/usr/bin/sing-box", "/usr/local/bin/hysteria"} {
			_ = sys.Remove(p)
		}
		o.log("removed core binaries")
	}
	return nil
}

// CleanupOptions is the granular rollback surfaced by the TUI "Cleanup"
// checklist. Each action is independent; nothing is implied.
type CleanupOptions struct {
	RestoreNginx   bool // copy the latest pre-ngray nginx.conf backup over the managed file + reload
	StopServices   bool // stop + disable every ngray core service
	RemoveConfigs  bool // managed config files (backed up first)
	ClearFirewall  bool // flush the NGRAY chain
	RemoveState    bool // config.yml, proxy-state.json, sub certs/bundles, cf_token, SNI pool copy
	RemoveBinaries bool // core binaries installed by ngray
}

// Cleanup executes exactly the selected rollbacks, backing up before deleting.
// Skipped (unticked) items are never touched; effects are logged so the run is
// auditable.
func Cleanup(ctx context.Context, o ManageOptions, opt CleanupOptions) error {
	sys := system.New()
	backup := "/etc/ngray/backup/" + time.Now().Format("20060102-150405")

	if opt.RestoreNginx {
		// The managed config is only restored when a foreign (pre-ngray)
		// snapshot exists — never truncate the current file with nothing.
		if best := latestNginxBackup(sys); best != "" {
			if err := sys.CopyFile(best, nginx.ConfPath); err != nil {
				o.log("restore nginx.conf: %v", err)
			} else {
				o.log("restored nginx.conf from %s", best)
				_, _ = sys.Run(ctx, "systemctl", "restart", "nginx")
			}
		} else {
			o.log("no nginx backup found — skipped restore")
		}
	}

	if opt.StopServices {
		for _, core := range cores.Registry() {
			_, _ = sys.Run(ctx, "systemctl", "stop", core.Service())
			_, _ = sys.Run(ctx, "systemctl", "disable", core.Service())
			_ = sys.Remove("/etc/systemd/system/" + core.Service() + ".service")
		}
		_, _ = sys.Run(ctx, "systemctl", "daemon-reload")
		o.log("stopped and disabled ngray core services")
	}

	if opt.RemoveConfigs {
		kept := 0
		for _, p := range managedPaths {
			if !sys.Exists(p) || p == nginx.ConfPath && opt.RestoreNginx {
				continue // restored in place; do not delete the fresh copy
			}
			if err := sys.CopyFile(p, backup+p); err == nil {
				kept++
			}
			_ = sys.Remove(p)
		}
		_ = kept
		o.log("removed ngray-managed configs (backup: %s)", backup)
	}

	if opt.ClearFirewall {
		firewall.New(sys).Clear(ctx)
		o.log("cleared the ngray firewall chain")
	}

	if opt.RemoveState {
		for _, p := range []string{
			config.Path(), filepath.Dir(config.Path()) + "/" + "proxy-state.json",
			"/etc/ngray/cf_token", "/etc/ngray",
		} {
			_ = sys.Remove(p)
		}
		o.log("removed ngray state/config/token")
	}

	if opt.RemoveBinaries {
		for _, p := range []string{
			"/usr/local/bin/shoes", "/usr/local/bin/xray", "/usr/bin/sing-box",
			"/usr/local/bin/hysteria", "/usr/local/bin/mita",
		} {
			if sys.Exists(p) && sys.Remove(p) == nil {
				o.log("removed %s", p)
			}
		}
	}
	return nil
}

// latestNginxBackup finds the newest foreign nginx.conf snapshot across the
// backup timestamp dirs, or "" when none exists.
func latestNginxBackup(sys *system.Env) string {
	entries, err := os.ReadDir("/etc/ngray/backup")
	if err != nil {
		return ""
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cand := filepath.Join("/etc/ngray/backup", e.Name(), "nginx.conf")
		if sys.Exists(cand) {
			return cand
		}
	}
	return ""
}

// coreCheck renders one provider's Doctor line (installed + active states).
func coreCheck(ctx context.Context, sys *system.Env, core cores.Core, cfg *config.Config) Check {
	if !core.Enabled(cfg) {
		return Check{}
	}
	ver, ok := core.Installed(ctx, sys)
	if !ok {
		return Check{core.Name(), Fail, "not installed", "ngray server setup"}
	}
	if sys.IsActive(ctx, core.Service()) {
		return Check{core.Name(), Pass, "active " + ver, ""}
	}
	return Check{core.Name(), Fail, "installed " + ver + " but service inactive",
		"journalctl -u " + core.Service() + " -n50  ·  systemctl restart " + core.Service()}
}

// domainCheckChecks reaches the CDN domain (DNS → CF edge → origin TLS),
// no token needed.
func domainCheckChecks(ctx context.Context, cfg *config.Config, st *state.State) []Check {
	cdn := cfg.Cores.Xray.CDN
	if cdn == nil || !cdn.Enabled || cdn.Domain == "" {
		return nil
	}
	ip := ""
	if st != nil {
		ip = st.IP
	}
	tctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	r := domain.Check(tctx, cdn.Domain, ip)
	cancel()
	fr := fmt.Sprintf("resolved=%d IP(s) · edge=HTTP %d · originTLS=%v", len(r.ResolvedIPs), r.EdgeStatus, r.OriginTLSOK)
	switch {
	case r.ViaCloudflare && r.OriginTLSOK:
		return []Check{{"domain " + cdn.Domain, Pass, "Cloudflare-proxied · " + fr, ""}}
	case len(r.ResolvedIPs) == 0:
		return []Check{{"domain " + cdn.Domain, Fail, "DNS does not resolve", "create the A record (TUI 'p  Domain / proxy')"}}
	case r.EdgeStatus == 404 || r.EdgeStatus == 502:
		return []Check{{"domain " + cdn.Domain, Pass, "proxied · edge " + fr + " (404/502 on / is normal when no web page is served)", ""}}
	default:
		return []Check{{"domain " + cdn.Domain, Warn, fr + " · verdict " + string(r.Verdict), "ngray domain check " + cdn.Domain}}
	}
}
