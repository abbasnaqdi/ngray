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
GO_HOME="${NGRAY_GO_HOME:-/usr/local/lib/ngray-go}"

say()  { printf '  %s\n' "$1"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$1"; }
warn() { printf '  \033[33m!\033[0m %s\n' "$1"; }
die()  { printf '  \033[31m✗\033[0m %s\n' "$1" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "run as root (use sudo)"

TMP=$(mktemp -d "${TMPDIR:-/tmp}/ngray-install.XXXXXX") || die "could not create a temp dir"
BIN_TMP=""
GO_ARCHIVE=""
GO_STAGE=""
cleanup() {
    if [ -n "$BIN_TMP" ]; then
        rm -f -- "$BIN_TMP" 2>/dev/null || true
    fi
    if [ -n "$GO_ARCHIVE" ]; then
        rm -f -- "$GO_ARCHIVE" 2>/dev/null || true
    fi
    if [ -n "$GO_STAGE" ]; then
        drop_stage "$GO_STAGE" || true
    fi
    rm -rf -- "${TMP:?}" 2>/dev/null || true
}
trap cleanup EXIT

# ── platform ─────────────────────────────────────────────────────────────────
case "$(uname -s)" in
    Linux)  OS="linux" ;;
    Darwin) OS="darwin" ;;
    *) die "unsupported OS: $(uname -s)" ;;
esac
case "$(uname -m)" in
    x86_64|amd64)  ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *) die "unsupported arch: $(uname -m) (supported: x86_64/amd64, arm64/aarch64)" ;;
esac
ASSET="ngray-${OS}-${ARCH}"

command -v curl >/dev/null 2>&1 || die "curl is required"
ok "${OS}/${ARCH}"

# ── helpers ──────────────────────────────────────────────────────────────────
# version_ge A B → true when dotted version A >= B.
version_ge() {
    awk -v a="$1" -v b="$2" 'BEGIN {
        sub(/^go/, "", a); sub(/^go/, "", b)
        split(a, A, "."); split(b, B, ".")
        for (i = 1; i <= 3; i++) {
            if ((A[i] + 0) > (B[i] + 0)) exit 0
            if ((A[i] + 0) < (B[i] + 0)) exit 1
        }
        exit 0
    }'
}

go_version() {
    "$1" version 2>/dev/null | awk '{
        for (i = 1; i <= NF; i++)
            if ($i ~ /^go[0-9]/) { sub(/^go/, "", $i); print $i; exit }
    }' || true
}

go_is_usable() {
    [ -n "${1:-}" ] && [ -x "$1" ] || return 1
    local v
    v=$(go_version "$1")
    [ -n "$v" ] && version_ge "$v" "$REQ"
}

# Remove an extraction staging directory, but only one we created.
drop_stage() {
    case "${1:-}" in
        "$tool_root"/.go-stage.*) rm -rf -- "$1" ;;
    esac
    return 0
}

