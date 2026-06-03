# Roadmap — additional cores

These cores are specced precisely so they can be added behind the existing `cores.Core`
interface without rework. Each follows the same pattern: `Install` (download + systemd unit),
`Build` (write config via `p.Sys.WriteFile`, allocate ports, return endpoints), `Enabled`,
`Installed`. Independent (UDP/TUN) cores get a public port + firewall hole like hysteria2.

## TUIC v5 (independent QUIC core, served by sing-box)
- Append a tuic inbound to the sing-box config; public UDP, self-signed cert (like hysteria2).
- Inbound:
  ```json
  {"type":"tuic","listen":"::","listen_port":PORT,
   "users":[{"uuid":UID,"password":PW}],"congestion_control":"bbr",
   "tls":{"enabled":true,"server_name":SNI,"alpn":["h3"],
          "certificate_path":CRT,"key_path":KEY}}
  ```
- Link: `tuic://UUID:PW@host:PORT?congestion_control=bbr&alpn=h3&sni=SNI&allow_insecure=1#tag`
- Firewall: open the TUIC UDP port. Config: `cores.tuic {enabled, port, sni}`.

## ShadowTLS v3 (TLS-mimic, via sing-box, behind nginx not possible — own port or 443 direct)
- sing-box `shadowtls` inbound v3 + a `shadowsocks` inbound it wraps; `handshake.server` points
  at a real TLS site. Public TCP port. Link is the shadowsocks URI with the shadow-tls plugin.

## mieru / mita (independent TCP/UDP core)
- Install from `enfein/mieru` releases (mita binary), systemd unit `mita`.
- Server config (`mita apply config server.json`):
  ```json
  {"portBindings":[{"port":PORT,"protocol":"TCP"}],
   "users":[{"name":"u","password":"PW"}],"loggingLevel":"INFO","mtu":1400}
  ```
- Client: mieru does not use a vless-style URI; emit a mieru client-config JSON (Notes), which
  NekoBox/Exclave import. Public TCP port band 14xxx, firewall hole.

## AmneziaWG (independent obfuscated-WireGuard core, Russia-strong)
- Install `amneziawg-go` + `awg`/`awg-quick`. Server `[Interface]` adds obfuscation params
  `Jc, Jmin, Jmax, S1, S2, H1, H2, H3, H4` (randomized per deployment) plus standard WG keys.
- Generate X25519 WG keypairs natively (crypto/ecdh, as REALITY keys are done today).
- Client: emit a `.conf` ([Interface]/[Peer] with the same obfuscation params) for the
  AmneziaWG/AmneziaVPN app. Public UDP port band 15xxx, firewall hole, `EnableIPForward`.

## Notes
TUIC is the cheapest high-value next step (sing-box already installed, standard link). mieru and
AmneziaWG broaden shape diversity against ML detection (see research-2026.md) but need their own
client-config outputs rather than the share-link/subscription path.
