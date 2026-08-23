#!/usr/bin/env bash
# Full-screen kiosk for the mining dashboard on the Pi's own display.
#
# The rmm backend runs separately as a systemd --user service and serves the
# dashboard on localhost:8080. This script only opens a full-screen browser
# against it via cage, a single-window Wayland compositor. Run it from tty1.
#
# Chromium flags are tuned for a 1 GB Pi: cache in RAM, one renderer, no
# first-run noise.
set -euo pipefail

URL="${RMM_KIOSK_URL:-http://localhost:8080}"

# Wait until the dashboard answers, so the browser never opens on a blank page
# while the backend is still starting.
for _ in $(seq 1 30); do
  if curl -sf -o /dev/null "$URL"; then break; fi
  sleep 1
done

CHROME_FLAGS=(
  --kiosk
  --app="$URL"
  --noerrdialogs
  --disable-infobars
  --no-first-run
  --disable-session-crashed-bubble
  --disable-features=Translate,MediaRouter,OptimizationHints
  --renderer-process-limit=1
  --disk-cache-dir=/dev/shm/chromium
  --disk-cache-size=33554432
  --overscroll-history-navigation=0
  --check-for-update-interval=31536000
)

exec cage -- chromium "${CHROME_FLAGS[@]}"