# Install a Go toolchain without touching an existing /usr/local/go.
install_go() {
    want="${GO_VERSION:-}"
    want="${want#go}"
    want="${want#v}"
    if [ -n "$want" ]; then
        version_ge "$want" "$REQ" || die "GO_VERSION=$want is older than go.mod requires ($REQ)"
    else
        latest=$(curl -fsSL --retry 2 --connect-timeout 10 \
            "https://go.dev/VERSION?m=text" 2>/dev/null | head -n 1 || true)
        latest="${latest#go}"
        latest="${latest#v}"
        if [ -n "$latest" ] && version_ge "$latest" "$REQ"; then
            want="$latest"
        else
            want="$REQ"
        fi
    fi
    # Go downloads are named with full x.y.z releases.
    case "$want" in
        *.*.*) ;;
        *.*)   want="$want.0" ;;
        *)     want="$want.0.0" ;;
    esac
    case "$want" in
        ""|*[!0-9.]*) die "invalid Go version: $want" ;;
    esac

    tool_root="$GO_HOME"
    persistent=0
    if mkdir -p "$tool_root" 2>/dev/null && [ -w "$tool_root" ]; then
        persistent=1
    else
        tool_root="$TMP/toolchain"
        mkdir -p "$tool_root" || die "could not create a toolchain directory"
        warn "cannot write $GO_HOME — using a temporary Go toolchain"
    fi
    tool_root=$(cd "$tool_root" && pwd -P) || die "could not resolve $tool_root"

    dest="$tool_root/$want"
    if [ ! -x "$dest/bin/go" ]; then
        command -v tar >/dev/null 2>&1 || die "tar is required to install Go"
        say "installing Go ${want} (one-time)…"
        archive="$tool_root/.go${want}.$$.tgz"
        GO_ARCHIVE="$archive"
        curl -fsSL --retry 2 --connect-timeout 15 \
            "https://go.dev/dl/go${want}.${OS}-${ARCH}.tar.gz" -o "$archive" \
            || { rm -f -- "$archive"; die "could not download Go ${want} for ${OS}/${ARCH}"; }
        stage=$(mktemp -d "$tool_root/.go-stage.XXXXXX") \
            || { rm -f -- "$archive"; die "could not create a staging directory in $tool_root"; }
        GO_STAGE="$stage"
        if ! tar -xzf "$archive" -C "$stage"; then
            rm -f -- "$archive"
            drop_stage "$stage"
            die "could not extract Go"
        fi
        rm -f -- "$archive"
        GO_ARCHIVE=""
        [ -x "$stage/go/bin/go" ] \
            || { drop_stage "$stage"; die "downloaded Go archive is incomplete"; }
        if [ -x "$dest/bin/go" ]; then
            drop_stage "$stage"
        elif [ -e "$dest" ]; then
            drop_stage "$stage"
            die "incomplete Go toolchain at $dest — remove it and rerun"
        else
            mv "$stage/go" "$dest" \
                || { drop_stage "$stage"; die "could not install Go to $dest"; }
            drop_stage "$stage"
        fi
        GO_STAGE=""
    fi
    "$dest/bin/go" version >/dev/null 2>&1 \
        || die "Go at $dest does not run (is $tool_root mounted noexec?)"
    ok "Go $(go_version "$dest/bin/go")"

    if [ "$persistent" -eq 1 ]; then
        if [ -e "$GO_HOME/current" ] && [ ! -L "$GO_HOME/current" ]; then
            warn "ignoring $GO_HOME/current (not a symlink)"
        else
            ln -sfn "$want" "$GO_HOME/current" || die "could not update $GO_HOME/current"
        fi
    fi

    GO_BIN="$dest/bin/go"
    GOROOT="$dest"
    export GOROOT
    export PATH="$dest/bin:$PATH"
}

# ── source build (fallback) ──────────────────────────────────────────────────
build_from_source() {
    src=""
    if command -v git >/dev/null 2>&1; then
        say "fetching source…"
        if GIT_TERMINAL_PROMPT=0 git clone --depth 1 --branch main \
            "https://github.com/${REPO}.git" "$TMP/src" >/dev/null 2>&1; then
            src="$TMP/src"
        else
            warn "git clone failed — trying the source archive"
        fi
    else
        say "git not found — using the source archive"
    fi

    if [ -z "$src" ]; then
        command -v tar >/dev/null 2>&1 || die "tar is required to extract the source archive"
        src="$TMP/src-archive"
        mkdir -p "$src"
        curl -fsSL --retry 2 --connect-timeout 15 \
            "https://codeload.github.com/${REPO}/tar.gz/refs/heads/main" -o "$TMP/src.tgz" \
            || die "could not download the source archive"
        tar -xzf "$TMP/src.tgz" -C "$src" --strip-components=1 \
            || die "could not extract the source archive"
    fi

    [ -f "$src/go.mod" ] || die "source tree is incomplete (go.mod missing)"
    REQ=$(awk '$1 == "go" { print $2; exit }' "$src/go.mod")
    REQ="${REQ:-1.24}"

    GO_BIN=""
    if command -v go >/dev/null 2>&1; then
        sys_go=$(command -v go)
        if go_is_usable "$sys_go"; then
            GO_BIN="$sys_go"
            ok "Go $(go_version "$GO_BIN")"
        else
            warn "Go $(go_version "$sys_go") is older than required $REQ"
        fi
    fi

    if [ -z "$GO_BIN" ]; then
        for candidate in /usr/local/go/bin/go /usr/lib/go/bin/go /opt/go/bin/go; do
            if go_is_usable "$candidate"; then
                GO_BIN="$candidate"
                GOROOT=$(cd "$(dirname "$candidate")/.." && pwd -P) \
                    || die "could not resolve $candidate"
                export GOROOT
                export PATH="$GOROOT/bin:$PATH"
                ok "Go $(go_version "$GO_BIN")"
                break
            fi
        done
    fi

    if [ -z "$GO_BIN" ] && go_is_usable "$GO_HOME/current/bin/go"; then
        GO_BIN="$GO_HOME/current/bin/go"
        GOROOT=$(cd "$GO_HOME/current" && pwd -P) \
            || die "could not resolve $GO_HOME/current"
        export GOROOT
        export PATH="$GOROOT/bin:$PATH"
        ok "Go $(go_version "$GO_BIN")"
    fi

    if [ -z "$GO_BIN" ]; then
        install_go
    fi

    say "building from source…"
    # Build directly (no make): minimal servers often ship neither make nor git.
    # Keep the build flags in sync with the Makefile's build target.
    version=$(sed -n 's/.*Version[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' \
        "$src/internal/version/version.go" 2>/dev/null | head -n 1 || true)
    version="${version:-dev}"
    commit=$(git -C "$src" rev-parse --short HEAD 2>/dev/null || true)
    commit="${commit:-none}"
    built=$(date -u +%Y-%m-%d)
    pkg="github.com/abbasnaqdi/ng-ray/internal/version"
    modflag=""
    if [ -f "$src/vendor/modules.txt" ]; then
        modflag="-mod=vendor"
    fi

    ( cd "$src" && CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS= \
        "$GO_BIN" build $modflag -trimpath \
        -ldflags "-s -w -X ${pkg}.Version=${version} -X ${pkg}.Commit=${commit} -X ${pkg}.Date=${built}" \
        -o "$src/ngray" ./cmd/ngray ) || die "build failed — see the Go error above"

    install -m 0755 "$src/ngray" "$TMP/ngray"
    if [ -f "$src/snis/candidates.sni" ]; then
        cp "$src/snis/candidates.sni" "$TMP/candidates.sni"
    fi
    ok "built from source (ngray $version)"
}

