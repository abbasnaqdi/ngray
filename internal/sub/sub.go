// Package sub holds the on-disk layout and URL of the self-hosted subscription
// endpoint served by nginx on port 80 (plain HTTP).
// Plain HTTP is intentional: TLS for IP-only servers requires a self-signed
// cert that every client rejects. The 32-hex token provides access obscurity.
package sub

import (
	"fmt"

	"github.com/abbasnaqdi/ng-ray/internal/system"
)

const (
	Dir        = "/etc/ngray"
	BundleFile = Dir + "/bundle"
	HTTPPort   = 80 // plain HTTP — no cert, no TLS errors, works everywhere
)

// URL returns the subscribe link. Uses plain HTTP so clients never see a cert error.
func URL(serverIP, token string) string {
	return fmt.Sprintf("http://%s/s/%s", serverIP, token)
}

// WriteBundle persists the base64 subscription content served at /s/<token>.
func WriteBundle(sys *system.Env, content string) error {
	return sys.WriteString(BundleFile, content, 0o644)
}
