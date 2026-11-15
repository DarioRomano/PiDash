#!/bin/sh
# Opens the dashboard in fullscreen Chromium inside the CURRENT desktop session
# (Wayland/labwc, Wayfire or X11). It uses its own browser profile, so your
# normal Chromium windows, tabs and logins are untouched.
# Installed to ~/.local/share/pidash/kiosk.sh and run by pidash-kiosk.service.
set -eu

PROFILE="${XDG_CACHE_HOME:-$HOME/.cache}/pidash-chromium"
mkdir -p "$PROFILE"

# systemd user services do not always inherit the desktop's display variables,
# so find the running session ourselves.
: "${XDG_RUNTIME_DIR:=/run/user/$(id -u)}"
export XDG_RUNTIME_DIR
if [ -z "${WAYLAND_DISPLAY:-}" ]; then
  for s in "$XDG_RUNTIME_DIR"/wayland-[0-9]*; do
    if [ -S "$s" ]; then WAYLAND_DISPLAY="$(basename "$s")"; break; fi
  done
fi
if [ -n "${WAYLAND_DISPLAY:-}" ]; then
  export WAYLAND_DISPLAY
  PLATFORM=wayland
elif [ -n "${DISPLAY:-}" ] || [ -S /tmp/.X11-unix/X0 ]; then
  export DISPLAY="${DISPLAY:-:0}"
  [ -n "${XAUTHORITY:-}" ] || { [ -f "$HOME/.Xauthority" ] && export XAUTHORITY="$HOME/.Xauthority"; }
  PLATFORM=x11
else
  echo "no desktop session found (no Wayland socket in $XDG_RUNTIME_DIR, no X display)." >&2
  echo "PiDash runs on top of the Raspberry Pi desktop: log in to the desktop first." >&2
  exit 1
fi

# Pick whichever Chromium binary this image ships.
CHROMIUM=""
for c in chromium chromium-browser; do
  if command -v "$c" >/dev/null 2>&1; then CHROMIUM="$c"; break; fi
done
[ -n "$CHROMIUM" ] || { echo "chromium not found: sudo apt install chromium" >&2; exit 1; }

# Never show the "Chromium didn't shut down correctly" bubble after a power cut.
PREFS="$PROFILE/Default/Preferences"
if [ -f "$PREFS" ]; then
  sed -i 's/"exit_type":"Crashed"/"exit_type":"Normal"/; s/"exited_cleanly":false/"exited_cleanly":true/' "$PREFS" || true
fi

# Where is the dashboard? The backend records its real address when it starts:
# normally port 8080, another one if that was taken. PIDASH_URL overrides it.
URL_FILE="$XDG_RUNTIME_DIR/pidash/url"
dashboard_url() {
  if [ -n "${PIDASH_URL:-}" ]; then
    echo "$PIDASH_URL"
  elif [ -s "$URL_FILE" ]; then
    head -n 1 "$URL_FILE"
  else
    echo "http://127.0.0.1:8080/"
  fi
}

# Wait for the backend so the browser never opens on an error page.
i=0
while :; do
  URL="$(dashboard_url)"
  if curl -fs -o /dev/null "${URL%/}/healthz"; then break; fi
  i=$((i + 1))
  if [ "$i" -ge 30 ]; then
    echo "The PiDash backend is not answering at $URL." >&2
    echo "Run this to see why:  bash ~/.local/share/pidash/doctor.sh" >&2
    exit 1
  fi
  sleep 1
done

echo "starting $CHROMIUM ($PLATFORM) -> $URL"
exec "$CHROMIUM" \
  --kiosk \
  --ozone-platform="$PLATFORM" \
  --user-data-dir="$PROFILE" \
  --class=pidash \
  --no-first-run \
  --no-default-browser-check \
  --password-store=basic \
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
  --disable-background-timer-throttling \
  --disable-renderer-backgrounding \
  --remote-debugging-port="${PIDASH_DEVTOOLS_PORT:-9222}" \
  "$URL"
