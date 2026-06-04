# Protocols and cores — capability matrix

## Which core implements what

| Protocol       | xray       | shoes (Rust) | sing-box    | standalone |
|----------------|------------|--------------|-------------|------------|
| VLESS+Vision   | ✅ best    | ✅ Rust fp   | ✅ good     | —          |
| AnyTLS         | ❌         | ✅ best      | ✅ good     | —          |
| XHTTP          | ✅ **only**| ❌           | ⚠️ limited  | —          |
| TUIC v5        | ❌         | ✅ Rust fp   | ✅ good     | —          |
| Hysteria2      | ❌         | ✅ Rust fp   | ⚠️ server   | ✅ **best**|
| ShadowTLS v3   | ❌         | ❌           | ✅ **only** | —          |
| VLESS+WS+TLS   | ✅ **only**| ❌           | ❌          | —          |
| Salamander obfs| —          | ✅           | —           | ✅ best    |

**Assignment rules derived from this matrix:**
- XHTTP configs → xray exclusively (and **not** Cloudflare-frontable — CF Free buffers responses; disabled by default)
- AnyTLS → prefer shoes (best Rust impl), then sing-box; not xray
- TUIC → shoes (Rust) + sing-box; not xray
- Hysteria2 server → standalone binary (reference); shoes Rust Hy2/TUIC disabled by default (redundant; current shoes rejects their config)
- ShadowTLS → sing-box exclusively
- VLESS+WS+TLS → xray only, **the one shape that traverses Cloudflare** (REALITY and UDP cannot — CF terminates TLS at its edge and won't proxy arbitrary UDP)

## Bypass layer coverage

| Layer                        | Protocol(s)                              |
|------------------------------|------------------------------------------|
| TLS disguise (REALITY)       | VLESS+Vision via xray / shoes / singbox  |
| HTTP disguise                | XHTTP via xray                           |
| Padding obfuscation          | AnyTLS via shoes / singbox               |
| QUIC (loss-tolerant UDP)     | Hysteria2 standalone + shoes Rust        |
| QUIC (standard h3 stack)     | TUIC v5 via shoes + singbox              |
| TLS-mimic (blocks deep DPI)  | ShadowTLS v3 via singbox                 |
| CDN-fronted (IP-independent) | VLESS+WS+TLS via xray behind Cloudflare  |
| Full-tunnel VPN              | fptn                                     |
| ML-resistant tunnel          | mieru (no fingerprint)                   |
| Obfuscated WireGuard         | AmneziaWG (Russia-grade)                 |

## SNI assignment strategy

nginx:443 routes exactly one SNI to one backend.
`www.rust-lang.org` and `rust-lang.org` are different nginx keys.
Using both variants of a domain doubles nginx coverage:

```
www.rust-lang.org  →  xray   VLESS+Vision (port 443)
rust-lang.org      →  shoes  AnyTLS       (port 443)
```

Both clients see `port 443` on the same server; the different SNI they send
determines which core handles them. Effective strategy: assign www. variants to
xray (primary workhorse), non-www variants to shoes (Rust backup).

## Hysteria2 choice

The standalone `hysteria` binary is the reference implementation with the most
complete Salamander obfuscation and brutal CC — the primary (and now only)
Hysteria2 server. shoes' Rust Hysteria2/TUIC inbounds are **disabled by default**:
current shoes rejects their config (a top-level `tls:` key it no longer accepts),
and they duplicate the standalone hysteria + sing-box TUIC anyway. Note that all
Hysteria2/TUIC are UDP/QUIC and fail wholesale where the operator throttles UDP
(e.g. Irancell), independent of the server — keep TCP shapes as the real fallback.

## REALITY SNI selection rules

`ngray sni test` enforces these; only `OK` results should be used:

- **TLS 1.3** support
- **h2** in ALPN, ordered before http/1.1
- **X25519** key exchange
- Not a redirector
- Bonus: target IP near VPS; OCSP stapling (`dl.google.com` is excellent)

## Subscribe URLs

Two tiers are produced per deployment, each in two formats:

| URL | Format | Best for |
|-----|--------|----------|
| `https://www.DOMAIN/sb/TOKEN` | sing-box JSON (primary, CF) | NekoBox/husi · sing-box · Hiddify — full set, **incl. mieru + AmneziaWG** |
| `https://www.DOMAIN/s/TOKEN` | base64 URI list (primary, CF) | v2rayNG/N · Streisand · Shadowrocket — URI configs only |
| `https://SERVER-IP:2053/sb/TOKEN` | sing-box JSON (fallback) | direct-IP, self-signed — skip-cert-verify |
| `https://SERVER-IP:2053/s/TOKEN` | base64 URI list (fallback) | direct-IP, self-signed |
| `http://SERVER-IP/s/TOKEN` | base64 over plain HTTP | no cert required anywhere |

**Primary (Cloudflare):** when a CDN domain is set, the subscription is served on
`www.DOMAIN` over 443 via Cloudflare — port-free, **trusted edge cert** (no
skip-verify), origin IP hidden. The origin presents the Cloudflare **Origin
Certificate** (install via `ngray domain cert`).

**Fallback (direct IP):** a self-signed ECDSA P-256 cert (server IP as CN + IP
SAN) on port 2053; clients enable skip-cert-verify or pin the printed `pinSHA256`.
Regenerated automatically if the server IP changes.

**Sing-box JSON** (`/sb/`) is the only format carrying **mieru** (`type: mieru`)
and **AmneziaWG** (`type: wireguard` + junk params) — they have no share URI — plus
the dual-outbound ShadowTLS chain and three failover groups:

- `auto` (urltest) — health-checks **every** endpoint, routes the lowest-latency live one.
- `blackout` (urltest) — the subset that can cross a national protocol+SNI allowlist
  (Iran, Jan 2026): TCP/443 REALITY + ShadowTLS shapes sending a borrowed allowlistable
  SNI. Excludes UDP cores (hysteria2/tuic/amneziawg), mieru's side-port, and CF-WS
  (custom-domain SNI). The default stays `auto`; switch `select` to `blackout` when even
  UDP is dropped. See [research-iran-dpi.md](research-iran-dpi.md).
- `select` (selector) — pin a specific endpoint or group, default `auto`.

The base64 list (`/s/`) carries only URI-expressible configs.
