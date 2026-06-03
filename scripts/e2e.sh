#!/usr/bin/env bash
# End-to-end smoke test: build ngray and run it for real inside a throwaway
# Ubuntu container, asserting config generation and nginx validity. The host is
# never touched. Full systemd service assertions need a systemd-enabled image;
# this validates install paths, config writing, nginx -t and link output.
set -euo pipefail

img=ngray-e2e
echo "building $img…"
docker build -t "$img" .

echo "running e2e…"
docker run --rm --cap-add=NET_ADMIN "$img" -c '
  set -e
  ngray version
  # sandbox run first: must touch nothing under /etc
  ngray server setup --sandbox --root /tmp/sbx >/dev/null
  test -f /tmp/sbx/etc/nginx/nginx.conf || { echo "FAIL: sandbox nginx.conf missing"; exit 1; }
  test ! -e /etc/shoes || { echo "FAIL: sandbox leaked to /etc"; exit 1; }
  echo "PASS: sandbox isolated"

  # real run: installs cores, writes configs, validates nginx
  yes | ngray server setup
  nginx -t
  test -f /etc/nginx/nginx.conf
  grep -q "managed by ng-ray" /etc/nginx/nginx.conf || { echo "FAIL: marker missing"; exit 1; }
  ngray client links | grep -q "vless://" || { echo "FAIL: no links"; exit 1; }
  echo "PASS: real provisioning produced valid nginx + links"
'
echo "e2e ok"
