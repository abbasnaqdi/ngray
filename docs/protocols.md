# Protocols and cores

| Protocol / core | Transport | Why it's here | Crosses MCI | Crosses MTN |
|---|---|---|---|---|
| REALITY (xray / sing-box / shoes) | TCP/TLS 1.3 | Real ClientHello to a whitelisted site; no cert/domain. The one thing that reliably crosses both. | ✓ | ✓ |
| XHTTP (xray, also sing-box) | chunked HTTP up/down | Real nginx on 443 with xray behind it → authentic server fingerprint; split up/down streams; works even over TLS 1.2. | ✓ | partial |
| VLESS + Vision | TCP/TLS | Low-overhead REALITY flow, broad client support. | ✓ | ✓ |
| AnyTLS | TCP/TLS + padding | Different traffic shape (padding-based) for when Vision is fingerprinted; client support limited to sing-box/husi/exclave. | ✓ | ✓ |
| Hysteria2 | QUIC/UDP + Salamander | Brutal congestion control + obfs beats MTN's ~50% loss; mimics HTTP/3 on UDP 443. | partial | ✓ |
| fptn | TUN VPN (token) | Independent full-tunnel fallback, SNI-masqueraded. | partial | partial |

**shoes** (Rust) is kept first-class: empirically its REALITY/AnyTLS combinations sometimes
connect where xray and sing-box do not.

## REALITY SNI selection rules

`ngray sni test` enforces these; only `OK` SNIs should be used for REALITY:

- **TLS 1.3** support (handshake fails otherwise).
- **h2** advertised in ALPN, ordered before `http/1.1`.
- **X25519** key-exchange group (ng-ray pins it and checks the handshake succeeds).
- Domain must **not** be a redirector.
- Bonus: target IP near the VPS, handshake encrypted after ServerHello (e.g. `dl.google.com`),
  OCSP stapling.

Seed candidates live in `snis/candidates.sni`; `ngray sni test` classifies each as
`OK / NO_TLS13 / NO_H2 / NO_X25519 / BLOCKED / DNS_FAIL` with median latency and jitter.

## Sources
- XTLS/REALITY README; Xray fallbacks-with-SNI docs
- Xray XHTTP write-ups (habr, timharbakon)
- sing-box AnyTLS / Hysteria2 documentation; cfal/shoes
- net4people/bbs #528 — protocol viability in 2025