# ── download prebuilt binary, or build ───────────────────────────────────────
download_release() {
    curl -fsSL --retry 1 --connect-timeout 10 \
        "${BASE}/SHA256SUMS" -o "$TMP/SHA256SUMS" 2>/dev/null || return 1
    expected=$(awk -v a="$ASSET" '{
        n = $NF; sub(/\r$/, "", n); sub(/^\*/, "", n); sub(/^.*\//, "", n)
        if (n == a) { print $1; exit }
    }' "$TMP/SHA256SUMS")
    [ -n "$expected" ] || return 1

    curl -fsSL --retry 2 --connect-timeout 10 \
        "${BASE}/${ASSET}" -o "$TMP/ngray" 2>/dev/null || return 1

    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(sha256sum "$TMP/ngray" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        actual=$(shasum -a 256 "$TMP/ngray" | awk '{print $1}')
    elif command -v openssl >/dev/null 2>&1; then
        actual=$(openssl dgst -sha256 "$TMP/ngray" | awk '{print $NF}')
    else
        warn "no SHA-256 tool found — building from source instead"
        return 1
    fi
    expected=$(printf '%s' "$expected" | tr 'A-F' 'a-f')
    actual=$(printf '%s' "$actual" | tr 'A-F' 'a-f')
    [ "$expected" = "$actual" ] || die "checksum mismatch for ${ASSET}"
    chmod 0755 "$TMP/ngray"

    if curl -fsSL --retry 1 --connect-timeout 10 \
        "${BASE}/candidates.sni" -o "$TMP/candidates.sni.part" 2>/dev/null; then
        mv "$TMP/candidates.sni.part" "$TMP/candidates.sni"
    fi
    ok "downloaded + verified"
    return 0
}

if ! download_release; then
    warn "no prebuilt release available — building from source"
    build_from_source
fi

# ── install ──────────────────────────────────────────────────────────────────
install -d -m 0755 "$(dirname "$BIN")"
BIN_TMP="${BIN}.new.$$"
install -m 0755 "$TMP/ngray" "$BIN_TMP" || die "could not write $BIN_TMP"
"$BIN_TMP" version >/dev/null 2>&1 || die "binary does not run on this system"
mv -f "$BIN_TMP" "$BIN" || die "could not replace $BIN"
BIN_TMP=""
install -d -m 0750 "$DIR" "$DIR/snis" || die "could not create $DIR"
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

ver=$("$BIN" version 2>/dev/null | awk '{print $2; exit}' || true)
say ""
if [ -n "$ver" ]; then
    say "ngray $ver is ready."
else
    say "ngray is ready."
fi
say "  ngray              open the TUI"
say "  ngray proxy add …  put a service behind a domain"
say ""

# ── launch TUI when attached to a terminal ───────────────────────────────────
if [ -t 0 ] && [ -t 1 ] && [ "${NGRAY_NO_TUI:-}" != "1" ]; then
    "$BIN" || true
fi
