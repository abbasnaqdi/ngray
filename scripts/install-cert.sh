#!/usr/bin/env bash
# Install a Cloudflare Origin Certificate on the server, then reprovision so the
# CF-fronted WS/XHTTP inbounds come up. Takes cert/key as file paths — no secret
# is stored in the repo. Regenerates config.yml (so CF inbounds build with the
# cert present) while preserving proxy-state.json (stable keys/token).
# Usage: bash scripts/install-cert.sh <cert.pem> <key.pem> [ssh-host]
set -euo pipefail
CRT="${1:?usage: install-cert.sh <cert.pem> <key.pem> [host]}"
KEY="${2:?usage: install-cert.sh <cert.pem> <key.pem> [host]}"
HOST="${3:?usage: install-cert.sh <cert.pem> <key.pem> <ssh-host>}"
cd "$(dirname "$0")/.."
[ -f "$CRT" ] && [ -f "$KEY" ] || { echo "cert/key file not found"; exit 1; }

echo "==> building package"
make package >/dev/null
tar -C pkg/release -czf /tmp/ngray-release.tgz ngray config.example.yml snis/candidates.sni

echo "==> shipping to $HOST"
scp -q /tmp/ngray-release.tgz "$HOST:/tmp/ngray-release.tgz"
scp -q "$CRT" "$HOST:/tmp/cf-origin.crt"
scp -q "$KEY" "$HOST:/tmp/cf-origin.key"

echo "==> installing cert + reprovisioning on $HOST"
ssh "$HOST" 'bash -s' <<'REMOTE'
set -e
mkdir -p /root/ngray/snis; cd /root/ngray
ts=$(date +%Y%m%d-%H%M%S)
[ -f proxy-state.json ] && cp proxy-state.json "proxy-state.json.bak-$ts"
[ -f config.yml ]       && cp config.yml       "config.yml.bak-$ts"
tar -xzf /tmp/ngray-release.tgz -C /root/ngray
chmod +x ngray
./ngray domain cert /tmp/cf-origin.crt /tmp/cf-origin.key
rm -f /tmp/cf-origin.crt /tmp/cf-origin.key
rm -f config.yml   # regenerate so the CF inbounds build with the cert present
./ngray server setup < /dev/null | tail -6
echo
echo "==> done"
REMOTE
echo "==> done"
