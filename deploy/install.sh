#!/bin/bash
# Installs PiDash on an EXISTING Linux desktop, for the current user: Raspberry
# Pi OS above all, and Debian, Ubuntu, Fedora or Arch on other boards and PCs.
#
# It does not change how the machine boots and does not touch your desktop:
#   - files go to   ~/.local/share/pidash  and  ~/.config/pidash
#   - two systemd USER units (backend + fullscreen browser)
#   - a "PiDash" entry in the desktop menu
#   - packages only if missing: chromium, v4l-utils (CEC), curl, the Inter
#     typeface, firefox, and on Raspberry Pi OS libwidevinecdm0 (for Netflix)
#   - one rule under /etc/udev/rules.d, so the TV remote's buttons reach apps
# Remove everything again with deploy/uninstall.sh.
#
# Usage (as your normal user, NOT with sudo):
#   bash deploy/install.sh [--autostart] [--console-apps] [--phone-input] [--speakers] [--binary PATH]
#     --autostart     also open the dashboard automatically when the desktop starts
#     --console-apps  allow apps with mode = "console" (Moonlight, Kodi, ...) to run
#                     on their own virtual terminal. This is the one part that is
#                     system-wide: it adds a systemd unit, a small helper script
#                     and a polkit rule under /etc and /usr/local (needs sudo).
#     --phone-input   install the small tool the phone remote uses for typing
#                     text. To let the phone act as mouse and keyboard you also
#                     need mouse_keyboard = true under [remote] in the settings.
#     --speakers      install what makes the Pi a speaker for phones, where this
#                     system offers it: shairport-sync (AirPlay) and librespot
#                     (Spotify). Their own system services are switched off;
#                     PiDash runs them when its settings say so.
#     --binary        path to the pidash binary (default: looks next to deploy/)
set -euo pipefail
# Not every way of starting a script sets USER.
USER="${USER:-$(id -un)}"

# An automatic update runs with nobody there to type a password. Steps that
# need administrator rights then run only where sudo works without asking, as
# it does by default on Raspberry Pi OS; elsewhere they are skipped and said so.
# The files such steps installed earlier stay as they are.
if [ "${PIDASH_UNATTENDED:-0}" = 1 ]; then
  if command sudo -n true 2>/dev/null; then
    sudo() { command sudo -n "$@"; }
  else
    sudo() { echo "    skipped, it needs a password: sudo $*" >&2; }
  fi
fi

if [ "$(id -u)" -eq 0 ]; then
  echo "Run this as your normal desktop user, without sudo." >&2
  exit 1
fi

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
GIVEN_FLAGS=()
AUTOSTART=0
CONSOLE_APPS=0
PHONE_INPUT=0
SPEAKERS=0
CONSOLE_VT="${PIDASH_CONSOLE_VT:-8}"
BINARY=""
while [ $# -gt 0 ]; do
  case "$1" in
    --autostart) AUTOSTART=1; GIVEN_FLAGS+=("$1") ;;
    --console-apps) CONSOLE_APPS=1; GIVEN_FLAGS+=("$1") ;;
    --phone-input) PHONE_INPUT=1; GIVEN_FLAGS+=("$1") ;;
    --speakers) SPEAKERS=1; GIVEN_FLAGS+=("$1") ;;
    --binary) BINARY="${2:?--binary needs a path}"; shift ;;
    -h|--help) sed -n '2,27p' "$0"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 1 ;;
  esac
  shift
done

if [ -z "$BINARY" ]; then
  # "pidash" next to deploy/ is the layout of a release package.
  for c in "$ROOT/pidash" "$ROOT/pidash-arm64" "$ROOT/bin/pidash-arm64" "$ROOT/bin/pidash"; do
    if [ -f "$c" ]; then BINARY="$c"; break; fi
  done
fi
if [ -z "$BINARY" ] || [ ! -f "$BINARY" ]; then
  echo "pidash binary not found. Build it first ('make build' on the Pi, or 'make install PI_HOST=...' from another machine)." >&2
  exit 1
fi

APP_DIR="$HOME/.local/share/pidash"
CONF_DIR="$HOME/.config/pidash"
UNIT_DIR="$HOME/.config/systemd/user"
MENU_DIR="$HOME/.local/share/applications"
AUTOSTART_DIR="$HOME/.config/autostart"

