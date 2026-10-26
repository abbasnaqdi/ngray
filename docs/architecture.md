# Architecture

Single Go binary, `cmd/ngray`, dispatching to `internal/` packages. The no-arg launch shows
a bubbletea TUI (`internal/tui`) whose items re-exec the matching subcommand.

```
cmd/ngray            CLI router + per-command handlers
internal/
  config             config.yml schema, defaults, validation
  state              proxy-state.json: keys, secrets, ports, sub token, deploy snapshot (0600)
  system             host boundary: exec, apt, GitHub releases, downloads, systemd,
                     sysctl/BBR, self-signed certs, native X25519 REALITY keypairs
  ports              per-core loopback ranges + free-port preflight
  firewall           lockout-safe default-drop via a dedicated NGRAY chain
  sni                REALITY-fitness tester (TLS1.3 + h2 + X25519 + latency)
  domain             Cloudflare/origin health check (DNS, CF edge status, origin TLS)
  cores              Core interface + xray/shoes/sing-box/hysteria/fptn/mieru/amneziawg
  nginx              ssl_preread SNI mux + subscription server (IP + CF-fronted on domain)
  sub                subscription on-disk layout + URL
  links              Endpoint -> vless(reality/ws)/anytls/hysteria2/tuic/shadowtls URIs,
                     mieru + wireguard sing-box outbounds, base64 bundle, sing-box JSON
  server             Setup orchestrator, client renderer, update logic
  cdnscan            CDN edge scanner (bubbletea), `ngray scan`
  tui                main menu
  version            ldflags build metadata
```

## Traffic surface

nginx terminates **TCP 443** and uses `ssl_preread` to route by client SNI to a loopback
backend, with an unknown-SNI sink returning 444:

```
443/tcp ── nginx ssl_preread map ──┬─ www.rust-lang.org  → 127.0.0.1:12000 (shoes Vision)
                                    ├─ www.debian.org     → 127.0.0.1:12001 (shoes AnyTLS)
                                    ├─ example.com       → 127.0.0.1:11xxx (xray VLESS+WS+TLS, CF)
                                    ├─ www.example.com   → 127.0.0.1:8444  (subscription, CF, origin cert)
                                    └─ default             → 127.0.0.1:8080  (444)
```

Each core owns a distinct backend band — **xray 11000–11999, shoes 12000–12999, sing-box
13000–13999** — bound to `127.0.0.1` only, so DPI never sees them; the band choice is just
clean bookkeeping. **hysteria2** (QUIC/UDP), **mieru** (TCP), **amneziawg** (UDP) and **fptn**
(TUN) are independent cores with their own public ports.

The 443 mux carries everything TLS-shaped, including two **Cloudflare-fronted** SNIs when a
CDN domain is configured: `DOMAIN` runs a `VLESS + WS + TLS` inbound that terminates TLS with
the Cloudflare **Origin Certificate** (REALITY can't front through CF, which terminates TLS at
its edge), and `www.DOMAIN` routes to a loopback nginx TLS server that serves the subscription
on 443 — so the subscribe URL is port-free behind Cloudflare with the origin IP hidden.

## Subscription

`/s/<token>` serves the base64 URI list, `/sb/<token>` the sing-box JSON outbound
list (the only place **mieru** and **AmneziaWG** appear, as they have no URI). The
sing-box bundle adds three failover groups — `auto` (urltest over everything),
`blackout` (urltest over the TCP/443 REALITY + ShadowTLS subset that survives a
national SNI allowlist), and `select` (manual pin, default `auto`).
nginx serves these on up to three listeners:

- **HTTP :80** — plain-text fallback (base64 only)
- **HTTPS :2053** — self-signed cert (IP SAN); direct-IP fallback, skip-cert-verify
- **`https://www.DOMAIN/` (443, via Cloudflare)** — *primary when a CDN domain is
  set*: a loopback TLS server (`127.0.0.1:8444`) presenting the **Origin
  Certificate**, reached through the 443 mux on the `www.DOMAIN` SNI. Port-free,
  trusted edge cert, origin IP hidden.

The token (32 hex chars) is generated once and persisted in `proxy-state.json`
so the subscribe URL is stable across re-runs. The self-signed cert is
regenerated automatically when the server IP changes (`CertHasIPSAN` check).

## Reverse proxy routes

`config.proxy_routes` (or `ngray proxy add <domain> --target <url>`) serves an
arbitrary HTTP(S) service behind a trusted domain. Each route:

1. gets an SNI entry in the 443 `ssl_preread` map, pointing at `127.0.0.1:8445`;
2. is rendered as a loopback TLS server on `8445` that presents the route's
   cert/key and `proxy_pass`es to `target` with `proxy_ssl_server_name on`
   (so HTTPS upstreams get the right SNI) and `Host $proxy_host`.

Backend ports: `8444` = Cloudflare-fronted subscription, `8445` = reverse proxy
routes. Both are loopback-only and share the single 443 mux, so clients reach
them only through their configured domain.

## Safety

- **Firewall** (`internal/firewall`): all rules go in a dedicated `NGRAY` chain, populated
  fully (lo, ESTABLISHED, SSH, then the configured public ports, then DROP) before INPUT
  jumps to it. Flushing our own chain never drops the live INPUT policy, so SSH is never cut
  mid-apply. Applied to IPv4 and IPv6.
- **Port preflight** (`internal/ports`): a candidate port is bound-tested before assignment;
  prior assignments are reused if still free.
- **Idempotency**: keys, credentials and port assignments are generated once and reused from
  `proxy-state.json`; re-running `server setup` produces the same links and a stable subscribe
  URL.
