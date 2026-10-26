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

if ! command -v go >/dev/null 2>&1; then
    echo "error: Go is not installed. Install it with:" >&2
    echo "  curl -fsSL https://go.dev/dl/go1.26.0.$(uname -s | tr A-Z a-z)-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz \\" >&2
    echo "    | sudo tar -C /usr/local -xz" >&2
    echo "  export PATH=\$PATH:/usr/local/go/bin" >&2
    exit 1
fi

echo "==> building"
make build

echo "==> installing to /usr/local/bin/ngray"
SUDO=""
[ "$(id -u)" -eq 0 ] || SUDO="sudo"
$SUDO make install

echo "==> done: $("$(pwd)/ngray" version)"