echo "==> Checking packages"
# The same things go by different names in different distributions. PiDash is
# made on Raspberry Pi OS, which like Debian and Ubuntu uses apt; Fedora's dnf
# and Arch's pacman are known too. Elsewhere, what is missing is only named.
if command -v apt-get >/dev/null 2>&1; then PM=apt
elif command -v dnf >/dev/null 2>&1; then PM=dnf
elif command -v pacman >/dev/null 2>&1; then PM=pacman
else PM=none; fi

# pkg THING prints the names this system may know the package for a thing by.
pkg() {
  case "$PM:$1" in
    *:browser) echo chromium ;;
    *:cec) echo v4l-utils ;;
    *:curl) echo curl ;;
    apt:font) echo fonts-inter ;;
    dnf:font) echo rsms-inter-fonts ;;
    pacman:font) echo inter-font ;;
    apt:firefox) echo firefox firefox-esr ;;
    *:firefox) echo firefox ;;
    apt:widevine) echo libwidevinecdm0 ;;
    pacman:pactl) echo libpulse ;;
    *:pactl) echo pulseaudio-utils ;;
    *:wlr-randr) echo wlr-randr ;;
    *:chvt) echo kbd ;;
    *:wtype) echo wtype ;;
    *:xdotool) echo xdotool ;;
    *:airplay) echo shairport-sync ;;
    *:spotify) echo librespot raspotify ;;
  esac
}
# offered NAME: whether this system's package source has a package of that name.
offered() {
  case "$PM" in
    apt) apt-cache show "$1" >/dev/null 2>&1 ;;
    dnf) dnf -q info "$1" >/dev/null 2>&1 ;;
    pacman) pacman -Si "$1" >/dev/null 2>&1 ;;
    *) return 1 ;;
  esac
}
missing=()
without=()
# want THING WHAT-FOR: notes the package to install, or that there is none here.
want() {
  local name
  for name in $(pkg "$1"); do
    if offered "$name"; then missing+=("$name"); return 0; fi
  done
  without+=("$2")
}

command -v chromium >/dev/null 2>&1 || command -v chromium-browser >/dev/null 2>&1 || command -v google-chrome-stable >/dev/null 2>&1 || command -v google-chrome >/dev/null 2>&1 \
  || want browser "Chromium or Google Chrome, which shows the dashboard: install one of them"
command -v cec-ctl >/dev/null 2>&1 || want cec "cec-ctl (v4l-utils), for the TV's remote and switching the TV on and off over HDMI"
command -v curl >/dev/null 2>&1 || want curl "curl"
fc-list 2>/dev/null | grep -qi '/inter' || want font "the typeface Inter; another one is used without it"
# Website apps set to Firefox (for its ad blocker, uBlock Origin) need it.
command -v firefox >/dev/null 2>&1 || command -v firefox-esr >/dev/null 2>&1 || want firefox "Firefox, only for website apps set to use it"
# Netflix only plays with the Widevine DRM module, which Raspberry Pi OS packages
# for Chromium. On a PC, Google Chrome brings it; other systems do without.
if [ "$PM" = apt ] && ! dpkg -s libwidevinecdm0 >/dev/null 2>&1 && offered libwidevinecdm0; then missing+=(libwidevinecdm0); fi
# Chooses the sound output and its volume from the settings page.
command -v pactl >/dev/null 2>&1 || want pactl "pactl, for choosing the sound output in the settings"
# Sets the picture sent to the TV, for the "1920 x 1080 while the dashboard runs" setting.
command -v wlr-randr >/dev/null 2>&1 || want wlr-randr "wlr-randr, for the setting that sends the TV a smaller picture"
if [ "$CONSOLE_APPS" -eq 1 ]; then
  command -v chvt >/dev/null 2>&1 || want chvt "chvt (kbd), for apps on their own virtual terminal"
fi
if [ "$PHONE_INPUT" -eq 1 ]; then
  # Typing text into apps: wtype on the Wayland desktop, xdotool on X11.
  command -v wtype >/dev/null 2>&1 || want wtype "wtype, for typing from the phone on a Wayland desktop"
  command -v xdotool >/dev/null 2>&1 || want xdotool "xdotool, for typing from the phone on an X11 desktop"
fi
if [ "$SPEAKERS" -eq 1 ]; then
  command -v shairport-sync >/dev/null 2>&1 || want airplay "shairport-sync, the AirPlay speaker"
  command -v librespot >/dev/null 2>&1 || want spotify "librespot, the Spotify speaker: see \"Speaker for phones\" in the README for where to get it"
