// Package sub holds the on-disk layout and URL helpers for the self-hosted
// subscription endpoint. Two transports are served by nginx:
//
//   - HTTP :80            — plain-text fallback, always available
//   - HTTPS :HTTPSPort    — TLS with self-signed cert (IP SAN); primary URL
//
// The 32-hex token provides access obscurity. HTTPS clients must enable
// skip-cert-verify / insecure since the cert is self-signed.
package sub

import (
	"fmt"

	"github.com/abbasnaqdi/ng-ray/internal/system"
)

const (
	Dir         = "/etc/ngray"
	BundleFile  = Dir + "/bundle"              // base64 URI list (v2ray/clash format)
	SingboxFile = Dir + "/singbox-bundle.json" // sing-box JSON outbound list
	CertFile    = Dir + "/sub-cert.pem"
	KeyFile     = Dir + "/sub-key.pem"

	HTTPPort         = 80   // plain HTTP fallback
	DefaultHTTPSPort = 2053 // HTTPS primary (override via config.subscription.https_port)
)

// URL returns the plain-HTTP fallback subscribe link.
func URL(serverIP, token string) string {
	return fmt.Sprintf("http://%s/s/%s", serverIP, token)
}

// HTTPSURL returns the primary HTTPS subscribe link (base64 URI list).
// port comes from config.Subscription.HTTPSPort (default DefaultHTTPSPort).
// Clients must enable skip-cert-verify since the cert is self-signed.
func HTTPSURL(serverIP, token string, port int) string {
	return fmt.Sprintf("https://%s:%d/s/%s", serverIP, port, token)
}

// SingboxURL returns the sing-box JSON subscription URL.
func SingboxURL(serverIP, token string, port int) string {
	return fmt.Sprintf("https://%s:%d/sb/%s", serverIP, port, token)
}

// WriteBundle persists the base64 subscription content at BundleFile.
func WriteBundle(sys *system.Env, content string) error {
	return sys.WriteString(BundleFile, content, 0o644)
}

// WriteSingboxBundle persists the sing-box JSON client config at SingboxFile.
func WriteSingboxBundle(sys *system.Env, data []byte) error {
	return sys.WriteFile(SingboxFile, data, 0o644)
}
