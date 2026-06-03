#!/usr/bin/env bash
# Build + install the AmneziaWG kernel module via DKMS for the server's running
# kernel, then start awg-quick@wg0. Use this to repair a server where the awg
# binary pre-existed (so ngray's Install — which builds the module — was skipped)
# or after a kernel upgrade. Idempotent.
# Usage: bash scripts/awg-build.sh [ssh-host]   (default: your-host)
set -uo pipefail
HOST="${1:-your-host}"
ssh "$HOST" 'bash -s' <<'REMOTE'
set -e
KVER=$(uname -r)
echo "== kernel: $KVER"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq || true
apt-get install -y dkms build-essential "linux-headers-$KVER" >/dev/null 2>&1 || true
if [ ! -e "/lib/modules/$KVER/build" ]; then
  echo "!! no kernel headers for $KVER — install linux-headers-$KVER and retry"; exit 1
fi
echo "== headers OK"

cd /tmp
curl -fsSL https://github.com/amnezia-vpn/amneziawg/archive/refs/heads/master.tar.gz -o awg-src.tgz
SRC=/usr/src/amneziawg-ngray1
rm -rf "$SRC"; mkdir -p "$SRC"
tar -xzf awg-src.tgz --strip-components=1 -C "$SRC"
cat > "$SRC/dkms.conf" <<'EOF'
PACKAGE_NAME="amneziawg"
PACKAGE_VERSION="ngray1"
BUILT_MODULE_NAME[0]="amneziawg"
DEST_MODULE_LOCATION[0]="/kernel/net/wireguard/"
AUTOINSTALL="yes"
MAKE[0]="make -C src KERNELDIR=/lib/modules/${kernelver}/build"
CLEAN="make -C src KERNELDIR=/lib/modules/${kernelver}/build clean"
EOF

dkms remove amneziawg/ngray1 --all >/dev/null 2>&1 || true
dkms add amneziawg/ngray1 >/dev/null 2>&1 || true
echo "== dkms build"
dkms build amneziawg/ngray1 -k "$KVER" 2>&1 | tail -8
echo "== dkms install"
dkms install amneziawg/ngray1 -k "$KVER" 2>&1 | tail -6

if modprobe amneziawg 2>/dev/null; then
  echo "== module loads OK"
  systemctl enable --now awg-quick@wg0 2>&1 | tail -3 || systemctl restart awg-quick@wg0
  sleep 1
  echo "== awg-quick@wg0: $(systemctl is-active awg-quick@wg0)"
  awg show 2>/dev/null | head -6 || true
else
  echo "!! module still not loadable after build"; exit 1
fi
REMOTE
