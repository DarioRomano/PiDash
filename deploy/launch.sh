#!/bin/sh
# Started by the "PiDash" entry in the desktop menu. Shows the dashboard, and
# if it cannot start, says so in a window instead of doing nothing.
set -u

APP_DIR="$(cd "$(dirname "$0")" && pwd)"
: "${XDG_RUNTIME_DIR:=/run/user/$(id -u)}"
URL_FILE="$XDG_RUNTIME_DIR/pidash/url"

systemctl --user start pidash.service 2>/dev/null
systemctl --user --no-block start pidash-kiosk.service 2>/dev/null

dashboard_url() {
  if [ -s "$URL_FILE" ]; then head -n 1 "$URL_FILE"; else echo "http://127.0.0.1:8080/"; fi
}

# Give the backend a moment; it normally answers within a second.
i=0
while [ "$i" -lt 20 ]; do
  URL="$(dashboard_url)"
  if curl -fs -o /dev/null "${URL%/}/healthz" 2>/dev/null; then
    # The backend is up. If the browser unit has failed, that is the problem.
    sleep 3
    state="$(systemctl --user is-failed pidash-kiosk.service 2>/dev/null)"
    [ "$state" = "failed" ] || exit 0
    break
  fi
  i=$((i + 1))
  sleep 1
done

details="$(systemctl --user status pidash.service pidash-kiosk.service -l --no-pager -n 6 2>&1 | grep -vE '^\s*(Loaded|Tasks|CPU|CGroup|Memory|Main PID|Process|Docs):' | tail -n 24)"
message="PiDash could not start.

For a full report, open a terminal and run:
    bash $APP_DIR/doctor.sh

What the system says:
$details"

# Use whichever way of showing a message this desktop has.
if command -v zenity >/dev/null 2>&1; then
  zenity --error --title="PiDash" --width=760 --no-markup --text="$message" 2>/dev/null
elif command -v kdialog >/dev/null 2>&1; then
  kdialog --title "PiDash" --error "$message"
elif command -v notify-send >/dev/null 2>&1; then
  notify-send -u critical "PiDash could not start" "Run: bash $APP_DIR/doctor.sh"
elif command -v xmessage >/dev/null 2>&1; then
  xmessage -center "$message"
fi
exit 1
