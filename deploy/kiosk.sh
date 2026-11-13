#!/bin/sh
# Launches Chromium in kiosk mode inside cage (a single-app Wayland compositor).
# Installed to /opt/pidash/kiosk.sh and started by pidash-kiosk.service.
set -eu

URL="${PIDASH_URL:-http://127.0.0.1:8080/}"
PROFILE="${XDG_CACHE_HOME:-$HOME/.cache}/pidash-chromium"
mkdir -p "$PROFILE"

# Pick whichever Chromium binary this image ships.
for c in chromium chromium-browser; do
  if command -v "$c" >/dev/null 2>&1; then CHROMIUM="$c"; break; fi
done
: "${CHROMIUM:?chromium not found; run deploy/install.sh}"

# Never show the "Chromium didn't shut down correctly" bubble after a power cut.
PREFS="$PROFILE/Default/Preferences"
if [ -f "$PREFS" ]; then
  sed -i 's/"exit_type":"Crashed"/"exit_type":"Normal"/; s/"exited_cleanly":false/"exited_cleanly":true/' "$PREFS" || true
fi

# -d: no client-side decorations. -s: allow VT switching (handy for debugging).
exec cage -d -s -- "$CHROMIUM" \
  --kiosk \
  --ozone-platform=wayland \
  --user-data-dir="$PROFILE" \
  --no-first-run \
  --noerrdialogs \
  --disable-infobars \
  --disable-session-crashed-bubble \
  --hide-crash-restore-bubble \
  --disable-features=TranslateUI,Translate \
  --disable-pinch \
  --overscroll-history-navigation=0 \
  --autoplay-policy=no-user-gesture-required \
  --enable-gpu-rasterization \
  --enable-zero-copy \
  --ignore-gpu-blocklist \
  --disable-background-timer-throttling \
  --disable-renderer-backgrounding \
  --remote-debugging-port=9222 \
  --remote-allow-origins='*' \
  --window-size=1920,1080 \
  --start-fullscreen \
  "$URL"
