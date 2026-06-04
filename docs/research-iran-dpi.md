# Iranian DPI, per operator

Iran's filtering is **not uniform across carriers**. An IP or protocol that works on one
operator often fails on another, which is the single most important fact shaping ng-ray's
design: it generates many `(SNI × core × transport)` shapes so at least one lands on each
network.

## MCI (Hamrah-e-Aval)
IP-based blocking. A given VPS IP is either reachable or blackholed wholesale. REALITY
works when the IP is reachable. Strategy: find IPs that survive on MCI — rotate VPS IPs and
use the CDN edge scanner (`ngray scan`) to locate reachable edges.

## Irancell / MTN
The harder network. Two mechanisms observed:
- **Protocol whitelist.** A TCP connection is blackholed within its first few packets unless
  it looks like HTTP or TLS. If it is TLS, the **SNI must be on a whitelist** or the
  connection is broken. This is exactly why REALITY (a real TLS 1.3 ClientHello toward a
  whitelisted site) survives, and why "naked" VMess/Trojan do not.
- **Throttling.** Heavy packet loss (~50%) regardless of protocol. TCP suffers; this is where
  **Hysteria2** wins, because Salamander obfuscation hides the QUIC pattern and its "brutal"
  congestion control keeps pushing at a fixed rate through the loss.

## Rightel
Behaves like Irancell/MTN; IPs that work on MTN tend to work on Rightel too.

## Cross-operator reality
It is hard to find one VPS IP that works on **both** MCI and MTN. ng-ray therefore fans out
across SNIs, cores and transports rather than betting on one config. Validate the survivors
per operator from a device on that network (server-side `ngray sni test` proves reachability
and REALITY fitness, not an operator's whitelist membership).

## National allowlist mode (Jan 2026)

The January 2026 shutdown was not a BGP blackout (unlike 2019) but a **protocol +
SNI allowlist** at the national gateway — weeks long, partially restored May 2026,
~90% drop in international traffic with IPv4 prefixes still globally routed (filtering
above the BGP layer). Two facts reshape the per-operator picture above:

- Only **DNS/53, HTTP/80, HTTPS/443 outbound** crossed; everything else was dropped
  silently. **Every UDP core dies** in this mode — Hysteria2, TUIC and AmneziaWG are
  gone regardless of operator, not just on Irancell. Only TCP shapes survive.
- HTTPS crossed **only for an explicit domain allowlist** (Google, Bing, Gmail/Outlook,
  GitHub, Apple, Samsung, ChatGPT/OpenAI, Play/App Store, PlayStation, Maps/Meet); any
  other SNI got an injected RST. So **REALITY survives a blackout only when its SNI is
  one of the allowlisted domains.**

ng-ray's response: the candidate pool carries a **blackout-survivor** set (ChatGPT/
OpenAI, App Store, plus AI/dev-infra domains the censor tends to permit), and the
sing-box bundle ships a **`blackout`** urltest group of exactly the TCP/443 REALITY +
ShadowTLS shapes that send a borrowed allowlistable SNI. Note that CF-fronted WS
survives an **IP blacklist** but **not** the SNI allowlist — the client still sends the
custom domain as SNI, which is RST'd. What actually moved data during the full blackout
was off-VPS: Starlink and BLE mesh (Bitchat / its Iranian fork Noghteha) — out of scope
for a VPS proxy but worth knowing as the boundary of what this tool can do.

## Sources
- net4people/bbs #253 — A report on Iran's GFW (per-operator MCI vs MTN/Rightel behaviour)
- v2rayNG #5232 — anti-DPI feature requests under Iran filtering conditions
- apernet/hysteria + sing-box docs — Salamander obfs and brutal congestion control
- 2026 reporting on MTN-Irancell DPI build-out (Global Voices, National Security News, CNN)
- arXiv 2603.28753 — *Iran's January 2026 Internet Shutdown* (allowlist mechanism, surviving protocols)
- IODA / Georgia Tech — comparative report on Iran shutdowns 2019 / 2022 / 2025 / 2026
- Schneier on Security (Feb 2026) — Tehran's two-tiered internet
