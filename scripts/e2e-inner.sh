#!/usr/bin/env bash
# Runs inside the container (see compose.yaml / Dockerfile). Asserts sandbox
# isolation, then real provisioning produces a valid nginx + client links.
set -euo pipefail

ngray version

# 1) sandbox must touch nothing under /etc
ngray server setup --sandbox --root /tmp/sbx >/dev/null
test -f /tmp/sbx/etc/nginx/nginx.conf || { echo "FAIL: sandbox nginx.conf missing"; exit 1; }
test ! -e /etc/shoes || { echo "FAIL: sandbox leaked to /etc"; exit 1; }
echo "PASS: sandbox isolated"

# 2) real provisioning — yes(1) exits on SIGPIPE once setup stops reading, so
# disable pipefail for this one pipeline instead of treating 141 as failure.
( set +o pipefail; yes | ngray server setup )
nginx -t
grep -q "managed by ng-ray" /etc/nginx/nginx.conf || { echo "FAIL: marker missing"; exit 1; }
links=$(ngray client links)
grep -q "vless://" <<<"$links" || { echo "FAIL: no links"; exit 1; }
echo "PASS: real provisioning produced valid nginx + links"
