<div align="center">

# ◆ ngray toolkit

**One static Go binary that builds a full multi-core anti-DPI proxy server and hands you the client configs.**
Tuned for how Iran (MCI · Irancell · Rightel), China (GFW) and Russia (TSPU) actually censor.
No domain required. No manual SNI selection. No configuration knowledge required on the first run.

</div>

---

```
◆ ngray toolkit  v0.8.1
  YOUR.SERVER.IP · nginx :443 · hy2 udp :443 · BBR on  ✓ up to date

  1  Server setup / repair
  r  Re-select SNIs
  2  Client links · subscribe
  3  Test SNIs (live)
  4  Scan CDN edges
  5  Status
  6  Update cores
  7  Firewall
  8  Doctor
  p  Domain / proxy   (Cloudflare cert + DNS + reverse proxy)
  u  Update ngray     (self-update: release w/ SHA256, or source rebuild)
  c  Cleanup          (roll back: nginx, services, configs, firewall, state, binaries)
  9  Regenerate credentials
```

Updating ngray itself is one command — no git, no Go, no scripts:

```bash
sudo ngray update --self      # CLI, same as menu item `u`
```

It replaces the binary with the latest release (SHA256-verified against the
published SHA256SUMS and refreshes the SNI pool). If the repo has no release
yet (private / Actions billing locked) and you are in a checkout, it falls
back to `scripts/update.sh` (fetch + build + install) automatically.

Every plain `sudo ngray` launch also does a bounded, silent self-update check
first: if a newer release exists it is downloaded, SHA256-verified, installed,
and the TUI re-executes into the new binary (no manual restart). Opt out with
`NGRAY_NO_SELFUPDATE=1`.

---

## What it does

On a fresh Ubuntu VPS, `sudo ./ngray` runs an interactive console. Choose **Server setup**:

1. Sweeps a tiered pool of ~1378 candidate SNIs for REALITY fitness — a fast two-phase scan (a cheap probe across every host, then full latency measurement only on the ones that pass) — and assigns the best from a proven, operator-whitelist-friendly **preferred** tier (now including AI/dev-infra/crypto/education domains and a **blackout-survivor** set Iran allowlists even during a national shutdown) before ever touching the **extended** fallback tier.
2. Installs every enabled core (`xray`, `shoes`, `sing-box`, `hysteria2` and `mieru`) — with your permission, one by one.
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
| ML-resistant tunnel | mieru | — | No protocol fingerprint (China/Russia) |

**SNI pairing strategy**: `www.rust-lang.org` and `rust-lang.org` are different nginx routing keys — both pass REALITY fitness. This doubles coverage: the www variant routes to xray (Vision), the non-www routes to shoes (AnyTLS, Rust fingerprint).

**Newer protocol candidates — verified 2026 assessment** (kept out of the core set on purpose; every entry below was checked against its project, not folklore):

- **juicity** — QUIC proxy in the same family as TUIC/Hysteria2; the Go implementation has not seen a release for years (a community Rust rewrite appeared in 2026). Hysteria2 supersedes it feature-for-feature, so it adds no fingerprint diversity. ✗ skipped.
- **naive (NaïveProxy)** — genuinely probe-resistant, but its server needs the patched *Caddy + forwardproxy plugin* — a second full web server next to nginx for one credential shape. ✓ concept, ✗ skipped (operational cost > value alongside REALITY/XHTTP).
- **snell v5/v6** — sing-box can now serve it, but the client ecosystem is split (Clash-family uses v4, sing-box only v5+); without a verified client on the sender side a server would hand out dead configs. ⏸ re-check when the client side settles.
- **trusttunnel** (HTTP/2+HTTP/3 tunnel) — promising and young; supported only in mihomo 1.19+ and custom sing-box forks, no distribution-track server binary. ⏸ watch.
- **MASQUE** (HTTP/3 CONNECT-IP) — the one verified 2026 addition: sing-box 1.14 (Oct 2026) ships client *and* server endpoints, mobile clients are adding it right now. ⏸ planned: enable as a sing-box inbound once the client apps you use support it.
- **XHTTP via Cloudflare** (`cdn.xhttp: true`) — packet-up/GET-mode XHTTP is designed for CDN traversal, but CF Free response-buffering historically breaks it; Flask-free configs keep it opt-in until *your* `domain check` + client test proves it on your free plan.

