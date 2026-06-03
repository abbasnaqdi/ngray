package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
	any := false
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
		any = true
	}
	if !any {
		log("all cores checked")
	}
	return nil
}

// SelfUpdate replaces the running ngray binary with the latest release.
func SelfUpdate(ctx context.Context, log func(string, ...any)) error {
	sys := system.New()
	latest, err := sys.LatestVersion(ctx, SelfRepo)
	if err != nil {
		return fmt.Errorf("no ng-ray release found: %w", err)
	}
	log("current: %s  latest: %s", version.Version, latest)
	if latest == version.Version {
		log("ng-ray %s is up to date", version.Version)
		return nil
	}
	url, err := sys.AssetURL(ctx, SelfRepo, `ngray.*linux.*amd64`)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	log("downloading %s…", latest)
	tmp := filepath.Join(filepath.Dir(exe), ".ngray.new")
	if err := sys.Download(ctx, url, tmp); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}
	log("ng-ray updated %s → %s — restart to apply", version.Version, latest)
	return nil
}
