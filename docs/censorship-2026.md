# Censorship-bypass notes (2026)

Status of the protocols ngray ships, and what is worth adding next. This is a
living document — verify against the upstream release notes before relying on
any item.

## Security advisory (Oct 2026): upgrade your cores

A compromise of several eMudhra intermediate CAs means any TLS-verifying core
that trusts a stale CA store can be MITM'd. Fixed in **Xray v26.10.21**,
**Shoes v0.3.5**, and **NaiveProxy v154.0.8037.49-5** (and current sing-box /
Hysteria builds). ngray always installs the newest upstream release, so a fresh
`server setup` is safe; an existing install should run:

```bash
ngray update        # upgrade every installed core
```

## What still works (2026)

| Region | Load-bearing shape | Notes |
|---|---|---|
| Iran | TCP/443 VLESS+REALITY+Vision, SNI from the national allowlist | Rotate IP/SNI; cap sustained throughput (volume triggers re-route). During blackouts only allowlisted SNIs and sometimes only ICMP pass. |
| China | VLESS+REALITY+Vision / XHTTP over direct TCP 443 | Avoid TLS-in-TLS (Vision matters). QUIC and ECH are liabilities. Rotate IP after volume. |
| Russia | REALITY, but IP/AS reputation dominates | Prefer less-popular ASNs; split IPs per protocol family. Mobile whitelist mode cannot be served from a foreign VPS. |

## Deprecated / ineffective

- **TLS-hello fragmentation (`tlshello`)** — DPI reassembles TCP; dead in Iran since ~mid-2026.
- **Cloudflare WS/XHTTP with a custom-domain SNI and no ECH** — the SNI allowlist extracts it; do not count it as a blackout survivor.
- **uTLS fingerprint spoofing** — sing-box docs call it unsuitable for censorship circumvention; prefer real stacks (Naive, Chrome QUIC parrot).
- **Plain QUIC / Hysteria2 / TUIC without obfs** — filtered by SNI in RU (≤2023) and CN (since 2024). Salamander/Gecko mandatory.
- **AmneziaWG 1.x fixed `Jc/S1/S2/H1–H4`** — weak against 2026 statistical analysis; upgrade to 2.0 params.
- **TUIC / Hysteria2 / AWG as an Iran blackout fallback** — UDP is dropped wholesale; keep them out of the blackout group.

## Worth adding (opt-in, not defaults)

1. **Xray FinalMask** (`XTLS/Xray-core`, 2026): TCP `sudoku`, `fragment`, `header-custom`; UDP `salamander`, `gecko`, `xdns`, `xicmp`. Only path that covers an ICMP/DNS-only blackout without a new binary. New and client support is sparse — ship disabled.
2. **Hysteria2 Gecko obfs** — set `cores.hysteria2.obfs: gecko` (config already accepts any obfs type; clients must support it).
3. **AmneziaWG 2.0** — `S3/S4`, `H1–H4` *ranges*, `I1–I5` CPS mimic packets. Requires 2.0-capable `awg-tools` and clients; keep 1.x as the default until that is universal.
4. **NaiveProxy inbound via sing-box** — real Chromium TLS/HTTP2 stack; needs a real domain+cert. Useful for RU/CN, not an Iran allowlist bypass.
5. **ECH on the Cloudflare path** — changes the visible SNI to `cloudflare-ech.com`; one Iran report shows it working where plain CF-WSS fails, but Russia blocks that outer name.

## Not recommended

- **MASQUE / HTTP-3 CONNECT-UDP** — standards-based, but QUIC SNI is blocked in RU and UDP dropped in IR.
- **WatchTunnel / tiny unaudited tunnels** — reserve for experiments, never default.
- **VMess / Trojan / plain Shadowsocks / OpenVPN / plain WireGuard** — already absent or only inside ShadowTLS.

## Practical guidance

- One IP/domain per protocol family; never put all cores on one IP.
- REALITY destination must actually serve TLS1.3 + h2 and be allowlisted for the target region.
- Cap per-IP throughput; sustained high volume is the fastest path to a block.
- Validate camouflage with a DPI differential tester (e.g. Revizor) before deploying.
