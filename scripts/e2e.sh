#!/usr/bin/env bash
# End-to-end smoke test: build ngray and run it for real inside a throwaway
# container (see scripts/e2e-inner.sh for the assertions). The host is never
# touched. Works with docker or podman.
set -euo pipefail

if command -v docker >/dev/null 2>&1; then CT=docker
elif command -v podman >/dev/null 2>&1; then CT=podman
else echo "need docker or podman"; exit 127; fi

img=ngray-e2e
root=$(cd "$(dirname "$0")/.." && pwd)
echo "building $img with $CT…"
"$CT" build -t "$img" "$root"

echo "running e2e…"
"$CT" run --rm --cap-add=NET_ADMIN \
    -v "$root/scripts:/work/scripts:ro" \
    "$img" -c "bash /work/scripts/e2e-inner.sh"
echo "e2e ok"
