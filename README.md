<div align="center">

# ◆ ngray toolkit

**One static Go binary that builds a full multi-core anti-DPI proxy server and hands you the client configs.**
Tuned for how Iran (MCI · Irancell · Rightel), China (GFW) and Russia (TSPU) actually censor.
No domain required. No manual SNI selection. No configuration knowledge required on the first run.

</div>

---

```
◆ ngray toolkit  v0.8.0
  YOUR.SERVER.IP · nginx :443 · hy2 udp :443 · BBR on  ✓ up to date

  1  Server setup / repair
  2  Client links · subscribe
  3  Test SNIs  (REALITY-fitness + latency, live)
  r  Re-select SNIs  (re-rank the pool, rewrite config.yml)
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

1. Sweeps a tiered pool of ~470 candidate SNIs for REALITY fitness — a fast two-phase scan (a cheap probe across every host, then full latency measurement only on the ones that pass) — and assigns the best from a proven, operator-whitelist-friendly **preferred** tier (now including AI/dev-infra/crypto/education domains and a **blackout-survivor** set Iran allowlists even during a national shutdown) before ever touching the **extended** fallback tier.
2. Installs every enabled core (`xray`, `shoes`, `sing-box`, `hysteria2`, `fptn`, and optionally `mieru`, `amneziawg`) — with your permission, one by one.
3. Configures nginx on TCP 443 as an SNI-multiplexer routing different client SNIs to different protocol backends — all invisible on the public interface.
4. Generates keys, credentials and short-IDs that persist across re-runs.
5. Applies a lockout-safe firewall (SSH always survives).
6. Prints every client link plus a subscribe URL. Add the subscribe URL once in your client — all configs appear and auto-update. The sing-box JSON bundle includes an **auto** (urltest) group, so the client health-checks every endpoint and routes through whichever survive your operator that day — plus a **blackout** group (the TCP/REALITY subset that can cross a national protocol+SNI allowlist) to switch to when even UDP is dropped.

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
| CDN-fronted | VLESS + WS + TLS | xray (via Cloudflare) | IP-independent: survives a direct-IP blacklist |
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
scp -r pkg/release/ root@SERVER:/root/ngray/
ssh root@SERVER
cd /root/ngray
sudo ./ngray                          # → TUI, choose Server setup / repair
```

`pkg/release/` contains the binary, `config.example.yml` (reference) and the SNI
candidate list. It deliberately ships **no** `config.yml`, so the first
`server setup` takes the fresh-install path and **auto-selects optimal SNIs**
from the server's own vantage; the generated `config.yml` then survives later
deploys. No editing needed for the first run.

One-command deploy (build → ship → provision, preserving `proxy-state.json`):

```bash
bash scripts/deploy.sh [ssh-host]     # pass the ssh host as the first argument
```

### Or install locally

```bash
make build && sudo make install       # installs to /usr/local/bin/ngray
sudo ngray                            # TUI
```

### Or run commands directly

```bash
sudo ngray server setup               # provision everything
ngray client links                    # print all links + configs + sing-box JSON
ngray sni test rust-lang.org          # check if an SNI is REALITY-fit
```

---

## Cloudflare fronting (optional)

Put a real domain in front of the subscription and a service so the server IP is
hidden and the cert is trusted. The token is read from `NGRAY_CF_TOKEN`,
`CF_API_TOKEN`, or `/etc/ngray/cf_token` (0600) — never hardcoded.

```bash
ngray domain set app.example.com          # enable CDN fronting in config.yml
ngray domain dns app.example.com          # upsert the proxied A record
ngray domain issue app.example.com        # issue + install an Origin certificate
ngray domain check app.example.com        # verify DNS, CF edge, origin TLS
ngray server setup                        # apply nginx + bundle
```

Set Cloudflare SSL/TLS to **Full (strict)**. The origin cert is an ECDSA
Cloudflare Origin CA certificate valid for the domain (+ `www.`).

## Commands

