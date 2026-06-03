<div align="center">

# ◆ ngray toolkit

**One static Go binary that builds a full multi-core anti-DPI proxy server and hands you the client configs.**
Tuned for how Iran (MCI · Irancell · Rightel), China (GFW) and Russia (TSPU) actually censor.
No domain required. No manual SNI selection. No configuration knowledge required on the first run.

</div>

---

```
◆ ngray toolkit  v0.3.0
  YOUR.SERVER.IP · nginx :443 · hy2 udp :443 · BBR on  ✓ up to date

  1  Server setup / repair
  2  Client links · QR · subscribe
  3  Test SNIs  (REALITY-fitness + latency, live)
  4  Scan CDN edges
  5  Status
  6  Update cores
  7  Firewall
  8  Doctor
  9  Regenerate credentials
```

---

## What it does

On a fresh Ubuntu VPS, `sudo ./ngray` runs an interactive console. Choose **Server setup**:

1. Automatically tests 170 candidate SNIs against your server and picks the best ones for your VPS location.
2. Installs every enabled core (`xray`, `shoes`, `sing-box`, `hysteria2`, `fptn`, and optionally `mieru`, `amneziawg`) — with your permission, one by one.
3. Configures nginx on TCP 443 as an SNI-multiplexer routing different client SNIs to different protocol backends — all invisible on the public interface.
4. Generates keys, credentials and short-IDs that persist across re-runs.
5. Applies a lockout-safe firewall (SSH always survives).
6. Prints every client link plus a QR code and a subscribe URL. Add the subscribe URL once in your client — all configs appear and auto-update.

The result is a stack of **9 different protocol shapes** across 4 cores, all on port 443, covering REALITY, XHTTP, AnyTLS, TUIC, ShadowTLS, Hysteria2, plus independent cores for Rust-based TUIC and Hysteria2 — so at least one survives each operator's DPI on any given day.

---

## Why so many cores and shapes

No single VPS IP reliably crosses both MCI and Irancell at the same time. MCI uses IP-based blocking — REALITY works. Irancell uses a protocol+SNI whitelist with ~50% induced packet loss — Hysteria2 Salamander punches through the loss, REALITY survives if the SNI is whitelisted. You cannot know in advance which shape a specific operator will allow for a specific IP on a specific day, so the toolkit generates the maximum variety:

| Layer | Protocol | Core | Why |
|---|---|---|---|
| TLS disguise | VLESS + REALITY + Vision | xray | Reference impl; best MCI cross |
| HTTP disguise | XHTTP | xray only | Chunked HTTP behind real nginx; xray-exclusive |
| Padding obfuscation | AnyTLS | shoes (Rust), sing-box | Different TLS fingerprint |
| QUIC + obfuscation | Hysteria2 + Salamander | standalone + shoes Rust | Beats 50% packet loss on MTN |
| QUIC standard stack | TUIC v5 | shoes Rust, sing-box | h3 ALPN; different from hy2 |
| TLS mimic | ShadowTLS v3 | sing-box only | Proxies real TLS handshake to live site |
| Full-tunnel VPN | fptn | — | Token-based fallback |
| ML-resistant tunnel | mieru | — | No protocol fingerprint (China/Russia) |
| Obfuscated WireGuard | AmneziaWG | — | Junk headers; Russia-grade |

**SNI pairing strategy**: `www.rust-lang.org` and `rust-lang.org` are different nginx routing keys — both pass REALITY fitness. This doubles coverage: the www variant routes to xray (Vision), the non-www routes to shoes (AnyTLS, Rust fingerprint).

The detailed reasoning is in [docs/protocols.md](docs/protocols.md) and [docs/research-iran-dpi.md](docs/research-iran-dpi.md).

---

## Quickstart

### Deploy to a server

```bash
# Build and package (version auto-read from version.go)
make package

# Upload and run
scp -r pkg/release/ root@SERVER:/opt/ngray/
ssh root@SERVER
cd /opt/ngray
sudo ./ngray                          # → TUI, choose Server setup / repair
```

`pkg/release/` contains the binary, a ready-to-run `config.yml` and the SNI candidate list. No editing needed for the first run — auto-SNI picks optimal SNIs for your VPS location.

### Or install locally

```bash
make build && sudo make install       # installs to /usr/local/bin/ngray
sudo ngray                            # TUI
```

### Or run commands directly

```bash
sudo ngray server setup               # provision everything
ngray client links --qr              # print all links + QR codes
ngray sni test rust-lang.org         # check if an SNI is REALITY-fit
```

---

## Commands

