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
for b in chromium chromium-browser google-chrome-stable google-chrome firefox firefox-esr; do
  if have "$b"; then echo "$b: $(command -v "$b")  $("$b" --version 2>/dev/null | head -n 1)"; fi
done
have chromium || have chromium-browser || have google-chrome-stable || have google-chrome || echo "Neither Chromium nor Google Chrome is installed: sudo apt install chromium"
for b in chromium chromium-browser; do
  if have "$b" && { case "$(readlink -f "$(command -v "$b")")" in /snap/* | */snap) true ;; *) head -c 2048 "$(command -v "$b")" | grep -qa '/snap/bin/' ;; esac; }; then
    echo "$b is a snap. It works for the dashboard, but website apps cannot load browser extensions with it; Google Chrome, or Chromium from a package, has no such limit."
  fi
done

section "Video player"
if have mpv; then echo "mpv: $(command -v mpv)  $(mpv --version 2>/dev/null | head -n 1)"
elif have vlc; then echo "vlc: $(command -v vlc) (mpv is not installed; the Videos page works best with it: sudo apt install mpv)"
else echo "neither mpv nor VLC is installed; the Videos page needs one: sudo apt install mpv"; fi

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
if have pactl; then
  echo "sound: pactl is installed (the settings page can choose the output and its volume)"
else
  echo "sound: pactl is not installed (sudo apt install pulseaudio-utils)"
fi
echo "keyboard for open apps: see the status page on the TV (PiDash asks the desktop itself; no extra tool is needed)"

section "TV over HDMI-CEC"
if have cec-ctl; then "$APP_DIR/pidash" -cec status 2>&1; else echo "cec-ctl is not installed (sudo apt install v4l-utils)"; fi
