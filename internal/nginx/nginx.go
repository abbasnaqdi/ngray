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
	MixedFile   string // filename within Dir for /sm/<token> — plain text: URIs + JSON
	CertFile    string // absolute PEM cert path (empty → HTTP-only)
	KeyFile     string // absolute PEM key path
	HTTPSPort   int    // HTTPS listening port; 0 = HTTP-only
	Token       string // required sub-token: any other /s/…/sb/… path 404s
	PathPorts   []int  // local services exposed by path: <anything>/<PORT>/… → 127.0.0.1:PORT
}

type Config struct {
	Port int
	Sink int // loopback default backend (returns 444)

	Routes      []Route
	Sub         *Subscription
	CDNSub      *CDNSub
	ProxyRoutes []ProxyRoute
	// Includes are extra http{} include lines writeNginx found SAFE: foreign
	// server blocks from other projects, verified not to listen on our ports.
	Includes []string
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
	MixedFile   string    // plain-text mixed subscription (mirrors Subscription.MixedFile)
	Token       string    // required sub-token (mirrors Subscription.Token)
	PathPorts   []int     // path-proxied local services (mirrors Subscription.PathPorts)
	WSRoutes    []WSRoute // per-SNI WS twins: client wss://DOMAIN/<path> → plain-ws backends
}

// WSRoute exposes one tunnel backend under a path inside the TLS-wrapped CDNSub
// server. The config that generated it carries the same path in its inbound
// settings; this is how every SNI gets a domain-fronted twin.
type WSRoute struct {
	Path    string
	Backend int
}

