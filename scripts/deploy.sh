#!/usr/bin/env bash
# Build the release locally, ship it to the ng-ray server, and (re)provision.
#
# Ships the release WITHOUT config.yml so `ngray server setup` auto-populates the
# full SNI set from the SERVER's own vantage (the only correct vantage), then
# deploys every core, the nginx SNI mux, the firewall and the subscription
# bundle. proxy-state.json is preserved, so REALITY keys / UUIDs / the
# subscription token stay stable and existing subscription clients keep working.
#
# Usage: bash scripts/deploy.sh [ssh-host]      (pass <ssh-host> as the first argument)
set -euo pipefail

HOST="${1:?usage: $0 <ssh-host>}"
cd "$(dirname "$0")/.."

echo "==> building package"
make package >/dev/null
tar -C pkg/release -czf /tmp/ngray-release.tgz ngray config.example.yml snis/candidates.sni

echo "==> shipping to $HOST"
scp -q /tmp/ngray-release.tgz "$HOST:/tmp/ngray-release.tgz"

echo "==> provisioning on $HOST"
ssh "$HOST" 'bash -s' <<'REMOTE'
set -e
echo "-- host: $(hostname)"
printf -- "-- pubip: "; curl -s --max-time 6 https://api.ipify.org || true; echo
mkdir -p /root/ngray/snis; cd /root/ngray
ts=$(date +%Y%m%d-%H%M%S)
[ -f proxy-state.json ] && cp proxy-state.json "proxy-state.json.bak-$ts" && echo "-- backed up proxy-state.json -> proxy-state.json.bak-$ts"
[ -f config.yml ]       && cp config.yml       "config.yml.bak-$ts"       && echo "-- backed up config.yml -> config.yml.bak-$ts"
tar -xzf /tmp/ngray-release.tgz -C /root/ngray
chmod +x ngray
echo "-- version: $(./ngray version)"
rm -f config.yml   # force the fresh-install path so setup auto-populates the SNI pool
LOG="setup-$(date +%H%M%S).log"
echo "==> running: ngray server setup  (detached on server, streaming $LOG)"
# setsid so the run survives a dropped ssh or a long SNI sweep — otherwise an
# interrupted setup leaves config.yml removed and never regenerated. We stream
# the log, but the setup process owns its own lifetime.
setsid bash -c "./ngray server setup < /dev/null > $LOG 2>&1" &
sleep 1
tail -f "$LOG" & TAILPID=$!
while pgrep -f 'ngray server setup' >/dev/null; do sleep 3; done
sleep 1; kill "$TAILPID" 2>/dev/null || true
echo
echo "==> SERVER STATUS"
./ngray server status || true
echo "==> generated SNIs in config.yml: $(grep -c 'sni:' config.yml 2>/dev/null || echo '?')"
echo "==> client config links: $(./ngray client links 2>/dev/null | grep -cE '://' || echo '?')"
REMOTE
echo "==> done"
