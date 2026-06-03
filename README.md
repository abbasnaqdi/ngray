<div align="center">

# ◆ ngray toolkit

**One Go binary that builds and runs a multi-core, anti-DPI proxy server — and hands you the client configs.**
Tuned for how Iran (MCI · Irancell · Rightel), China (GFW) and Russia (TSPU) actually censor. No domain required.

`shoes` · `xray` · `sing-box` · `hysteria2` · `fptn`  +  CDN scanner · SNI tools · DNS tools

</div>

---

```
◆ ngray toolkit
  1.2.3.4 · nginx :443 · hy2 udp :443 · BBR on

  1  Server setup / repair      6  Update cores
  2  Client links · QR · sub    7  Firewall
  3  Test SNIs (live)           8  Doctor
  4  Scan CDN edges             9  Regenerate credentials
  5  Status
```

Run `ngray` for the interactive console, or any command directly.

## Why

No single VPS IP reliably crosses **both** MCI and Irancell, and naked protocols are detected
within seconds. So ngray fans out across many `SNI × core × transport` shapes — REALITY for the
cross-operator path, Hysteria2+Salamander for lossy mobile networks, XHTTP behind a real nginx
for an authentic fingerprint — so at least one lands on each operator, and no single pattern
carries all your traffic. The reasoning is in [docs/](docs/): per-operator DPI, the protocol
matrix, the 2026 landscape, and the architecture.

## Quickstart

**Server** (Ubuntu, root):
```bash
make build && sudo make install
cp config.example.yml config.yml      # edit + keep every REALITY sni unique
sudo ngray server setup               # validates SNIs, installs cores, provisions, prints links
```

**Client** — import the printed links or the one fixed subscribe URL into v2rayN / nekoray /
nekobox / husi / exclave:
```bash
ngray client links --qr
```

**Test it safely on your own machine first** — `--sandbox` writes under `./sandbox` and never
runs apt/systemctl/iptables on your host:
```bash
ngray server setup --sandbox
```

## Commands

| | |
|---|---|
| `ngray` | interactive console (esc = back, q = quit) |
| `server setup [--sandbox]` | validate SNIs → install cores → nginx SNI-mux + subscribe link → firewall |
| `server status` · `doctor` | versions/services · diagnose cores, nginx, BBR, foreign panels |
| `client links [--qr]` · `client singbox` | share links + subscribe URL · sing-box JSON |
| `sni test [host…]` · `sni discover` | rank SNIs for REALITY fitness · find SNIs hosted near your server |
| `scan` | find CDN edge IPs reachable from this network (survive MCI) |
| `dns validate` · `dns doh [n]` | find resolvers for DNS tunneling · benchmark DoH providers |
| `firewall apply [--yes]` | lock-out-safe default-drop ruleset |
| `update [--self]` · `clean` · `remove` | update cores/self · back up + remove ngray configs · full uninstall |

## Safe by design

- **Only 443/tcp, the hysteria2 UDP port, the fptn port and SSH are ever public.** Core backends
  bind to `127.0.0.1` and are SNI-multiplexed by nginx.
- **Never locks you out:** the firewall builds its rules in a dedicated chain with SSH/lo/
  established accepted *before* the drop.
- **Never touches what it doesn't own:** x3-ui / 3x-ui / hiddify / marzban are detected and left
  alone; a pre-existing `nginx.conf` is backed up before rebuild.
- **Idempotent:** keys, credentials and the subscribe token persist in `proxy-state.json` (0600).

## Build

```bash
make build     # CGO-free static binary, version stamped from git
make check     # fmt + vet + test
make e2e       # real run inside a throwaway Ubuntu container (Docker/Podman)
```

Go 1.26+. Tools cooperate: `server setup` runs the SNI tester before generating configs; the CDN
scanner and `sni discover` feed it good SNIs/IPs.
