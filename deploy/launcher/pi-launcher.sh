#!/usr/bin/env bash
# Boot-time chooser for the two Pi projects.
#
# The Pi boots to a text console, so this is a plain console menu shown on
# login. Pick a program; when it exits, the menu returns. Nothing here is
# destructive, and Ctrl-C or "s" always drops to a normal shell.
#
# Install: source it from ~/.bash_profile (see install-launcher.sh).

# Only take over an interactive login on the physical console (tty1), never an
# SSH session, so remote administration is unaffected.
case "$(tty)" in
  /dev/tty1) ;;
  *) return 0 2>/dev/null || exit 0 ;;
esac

RMM_DIR="${RMM_DIR:-$HOME/rmm}"
ANTS_DIR="${ANTS_DIR:-$HOME/Pi4-Ants}"
KIOSK="${RMM_KIOSK:-$HOME/rmm/rmm-kiosk.sh}"

# The rmm backend runs as a systemd --user service (rmm-demo) and is always on.
# This opens the dashboard full-screen on the local display via cage+chromium.
run_mining_monitor() {
  clear
  echo ">> Mining Monitor kiosk. Backend: http://$(hostname -I | awk '{print $1}'):8080"
  if [ -x "$KIOSK" ]; then
    "$KIOSK"
  else
    echo "!! kiosk script not found at $KIOSK"
    sleep 3
  fi
}

run_ants() {
  clear
  echo ">> Ants Display"
  if command -v python3 >/dev/null && [ -f "$ANTS_DIR/src/ants_display_demo.py" ]; then
    ( cd "$ANTS_DIR" && python3 src/ants_display_demo.py )
  else
    echo "!! python3 or $ANTS_DIR/src/ants_display_demo.py not found"
    sleep 3
  fi
}

while true; do
  clear
  cat <<'MENU'
  ┌────────────────────────────────────────────┐
  │            PI4-ANTS  ·  LAUNCHER            │
  ├────────────────────────────────────────────┤
  │   1)  Mining Monitor   (dashboard)          │
  │   2)  Ants Display     (art installation)   │
  │   s)  Shell                                 │
  │   r)  Reboot                                │
  │   o)  Shutdown                              │
  └────────────────────────────────────────────┘
MENU
  # A keyboard user can choose. With no keyboard, the touchscreen cannot drive a
  # text menu, so after a countdown it defaults to the Mining Monitor kiosk,
  # which is touch-usable. Press any listed key to override.
  echo "  Auto-starting Mining Monitor in 15s. Press a key to choose."
  printf "  Choice [1/2/s/r/o]: "
  if ! read -t 15 -r choice; then
    choice=1
    echo
  fi
  case "$choice" in
    1) run_mining_monitor ;;
    2) run_ants ;;
    s) echo "Dropping to shell. Type 'exit' to return to the launcher."; bash --login -i ;;
    r) sudo systemctl reboot ;;
    o) sudo systemctl poweroff ;;
    *) ;;
  esac
done
