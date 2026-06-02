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

## Sources
- net4people/bbs #253 — A report on Iran's GFW (per-operator MCI vs MTN/Rightel behaviour)
- v2rayNG #5232 — anti-DPI feature requests under Iran filtering conditions
- apernet/hysteria + sing-box docs — Salamander obfs and brutal congestion control
- 2026 reporting on MTN-Irancell DPI build-out (Global Voices, National Security News, CNN)
