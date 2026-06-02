# ng-ray

A single Go binary that provisions and manages a multi-core anti-DPI proxy
server and emits client configs — tuned for how Iranian mobile carriers
(MCI / Irancell-MTN / Rightel) actually censor. No domain required.

```
ngray            # interactive TUI
ngray <command>  # same actions, scriptable
```

## What it is / project goals

One tool that consolidates what used to be several scripts. It:

- **Provisions a server** with multiple proxy cores behind one nginx on **TCP 443**,
  SNI-multiplexed, so the only public TCP surface looks like ordinary HTTPS.
- **Runs five cores** — `xray` (REALITY + XHTTP), `shoes` (VLESS+Vision / AnyTLS),
  `sing-box` (AnyTLS / Vision), and two **independent** cores `hysteria2` (QUIC/UDP)
  and `fptn` (TUN VPN). It downloads and installs each one on first use, with your
  permission.
- **Generates many config shapes** — `(SNI × core × transport)` — because no single
  VPS IP reliably crosses both MCI and MTN, so the goal is that at least one shape
  lands on each operator. See [docs/research-iran-dpi.md](docs/research-iran-dpi.md).
- **Emits client output**: `vless://`, `anytls://`, `hysteria2://` links, a sing-box
  JSON bundle, terminal QR codes, and a fixed **subscribe link** served by nginx on
  443 under a random token path (import once into v2rayN / nekoray / nekobox / husi /
  exclave; it auto-updates).
- **Stays safe**: a lockout-proof firewall, free-port preflight before binding,
  loopback-only core backends, idempotent re-runs that reuse keys.

Everything is driven by `config.yml` next to the binary. State (keys, credentials,
port assignments, the subscribe token) lives in `proxy-state.json` (mode 0600) and is
reused across runs.

> Design rationale, the per-operator DPI research, the protocol matrix and the
> architecture are in [docs/](docs/). Read those to understand *why* before changing
> behaviour.

## Quickstart

### Server (Ubuntu, as root)

```bash
make build && sudo make install
cp config.example.yml config.yml      # edit SNIs/ports to taste
sudo ngray server setup               # installs cores (asks first), provisions, prints links
```

`server setup` detects the public IP, installs nginx + the enabled cores, writes each
core config, builds the nginx SNI mux + subscription endpoint, enables BBR, starts the
services, applies the firewall, and prints every share link plus the subscribe URL.

### Client

```bash
ngray client links          # share links + subscribe URL
ngray client links --qr     # also print QR codes
ngray client singbox        # sing-box JSON bundle
```

Add the printed `https://<front-sni>/s/<token>` subscribe link in your client
(allow-insecure / skip-cert-verify, since the endpoint uses a self-signed cert — no
domain required).

## Commands

| Command | Does |
|---|---|
| `ngray` | interactive TUI menu |
| `ngray server setup` | provision/repair the server from `config.yml` |
| `ngray server status` | core versions, services, and available updates |
| `ngray client links [--qr]` | share links, QR, subscribe URL |
| `ngray client singbox` | sing-box client JSON |
| `ngray sni test [host…]` | REALITY fitness (TLS1.3 + h2 + X25519) + latency |
| `ngray scan` | CDN edge-IP scanner (find IPs that survive MCI) |
| `ngray firewall apply [--yes]` | preview / apply the safe firewall |
| `ngray update [--self]` | update cores, or ngray itself |
| `ngray regen` | clear keys/credentials before a fresh setup |
| `ngray version` | build version |

## config.yml

See [config.example.yml](config.example.yml). Each core's REALITY entries bind one
whitelisted SNI; nginx routes that SNI to a loopback backend in the core's port band
(xray 11xxx, shoes 12xxx, sing-box 13xxx — all `127.0.0.1`, never exposed). `hysteria2`
and `fptn` get their own public ports. `subscription.front_sni` must be a dedicated SNI
not used by any core route (validated at load).

## Build

```bash
make build        # CGO-free static binary with version stamped from git
make test vet
```

Go 1.26+. The only public ports are 443/tcp (nginx), the hysteria2 UDP port, the fptn
TCP port, and SSH.