What ngray does keep verified-current in 2026: Xray 26.x warns that **plain WS is deprecated** — nginx-hide XHTTP is the recommended direct path (already generated), REALITY RST-floods from Iran's DPS are answered by SNI rotation + the CDN twins, and sing-box 1.14.4 is the deployed core (ngray pins/updates it).

The detailed reasoning is in [docs/protocols.md](docs/protocols.md) and [docs/research-iran-dpi.md](docs/research-iran-dpi.md).

---

## Quickstart

### One-command install (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/abbasnaqdi/ngray/main/install.sh | sudo bash
```

Downloads the latest prebuilt binary from GitHub Releases (verified with
SHA256SUMS), installs it to `/usr/local/bin/ngray`, fetches the SNI list,
initializes `/etc/ngray/config.yml`, and hands off to the TUI. If no release
binary is available yet, it builds from source and installs Go if missing.
Every push to `main` republishes the binary, so re-running this upgrades in
place.

### Update

```bash
sudo ngray update --self    # one command; falls back to scripts/update.sh in a checkout
```

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

**Two ways, pick one:**

- **API Token (recommended, automatic):** `ngray domain token` prints the exact
  2-minute recipe: create it under **My Profile → API Tokens** (NOT
  "Manage Account → Account API Tokens" — those account-owned tokens start
  with `cfat_` and cannot be used), with two permissions: `Zone · DNS · Edit`
  + `Zone · SSL and Certificates · Edit` (the second one issues the Origin CA
  certificate). A user token starts with `cfut_`. Then the TUI `p` form does
  everything; pasted tokens are auto-cleaned (quotes, `Bearer`, copied
  whitespace/RTL marks). Violations fail fast with the right fix. SSL mode →
  **Full (strict)**.
- **No token (manual DNS):** create the A record yourself in the dashboard,
  leave the token empty, and ngray issues a **self-signed origin cert** instead —
  set SSL mode → **Full (non-strict)**. Works end-to-end, no credentials stored.

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
| `doctor` | diagnose nginx, cores, the domain path, LIVE subscription probes (all URLs, auto-repairs unreadable-bundle 404s), certs expiry, BBR, foreign panels |
| `client links` | print all share links + mieru config + sing-box JSON |
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
| **mieru / nekobox** | All | For mieru configs (import JSON) |

`client links` prints the subscribe URLs in two tiers — pick by format and by whether you've set up a Cloudflare domain (optional, see below):

**Primary — via Cloudflare** (when a CDN domain is configured): served on the domain over port 443, so there is **no port in the URL**, the cert is Cloudflare's **trusted edge cert** (no skip-verify), and your **origin IP is hidden**.

| URL | Format | Best for |
|---|---|---|
| `https://www.DOMAIN/sm/TOKEN` | **mixed plain-text** — raw share links + full sing-box JSON, **nothing base64-encoded** | nekoray/nekobox & any client reading text subscriptions — ONE URL for everything (incl. mieru via the JSON) |
| `https://www.DOMAIN/sb/TOKEN` | **sing-box JSON** (auto + select groups) | NekoBox/husi · sing-box · Hiddify — full set, **incl. mieru** |
| `https://www.DOMAIN/s/TOKEN` | base64 URI list | v2rayNG · v2rayN · Streisand · Shadowrocket — URI configs (mieru only in JSON) |

**Fallback — direct IP** (always produced; use when the domain is blocked): self-signed TLS, so enable skip-cert-verify or pin the printed `pinSHA256`.

| URL | Format |
|---|---|
| `https://SERVER-IP:2053/sm/TOKEN` | mixed plain-text (raw links + JSON) |
| `https://SERVER-IP:2053/sb/TOKEN` | sing-box JSON |
| `https://SERVER-IP:2053/s/TOKEN` | base64 URI list |
| `http://SERVER-IP/s/TOKEN` | base64 over plain HTTP (no cert errors anywhere) |

Add one URL in any app; all configs appear and auto-update, and the sing-box `auto` group health-checks every endpoint so dead ones are skipped automatically. **mieru has no share-link URI**, so it rides only the **sing-box JSON** bundle (`/sb/`) — import that one in NekoBox/husi to get it. The raw mieru JSON is also printed by `client links` for the standalone mieru app.