| Command | What it does |
|---|---|
| `ngray` | interactive TUI (ESC = back, q = quit) |
| `server setup [--sandbox]` | auto-SNI → install cores → nginx → subscribe link → firewall |
| `server status` | installed version + service state for every core |
| `doctor` | diagnose nginx, core configs, BBR, cert expiry, foreign panels |
| `client links` | print all share links + mieru/AmneziaWG/fptn configs + sing-box JSON |
| `client singbox` | print the sing-box client JSON bundle (with auto-failover groups) |
| `sni test [host…]` | REALITY fitness (TLS1.3 + h2 + X25519) + latency, two-phase sweep (scales to 1000+) |
| `sni discover` | scan /24 near your server to find nearby REALITY-fit IPs |
| `domain check <d>` | is a domain a working Cloudflare-fronted path to this server? (DNS via 1.1.1.1/9.9.9.9/AdGuard, CF edge status, origin TLS) with scenario advice |
| `domain set <d>` | enable Cloudflare fronting for a domain in `config.yml` |
| `domain issue <d>` | issue + install a Cloudflare Origin certificate (token from env or `/etc/ngray/cf_token`) |
| `domain dns <d> [--ip IP]` | create/update the Cloudflare A record (`--proxied=false` for grey-cloud) |
| `domain cert <crt> <key>` | install an existing Cloudflare Origin Certificate + key |
| `proxy add <d> --target <url>` | serve an arbitrary HTTP(S) service behind a trusted domain (nginx reverse proxy) |
| `proxy list` / `proxy remove <d>` | list / remove configured proxy routes |
| `config init` | write a default config.yml (auto-detects location) |
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

`client links` prints the subscribe URLs in two tiers — pick by format and by whether you've set up a Cloudflare domain (optional, see below):

**Primary — via Cloudflare** (when a CDN domain is configured): served on the domain over port 443, so there is **no port in the URL**, the cert is Cloudflare's **trusted edge cert** (no skip-verify), and your **origin IP is hidden**.

| URL | Format | Best for |
|---|---|---|
| `https://www.DOMAIN/sb/TOKEN` | **sing-box JSON** (auto + select groups) | NekoBox/husi · sing-box · Hiddify — full set, **incl. mieru + AmneziaWG** |
| `https://www.DOMAIN/s/TOKEN` | base64 URI list | v2rayNG · v2rayN · Streisand · Shadowrocket — URI configs (no mieru/AmneziaWG) |

**Fallback — direct IP** (always produced; use when the domain is blocked): self-signed TLS, so enable skip-cert-verify or pin the printed `pinSHA256`.

| URL | Format |
|---|---|
| `https://SERVER-IP:2053/sb/TOKEN` | sing-box JSON |
| `https://SERVER-IP:2053/s/TOKEN` | base64 URI list |
| `http://SERVER-IP/s/TOKEN` | base64 over plain HTTP (no cert errors anywhere) |

Add one URL in any app; all configs appear and auto-update, and the sing-box `auto` group health-checks every endpoint so dead ones are skipped automatically. **mieru and AmneziaWG have no share-link URI**, so they ride only the **sing-box JSON** bundle (`/sb/`) — import that one in NekoBox/husi to get them. The raw mieru JSON / AmneziaWG `.conf` are also printed by `client links` for the standalone mieru / AmneziaVPN apps.

---

## Configuration

`config.yml` lives next to the binary. `config.example.yml` shows all options with explanations. On a fresh server with no `config.yml`, setup auto-selects optimal SNIs from `snis/candidates.sni`.

