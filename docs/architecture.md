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
  cores              Core interface + xray/shoes/sing-box/hysteria/fptn
  nginx              ssl_preread SNI mux + subscription server config
  sub                subscription on-disk layout + URL
  links              Endpoint -> vless/anytls/hysteria2 URIs, QR, sub bundle, sing-box JSON
  server             Setup orchestrator, client renderer, update logic
  cdnscan            CDN edge scanner (bubbletea), `ngray scan`
  tui                main menu
  version            ldflags build metadata
```

## Traffic surface

nginx terminates **TCP 443** and uses `ssl_preread` to route by client SNI to a loopback
backend, with an unknown-SNI sink returning 444:

```
443/tcp ── nginx ssl_preread map ──┬─ www.rust-lang.org → 127.0.0.1:12000  (shoes Vision)
                                    ├─ www.debian.org    → 127.0.0.1:12001  (shoes AnyTLS)
                                    ├─ pypi.org          → 127.0.0.1:11003  (xray XHTTP)
                                    ├─ <front-sni>       → 127.0.0.1:8443   (subscription, TLS)
                                    └─ default           → 127.0.0.1:8080   (444)
```

Each core owns a distinct backend band — **xray 11000–11999, shoes 12000–12999, sing-box
13000–13999** — bound to `127.0.0.1` only, so DPI never sees them; the band choice is just
clean bookkeeping. **hysteria2** (QUIC/UDP) and **fptn** (TUN) are independent cores with
their own public ports. The only public ports are 443/tcp, the hysteria2 UDP port, the fptn
TCP port, and SSH.

## Subscription

nginx serves a TLS endpoint for a dedicated `front_sni` on a loopback port (8443), routed by
the same SNI map. It returns the base64 link bundle at `/s/<random-token>`. The token is
persisted in state so the subscribe URL stays stable across re-runs. Self-signed cert (no
domain), so clients fetch it allow-insecure.

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
