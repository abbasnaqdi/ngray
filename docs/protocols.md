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
| Salamander obfs| —          | ✅           | —           | ✅ best    |

**Assignment rules derived from this matrix:**
- XHTTP configs → xray exclusively
- AnyTLS → prefer shoes (best Rust impl), then sing-box; not xray
- TUIC → shoes (Rust) + sing-box; not xray
- Hysteria2 server → standalone binary (reference); shoes Rust as secondary
- ShadowTLS → sing-box exclusively

## Bypass layer coverage

| Layer                        | Protocol(s)                              |
|------------------------------|------------------------------------------|
| TLS disguise (REALITY)       | VLESS+Vision via xray / shoes / singbox  |
| HTTP disguise                | XHTTP via xray                           |
| Padding obfuscation          | AnyTLS via shoes / singbox               |
| QUIC (loss-tolerant UDP)     | Hysteria2 standalone + shoes Rust        |
| QUIC (standard h3 stack)     | TUIC v5 via shoes + singbox              |
| TLS-mimic (blocks deep DPI)  | ShadowTLS v3 via singbox                 |
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
complete Salamander obfuscation and brutal CC. Use it as the primary Hysteria2
server. shoes Rust Hysteria2 (`port 36713`) is a secondary with a different
binary fingerprint — useful as a failover and for clients that try multiple
endpoints automatically.

## REALITY SNI selection rules

`ngray sni test` enforces these; only `OK` results should be used:

- **TLS 1.3** support
- **h2** in ALPN, ordered before http/1.1
- **X25519** key exchange
- Not a redirector
- Bonus: target IP near VPS; OCSP stapling (`dl.google.com` is excellent)

## Subscribe URLs

Three URLs are produced per deployment:

| URL | Format | Notes |
|-----|--------|-------|
| `https://SERVER-IP:8443/s/TOKEN` | base64 URI list (primary) | self-signed cert — enable skip-cert-verify in client |
| `https://SERVER-IP:8443/sb/TOKEN` | sing-box JSON outbound list | import directly into husi/exclave/sing-box |
| `http://SERVER-IP/s/TOKEN` | base64 URI list (fallback) | plain HTTP, no cert required |

**Certificate:** a self-signed ECDSA P-256 cert is generated at setup time with
the server IP as both CN and IP SAN. nginx serves it on port 8443. Since it is
self-signed (not CA-signed), clients must enable skip-cert-verify / insecure mode
when importing the HTTPS subscription URL. The cert is regenerated automatically
if the server IP changes.

**Sing-box JSON** endpoint (`/sb/`) is served only over HTTPS and contains a
proper dual-outbound structure for ShadowTLS (shadowtls + shadowsocks chained)
and full xhttp transport for VLESS+XHTTP entries.

If a `front_sni` is set in config, it is added as a DNS SAN to the certificate
(useful if DNS resolves the SNI hostname to the server IP).
