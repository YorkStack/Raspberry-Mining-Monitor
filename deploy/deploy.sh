#!/usr/bin/env bash
# Cross-compile the ARM64 binary and deploy it to a Raspberry Pi over SSH.
#
# The Pi has no Go toolchain, so the binary is always built on the dev machine
# and copied over. Nothing here is destructive: it uploads to a temp path,
# verifies the checksum, then moves the binary into place.
#
# Host details are not baked in. Set them via environment or deploy/deploy.env
# (which is gitignored), for example:
#
#   RMM_SSH=my-pi               # ssh alias or user@host
#   RMM_TARGET=~/rmm            # directory on the Pi
#   RMM_SERVICE=rmm             # optional systemd unit to restart
#
#   ./deploy/deploy.sh              # build + upload + place
#   ./deploy/deploy.sh --restart    # also restart RMM_SERVICE
set -euo pipefail

cd "$(dirname "$0")/.."
[ -f deploy/deploy.env ] && . deploy/deploy.env

SSH="${RMM_SSH:?set RMM_SSH to an ssh alias or user@host}"
TARGET="${RMM_TARGET:-rmm}"   # relative path resolves under the Pi user home
SERVICE="${RMM_SERVICE:-}"
BINARY="rmm-linux-arm64"

echo ">> building $BINARY"
make pi

sum_local=$(shasum -a 256 "$BINARY" | awk '{print $1}')
echo ">> local  sha256 $sum_local"

echo ">> uploading to $SSH:/tmp/rmm.new"
scp -q "$BINARY" "$SSH:/tmp/rmm.new"

sum_remote=$(ssh "$SSH" 'sha256sum /tmp/rmm.new | cut -d" " -f1')
echo ">> remote sha256 $sum_remote"
[ "$sum_local" = "$sum_remote" ] || { echo "!! checksum mismatch, aborting"; exit 1; }

echo ">> placing binary in $TARGET"
ssh "$SSH" "mkdir -p $TARGET && mv /tmp/rmm.new $TARGET/rmm && chmod 750 $TARGET/rmm && $TARGET/rmm --version"

if [ "${1:-}" = "--restart" ] && [ -n "$SERVICE" ]; then
  echo ">> restarting $SERVICE"
  ssh "$SSH" "sudo systemctl restart $SERVICE && systemctl status $SERVICE --no-pager -n 5"
fi

echo ">> done"
