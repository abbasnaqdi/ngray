# Development guide

## Version management

Version lives in ONE place: `internal/version/version.go` → `Version = "x.y.z"`.

The Makefile reads it automatically — no need to pass `VERSION=` anywhere.

```bash
# Build locally
make build

# Package for deployment (pkg/release/: binary + config.example.yml + SNI list;
# ships NO config.yml so the first setup auto-selects SNIs)
make package

# Deploy (or: bash scripts/deploy.sh [ssh-host])
scp -r pkg/release/ root@SERVER:/root/ngray/
ssh root@SERVER "cd /root/ngray && sudo ./ngray"

# Cross-compile + publish GitHub release
make release
```

## Version bump policy

- **Patch** (x.y.Z): bug fixes, doc updates
- **Minor** (x.Y.0): new protocol, new core, new TUI feature
- **Major** (X.0.0): breaking config schema change

Bump `internal/version/version.go` before committing the release commit.

## Commit flow

Every completed, verified feature gets its own commit. Gate: `make check` must
pass (fmt + vet + tests). No permission needed to commit in this project.

## Research → docs

When you learn something new about a core's behavior, DPI technique, or
protocol property, write it into `docs/`. These files are the memory of the
project and inform future decisions. Key docs:

- `docs/protocols.md` — core capability matrix, assignment rules
- `docs/research-iran-dpi.md` — per-operator bypass strategy
- `docs/research-2026.md` — emerging protocols and ML detection landscape
- `docs/roadmap.md` — future cores (specced but not yet implemented)

## Architecture decisions recorded here

**Why backends listen on 127.0.0.1 only:** nginx ssl_preread mux routes to
loopback. Binding to 0.0.0.0 would expose backend ports to the internet even
with firewall default-drop, which is a defense-in-depth violation.

**Why subscription uses plain HTTP port 80:** There is no way to obtain a
browser-trusted TLS cert for a bare IP without a domain. Self-signed certs
cause client errors that confuse users. Plain HTTP is standard for proxy sub
servers; the 32-hex token provides access obscurity.

**Why shoes is first-class alongside xray:** Empirically, shoes' Rust TLS stack
produces a different ClientHello fingerprint from Go-based xray and sing-box.
In some operator/IP combinations this matters. Never reduce it to a fallback.

**Why epDirect was removed from nginx-backed cores:** Loopback backend ports
(11000-13999) are not reachable from outside the server by design. Advertising
them as client endpoints was misleading.

**Why Hysteria2 standalone binary is preferred over singbox:** The standalone
hysteria2 binary is the reference implementation with the most complete
Salamander obfuscation and brutal CC implementation. singbox Hysteria2 is
marked ⚠️ because it may lag behind in obfuscation updates.

## Adding a new core

1. Add config struct to `internal/config/config.go` (Enabled/Port/SNI fields)
2. Create `internal/cores/<name>.go` implementing the `Core` interface
3. Register in `cores.Registry()` in `internal/cores/core.go`
4. Wire firewall in `internal/server/setup.go applyFirewall()`
5. Add to `config.example.yml` with `enabled: false` if non-default
6. Document in `docs/protocols.md` capability matrix
