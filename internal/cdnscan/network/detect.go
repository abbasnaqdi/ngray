package network

import (
	"net"
	"os"
	"strings"
)

// Interface represents a network interface relevant to the scanner.
type Interface struct {
	Name  string
	Addr  string // IPv4 address
	IsVPN bool
}

// ProxyAddr returns the first non-empty HTTPS or HTTP proxy from the environment.
func ProxyAddr() string {
	for _, k := range []string{"https_proxy", "HTTPS_PROXY", "http_proxy", "HTTP_PROXY"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// Interfaces returns all up, non-loopback network interfaces with IPv4 addresses.
func Interfaces() ([]Interface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	vpnPrefixes := []string{
		"tun", "tap", "wg", "vpn", "ppp", "ipsec",
		"utun", "cloudflare", "zerotier", "tailscale",
	}

	var result []Interface
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		var ipv4 string
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok {
				if v4 := ipnet.IP.To4(); v4 != nil {
					ipv4 = v4.String()
					break
				}
			}
		}
		if ipv4 == "" {
			continue
		}

		name := strings.ToLower(iface.Name)
		isVPN := false
		for _, pfx := range vpnPrefixes {
			if strings.HasPrefix(name, pfx) || strings.Contains(name, pfx) {
				isVPN = true
				break
			}
		}

		result = append(result, Interface{Name: iface.Name, Addr: ipv4, IsVPN: isVPN})
	}
	return result, nil
}

// HasVPN returns true if any VPN-like interface is up.
func HasVPN(ifaces []Interface) bool {
	for _, i := range ifaces {
		if i.IsVPN {
			return true
		}
	}
	return false
}

// PhysicalInterface returns the first non-VPN interface, or a zero Interface.
func PhysicalInterface(ifaces []Interface) Interface {
	for _, i := range ifaces {
		if !i.IsVPN {
			return i
		}
	}
	return Interface{}
}

// MakeDialer returns a net.Dialer that routes traffic through the given interface.
//
// It binds the source address to iface.Addr so the kernel routes the packet
// through the physical interface regardless of VPN policy routes.
// When iface is zero (empty Addr), a plain dialer is returned.
func MakeDialer(iface Interface) *net.Dialer {
	if iface.Addr == "" {
		return &net.Dialer{}
	}
	return &net.Dialer{
		LocalAddr: &net.TCPAddr{IP: net.ParseIP(iface.Addr)},
	}
}
