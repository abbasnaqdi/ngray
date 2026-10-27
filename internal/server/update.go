package server

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/abbasnaqdi/ng-ray/internal/cores"
	"github.com/abbasnaqdi/ng-ray/internal/system"
	"github.com/abbasnaqdi/ng-ray/internal/version"
)

// SelfRepo is where ng-ray releases are published.
const SelfRepo = "abbasnaqdi/ngray"

// UpdateCores checks every installed core, logs the result for each one,
// and upgrades those that are behind with confirmation.
func UpdateCores(ctx context.Context, confirm func(string) bool, log func(string, ...any)) error {
	sys := system.New()
	updated := false
	for _, core := range cores.Registry() {
		ver, ok := core.Installed(ctx, sys)
		if !ok {
			log("%-14s not installed — skipping", core.Name())
			continue
		}
		latest, err := sys.LatestVersion(ctx, core.Repo())
		if err != nil || latest == "" {
			log("%-14s %s  (could not check upstream)", core.Name(), ver)
			continue
		}
		if ver == latest || ver == "installed" && latest != "" {
			// "installed" means the binary exists but has no parseable semver;
			// offer a reinstall so the version string gets resolved.
			if ver == "installed" {
				if !confirm(fmt.Sprintf("reinstall %s (version unknown → %s)?", core.Name(), latest)) {
					log("%-14s version unknown, skipped", core.Name())
					continue
				}
			} else {
				log("%-14s %s  ✓ up to date", core.Name(), ver)
				continue
			}
		} else if ver != latest {
			if !confirm(fmt.Sprintf("update %s  %s → %s?", core.Name(), ver, latest)) {
				log("%-14s %s  (skipped)", core.Name(), ver)
				continue
			}
		}
		log("updating %s…", core.Name())
		if err := core.Install(ctx, sys); err != nil {
			log("%-14s ERROR: %v", core.Name(), err)
			continue
		}
		_ = sys.Restart(ctx, core.Service())
		log("%-14s updated to %s", core.Name(), latest)
		updated = true
	}
	if !updated {
		log("all cores checked")
	}
	return nil
}

// SelfUpdate replaces the running ngray binary with the latest release.
//
// Two paths:
//  1. Latest release asset (ngray-<os>-<arch>), verified against the published
//     SHA256SUMS before anything is replaced. The SNI candidate pool ships in
//     the release too and is refreshed alongside the binary.
//  2. When allowSource is set and no release exists (the repo is private /
//     Actions billing locked) and this invocation runs from a git checkout,
//     fall back to the source path: `scripts/update.sh` (fetch → build →
//     install), streamed to log. The launch-time auto-update passes false so
//     startup never waits on a source rebuild.
func SelfUpdate(ctx context.Context, log func(string, ...any), allowSource bool) error {
	sys := system.New()
	latest, err := sys.LatestVersion(ctx, SelfRepo)
	if err == nil {
		tag, _ := sys.LatestTag(ctx, SelfRepo)
		expected := "v" + version.Version
		if version.Commit != "" && version.Commit != "none" {
			expected += "-" + version.Commit
		}
		log("current: %s  latest: %s", version.Version, latest)
		if latest == version.Version && (tag == "" || tag == expected) {
			log("ng-ray %s is up to date", version.Version)
			return nil
		}
		return selfUpdateRelease(ctx, sys, latest, log)
	}
	// No (reachable) release — a source checkout can still self-update.
	if allowSource && checkoutRoot() != "" {
		log("no ng-ray release (repo private or Actions disabled) — updating from source instead")
		return selfUpdateSource(ctx, log)
	}
	return fmt.Errorf("no ng-ray release found: %w\nnot a git checkout either — install from source:\n  git clone %s && cd ngray && sudo make install", err, "https://github.com/abbasnaqdi/ng-ray")
}

// AutoUpdateOnLaunch is the pre-TUI self-update check: bounded, silent on any
// failure (never delays boot), release-only (no source rebuilds at startup).
// Returns true only when the binary on disk was actually replaced with a
// newer version; the caller then re-executes.
func AutoUpdateOnLaunch(ctx context.Context, log func(string, ...any)) bool {
	if os.Geteuid() != 0 {
		return false // needs root to replace the binary; skip silently
	}
	cctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	quiet := func(_ string, _ ...any) {}

	sys := system.New()
	latest, err := sys.LatestVersion(cctx, SelfRepo)
	if err != nil || latest == "" || latest == version.Version {
		return false // no release yet / network down / up to date
	}
	tag, _ := sys.LatestTag(ctx, SelfRepo)
	expected := "v" + version.Version
	if version.Commit != "" && version.Commit != "none" {
		expected += "-" + version.Commit
	}
	if tag != "" && tag == expected {
		return false
	}
	if err := selfUpdateRelease(cctx, sys, latest, quiet); err != nil {
		return false // keep launching the current image
	}
	log("self-updated %s → %s — restarting into the new binary…", version.Version, latest)
	return true
}

