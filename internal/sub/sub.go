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
// It uses the server IP (not the front SNI) because frontSNI resolves to the
// real domain's servers, not the user's VPS. Clients fetch this URL through
// the proxy or directly via IP.
func URL(serverIP string, port int, token string) string {
	host := serverIP
	if port != 443 {
		host = fmt.Sprintf("%s:%d", serverIP, port)
	}
	return fmt.Sprintf("https://%s/s/%s", host, token)
}

// WriteBundle persists the base64 subscription content served at /s/<token>.
func WriteBundle(sys *system.Env, content string) error {
	return sys.WriteString(BundleFile, content, 0o644)
}
