#!/usr/bin/env bash
# Install the console launcher for the logged-in user on the Pi.
#
# Copies pi-launcher.sh to ~/.local/bin and sources it from ~/.bash_profile so
# it runs on console login (tty1 only). Idempotent and reversible: remove the
# marked block from ~/.bash_profile to undo.
set -euo pipefail

SRC="$(dirname "$0")/pi-launcher.sh"
DEST="$HOME/.local/bin/pi-launcher.sh"
PROFILE="$HOME/.bash_profile"
MARK="# >>> pi4-ants launcher >>>"

mkdir -p "$HOME/.local/bin"
cp "$SRC" "$DEST"
chmod +x "$DEST"

touch "$PROFILE"
if ! grep -qF "$MARK" "$PROFILE"; then
  {
    echo ""
    echo "$MARK"
    echo "[ -f \"$DEST\" ] && . \"$DEST\""
    echo "# <<< pi4-ants launcher <<<"
  } >> "$PROFILE"
  echo "launcher wired into $PROFILE"
else
  echo "launcher already wired into $PROFILE"
fi
echo "installed to $DEST"
