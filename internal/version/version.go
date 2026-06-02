// Package version exposes build metadata stamped via -ldflags.
package version

import "fmt"

var (
	Version = "0.1.0-dev"
	Commit  = "none"
	Date    = "unknown"
)

func String() string {
	return fmt.Sprintf("ng-ray %s (commit %s, built %s)", Version, Commit, Date)
}