| Command | What it does |
|---|---|
| `ngray` | interactive TUI (ESC = back, q = quit) |
| `server setup [--sandbox]` | auto-SNI → install cores → nginx → subscribe link → firewall |
| `server status` | installed version + service state for every core |
| `doctor` | diagnose nginx, core configs, BBR, cert expiry, foreign panels |
| `client links [--qr]` | print all share links; --qr adds a terminal QR per link |
| `client singbox` | print the sing-box client JSON bundle |
| `sni test [host…]` | REALITY fitness (TLS1.3 + h2 + X25519) + latency ranking |
| `sni discover` | scan /24 near your server to find nearby REALITY-fit IPs |
| `scan` | CDN edge-IP scanner — find IPs that survive MCI (Akamai/Cloudflare/Fastly) |
| `dns validate` | find working DNS resolvers from this network |
| `dns doh [n]` | benchmark DoH providers by latency |
| `firewall apply [--yes]` | preview + apply lockout-safe default-drop ruleset |
| `update [--self]` | update installed cores; --self replaces the ngray binary |
| `clean` | back up then remove ngray-owned configs |
| `remove` | full uninstall (leaves x3-ui/hiddify/marzban untouched) |
| `regen` | clear all keys and credentials; next setup regenerates everything |
| `version` | print semver, commit, build date |

---

## Client apps

Generated links import into:

| App | Platforms | Notes |
|---|---|---|
| **v2rayN** | Windows | Subscribe URL, all link types |
| **nekoray / nekobox** | Windows, Linux, Android | Full sing-box format |
| **husi** | Android | Native sing-box; best AnyTLS/ShadowTLS support |
| **exclave** | iOS | Native sing-box |
| **AmneziaVPN** | All | For AmneziaWG configs (import .conf) |
| **mieru / nekobox** | All | For mieru configs (import JSON) |

**Subscribe URL** (`http://SERVER-IP/s/TOKEN`): add once in any app, all configs appear and update automatically. Plain HTTP — no cert errors, works everywhere.

---

## Configuration

`config.yml` lives next to the binary. `config.example.yml` shows all options with explanations. On a fresh server with no `config.yml`, setup auto-selects optimal SNIs from `snis/candidates.sni`.

Key facts:
- Every REALITY SNI across all cores must be unique (nginx routes one SNI to one backend).
- `www.X` and `X` count as separate SNIs — use both for double coverage.
- Cores without nginx routing (hysteria2, TUIC, ShadowTLS, fptn, mieru, AmneziaWG) have their own public ports.
- `proxy-state.json` (0600) persists all generated keys, credentials and the subscribe token — re-running setup is idempotent.

---

## Safe by design

- **Backends on loopback only.** Core backends (`127.0.0.1:11000-13999`) are invisible to DPI. Only ports 443/tcp, 80/tcp (subscribe), hysteria2/udp and SSH are ever public.
- **Never locks you out.** The NGRAY iptables chain accepts `lo`, `ESTABLISHED`, and SSH *before* the DROP rule. Rules are populated completely before INPUT jumps to the chain.
- **Never touches foreign panels.** x3-ui / 3x-ui / hiddify / marzban are detected and left entirely alone.
- **Never overwrites silently.** A pre-existing `nginx.conf` not owned by ngray is backed up before replacement.
- **Idempotent.** Keys, UUIDs, short-IDs, obfuscation passwords and the subscribe token are generated once and reused forever.
- **Hermetic build.** `CGO_ENABLED=0`, vendored dependencies, `GOPROXY=off` — builds without internet access, verifiable.

---

## Build

```bash
make build      # static binary, version stamped from git
make check      # gofmt + vet + tests
make package    # pkg/release/ — binary + config.yml + SNI list, ready to scp
make dist       # cross-compile linux/amd64 + linux/arm64 with SHA256SUMS
make release    # tag + publish GitHub release (needs gh)
make e2e        # smoke test in a throwaway Ubuntu container (Docker or Podman)
```

Go 1.22+. No CGO. No apt dependencies on the build machine.

---

## Research and docs

| File | Contents |
|---|---|
| [docs/protocols.md](docs/protocols.md) | Core capability matrix, SNI assignment rules, subscribe URL rationale |
| [docs/research-iran-dpi.md](docs/research-iran-dpi.md) | Per-operator DPI: MCI, Irancell/MTN, Rightel |
| [docs/architecture.md](docs/architecture.md) | nginx SNI-mux, port ranges, state model |
| [docs/research-2026.md](docs/research-2026.md) | ML-based DPI, emerging evasion techniques |
| [docs/roadmap.md](docs/roadmap.md) | Future cores (specced with config and link format) |
| [docs/development.md](docs/development.md) | Dev guide: versioning, commit flow, adding cores |
