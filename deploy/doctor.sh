#!/bin/bash
# Collects what is needed to see why PiDash does not start or misbehaves.
# It only reads; nothing is changed. Passwords in your settings are not shown.
#
#   bash ~/.local/share/pidash/doctor.sh
#
# Copy the whole output when asking for help.

APP_DIR="$HOME/.local/share/pidash"
CONF="$HOME/.config/pidash/config.toml"
: "${XDG_RUNTIME_DIR:=/run/user/$(id -u)}"
URL_FILE="$XDG_RUNTIME_DIR/pidash/url"

section() { printf '\n=== %s\n' "$1"; }
have() { command -v "$1" >/dev/null 2>&1; }

section "PiDash"
if [ -x "$APP_DIR/pidash" ]; then "$APP_DIR/pidash" -version; else echo "not installed in $APP_DIR"; fi
echo "install options: $(tr '\n' ' ' < "$APP_DIR/install-flags" 2>/dev/null)"

section "System"
grep -E '^(PRETTY_NAME|VERSION_CODENAME)=' /etc/os-release 2>/dev/null
echo "machine: $(uname -m), kernel $(uname -r)"
[ -r /proc/device-tree/model ] && echo "model: $(tr -d '\0' < /proc/device-tree/model)"
echo "groups: $(id -nG)"

section "Desktop session"
echo "desktop: ${XDG_CURRENT_DESKTOP:-?}   session type: ${XDG_SESSION_TYPE:-?}"
echo "WAYLAND_DISPLAY=${WAYLAND_DISPLAY:-}   DISPLAY=${DISPLAY:-}"
echo "wayland sockets: $(find "$XDG_RUNTIME_DIR" -maxdepth 1 -name 'wayland-*' ! -name '*.lock' -printf '%f ' 2>/dev/null)"
echo "login screen: $(cat /etc/X11/default-display-manager 2>/dev/null || echo '?')"
echo "running: $(pgrep -x -l 'labwc|wayfire|gnome-shell|mutter|kwin_wayland|Xorg|Xwayland' 2>/dev/null | awk '{print $2}' | sort -u | tr '\n' ' ')"

section "Browsers"
for b in chromium chromium-browser firefox firefox-esr; do
  if have "$b"; then echo "$b: $(command -v "$b")  $("$b" --version 2>/dev/null | head -n 1)"; fi
done
have chromium || have chromium-browser || echo "Chromium is missing: sudo apt install chromium"

section "Settings file"
if [ -f "$CONF" ]; then
  "$APP_DIR/pidash" -config "$CONF" -check-config 2>&1
else
  echo "no settings file at $CONF (built-in settings are used)"
fi

section "Backend"
if [ -s "$URL_FILE" ]; then
  URL="$(head -n 1 "$URL_FILE")"
  echo "the backend says it is at: $URL"
else
  URL="http://127.0.0.1:8080/"
  echo "the backend has not recorded an address (it is not running); trying $URL"
fi
if have curl; then
  echo "answer: $(curl -s -m 3 "${URL%/}/healthz" 2>&1 || echo 'no answer')"
fi

section "Programs listening on ports 8080 to 8090 and 9222"
if have ss; then
  ss -ltnp 2>/dev/null | awk 'NR==1 || /:(80[89][0-9]|9222) /' | sed 's/users:(//; s/)$//'
  echo "(a line without a program name belongs to another user; 'sudo ss -ltnp' names it)"
else
  echo "ss is not installed"
fi

section "Services"
systemctl --user status pidash.service pidash-kiosk.service -l --no-pager -n 25 2>&1

section "Recent log"
journalctl --user-unit pidash.service --user-unit pidash-kiosk.service -n 40 --no-pager -o short 2>&1 | tail -n 45

section "TV remote and keyboards"
if [ -r /proc/bus/input/devices ]; then
  echo "input devices: $(sed -n 's/^N: Name="\(.*\)"$/\1/p' /proc/bus/input/devices | paste -sd';' | sed 's/;/; /g')"
fi
readable=0; total=0
for d in /dev/input/event*; do
  [ -e "$d" ] || continue
  total=$((total + 1)); [ -r "$d" ] && readable=$((readable + 1))
done
echo "PiDash can read $readable of $total input devices (it needs to, for the remote's OK and Back buttons)"
if [ -w /dev/uinput ]; then
  echo "virtual keyboard: allowed (buttons can be passed on to apps)"
elif [ -e /dev/uinput ]; then
  echo "virtual keyboard: NOT allowed. Run the update again: bash $APP_DIR/update.sh"
else
  echo "virtual keyboard: /dev/uinput does not exist (sudo modprobe uinput)"
fi
if have wlrctl; then
  echo "keyboard for open apps: wlrctl is installed (PiDash can hand the keyboard to an app's window)"
else
  echo "keyboard for open apps: wlrctl is not installed (sudo apt install wlrctl)"
fi

section "TV over HDMI-CEC"
if have cec-ctl; then "$APP_DIR/pidash" -cec status 2>&1; else echo "cec-ctl is not installed (sudo apt install v4l-utils)"; fi
