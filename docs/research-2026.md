# Research notes — 2026 circumvention landscape

Captured from research while building ngray, to justify the design and guide future work.

## What still crosses heavy DPI (Iran / China / Russia)
- **VLESS + REALITY + XTLS-Vision** is the configuration that works where almost nothing else
  does. REALITY borrows a real site's TLS fingerprint and **passes active probing** because the
  server responds exactly like the site it mimics. Community reports put VLESS+REALITY near
  ~99.5% bypass against Russia's TSPU through blocking waves.
- **OpenVPN and WireGuard are ~100% detected** inside Russia (TSPU) and heavily blocked in China;
  Shadowsocks/VMess/Trojan detection climbed through 2025 (Trojan ~90%, VMess ~80% after the
  Aug/Sep 2025 GFW upgrades). Naked protocols are dead; camouflage is mandatory.
- **Hysteria2 (QUIC) + Salamander** wins on lossy/throttled links (MTN's ~50% loss) via brutal
  congestion control, but standard QUIC is now fingerprinted — obfs is required, not optional.
- **Emerging ML detection:** China's GFW Q2-2026 ML upgrade is closing in on junk-byte patterns,
  pressuring even REALITY. The defensive answer is *shape diversity* — exactly ngray's many-
  `(SNI × core × transport)` fan-out — so no single detectable pattern carries all traffic.

## Iran Jan 2026 — the allowlist shutdown
The January 2026 blackout (weeks long, partially restored May 2026) was a **protocol +
SNI allowlist** at the national gateway, not a BGP withdrawal: only DNS/53, HTTP/80 and
HTTPS/443 to an **explicit domain allowlist** (Google, Bing, GitHub, Apple, Samsung,
ChatGPT/OpenAI, Play/App Store, PlayStation…) crossed; all UDP and any other SNI were
dropped/RST. Design consequences now baked into ng-ray:
- **UDP cores are useless in this mode** (Hysteria2/TUIC/AmneziaWG all dropped). Keep
  TCP/443 REALITY as the load-bearing shape.
- **REALITY survives only with an allowlisted SNI** — the pool gained a *blackout-
  survivor* tier (ChatGPT/OpenAI, App Store, AI/dev-infra) and the sing-box bundle a
  `blackout` urltest group of the TCP-REALITY/ShadowTLS shapes that send a borrowed
  allowlistable SNI. CF-WS survives an IP block but **not** the SNI allowlist.
- For REALITY the **SNI is camouflage** — the client never reaches the real site, so a
  domain that *sanctions* Iran (e.g. ChatGPT) still works as an SNI, and is in fact the
  best blackout dest because the censor permits it. What it can't survive is the censor
  filtering that domain inside Iran.
- Off-VPS lifelines during a full blackout (out of ng-ray's scope): Starlink, BLE mesh
  (Bitchat / its Noghteha fork). See [research-iran-dpi.md](research-iran-dpi.md).

## Cores worth tracking (beyond xray/shoes/sing-box)
- **mihomo (clash-meta)** — dominant *client* kernel on desktop in 2026; server side still xray.
- **mieru / mita** — strong padding-based obfuscation (TCP/UDP), hard to fingerprint; a different
  shape from REALITY, valuable when REALITY is under ML pressure.
- **AmneziaWG** — obfuscated WireGuard with Jc/Jmin/Jmax/S1/S2/H1-4 junk params; the strongest
  performer against Russia's TSPU; different paradigm (WG), its own clients.
- **TUIC v5 / juicity** — QUIC, "refined Hysteria" with cleaner congestion control; standard
  `tuic://` share link, broad client support; complements Hysteria2.
- **ShadowTLS v3** — wraps traffic as a handshake to a real TLS site; supported by sing-box/shoes.

## OTF / Refraction mechanisms (ideas, not yet integrated)
The Open Technology Fund backs the techniques worth learning from:
- **Snowflake / Snowstorm** — ephemeral WebRTC proxies (Tor pluggable transport); thousands of
  short-lived peers are hard to bulk-block. Lesson: ephemerality beats static endpoints.
- **Conjure (Refraction Networking)** — uses unused address space at cooperating ISPs as decoy
  routers; no fixed server IP to block. Lesson: decoy routing defeats IP blocklists.
- **obfs4** — the baseline polymorphic obfuscation transport.
These need infrastructure (volunteer proxies / cooperating ISPs) beyond a single VPS, so they're
reference material rather than ngray cores — but the *ephemeral-endpoint* and *decoy* ideas
inform the CDN-edge scanning and SNI-rotation strategy.

## DNS tooling rationale (folded into `ngray dns`)
- In filtered networks DNS is dropped 30–70%, not blocked outright, so a single `dig` probe is
  unreliable. The validator sends multiple raw-UDP probes and accepts on the first reply —
  matching how MasterDnsVPN/dnstt tolerate loss — and queries random subdomains to force NS
  delegation (avoiding ISP-cached/poisoned A records). See `internal/dnsval`.
- DoH endpoint choice matters for the client's last DNS leak; `ngray dns doh` ranks reachable
  providers by latency.

## Sources
net4people/bbs #253 & #528 · XTLS/REALITY · Xray XHTTP write-ups · sing-box docs (AnyTLS,
Hysteria2, TUIC, ShadowTLS) · cfal/shoes · apernet/hysteria (Salamander) · opentech.fund
(Snowflake/Snowstorm, Conjure) · Tor Project Snowflake · enfein/mieru · amnezia-vpn/amneziawg ·
2026 reporting on TSPU and MTN-Irancell DPI (Global Voices, CNN, National Security News).