// selfUpdateRelease downloads + verifies + installs the release binary.
func selfUpdateRelease(ctx context.Context, sys *system.Env, latest string, log func(string, ...any)) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("self-update rewrites %s — run as root: `sudo ngray update --self`", selfExeDir())
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	asset := fmt.Sprintf("ngray-%s-%s", runtime.GOOS, runtime.GOARCH)
	url, err := sys.AssetURL(ctx, SelfRepo, asset)
	if err != nil {
		return err
	}
	log("downloading %s…", latest)
	tmp := filepath.Join(selfExeDir(), ".ngray.new")
	if err := sys.Download(ctx, url, tmp); err != nil {
		return err
	}
	// Verify against the published SHA256SUMS before touching anything.
	if ok, err := selfVerifyAsset(ctx, sys, asset, tmp, log); err != nil {
		log("warning: could not verify %s against SHA256SUMS: %v", asset, err)
	} else if !ok {
		os.Remove(tmp)
		return fmt.Errorf("SHA256 mismatch for %s — download corrupted or tampered; use a source build instead", asset)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}
	selfRefreshCandidates(ctx, sys, log)
	log("ng-ray updated %s → %s — `ngray version` / restart to apply", version.Version, latest)
	return nil
}

// selfVerifyAsset downloads SHA256SUMS and checks the downloaded file.
// Returns (false, nil) on a hash mismatch, an error when the sums file is
// absent/unmatchable (caller may treat that as non-fatal).
func selfVerifyAsset(ctx context.Context, sys *system.Env, asset, downloaded string, log func(string, ...any)) (bool, error) {
	sumsURL, err := sys.AssetURL(ctx, SelfRepo, `^SHA256SUMS$`)
	if err != nil {
		return false, fmt.Errorf("SHA256SUMS asset: %w", err)
	}
	tmp := filepath.Join(selfExeDir(), ".ngray.sums")
	if err := sys.Download(ctx, sumsURL, tmp); err != nil {
		return false, err
	}
	defer os.Remove(tmp)
	f, err := os.Open(tmp)
	if err != nil {
		return false, err
	}
	defer f.Close()
	var want string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && fields[1] == asset {
			want = strings.ToLower(fields[0])
		}
	}
	if want == "" {
		return false, fmt.Errorf("%s not listed in SHA256SUMS", asset)
	}
	b, err := os.ReadFile(downloaded)
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256(b)
	got := hex.EncodeToString(sum[:])
	if got != want {
		log("sha256 want %s, got %s", want, got)
		return false, nil
	}
	return true, nil
}

// selfRefreshCandidates updates the SNI pool from the release (best-effort).
func selfRefreshCandidates(ctx context.Context, sys *system.Env, log func(string, ...any)) {
	url, err := sys.AssetURL(ctx, SelfRepo, `^candidates\.sni$`)
	if err != nil {
		return // release without a candidates asset: nothing to do
	}
	tmp := filepath.Join(selfExeDir(), ".ngray.candidates")
	if err := sys.Download(ctx, url, tmp); err != nil {
		log("warning: could not download candidates.sni: %v", err)
		return
	}
	installed := false
	dir := "/etc/ngray/snis"
	if err := os.MkdirAll(dir, 0o750); err == nil {
		if err := os.Rename(tmp, filepath.Join(dir, "candidates.sni")); err == nil {
			installed = true
		}
	}
	if !installed { // not root / read-only /etc: keep next to the binary
		_ = os.Rename(tmp, filepath.Join(selfExeDir(), "snis", "candidates.sni"))
	}
	os.Remove(tmp)
	log("SNI candidate pool refreshed")
}

// selfUpdateSource runs scripts/update.sh inside the checkout, streaming its
// output through log. The script performs fetch/reset, build and install —
// the exact flow documented for source installs, so there is one code path.
func selfUpdateSource(ctx context.Context, log func(string, ...any)) error {
	root := checkoutRoot()
	cmd := exec.CommandContext(ctx, "bash", filepath.Join(root, "scripts", "update.sh"))
	cmd.Dir = root
	out, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout //nolint // same pipe is fine for streaming
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start scripts/update.sh: %w", err)
	}
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		log("%s", sc.Text())
	}
	return cmd.Wait()
}

// checkoutRoot returns the directory containing .git, walking up from the CWD
// (the install layout is usually a checkout cloned into ~, this is also how
// servers get updated before releases exist).
func checkoutRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if fi, err := os.Stat(filepath.Join(dir, ".git")); err == nil && (fi.IsDir() || fi.Mode().IsRegular()) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func selfExeDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	return "."
}
