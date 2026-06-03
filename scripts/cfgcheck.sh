#!/usr/bin/env bash
# Show the server's config.yml validation result and key sections.
# Usage: bash scripts/cfgcheck.sh [ssh-host]
set -uo pipefail
HOST="${1:-your-host}"
ssh "$HOST" 'bash -s' <<'REMOTE'
cd /root/ngray
echo "== ngray config (validation) =="
./ngray config 2>&1 || true
echo
echo "== cdn block =="
grep -n -A8 'cdn:' config.yml 2>/dev/null || echo "(no cdn block)"
echo "== subscription.front_sni =="
grep -n 'front_sni' config.yml 2>/dev/null
echo "== duplicate SNIs across config (should be none) =="
grep -oE 'sni: .*' config.yml | sort | uniq -d | head
echo "== total sni lines =="
grep -c 'sni:' config.yml
REMOTE