// subLocations renders the token-guarded /s/ and /sb/ locations. The capture
// compares the URL token against the CURRENT expected token, so rotated links
// (old token) stop serving immediately. Empty token → unguarded legacy block.
func subLocations(sub *Subscription) string {
	var b strings.Builder
	if sub.Token == "" {
		// No token context (tests, exotic callers): unguarded legacy block.
		fmt.Fprintf(&b, `
        location ~ ^/s/ { default_type text/plain; try_files /%s =404; }
`, sub.BundleFile)
		if sub.SingboxFile != "" {
			fmt.Fprintf(&b, `
        location ~ ^/sb/ { default_type application/json; try_files /%s =404; }
`, sub.SingboxFile)
		}
		if sub.MixedFile != "" {
			fmt.Fprintf(&b, `
        location ~ ^/sm/ { default_type text/plain; try_files /%s =404; }
`, sub.MixedFile)
		}
		return b.String()
	}
	fmt.Fprintf(&b, `
        location ~ ^/s/(?<subtok>[0-9a-f]+)$ {
            if ($subtok != "%s") { return 404; }
            default_type text/plain;
            try_files /%s =404;
        }
`, sub.Token, sub.BundleFile)
	if sub.SingboxFile != "" {
		fmt.Fprintf(&b, `
        location ~ ^/sb/(?<subsb>[0-9a-f]+)$ {
            if ($subsb != "%s") { return 404; }
            default_type application/json;
            try_files /%s =404;
        }
`, sub.Token, sub.SingboxFile)
	}
	if sub.MixedFile != "" {
		fmt.Fprintf(&b, `
        location ~ ^/sm/(?<subsm>[0-9a-f]+)$ {
            if ($subsm != "%s") { return 404; }
            default_type text/plain;
            try_files /%s =404;
        }
`, sub.Token, sub.MixedFile)
	}
	return b.String()
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

// wsLocations renders the WS-twin locations: upgrade-aware plain proxies into
// the loopback inbounds. ^~ keeps these prefixes regex-free so the /s/ /sb/
// token locations stay fastest and unshadowed.
func wsLocations(routes []WSRoute) string {
	var b strings.Builder
	for _, w := range routes {
		fmt.Fprintf(&b, `
        location ^~ %s {
            proxy_pass http://127.0.0.1:%d;
            proxy_http_version 1.1;
            proxy_set_header Upgrade $http_upgrade;
            proxy_set_header Connection "upgrade";
            proxy_read_timeout 3600s;
            proxy_set_header Host $http_host;
            proxy_set_header X-Real-IP $remote_addr;
        }
`, w.Path, w.Backend)
	}
	return b.String()
}

// portLocations renders reverse-proxy locations for the local services listed
// by port. Request /<PORT>/rest is proxied to 127.0.0.1:<PORT>/rest (trailing
// slashes on both prefix and upstream strip the numeric segment), so a service
// takes over "http(s)://ANY-CONFIGURED-DOMAIN/20128/dashboard" as
// "127.0.0.1:20128/dashboard". Useful when a censor blocks arbitrary ports:
// only 443/80 need to survive. Digit prefixes can never shadow /s/ or /sb/.
func portLocations(ports []int) string {
	var b strings.Builder
	seen := map[int]bool{}
	for _, p := range ports {
		if p <= 0 || p == 80 || seen[p] {
			continue
		}
		seen[p] = true
		fmt.Fprintf(&b, `
        location = /%d { return 301 /%d/; }
        location /%d/ {
            proxy_pass http://127.0.0.1:%d/;
            proxy_http_version 1.1;
            proxy_read_timeout 3600s;
            client_max_body_size 0;   # dashboards upload bigger than the 1m default
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;
            proxy_set_header Host $http_host;
        }
`, p, p, p, p)
	}
	return b.String()
}

// NormalizePorts keeps unique ports in 1..65535, dropping any that collide with
// a reserved listener (80, the mux port, the sub HTTPS port…), sorted so the
// rendered config is stable.
func NormalizePorts(ports []int, reserved ...int) []int {
	skip := map[int]bool{80: true}
	for _, r := range reserved {
		if r > 0 {
			skip[r] = true
		}
	}
	seen := map[int]bool{}
	out := make([]int, 0, len(ports))
	for _, p := range ports {
		if p < 1 || p > 65535 || skip[p] || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
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
`)
	for _, inc := range c.Includes {
		fmt.Fprintf(&b, "    %s\n", inc)
	}
	b.WriteString(`
    server {
        listen 127.0.0.1:`)
	fmt.Fprintf(&b, "%d default_server;\n        server_name _;\n        return 444;\n    }\n", c.Sink)

	if c.Sub != nil {
		// HTTP :80 — plain-text fallback, always available
		fmt.Fprintf(&b, `
    server {
        listen 80 default_server;
        server_name _;
        root %s;
%s%s        location / { return 444; }
    }
`, c.Sub.Dir, subLocations(c.Sub), portLocations(c.Sub.PathPorts))

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
%s%s        location / { return 444; }
    }
`, c.Sub.HTTPSPort, c.Sub.CertFile, c.Sub.KeyFile, c.Sub.Dir, subLocations(c.Sub), portLocations(c.Sub.PathPorts))
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
%s%s%s        location / { return 404; }
    }
`, c.CDNSub.Backend, c.CDNSub.CertFile, c.CDNSub.KeyFile, c.CDNSub.Domain,
			c.CDNSub.Dir, subLocations(&Subscription{
				Dir: c.CDNSub.Dir, BundleFile: c.CDNSub.BundleFile,
				SingboxFile: c.CDNSub.SingboxFile, MixedFile: c.CDNSub.MixedFile, Token: c.CDNSub.Token,
			}), wsLocations(c.CDNSub.WSRoutes), portLocations(c.CDNSub.PathPorts))
	}

	// Reverse proxy routes: each domain gets a loopback TLS server that
	// presents the origin cert and proxies to the configured target.
	for _, pr := range c.ProxyRoutes {
		if pr.Domain == "" || pr.CertFile == "" || pr.KeyFile == "" {
			continue
		}
		backend := pr.Backend
		if backend <= 0 {
			backend = 8445
		}
		fmt.Fprintf(&b, `
    server {
        listen 127.0.0.1:%d ssl;
        ssl_certificate     %s;
        ssl_certificate_key %s;
        ssl_protocols       TLSv1.2 TLSv1.3;
        server_name %s;
        location / {
            proxy_pass "%s";
            proxy_ssl_server_name on;
            proxy_set_header Host $proxy_host;
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
