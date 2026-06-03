#!/usr/bin/env bash
# Diagnose the Cloudflare-fronted path: xray CF inbound, nginx route, local TLS.
# Usage: bash scripts/cfdiag.sh [ssh-host]
set -uo pipefail
HOST="${1:-your-host}"
ssh "$HOST" 'bash -s' <<'REMOTE'
echo "== xray active: $(systemctl is-active xray)"
echo "== cert present:"; ls -l /etc/ngray/cf-origin.crt /etc/ngray/cf-origin.key 2>&1 | sed 's/^/   /'
echo "== example routes in nginx.conf:"; grep -nE 'example' /etc/nginx/nginx.conf | sed 's/^/   /' || echo "   (none)"
echo "== example / ws / xhttp / cert in xray config.json:"
grep -nE 'example|"ws"|"xhttp"|cf-origin|certificateFile' /usr/local/etc/xray/config.json | sed 's/^/   /' || echo "   (none)"
echo "== xray journal (last 15):"; journalctl -u xray --no-pager -n 15 2>&1 | tail -15 | sed 's/^/   /'
echo "== local TLS to example.com via nginx (127.0.0.1:443):"
echo | timeout 8 openssl s_client -connect 127.0.0.1:443 -servername example.com 2>/dev/null | openssl x509 -noout -issuer -subject 2>/dev/null | sed 's/^/   /' || echo "   (no TLS — handshake failed)"
REMOTE
