#!/usr/bin/env bash
# Update a source checkout to the latest main and reinstall the binary.
# Config and state live in /etc/ngray, so resetting the git tree is safe.
#
#   bash scripts/update.sh
set -euo pipefail

cd "$(dirname "$0")/.."

echo "==> fetching origin/main"
git fetch origin main

if ! git merge --ff-only origin/main 2>/dev/null; then
    echo "==> local tree diverged; resetting to origin/main"
    git reset --hard origin/main
fi

REQ=$(awk '$1 == "go" { print $2; exit }' go.mod)
REQ=${REQ:-1.24}

go_version_of() {
    "$1" version 2>/dev/null | awk '{
        for (i = 1; i <= NF; i++)
            if ($i ~ /^go[0-9]/) { sub(/^go/, "", $i); print $i; exit }
    }' || true
}

go_version_ok() {
    [ -n "${1:-}" ] || return 1
    awk -v a="$1" -v b="$REQ" 'BEGIN {
        split(a, A, "."); split(b, B, ".")
        for (i = 1; i <= 3; i++) {
            if ((A[i] + 0) > (B[i] + 0)) exit 0
            if ((A[i] + 0) < (B[i] + 0)) exit 1
        }
        exit 0
    }'
}

GO_CMD=""
for candidate in "$(command -v go 2>/dev/null || true)" \
    /usr/local/go/bin/go /usr/lib/go/bin/go /opt/go/bin/go; do
    [ -n "$candidate" ] && [ -x "$candidate" ] || continue
    if go_version_ok "$(go_version_of "$candidate")"; then
        GO_CMD="$candidate"
        break
    fi
done
if [ -z "$GO_CMD" ]; then
    echo "error: Go ${REQ}+ is not installed (or not on PATH)." >&2
    echo "  Install it from https://go.dev/dl/ or run:" >&2
    echo "  curl -fsSL https://raw.githubusercontent.com/abbasnaqdi/ngray/main/install.sh | sudo bash" >&2
    exit 1
fi
export PATH="$(dirname "$GO_CMD"):$PATH"

echo "==> using Go $(go_version_of "$GO_CMD")"

echo "==> building"
# Keep the build flags in sync with the Makefile's build target.
VERSION=$(sed -n 's/.*Version[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' \
    internal/version/version.go | head -n 1 || true)
VERSION=${VERSION:-dev}
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo none)
DATE=$(date -u +%Y-%m-%d)
PKG="github.com/abbasnaqdi/ng-ray/internal/version"
MODFLAG=""
if [ -f vendor/modules.txt ]; then
    MODFLAG="-mod=vendor"
fi

CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS= go build $MODFLAG -trimpath \
    -ldflags "-s -w -X ${PKG}.Version=${VERSION} -X ${PKG}.Commit=${COMMIT} -X ${PKG}.Date=${DATE}" \
    -o ngray ./cmd/ngray

run_root() {
    if [ "$(id -u)" -eq 0 ]; then
        "$@"
    else
        sudo "$@"
    fi
}

echo "==> installing to /usr/local/bin/ngray"
run_root install -m 0755 ngray /usr/local/bin/ngray
if [ -f snis/candidates.sni ]; then
    run_root install -d -m 0750 /etc/ngray/snis
    run_root install -m 0640 snis/candidates.sni /etc/ngray/snis/candidates.sni
fi

echo "==> done: $(./ngray version)"
