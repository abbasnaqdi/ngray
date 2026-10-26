#!/usr/bin/env bash
# ngray installer — installs a prebuilt binary from GitHub Releases, or builds
# from source when no release is available yet (e.g. Actions not enabled).
#
#   curl -fsSL https://raw.githubusercontent.com/abbasnaqdi/ngray/main/install.sh | sudo bash
#
# Works with bash and zsh. No GitHub API calls (avoids rate limits).
set -euo pipefail

REPO="abbasnaqdi/ngray"
BIN="/usr/local/bin/ngray"
DIR="/etc/ngray"
BASE="https://github.com/${REPO}/releases/latest/download"

say()  { printf '  %s\n' "$1"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$1"; }
warn() { printf '  \033[33m!\033[0m %s\n' "$1"; }
die()  { printf '  \033[31m✗\033[0m %s\n' "$1" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "run as root (use sudo)"

# ── platform ─────────────────────────────────────────────────────────────────
case "$(uname -s)" in
    Linux)  OS="linux" ;;
    Darwin) OS="darwin" ;;
    *) die "unsupported OS: $(uname -s)" ;;
esac
case "$(uname -m)" in
    x86_64|amd64)  ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *) die "unsupported arch: $(uname -m)" ;;
esac
ASSET="ngray-${OS}-${ARCH}"

command -v curl >/dev/null 2>&1 || die "curl is required"
ok "${OS}/${ARCH}"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# ── source build (fallback) ──────────────────────────────────────────────────
build_from_source() {
    command -v git >/dev/null 2>&1 || die "git is required to build from source"
    if ! command -v go >/dev/null 2>&1; then
        gover="${GO_VERSION:-1.26.0}"
        say "installing Go ${gover} (one-time)…"
        curl -fsSL "https://go.dev/dl/go${gover}.${OS}-${ARCH}.tar.gz" -o "$TMP/go.tgz" \
            || die "could not download Go"
        rm -rf /usr/local/go
        tar -C /usr/local -xzf "$TMP/go.tgz" || die "could not extract Go"
        export PATH="/usr/local/go/bin:$PATH"
        ok "Go $(go version | awk '{print $3}')"
    fi
    src="$TMP/src"
    git clone --depth 1 --branch main "https://github.com/${REPO}.git" "$src" \
        || die "git clone failed (is the repository public?)"
    ( cd "$src" && make build ) || die "build failed"
    install -m 0755 "$src/ngray" "$TMP/ngray"
    if [ -f "$src/snis/candidates.sni" ]; then
        cp "$src/snis/candidates.sni" "$TMP/candidates.sni"
    fi
    ok "built from source"
}

# ── download prebuilt binary, or build ───────────────────────────────────────
if curl -fsSL "${BASE}/${ASSET}" -o "$TMP/ngray" 2>/dev/null \
   && curl -fsSL "${BASE}/SHA256SUMS" -o "$TMP/SHA256SUMS" 2>/dev/null; then
    curl -fsSL "${BASE}/candidates.sni" -o "$TMP/candidates.sni" 2>/dev/null || true
    EXPECTED=$(grep " ${ASSET}$" "$TMP/SHA256SUMS" | awk '{print $1}' | head -1 || true)
    [ -n "$EXPECTED" ] || die "no checksum for ${ASSET}"
    if command -v sha256sum >/dev/null 2>&1; then
        ACTUAL=$(sha256sum "$TMP/ngray" | awk '{print $1}')
    else
        ACTUAL=$(shasum -a 256 "$TMP/ngray" | awk '{print $1}')
    fi
    [ "$EXPECTED" = "$ACTUAL" ] || die "checksum mismatch"
    ok "downloaded + verified"
else
    warn "no prebuilt release available — building from source"
    build_from_source
fi

# ── install ──────────────────────────────────────────────────────────────────
install -d -m 0750 "$DIR" "$DIR/snis"
install -m 0755 "$TMP/ngray" "$BIN"
if [ -s "${TMP}/candidates.sni" ]; then
    install -m 0640 "$TMP/candidates.sni" "$DIR/snis/candidates.sni"
fi
ok "installed $BIN"

# ── config ───────────────────────────────────────────────────────────────────
if [ ! -f "$DIR/config.yml" ]; then
    if ( cd "$DIR" && "$BIN" config init >/dev/null 2>&1 ); then
        ok "config $DIR/config.yml"
    else
        warn "could not write $DIR/config.yml — run: sudo ngray config init"
    fi
fi

say ""
say "ngray $("$BIN" version 2>/dev/null | awk '{print $2}') is ready."
say "  ngray              open the TUI"
say "  ngray proxy add …  put a service behind a domain"
say ""

# ── launch TUI when attached to a terminal ───────────────────────────────────
if [ -t 0 ] && [ -t 1 ]; then
    "$BIN" || true
fi