fi
if [ ${#missing[@]} -gt 0 ]; then
  echo "    installing: ${missing[*]} (needs sudo)"
  case "$PM" in
    apt)
      sudo apt-get update -qq
      sudo apt-get install -y --no-install-recommends "${missing[@]}"
      ;;
    dnf) sudo dnf install -y "${missing[@]}" ;;
    pacman) sudo pacman -S --needed --noconfirm "${missing[@]}" ;;
  esac
else
  echo "    nothing to install"
fi
if [ ${#without[@]} -gt 0 ]; then
  echo "    not found in this system's packages; PiDash works without, less these:"
  printf '      - %s\n' "${without[@]}"
fi
if [ "$SPEAKERS" -eq 1 ]; then
  # Installed as packages, these start a copy of their own for the whole
  # system. PiDash runs its own, under its name and with the desktop's
  # sound; two copies would be in each other's way.
  for unit in shairport-sync raspotify librespot; do
    if systemctl is-enabled --quiet "$unit" 2>/dev/null || systemctl is-active --quiet "$unit" 2>/dev/null; then
      echo "    switching off the system's own $unit service: PiDash runs it itself (needs sudo)"
      sudo systemctl disable --now "$unit" >/dev/null 2>&1 || echo "    that did not work; by hand: sudo systemctl disable --now $unit"
    fi
  done
fi

echo "==> Installing files to $APP_DIR"
mkdir -p "$APP_DIR" "$CONF_DIR" "$UNIT_DIR" "$MENU_DIR"
# Install to a temp name and rename, so a running backend is replaced atomically.
install -m 0755 "$BINARY" "$APP_DIR/pidash.new"
mv -f "$APP_DIR/pidash.new" "$APP_DIR/pidash"
install -m 0755 "$HERE/kiosk.sh" "$HERE/launch.sh" "$HERE/doctor.sh" "$APP_DIR/"
rm -f "$APP_DIR/cec-setup.sh"   # replaced by "pidash -cec setup"
if [ -f "$HERE/uninstall.sh" ]; then install -m 0755 "$HERE/uninstall.sh" "$APP_DIR/"; fi
# The updater: fetches the latest release and runs its installer with the same options.
if [ -f "$ROOT/install.sh" ]; then install -m 0755 "$ROOT/install.sh" "$APP_DIR/update.sh"; fi
printf '%s\n' "${GIVEN_FLAGS[@]}" | sed '/^$/d' > "$APP_DIR/install-flags"
if [ ! -f "$CONF_DIR/config.toml" ]; then
  install -m 0644 "$ROOT/config.example.toml" "$CONF_DIR/config.toml"
  echo "    wrote $CONF_DIR/config.toml"
else
  echo "    kept existing $CONF_DIR/config.toml"
fi
# The settings can hold your NAS password: readable by you only.
chmod 600 "$CONF_DIR/config.toml"

echo "==> Installing user services and menu entry"
install -m 0644 "$HERE/pidash.service" "$HERE/pidash-kiosk.service" "$UNIT_DIR/"
# The menu entry needs the full path of the launcher.
sed "s|@APP_DIR@|$APP_DIR|g" "$HERE/pidash.desktop" > "$MENU_DIR/pidash.desktop"
systemctl --user daemon-reload
systemctl --user enable pidash.service >/dev/null
systemctl --user restart pidash.service

# Make sure the backend really came up, and learn which port it is on: it
# moves to the next free one when 8080 is taken by another program.
URL_FILE="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/pidash/url"
PORT=""
for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
  if [ -s "$URL_FILE" ]; then
    url="$(head -n 1 "$URL_FILE")"
    if curl -fs -o /dev/null "${url%/}/healthz" 2>/dev/null; then
      PORT="$(printf '%s' "$url" | sed -n 's|.*:\([0-9][0-9]*\)/*$|\1|p')"
      break
    fi
  fi
  sleep 1
done
if [ -z "$PORT" ]; then
  echo
  echo "WARNING: PiDash is installed, but its backend did not start."
  echo "         Run this to see why:  bash $APP_DIR/doctor.sh"
  PORT=8080
elif [ "$PORT" != "8080" ]; then
  echo "    port 8080 is used by another program on this Pi; PiDash is using port $PORT"
fi

if [ "$AUTOSTART" -eq 1 ]; then
  mkdir -p "$AUTOSTART_DIR"
  sed "s|@APP_DIR@|$APP_DIR|g" "$HERE/pidash.desktop" > "$AUTOSTART_DIR/pidash.desktop"
  echo "    autostart enabled (remove $AUTOSTART_DIR/pidash.desktop to disable)"
fi

if [ "$CONSOLE_APPS" -eq 1 ]; then
  echo "==> Setting up console mode for apps (system-wide, needs sudo)"
  case "$CONSOLE_VT" in ''|*[!0-9]*) echo "PIDASH_CONSOLE_VT must be a number" >&2; exit 1 ;; esac
  tmp="$(mktemp -d)"
  sed -e "s|@USER@|$USER|g" -e "s|@HOME@|$HOME|g" -e "s|@VT@|$CONSOLE_VT|g" \
    "$HERE/pidash-console@.service" > "$tmp/pidash-console@.service"
  sed -e "s|@USER@|$USER|g" "$HERE/50-pidash-console.rules" > "$tmp/50-pidash-console.rules"
  sudo install -d -m 0755 /usr/local/lib/pidash
  sudo install -m 0755 -o root -g root "$HERE/vt-switch.sh" /usr/local/lib/pidash/vt-switch.sh
  sudo install -m 0644 -o root -g root "$tmp/pidash-console@.service" "/etc/systemd/system/pidash-console@.service"
  sudo install -m 0644 -o root -g root "$tmp/50-pidash-console.rules" /etc/polkit-1/rules.d/50-pidash-console.rules
  sudo systemctl daemon-reload
  rm -r "$tmp"
  echo "    console apps will use virtual terminal $CONSOLE_VT (Ctrl+Alt+F$CONSOLE_VT)"
fi

# The TV remote's OK and Back buttons are not keyboard keys, so apps such as
# YouTube and Netflix ignore them. PiDash passes them on as Enter and Escape
# through a virtual keyboard, which needs this one rule. It lets members of the
# "input" group create virtual input devices.
RULE=/etc/udev/rules.d/70-pidash-uinput.rules
if ! cmp -s "$HERE/70-pidash-uinput.rules" "$RULE" 2>/dev/null; then
  echo "==> Letting PiDash pass the TV remote's buttons on to apps (one system file, needs sudo)"
  sudo install -m 0644 -o root -g root "$HERE/70-pidash-uinput.rules" "$RULE"
  sudo modprobe uinput 2>/dev/null || true
  sudo udevadm control --reload-rules
  sudo udevadm trigger --name-match=uinput 2>/dev/null || sudo udevadm trigger
fi

if [ "$PHONE_INPUT" -eq 1 ] && ! grep -Eq '^\s*mouse_keyboard\s*=\s*true' "$CONF_DIR/config.toml"; then
  echo "    phone as mouse and keyboard: now switch it on by setting"
  echo "    mouse_keyboard = true  under [remote] in $CONF_DIR/config.toml"
fi

if ! id -nG | tr ' ' '\n' | grep -qx input; then
  echo "NOTE: you are not in the 'input' group. The TV remote's OK and Back buttons"
  echo "      will not work until you are:"
  echo "      sudo usermod -aG input $USER   (then log out and in again)"
fi

if ! id -nG | tr ' ' '\n' | grep -qx video; then
  echo "NOTE: you are not in the 'video' group, so the TV remote (CEC) will not work."
  echo "      Fix with: sudo usermod -aG video $USER   (then log out and in again)"
fi

IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
cat <<MSG

PiDash $("$APP_DIR/pidash" -version 2>/dev/null | awk '{print $2}') is installed.

  Show on TV       : open "PiDash" from the desktop menu, or
                     systemctl --user start pidash-kiosk
  Back to desktop  : hold Back on the TV remote for 2 seconds, press Alt+F4,
                     or use the phone remote
  Phone remote     : http://${IP:-<pi-ip>}:$PORT/remote
  Settings         : http://${IP:-<pi-ip>}:$PORT/settings
                     (weather, background photos, apps)
  Advanced settings: $CONF_DIR/config.toml  (then: systemctl --user restart pidash)
  If it does not start: bash $APP_DIR/doctor.sh
  Update           : bash $APP_DIR/update.sh
  Uninstall        : bash $APP_DIR/uninstall.sh
MSG