**Endpoint pairs:** every SNI is served as a **pair** — one direct-IP link (`<core>-…-<sni>`) and one Cloudflare-fronted twin (`cf-ws-<sni>` on `https://www.DOMAIN/…`, no port, trusted cert). The TUI menu 2 is a pure link sheet: one URI per line, **no visual wrapping**; copying never depends on dragging wrapped text — `y` copies the primary link, `a` copies every link on the sheet, `1`–`9` copy exact entries (all via OSC 52, over SSH too). The plain-CLI `ngray client links` prints the same sheet unwrapped for scrollback copying.

---

## Configuration

`config.yml` lives next to the binary. `config.example.yml` shows all options with explanations. On a fresh server with no `config.yml`, setup auto-selects optimal SNIs from `snis/candidates.sni`.

Key facts:
- Volume policy: **≤24 REALITY routes per core** + ≤8 XHTTP (older configs are trimmed to this on every setup — hundreds-of-configs installs converge to the same readable size). The **primary subscribe link stays FIXED across setups**; rotation is opt-in (wizard toggle *Rotate sub link* — a rotated link kills all previously distributed URLs through nginx's token guard).
- Every REALITY SNI across all cores must be unique (nginx routes one SNI to one backend).
- `www.X` and `X` count as separate SNIs — use both for double coverage.
- Iran policy: hosts filtered/interfered with on Iranian networks (Google, YouTube, X/Twitter, Telegram, GitHub, Meta, Wikipedia, OpenAI, TikTok, …) are **excluded as SNI**, alongside destinations requiring >280ms from the server; each quality SNI is served as a *pair* — the CF-fronted domain subscription (primary) + its direct-IP share link. `subscription.front_sni` is given its own dedicated SNI so it never collides with a route.
- `cores.xray.cdn` enables optional Cloudflare fronting (domain WS proxy + CF-served subscription) — see [Cloudflare fronting](#cloudflare-fronting-optional-ip-independent-fallback).
- Cores without nginx routing (hysteria2, TUIC, ShadowTLS, mieru) have their own public ports.
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
- **A domain twin for every route SNI** — the domain+IP pairing rule: for each
  REALITY/XHTTP SNI there is one direct-IP config *and* one `cf-ws-<sni>` config
  on `https://www.DOMAIN/t/<token>/<fp>` (plain WS behind the nginx TLS server,
  rotating together with the subscription token). When Iran's DPI starts
  RST-flooding a REALITY handshake or the VPS IP is blocked, the twin still
  carries the same SNI's traffic. UDP cores (Hysteria2/TUIC/mieru) cannot ride
  Cloudflare — they stay direct-IP.

What can and cannot traverse Cloudflare:

- ✅ **VLESS + WebSocket + TLS** — works on the Cloudflare **Free** plan.
- ❌ **REALITY** — no. CF terminates TLS at its edge, breaking REALITY's
  origin-handshake authentication. REALITY stays direct-IP only.
- ❌ **Hysteria2 / TUIC (UDP/QUIC)** — no. CF doesn't proxy arbitrary UDP.
- ❌ **XHTTP / SplitHTTP** — not guaranteed on Free (CF response buffering);
  opt-in testable: `cores.xray.cdn.xhttp: true` adds a VLESS+XHTTP inbound on
  `www.DOMAIN` behind the same 443 mux (nginx hides the xray fingerprint, which
  is what the Xray project recommends). Verified by your own `domain check` +
  client test: if it 502s through CF, stay on the WS route.

### Reach services by path (no extra ports)

Censors block arbitrary ports, so anything not on 443/80 can become unreachable
from inside Iran. Add your project ports under `cores.xray.cdn.path_ports`
(with the TUI form: *Domain / proxy → Path ports*) and every one of them comes
up as a PATH on the domain, plus works with and WITHOUT Cloudflare:

```yaml
cores:
  xray:
    cdn:
      enabled: true
      domain: example.org
      ws: true
      path_ports: [20128, 3000]   # also editable in the TUI domain form
```

- `http://example.org/20128/dashboard` → `http://127.0.0.1:20128/dashboard`
- `https://www.example.org/20128/dashboard` → same service over TLS
- Trailing-slash redirect included (`/20128` → `/20128/`), requests carry the
  real client IP (`X-Real-IP`, `X-Forwarded-For`) and the original `Host`.
- Ports 80, 443, the mux port and the sub HTTPS port are rejected — digit
  prefixes can never shadow the `/s/` and `/sb/` subscription routes (those are
  regex locations and always win over prefix locations in nginx).

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
- UDP cores (Hysteria2, TUIC) also depend on UDP/QUIC being allowed;
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
