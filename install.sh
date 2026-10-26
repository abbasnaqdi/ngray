#!/usr/bin/env bash
# ngray one-command installer.
# Usage: curl -fsSL https://raw.githubusercontent.com/abbasnaqdi/ngray/main/install.sh | sudo bash
set -euo pipefail

REPO="abbasnaqdi/ngray"
INSTALL_BIN="/usr/local/bin/ngray"
CORE_DIR="/etc/ngray"
LOG_FILE="/var/log/ngray-install.log"

BOLD=$'\033[1m'; DIM=$'\033[2m'; GREEN=$'\033[32m'; CYAN=$'\033[36m'
RED=$'\033[31m'; YELLOW=$'\033[33m'; RESET=$'\033[0m'
step() { printf '\n%s▸ %s%s\n' "$CYAN" "$1" "$RESET"; }
ok()   { printf '  %s✓%s %s\n' "$GREEN" "$RESET" "$1"; }
info() { printf '  %s·%s %s\n' "$DIM" "$RESET" "$1"; }
warn() { printf '  %s!%s %s\n' "$YELLOW" "$RESET" "$1"; }
fail() { printf '  %s✗%s %s\n' "$RED" "$RESET" "$1" >&2; exit 1; }

umask 077
exec > >(tee -a "$LOG_FILE") 2>&1

[[ $EUID -eq 0 ]] || fail "must run as root (use sudo)"

step "ngray installer"
info "repo: github.com/$REPO"

# ── OS / arch detection ──────────────────────────────────────────────────────
case "$(uname -s)" in
    Linux) ;;
    *) fail "unsupported OS: $(uname -s) (Linux required)" ;;
esac
case "$(uname -m)" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) fail "unsupported arch: $(uname -m)" ;;
esac
ok "linux/$ARCH"

# ── system dependencies ──────────────────────────────────────────────────────
if ! command -v curl >/dev/null 2>&1; then
    if command -v apt-get >/dev/null 2>&1; then
        apt-get update -qq && apt-get install -y -qq curl ca-certificates
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y curl ca-certificates
    else
        fail "curl is required but not installed"
    fi
fi
if ! command -v python3 >/dev/null 2>&1; then
    if command -v apt-get >/dev/null 2>&1; then
        apt-get update -qq && apt-get install -y -qq python3
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y python3
    else
        fail "python3 is required but not installed"
    fi
fi

