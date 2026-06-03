#!/usr/bin/env bash
# Remote diagnostics for the ng-ray server: service states, journals for any
# inactive core, and a domain health check from the server's own vantage.
# Usage: bash scripts/diag.sh [ssh-host]   (default: your-host)
set -uo pipefail
HOST="${1:-your-host}"
ssh "$HOST" 'bash -s' <<'REMOTE'
echo "===== service states ====="
for s in xray shoes sing-box hysteria-server fptn-server mieru-server awg-quick@wg0; do
  printf "%-18s %s\n" "$s" "$(systemctl is-active "$s" 2>/dev/null)"
done
echo
for s in shoes mieru-server awg-quick@wg0; do
  echo "===== $s (status + last 18 journal lines) ====="
  systemctl status "$s" --no-pager -l 2>&1 | sed -n '1,4p'
  journalctl -u "$s" --no-pager -n 18 2>&1 | tail -18
  echo
done
echo "===== amneziawg kernel module / interface ====="
echo "-- uname: $(uname -r)"
echo "-- dkms status:"; dkms status 2>&1 | sed 's/^/   /'
echo "-- lsmod amnezia:"; lsmod 2>/dev/null | grep -i amnezia | sed 's/^/   /' || echo "   (not loaded)"
echo "-- modprobe amneziawg:"; modprobe amneziawg 2>&1 | sed 's/^/   /' && echo "   (modprobe ok)"
echo "-- manual awg-quick up wg0:"; awg-quick up wg0 2>&1 | sed 's/^/   /'; awg-quick down wg0 >/dev/null 2>&1
echo

echo "===== cloudflare cdn note (cert installed?) ====="
ls -l /etc/ngray/cf-origin.crt /etc/ngray/cf-origin.key 2>&1 | sed 's/^/  /'
echo
echo "===== ngray domain check (server vantage) ====="
cd /root/ngray && ./ngray domain check example.com --ip YOUR.SERVER.IP
REMOTE
