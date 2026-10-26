#!/usr/bin/env bash
# Diagnose Hysteria2 (UDP) and the Cloudflare-fronted WS/XHTTP configs.
# Usage: bash scripts/diag2.sh [ssh-host]
set -uo pipefail
HOST="${1:?usage: $0 <ssh-host>}"
DOMAIN="${DOMAIN:-}"
ssh "$HOST" "DOMAIN='$DOMAIN' bash -s" <<'REMOTE'
echo "########## HYSTERIA2 ##########"
echo "== service: $(systemctl is-active hysteria-server)"
echo "== config:"; find /etc/hysteria -name '*.yaml' -exec cat {} \; 2>/dev/null | sed 's/^/   /' | head -40
echo "== journal (last 10):"; journalctl -u hysteria-server --no-pager -n 10 2>&1 | tail -10 | sed 's/^/   /'
echo "== UDP 443 listener:"; ss -ulnp 2>/dev/null | grep -E ':443 ' | sed 's/^/   /' || echo "   (nothing on udp/443)"

echo
echo "########## CLOUDFLARE WS/XHTTP ##########"
CFG=/usr/local/etc/xray/config.json
# Extract WS / XHTTP-TLS paths with python3 (jq is not installed on the server).
xpath() { python3 - "$CFG" "$1" <<'PY' 2>/dev/null
import json,sys
cfg,net=sys.argv[1],sys.argv[2]
try: inb=json.load(open(cfg)).get("inbounds",[])
except Exception: sys.exit()
for i in inb:
    ss=i.get("streamSettings",{})
    if net=="ws" and ss.get("network")=="ws":
        print(ss.get("wsSettings",{}).get("path","")); break
    if net=="xhttp" and ss.get("network")=="xhttp" and ss.get("security")=="tls":
        print(ss.get("xhttpSettings",{}).get("path","")); break
PY
}
WSPATH=$(xpath ws)
XHPATH=$(xpath xhttp)
echo "== ws path: ${WSPATH:-NONE}   xhttp-tls path: ${XHPATH:-NONE}"
if [ -n "$DOMAIN" ]; then
  echo "== WS upgrade via CF ($DOMAIN) — expect 101:"
  curl -s -o /dev/null -w "   http=%{http_code} time=%{time_total}s\n" --max-time 12 \
    -H "Connection: Upgrade" -H "Upgrade: websocket" -H "Sec-WebSocket-Version: 13" \
    -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" "https://${DOMAIN}${WSPATH}"
  echo "== WS upgrade direct to origin (loopback, bypass CF) — expect 101:"
  curl -s -o /dev/null -w "   http=%{http_code}\n" --max-time 10 -k --resolve "${DOMAIN}:443:127.0.0.1" \
    -H "Connection: Upgrade" -H "Upgrade: websocket" -H "Sec-WebSocket-Version: 13" \
    -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" "https://${DOMAIN}${WSPATH}"
  echo "== XHTTP via CF (www.${DOMAIN}):"
  curl -s -o /dev/null -w "   http=%{http_code} time=%{time_total}s\n" --max-time 12 "https://www.${DOMAIN}${XHPATH}"
else
  echo "== (set DOMAIN=… to test the CF WS/XHTTP paths)"
fi
echo
echo "== firewall redirect rule count (dup check): $(iptables -t nat -S PREROUTING 2>/dev/null | grep -c REDIRECT)"
REMOTE
