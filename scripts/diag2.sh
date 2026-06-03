#!/usr/bin/env bash
# Diagnose Hysteria2 (UDP) and the Cloudflare-fronted WS/XHTTP configs.
# Usage: bash scripts/diag2.sh [ssh-host]
set -uo pipefail
HOST="${1:-your-host}"
ssh "$HOST" 'bash -s' <<'REMOTE'
echo "########## HYSTERIA2 ##########"
echo "== service: $(systemctl is-active hysteria-server)"
echo "== config:"; find /etc/hysteria -name '*.yaml' -exec cat {} \; 2>/dev/null | sed 's/^/   /' | head -40
echo "== journal (last 10):"; journalctl -u hysteria-server --no-pager -n 10 2>&1 | tail -10 | sed 's/^/   /'
echo "== UDP 443 listener:"; ss -ulnp 2>/dev/null | grep -E ':443 ' | sed 's/^/   /' || echo "   (nothing on udp/443)"

echo
echo "########## CLOUDFLARE WS/XHTTP ##########"
CFG=/usr/local/etc/xray/config.json
WSPATH=$(jq -r '.inbounds[] | select(.streamSettings.network=="ws") | .streamSettings.wsSettings.path' "$CFG" 2>/dev/null | head -1)
XHPATH=$(jq -r '.inbounds[] | select(.streamSettings.network=="xhttp" and .streamSettings.security=="tls") | .streamSettings.xhttpSettings.path' "$CFG" 2>/dev/null | head -1)
echo "== ws path: ${WSPATH:-NONE}   xhttp-tls path: ${XHPATH:-NONE}"
echo "== WS upgrade via CF (example.com) — expect 101:"
curl -s -o /dev/null -w "   http=%{http_code} time=%{time_total}s\n" --max-time 12 \
  -H "Connection: Upgrade" -H "Upgrade: websocket" -H "Sec-WebSocket-Version: 13" \
  -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" "https://example.com${WSPATH}"
echo "== WS upgrade direct to origin (loopback, bypass CF) — expect 101:"
curl -s -o /dev/null -w "   http=%{http_code}\n" --max-time 10 -k --resolve "example.com:443:127.0.0.1" \
  -H "Connection: Upgrade" -H "Upgrade: websocket" -H "Sec-WebSocket-Version: 13" \
  -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" "https://example.com${WSPATH}"
echo "== XHTTP via CF (www.example.com):"
curl -s -o /dev/null -w "   http=%{http_code} time=%{time_total}s\n" --max-time 12 "https://www.example.com${XHPATH}"
echo
echo "== firewall redirect rule count (dup check): $(iptables -t nat -S PREROUTING 2>/dev/null | grep -c REDIRECT)"
REMOTE
