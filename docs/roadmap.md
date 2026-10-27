# Roadmap

## Shipped

All of the originally-planned cores are implemented behind the `cores.Core`
interface:

- [x] **TUIC v5** — sing-box inbound (independent, self-signed cert, BBR),
      UDP port, firewall hole, `tuic://` share link. `cores.singbox.tuic`.
- [x] **ShadowTLS v3** — sing-box inbound wrapping a loopback Shadowsocks
      inbound; `cores.singbox.shadowtls`. Own public TCP port.
- [x] **mieru / mita** — independent core, ports 14xxx. Client config JSON via
      the sing-box bundle + Notes.
- [x] **AmneziaWG** — incl. AWG **2.0 params** (`S3`/`S4` padding, `H1`–`H4`
      as disjoint ranges) behind `cores.amneziawg.v2: true`. Default stays 1.x
      until 2.0-capable tools/clients are universal.

## Next candidates (opt-in, verify upstream before installing)

1. **Xray FinalMask** — TCP `sudoku`/`fragment`/`header-custom`, UDP
   `salamander`/`gecko`/`xdns`/`xicmp`. Only path that covers an ICMP/DNS-only
   blackout without a new binary. New; client support sparse → ship disabled.
2. **NaiveProxy inbound via sing-box** — real Chromium TLS/HTTP2 stack for
   RU/CN; needs a real domain + cert (TUI «Domain / proxy» covers that part).
3. **ECH on the Cloudflare path** — outer SNI `cloudflare-ech.com`; works in
   one Iran report, blocked by name in Russia.
4. **AWG CPS chains (I1–I5)** — client-side mimicry packets (QUIC/DNS/TLS
   look-alike). Not generated automatically upstream; needs hand-crafted
   payload templates per target DPI. Leaves 1.5-style parity gap.
