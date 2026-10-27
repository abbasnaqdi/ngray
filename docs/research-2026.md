# Research notes — 2026 circumvention landscape (Oct 2026 refresh)

Captured from research while building ngray, to justify the design and guide future work.
This refresh folds in the Oct 2026 upstream releases and the eMudhra CA incident.

## What still crosses heavy DPI (Iran / China / Russia)
- **VLESS + REALITY + XTLS-Vision** remains the load-bearing shape (~99.5% vs TSPU in community reports).
- **Hysteria2 (QUIC) + Salamander** wins on lossy/throttled links (MTN ~50% loss) via brutal CC; obfs mandatory.
- **Emerging ML detection:** China's GFW Q2-2026 ML upgrade targets junk-byte patterns, pressuring REALITY. Shape diversity is the defense.
- **eMudhra intermediate CA compromise (Oct 2026):** 8 Indian intermediate keys cracked due to weak keys. Every TLS-verifying core must be upgraded (Xray v26.10.21, NaiveProxy v154.0.8037.49-5, Shoes v0.3.5) or clients are MitM-able.

## Iran Jan 2026 — the allowlist shutdown
National gateway protocol+SNI allowlist: only DNS/53, HTTP/80, HTTPS/443 to an explicit domain
allowlist cross; UDP and other SNIs dropped/RST. UDP cores useless in this mode. REALITY survives
only with an allowlisted SNI; blackout-survivor tier added. CF-WS survives IP blocks but not the
SNI allowlist.

## Cores — current versions and what's new

### Xray-core — v26.10.21 (latest, security)
- Security: removed 3 root CAs affected by eMudhra compromise; built with Go 1.27.3 to filter bad certs even if OS CA store is stale. **Upgrade mandatory.**
- Date-based versioning now (v26.x). XHTTP remains xray-exclusive. FinalMask (TCP sudoku/fragment/header-custom; UDP salamander/gecko/xdns/xicmp) is the only path covering an ICMP/DNS-only blackout without a new binary — new, client support sparse; ship disabled.

### sing-box — 1.14.4 stable / 1.15.0-alpha.13
- 1.15.0-alpha.7: **MASQUE client/server (CONNECT-IP)** endpoints (HTTP/1.1, H2, H3; address assignment + route advertisement); HTTP proxy rewritten with **H2/H3 + CONNECT-UDP**; **full certificate pinning** (`certificate_sha256`, `client_certificate_sha256`).
- 1.15.0-alpha.9: NaiveProxy updated to 154.0.8037.49-2.
- AnyTLS supported natively. Certificate pinning is new and actionable.

### Hysteria 2 — 2.13.0
- QUIC protocol sniffing fixed for multi-packet ClientHello; domain ACL now applies to QUIC.
- TUN upgraded to sing-tun v0.9.6; `route.strict` removed.
- Chrome QUIC fingerprint parroting updated to Chrome 154; quic-go v0.63.0.
- `hysteria ech` subcommand to generate ECH keys/client config (v2.12.3).
- `quic.disableStatelessReset` option (v2.12.2); Porkbun/Namecheap/Njalla ACME (v2.12.1).
- **mimic** integration: disguise QUIC as TCP for UDP-restricted networks (Linux, requires mimic) (v2.12.0).
- Chrome QUIC fingerprint parroting by default (v2.11.0).
- **ECH support** (v2.10.0).
- **Gecko** experimental obfuscation: fragments QUIC handshake (v2.9.2).
- **Realms**: P2P NAT hole punching, no public IP needed (v2.9.1).

### TUIC — protocol v5 (unchanged core)
- 0-RTT TCP proxying, UDP fragmentation, connection-bound auth via TLS Keying Material Exporter. Server/client 1.0.0 (2023). No 2026 feature release; still relevant as a QUIC shape.

### ShadowTLS — v0.2.25 (Dec 2025)
- Config file support; TCP_FASTOPEN; **V3 protocol** (opt-in `--v3` / `V3=1`) solves known security issues; TLS1.2 in non-strict mode; wildcard SNI. V3 will be default in next major. Project quiet in 2026 — still usable but monitor.

### AnyTLS (anytls-go) — v0.0.13 (Jun 2026)
- Protocol v2 (Mar 2026): tunnel congestion handling + server state feedback.
- v0.0.13: `-dr` disables connection reuse for strict 1:1.
- Supported by sing-box and shoes.

### AmneziaWG
- Source-only (no releases on amneziawg-go). 1.x fixed Jc/S1/S2/H1-H4 are weak vs 2026 statistical analysis. **AmneziaWG 2.0** adds S3/S4, H1-H4 ranges, I1-I5 CPS mimic packets. Requires 2.0-capable awg-tools + clients; keep 1.x default until universal.

### Mieru / mita — v3.39.0 (Oct 2026)
- Active: v3.39.0 idle-TCP shutdown + memory optimization; v3.35.0 low-entropy mode; v3.34.0 padding traffic pattern + backpressure; v3.33.0 heartbeat jitter + socks5 reachability. Strong padding-based obfuscation, different shape from REALITY.

