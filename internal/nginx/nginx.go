// Package nginx renders an nginx.conf that terminates TCP 443 and multiplexes
// by SNI (ssl_preread) to loopback core backends, plus HTTP and HTTPS
// subscription endpoints.
package nginx

import (
	"fmt"
	"sort"
	"strings"
)

const ConfPath = "/etc/nginx/nginx.conf"

// Marker identifies an nginx.conf that ngray owns and may rebuild.
const Marker = "# managed by ng-ray"

// Route maps a client SNI to a loopback backend port.
type Route struct {
	SNI     string
	Backend int
}

// Subscription configures the subscription serving block.
// HTTP:80 is always included as a fallback.
// HTTPS on HTTPSPort is included when CertFile/KeyFile are provided.
type Subscription struct {
	Dir         string // root directory (e.g. /etc/ngray) served by nginx
	BundleFile  string // filename within Dir for /s/<token>  — base64 URI list
	SingboxFile string // filename within Dir for /sb/<token> — sing-box JSON
	CertFile    string // absolute PEM cert path (empty → HTTP-only)
	KeyFile     string // absolute PEM key path
	HTTPSPort   int    // HTTPS listening port; 0 = HTTP-only
}

type Config struct {
	Port        int
	Sink        int // loopback default backend (returns 444)
	Routes      []Route
	Sub         *Subscription
	CDNSub      *CDNSub
	ProxyRoutes []ProxyRoute
}

// CDNSub serves the subscription on a Cloudflare-proxied domain SNI over the
// 443 mux: the stream block routes Domain to 127.0.0.1:Backend, where this
// loopback TLS server presents the origin cert (so CF Full(strict) is happy)
// and serves /s/ and /sb/. Result: a port-free, CF-fronted, valid-cert
// subscribe URL with the origin IP hidden.
type CDNSub struct {
	Domain      string
	Backend     int
	CertFile    string
	KeyFile     string
	Dir         string
	BundleFile  string
	SingboxFile string
}

// ProxyRoute serves an arbitrary service behind a trusted domain via nginx
// reverse proxy. Each route gets its own SNI in the 443 mux, routed to a
// loopback TLS server that presents the origin cert and proxies to Target.
type ProxyRoute struct {
	Domain   string
	Target   string
	CertFile string
	KeyFile  string
	Backend  int
}

// Render produces the full nginx.conf.
func (c Config) Render() string {
	routes := append([]Route(nil), c.Routes...)
	// Proxy routes also need SNI routing in the 443 mux.
	for _, pr := range c.ProxyRoutes {
		if pr.Domain != "" && pr.Backend > 0 {
			routes = append(routes, Route{SNI: pr.Domain, Backend: pr.Backend})
		}
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].SNI < routes[j].SNI })

	var mapEntries strings.Builder
	for _, r := range routes {
		fmt.Fprintf(&mapEntries, "        %-32s 127.0.0.1:%d;\n", r.SNI, r.Backend)
	}

	var b strings.Builder
	b.WriteString(Marker + "\n")
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
        proxy_pass $backend;
        ssl_preread on;
        proxy_connect_timeout 10s;
        proxy_timeout 3600s;
        proxy_buffer_size 16k;
        tcp_nodelay on;
    }
}

`, c.Port)

	b.WriteString(`http {
    include /etc/nginx/mime.types;
    default_type application/octet-stream;
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
		// HTTP :80 — plain-text fallback, always available
		fmt.Fprintf(&b, `
    server {
        listen 80;
        server_name _;
        root %s;
        location ~ ^/s/ {
            default_type text/plain;
            try_files /%s =404;
        }
        location / { return 444; }
    }
`, c.Sub.Dir, c.Sub.BundleFile)

		// HTTPS on configured port — self-signed TLS; clients must enable skip-cert-verify
		if c.Sub.CertFile != "" && c.Sub.KeyFile != "" && c.Sub.HTTPSPort > 0 {
			fmt.Fprintf(&b, `
    server {
        listen %d ssl;
        ssl_certificate     %s;
        ssl_certificate_key %s;
        ssl_protocols       TLSv1.2 TLSv1.3;
        ssl_ciphers         ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:HIGH:!aNULL:!MD5;
        ssl_prefer_server_ciphers on;
        ssl_session_cache   shared:SUB:10m;
        ssl_session_timeout 10m;
        server_name _;
        root %s;
        location ~ ^/s/ {
            default_type text/plain;
            try_files /%s =404;
        }
`, c.Sub.HTTPSPort, c.Sub.CertFile, c.Sub.KeyFile, c.Sub.Dir, c.Sub.BundleFile)

			if c.Sub.SingboxFile != "" {
				fmt.Fprintf(&b, `        location ~ ^/sb/ {
            default_type application/json;
            try_files /%s =404;
        }
`, c.Sub.SingboxFile)
			}
			b.WriteString(`        location / { return 444; }
    }
`)
		}
	}

	// Cloudflare-fronted subscription: loopback TLS server with the origin cert,
	// reached via the 443 stream mux on the CDNSub.Domain SNI.
	if c.CDNSub != nil {
		fmt.Fprintf(&b, `
    server {
        listen 127.0.0.1:%d ssl;
        ssl_certificate     %s;
        ssl_certificate_key %s;
        ssl_protocols       TLSv1.2 TLSv1.3;
        server_name %s;
        root %s;
        location ~ ^/s/  { default_type text/plain;      try_files /%s =404; }
        location ~ ^/sb/ { default_type application/json; try_files /%s =404; }
        location / { return 404; }
    }
`, c.CDNSub.Backend, c.CDNSub.CertFile, c.CDNSub.KeyFile, c.CDNSub.Domain,
			c.CDNSub.Dir, c.CDNSub.BundleFile, c.CDNSub.SingboxFile)
	}

	// Reverse proxy routes: each domain gets a loopback TLS server that
	// presents the origin cert and proxies to the configured target.
	for _, pr := range c.ProxyRoutes {
		if pr.Domain == "" || pr.CertFile == "" || pr.KeyFile == "" {
			continue
		}
		backend := pr.Backend
		if backend <= 0 {
			backend = 8444
		}
		fmt.Fprintf(&b, `
    server {
        listen 127.0.0.1:%d ssl;
        ssl_certificate     %s;
        ssl_certificate_key %s;
        ssl_protocols       TLSv1.2 TLSv1.3;
        server_name %s;
        location / {
            proxy_pass %s;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;
        }
    }
`, backend, pr.CertFile, pr.KeyFile, pr.Domain, pr.Target)
	}

	b.WriteString("}\n")
	return b.String()
}