# ── resolve latest release ───────────────────────────────────────────────────
step "resolving latest release"
API="https://api.github.com/repos/$REPO/releases?per_page=1"
RELEASE_JSON=$(curl -fsSL "$API") || fail "could not query GitHub releases"
# API returns an array; unwrap to object
RELEASE_JSON=$(printf '%s' "$RELEASE_JSON" | python3 -c "import sys,json; d=json.load(sys.stdin); print(json.dumps(d[0] if isinstance(d,list) else d))")
ASSET_URL=$(printf '%s' "$RELEASE_JSON" | python3 -c "
import sys, json
data = json.load(sys.stdin)
for a in data.get('assets', []):
    if a['name'] == 'ngray-linux-$ARCH':
        print(a['browser_download_url'])
        break
") || fail "could not parse release JSON"
[[ -n "$ASSET_URL" ]] || fail "no binary asset found for linux/$ARCH"
CAND_ASSET_URL=$(printf '%s' "$RELEASE_JSON" | python3 -c "
import sys, json
data = json.load(sys.stdin)
for a in data.get('assets', []):
    if a['name'] == 'candidates.sni':
        print(a['browser_download_url'])
        break
") || true
ok "release resolved"

# ── download binary + candidates ─────────────────────────────────────────────
step "downloading ngray"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
curl -fsSL "$ASSET_URL" -o "$TMP/ngray" || fail "download failed"
if [[ -n "${CAND_ASSET_URL:-}" ]]; then
    curl -fsSL "$CAND_ASSET_URL" -o "$TMP/candidates.sni" || \
        warn "candidates.sni not downloaded (auto-SNI will use built-in defaults)"
else
    if [[ -n "${CAND_ASSET_URL:-}" ]]; then
        curl -fsSL "$CAND_ASSET_URL" -o "$TMP/candidates.sni" 2>/dev/null || \
            warn "candidates.sni not downloaded (auto-SNI will use built-in defaults)"
    else
        warn "candidates.sni not available in release (auto-SNI will use built-in defaults)"
    fi
fi
chmod +x "$TMP/ngray"
ok "downloaded"

# ── verify checksum (best-effort) ─────────────────────────────────────────────
SUMS_ASSET_URL=$(printf '%s' "$RELEASE_JSON" | python3 -c "
import sys, json
data = json.load(sys.stdin)
for a in data.get('assets', []):
    if a['name'] == 'SHA256SUMS':
        print(a['browser_download_url'])
        break
")
[[ -n "$SUMS_ASSET_URL" ]] || fail "no SHA256SUMS asset in latest release"
curl -fsSL "$SUMS_ASSET_URL" -o "$TMP/SHA256SUMS" || fail "could not download SHA256SUMS (refusing to install unverified binary)"
EXPECTED=$(grep "ngray-linux-$ARCH" "$TMP/SHA256SUMS" | awk '{print $1}' | head -1)
[[ -n "$EXPECTED" ]] || fail "no checksum entry for ngray-linux-$ARCH"
ACTUAL=$(sha256sum "$TMP/ngray" | awk '{print $1}')
[[ "$EXPECTED" == "$ACTUAL" ]] || fail "checksum mismatch for ngray-linux-$ARCH"
if [[ -f "$TMP/candidates.sni" ]]; then
    CEXP=$(grep "candidates.sni" "$TMP/SHA256SUMS" | awk '{print $1}' | head -1)
    if [[ -n "$CEXP" ]]; then
        CACT=$(sha256sum "$TMP/candidates.sni" | awk '{print $1}')
        [[ "$CEXP" == "$CACT" ]] || fail "checksum mismatch for candidates.sni"
    fi
fi
ok "checksum verified"

step "verifying"
if [[ -f "$TMP/candidates.sni" ]]; then
    ok "candidates.sni present ($(wc -l < "$TMP/candidates.sni") lines)"
fi
"$TMP/ngray" version || fail "binary is broken"
ok "$("$TMP/ngray" version)"

# ── install ──────────────────────────────────────────────────────────────────
step "installing"
install -d -m 0750 "$CORE_DIR"
install -d -m 0750 "$CORE_DIR/snis"
install -m 0755 "$TMP/ngray" "$INSTALL_BIN"
if [[ -f "$TMP/candidates.sni" ]]; then
    install -m 0640 "$TMP/candidates.sni" "$CORE_DIR/snis/candidates.sni"
fi
ok "$INSTALL_BIN"

# ── config init ──────────────────────────────────────────────────────────────
mkdir -p "$CORE_DIR"
if [[ ! -f "$CORE_DIR/config.yml" ]]; then
    ( cd "$CORE_DIR" && "$INSTALL_BIN" config init >/dev/null 2>&1 ) || true
    ok "config written to $CORE_DIR/config.yml"
else
    info "config exists — keeping"
fi

# ── cron for SNI refresh ─────────────────────────────────────────────────────
# NOTE: `ngray server regen` is not a valid subcommand; SNI regeneration is
# available via the TUI. Re-enable once a non-destructive CLI regen exists.
:

step "done"
printf '\n  %s✓ ngray is ready.%s\n' "$GREEN" "$RESET"
printf '  %s·%s ngray              — open the interactive TUI\n' "$DIM" "$RESET"
printf '  %s·%s ngray proxy add …  — put a service behind a domain\n' "$DIM" "$RESET"
printf '  %s·%s logs: %s%s\n\n' "$DIM" "$RESET" "$LOG_FILE" "$RESET"

# ── hand off to TUI ──────────────────────────────────────────────────────────
step "launching ngray TUI"
if command -v script >/dev/null 2>&1; then
    script -q -e -c "$INSTALL_BIN" /dev/null || warn "TUI exited — reopen with: sudo ngray"
else
    "$INSTALL_BIN" </dev/tty >/dev/tty 2>&1 || warn "TUI exited — reopen with: sudo ngray"
fi