Key facts:
- Every REALITY SNI across all cores must be unique (nginx routes one SNI to one backend).
- `www.X` and `X` count as separate SNIs — use both for double coverage.
- Auto-SNI consumes essentially the whole REALITY-fit pool (hundreds of configs), quality-first: the proven tier and low-latency HTTP-200 destinations lead, slow hosts are dropped. `subscription.front_sni` is given its own dedicated SNI so it never collides with a route.
- `cores.xray.cdn` enables optional Cloudflare fronting (domain WS proxy + CF-served subscription) — see [Cloudflare fronting](#cloudflare-fronting-optional-ip-independent-fallback).
- Cores without nginx routing (hysteria2, TUIC, ShadowTLS, fptn, mieru, AmneziaWG) have their own public ports.
- `proxy-state.json` (0600) persists all generated keys, credentials and the subscribe token — re-running setup is idempotent.

### SNI pool tiers

`snis/candidates.sni` is split by a `#@extended` marker into two tiers. Auto-SNI
assigns from the **preferred** tier (above the marker — proven CDN/dev/Linux/
Apple/MS/Samsung/Google domains, plus AI platforms/APIs, crypto data/wallets,
education, and a **blackout-survivor** set Iran allowlists even mid-shutdown:
ChatGPT/OpenAI, App Store, …) and only falls back to the **extended** tier
(crypto exchanges, Chinese/Russian/Azerbaijani sites for shape variety) when
preferred runs out of REALITY-fit hosts. Ranking is by latency *within* a
tier, so a faster-but-unwhitelisted domain never displaces a proven one. Paste
your own domains above the marker to prioritise them; below it to keep them as
fallback only.

**Auto-SNI runs only on a fresh install** (no `config.yml`). Rebuilding the
binary or editing the pool does **not** re-pick SNIs on its own — run **Re-select
SNIs** in the TUI (rewrites `config.yml` with the current tiered ranking), then
re-run **Server setup**; or delete `config.yml` and run setup.

---

## Cloudflare fronting (optional, IP-independent fallback)

Every direct-IP config — REALITY, AnyTLS, Hysteria2, etc. — dies at once the day
the censor blacklists your VPS IP. Putting one path behind Cloudflare fixes that:
the client connects to a Cloudflare edge IP (which the censor can't blanket-block
without huge collateral), and CF proxies to your origin. Configure a domain under
`cores.xray.cdn` and ngray adds:

- **A WebSocket proxy** on `DOMAIN` — `VLESS + WS + TLS`, served behind the same
  443 SNI-mux, terminating TLS with a Cloudflare **Origin Certificate**. This is
  the config that survives a direct-IP block.
- **The subscription** on `www.DOMAIN` — served on 443 via Cloudflare, so the
  subscribe URL has no port, a trusted cert, and a hidden origin IP.

What can and cannot traverse Cloudflare:

- ✅ **VLESS + WebSocket + TLS** — works on the Cloudflare **Free** plan.
- ❌ **REALITY** — no. CF terminates TLS at its edge, breaking REALITY's
  origin-handshake authentication. REALITY stays direct-IP only.
- ❌ **Hysteria2 / TUIC (UDP/QUIC)** — no. CF doesn't proxy arbitrary UDP.
- ❌ **XHTTP / SplitHTTP** — not on Free (CF buffers responses); use WS.

Setup (one-time):

1. Point the domain at your server in Cloudflare DNS, **Proxied** (orange cloud).
   A `www` CNAME → root, also proxied, hosts the subscription.
2. Cloudflare → SSL/TLS → **Origin Server → Create Certificate**, then on the
   server: `ngray domain cert origin.crt origin.key`.
3. Cloudflare → SSL/TLS → mode = **Full (strict)**.
4. `ngray server setup`, then verify: `ngray domain check DOMAIN` → expect
   `CF_PROXIED_HEALTHY`. (`525` means the origin cert isn't installed yet.)

`ngray domain check` is scenario-aware: it resolves via public DNS (1.1.1.1 /
9.9.9.9 / AdGuard, bypassing a poisoned local resolver), reads the Cloudflare
edge status, tests the origin TLS handshake, and tells you exactly which paths
the domain covers — so you know whether you're protected for an IP block, an
SNI block, or neither.

---

## If a config times out

A healthy server (all services `up`) with some dead configs is almost always a
**per-operator SNI whitelist** issue, not a server fault. REALITY only crosses
the DPI if the SNI you send is one the operator allows — and MCI, Irancell and
Rightel each allow a different set.

- The SNI fitness test runs **on the VPS**, which can reach every SNI, so it
  cannot see your operator's whitelist. Server-side "fit" ≠ client-usable.
- The only reliable signal is testing from **inside** the censored network: try
  several configs and keep the SNIs that connect, or run a TLS probe to your
  server's `:443` with each candidate SNI from your phone/PC.
- Import the **sbJSON** subscribe URL and use its `auto` group — it skips dead
  endpoints for you instead of you trying each by hand.
- UDP cores (Hysteria2, TUIC, AmneziaWG) also depend on UDP/QUIC being allowed;
  Irancell throttles UDP/443 heavily — the Hysteria2 alt-ports help.

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
| [docs/censorship-2026.md](docs/censorship-2026.md) | What works/deprecated in 2026 + opt-in upgrades (FinalMask, AWG 2.0, Gecko) |
| [docs/roadmap.md](docs/roadmap.md) | Future cores (specced with config and link format) |
| [docs/development.md](docs/development.md) | Dev guide: versioning, commit flow, adding cores |
