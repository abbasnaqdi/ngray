// Package nginx renders an nginx.conf that terminates TCP 443 and multiplexes
// by SNI (ssl_preread) to loopback core backends, plus an HTTPS subscription
// endpoint served under a dedicated front SNI.
package nginx

import (
	"fmt"
	"sort"
	"strings"
)

const ConfPath = "/etc/nginx/nginx.conf"

// Route maps a client SNI to a loopback backend port.
type Route struct {
	SNI     string
	Backend int
}

// Subscription is the HTTPS endpoint nginx serves under its own SNI.
type Subscription struct {
	FrontSNI   string
	Token      string
	HTTPSPort  int    // loopback ssl listener
	CertPath   string
	KeyPath    string
	BundleFile string // static file served at /s/<token>
}

type Config struct {
	Port   int
	Sink   int // loopback default backend (returns 444)
	Routes []Route
	Sub    *Subscription
}

// Render produces the full nginx.conf.
func (c Config) Render() string {
	routes := append([]Route(nil), c.Routes...)
	sort.Slice(routes, func(i, j int) bool { return routes[i].SNI < routes[j].SNI })

	var mapEntries strings.Builder
	for _, r := range routes {
		fmt.Fprintf(&mapEntries, "        %-32s 127.0.0.1:%d;\n", r.SNI, r.Backend)
	}
	if c.Sub != nil {
		fmt.Fprintf(&mapEntries, "        %-32s 127.0.0.1:%d;\n", c.Sub.FrontSNI, c.Sub.HTTPSPort)
	}

	var b strings.Builder
	b.WriteString(`user www-data;
worker_processes auto;
worker_rlimit_nofile 65535;
pid /run/nginx.pid;
error_log /var/log/nginx/error.log warn;
include /etc/nginx/modules-enabled/*.conf;

events {
    worker_connections 4096;
    multi_accept on;
    use epoll;
}

`)
	fmt.Fprintf(&b, "stream {\n    map $ssl_preread_server_name $backend {\n%s        %-32s 127.0.0.1:%d;\n    }\n\n",
		mapEntries.String(), "default", c.Sink)
	fmt.Fprintf(&b, `    server {
        listen %d reuseport;
        listen [::]:%d reuseport;
        proxy_pass $backend;
        ssl_preread on;
        proxy_connect_timeout 10s;
        proxy_timeout 3600s;
        proxy_buffer_size 16k;
        tcp_nodelay on;
    }
}

`, c.Port, c.Port)

	b.WriteString(`http {
    sendfile on;
    tcp_nopush on;
    tcp_nodelay on;
    keepalive_timeout 65;
    types_hash_max_size 2048;
    server_tokens off;
    access_log off;

    server {
        listen 127.0.0.1:`)
	fmt.Fprintf(&b, "%d default_server;\n        server_name _;\n        return 444;\n    }\n", c.Sink)

	if c.Sub != nil {
		s := c.Sub
		fmt.Fprintf(&b, `
    server {
        listen 127.0.0.1:%d ssl;
        server_name %s;
        ssl_certificate %s;
        ssl_certificate_key %s;
        ssl_protocols TLSv1.2 TLSv1.3;
        location = /s/%s {
            default_type text/plain;
            alias %s;
        }
        location / { return 444; }
    }
`, s.HTTPSPort, s.FrontSNI, s.CertPath, s.KeyPath, s.Token, s.BundleFile)
	}
	b.WriteString("}\n")
	return b.String()
}
