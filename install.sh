#!/usr/bin/env bash
# ngray installer — downloads the latest prebuilt binary from GitHub Releases.
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

# ── download ─────────────────────────────────────────────────────────────────
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
curl -fsSL "${BASE}/${ASSET}" -o "$TMP/ngray" || die "download failed: ${BASE}/${ASSET}"
curl -fsSL "${BASE}/SHA256SUMS" -o "$TMP/SHA256SUMS" || die "could not download SHA256SUMS"
curl -fsSL "${BASE}/candidates.sni" -o "$TMP/candidates.sni" 2>/dev/null || true
ok "downloaded"

# ── verify ───────────────────────────────────────────────────────────────────
EXPECTED=$(grep " ${ASSET}$" "$TMP/SHA256SUMS" | awk '{print $1}' | head -1)
[ -n "$EXPECTED" ] || die "no checksum for ${ASSET}"
ACTUAL=$(sha256sum "$TMP/ngray" | awk '{print $1}')
[ "$EXPECTED" = "$ACTUAL" ] || die "checksum mismatch"
ok "checksum verified"

# ── install ──────────────────────────────────────────────────────────────────
install -d -m 0750 "$DIR" "$DIR/snis"
install -m 0755 "$TMP/ngray" "$BIN"
[ -s "$TMP/candidates.sni" ] && install -m 0640 "$TMP/candidates.sni" "$DIR/snis/candidates.sni"
ok "installed $BIN"

# ── config ───────────────────────────────────────────────────────────────────
if [ ! -f "$DIR/config.yml" ]; then
    ( cd "$DIR" && "$BIN" config init >/dev/null 2>&1 ) || true
    ok "config $DIR/config.yml"
fi

say ""
say "ngray is ready."
say "  ngray              open the TUI"
say "  ngray proxy add …  put a service behind a domain"
say ""

# ── launch TUI when attached to a terminal ───────────────────────────────────
if [ -t 0 ] && [ -t 1 ]; then
    "$BIN" || true
fi
