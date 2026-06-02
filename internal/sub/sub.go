// Package sub holds the on-disk layout and URL of the self-hosted subscription
// endpoint served by nginx under a dedicated front SNI on 443.
package sub

import (
	"fmt"

	"github.com/abbasnaqdi/ng-ray/internal/system"
)

const (
	Dir        = "/etc/ngray"
	HTTPSPort  = 8443 // loopback; nginx terminates TLS here for the front SNI
	BundleFile = Dir + "/bundle"
	CertPath   = Dir + "/sub.crt"
	KeyPath    = Dir + "/sub.key"
)

// URL is the fixed subscribe link users add to their client.
func URL(frontSNI, token string) string {
	return fmt.Sprintf("https://%s/s/%s", frontSNI, token)
}

// WriteBundle persists the base64 subscription content served at /s/<token>.
func WriteBundle(sys *system.Env, content string) error {
	return sys.WriteString(BundleFile, content, 0o644)
}
