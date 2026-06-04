// Package version exposes build metadata stamped via -ldflags.
package version

import "fmt"

var (
	Version = "0.4.0"
	Commit  = "none"
	Date    = "unknown"
)

func String() string {
	return fmt.Sprintf("ng-ray %s (commit %s, built %s)", Version, Commit, Date)
}