### Shoes — v0.3.5 (Oct 2026)
- Native **VLESS encryption** (X25519 + ML-KEM key chains, session resumption, native Vision) — post-quantum.
- **ML-DSA-65 REALITY authentication** (`mldsa65_seed`/`mldsa65_verify`).
- Userspace **WireGuard** client/server (GotaTun 0.10.2).
- `url-test` and `fallback` client groups; XHTTP `matching_headers` for rotation.
- Shared **ACME** cert sources (HTTP-01/TLS-ALPN-01), **Fake DNS**, `sniff`, Shadowsocks 2022.
- REALITY rebuilt on rustls fork; eMudhra cert block. Mobile AAR/XCFramework removed (build native libs yourself).
- Upgrade from v0.3.2 baseline used by toolkit: significant.

### NaiveProxy — v154.0.8037.49-5 (Oct 2026)
- **Security: block compromised eMudhra intermediates — all users must upgrade.**
- QUIC throughput near TLS via higher receive window + BBR (v154.0.8037.49-4).
- **Preambles**: realistic Chrome requests before/during tunneled TLS handshake to defeat traffic analysis (v147).
- trust_anchors in ClientHello; tunnel lifetime limits; Caddy v2.11.7-naive2 required.

## ECH / SNI / domain fronting
- Hysteria ECH supported (v2.10.0+) with `hysteria ech` keygen. ECH hides SNI but Russia blocks the outer name `cloudflare-ech.com`; Iran reports mixed. Optional only.
- Cloudflare WS/XHTTP with custom-domain SNI and no ECH is extractable by SNI allowlists — not a blackout survivor.
- Domain fronting via CDN edges (toolkit's cdnscan) remains viable for IP blocks but not SNI allowlists.

## Censor behavior updates (2026)
- **Iran:** Jan 2026 allowlist shutdown (weeks; partial restore May 2026). National gateway protocol+SNI allowlist. UDP dropped. Volume triggers re-route. Blackout = allowlisted SNIs on 443 only.
- **China:** GFW Q2-2026 ML upgrade targeting junk-byte patterns; TLS-in-TLS detection; Vision required; QUIC/ECH liabilities; rotate IP after volume.
- **Russia:** TSPU; IP/AS reputation dominates; mobile whitelist mode cannot be served from foreign VPS; REALITY ~99.5% but ML pressure; AmneziaWG remains strong (not shipped by ngray).

## Deprecated / ineffective (do not add)
- TLS-hello fragmentation (`tlshello`) — DPI reassembles TCP; dead in Iran since ~mid-2026.
- CF WS/XHTTP custom-domain SNI without ECH — not a blackout survivor.
- uTLS fingerprint spoofing — unsuitable for circumvention.
- Plain QUIC/Hysteria2/TUIC without obfs.
- AmneziaWG 1.x fixed params — weak vs 2026 stats.
- TUIC/Hysteria2/AWG as Iran blackout fallback (UDP dropped).
- VMess non-AEAD (removed in Shoes).
- NaiveProxy client lacks Chrome networking fingerprint (Shoes naiveproxy client not equivalent).
- Old ShadowTLS V2 protocol — use V3.

## Actionable additions to the toolkit (prioritized)
1. **Security upgrade (urgent):** Xray v26.10.21, NaiveProxy v154.0.8037.49-5, Shoes v0.3.5 for eMudhra CA fix. Update updater pins.
2. **ShadowTLS V3:** enable `--v3` on both sides; update sing-box inbound config.
3. **Hysteria2 Gecko obfs option** (`cores.hysteria2.obfs: gecko`) alongside Salamander.
4. **Hysteria2 mimic** for UDP-restricted networks (Linux).
5. **Hysteria2 ECH** via `hysteria ech` keygen (optional).
6. **Hysteria2 Realms** for no-public-IP scenarios.
7. **Shoes 0.3.5 upgrade:** native VLESS encryption (PQ), ML-DSA-65 REALITY auth, url-test/fallback groups, matching_headers rotation, Fake DNS, sniff, ACME certs, userspace WireGuard.
8. **Xray FinalMask** (disabled by default) for ICMP/DNS-only blackout.
9. **AmneziaWG 2.0 params** behind flag; keep 1.x default.
10. **NaiveProxy inbound** via sing-box/Shoes with real domain+cert for RU/CN.
11. **MASQUE** only experimental, not default.
12. **Certificate pinning** (`certificate_sha256`) for sing-box/HTTP proxy.
13. **Userspace WireGuard** (Shoes) as additional shape.
14. Version pins: Xray v26.10.21, sing-box 1.14.4, Hysteria 2.13.0, Shoes 0.3.5, NaiveProxy 154.0.8037.49-5, ShadowTLS 0.2.25, Mieru 3.39.0, AnyTLS 0.0.13.

## Sources
Xray releases · sing-box releases · Hysteria releases · ShadowTLS releases · AnyTLS releases ·
Shoes releases · NaiveProxy releases · Mieru releases · censorship-2026.md · research-iran-dpi.md ·
net4people/bbs · 2026 reporting on TSPU/MTN-Irancell DPI.