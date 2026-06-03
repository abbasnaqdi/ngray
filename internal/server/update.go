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

// UpdateCores updates each installed core whose latest release is newer,
// gated by confirm, restarting the service afterwards.
func UpdateCores(ctx context.Context, confirm func(string) bool, log func(string, ...any)) error {
	sys := system.New()
	for _, core := range cores.Registry() {
		ver, ok := core.Installed(ctx, sys)
		if !ok {
			continue
		}
		latest, err := sys.LatestVersion(ctx, core.Repo())
		if err != nil || latest == "" {
			continue
		}
		if ver == latest {
			log("%s %s up to date", core.Name(), ver)
			continue
		}
		if !confirm(fmt.Sprintf("update %s %s → %s?", core.Name(), ver, latest)) {
			continue
		}
		if err := core.Install(ctx, sys); err != nil {
			return fmt.Errorf("update %s: %w", core.Name(), err)
		}
		_ = sys.Restart(ctx, core.Service())
		log("%s updated to %s", core.Name(), latest)
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
	if latest == version.Version {
		log("ng-ray %s up to date", version.Version)
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
	log("ng-ray updated %s → %s", version.Version, latest)
	return nil
}
