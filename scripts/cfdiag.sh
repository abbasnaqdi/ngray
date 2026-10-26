#!/usr/bin/env bash
# Diagnose the Cloudflare-fronted path: xray CF inbound, nginx route, local TLS.
# Usage: [DOMAIN=your.example.com] bash scripts/cfdiag.sh <ssh-host>
set -uo pipefail
HOST="${1:?usage: $0 <ssh-host>}"
DOMAIN="${DOMAIN:-}"
ssh "$HOST" "DOMAIN='$DOMAIN' bash -s" <<'REMOTE'
echo "== xray active: $(systemctl is-active xray)"
echo "== cert present:"; ls -l /etc/ngray/cf-origin.crt /etc/ngray/cf-origin.key 2>&1 | sed 's/^/   /'
echo "== CF routes in nginx.conf:"; grep -nE 'cf-origin|ws|xhttp' /etc/nginx/nginx.conf | sed 's/^/   /' || echo "   (none)"
echo "== ws / xhttp / cert in xray config.json:"
grep -nE '"ws"|"xhttp"|cf-origin|certificateFile' /usr/local/etc/xray/config.json | sed 's/^/   /' || echo "   (none)"
echo "== xray journal (last 15):"; journalctl -u xray --no-pager -n 15 2>&1 | tail -15 | sed 's/^/   /'
if [ -n "$DOMAIN" ]; then
    echo "== local TLS to $DOMAIN via nginx (127.0.0.1:443):"
    echo | timeout 8 openssl s_client -connect 127.0.0.1:443 -servername "$DOMAIN" 2>/dev/null | openssl x509 -noout -issuer -subject 2>/dev/null | sed 's/^/   /' || echo "   (no TLS — handshake failed)"
else
    echo "== (set DOMAIN=… to test the local TLS handshake)"
fi
REMOTE
