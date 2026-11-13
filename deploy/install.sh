#!/bin/bash
# Installs PiDash on an EXISTING Raspberry Pi OS desktop, for the current user.
#
# It does not change how the Pi boots and does not touch your desktop:
#   - files go to   ~/.local/share/pidash  and  ~/.config/pidash
#   - two systemd USER units (backend + fullscreen browser)
#   - a "PiDash" entry in the desktop menu
#   - apt packages only if missing: chromium, v4l-utils (CEC), curl, fonts-inter,
#     firefox (YouTube app), libwidevinecdm0 (DRM module for the Netflix app)
# Remove everything again with deploy/uninstall.sh.
#
# Usage (as your normal user, NOT with sudo):
#   bash deploy/install.sh [--autostart] [--console-apps] [--binary PATH]
#     --autostart     also open the dashboard automatically when the desktop starts
#     --console-apps  allow apps with mode = "console" (Moonlight, Kodi, ...) to run
#                     on their own virtual terminal. This is the one part that is
#                     system-wide: it adds a systemd unit, a small helper script
#                     and a polkit rule under /etc and /usr/local (needs sudo).
#     --binary        path to the pidash binary (default: looks next to deploy/)
set -euo pipefail

if [ "$(id -u)" -eq 0 ]; then
  echo "Run this as your normal desktop user, without sudo." >&2
  exit 1
fi

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
AUTOSTART=0
CONSOLE_APPS=0
CONSOLE_VT="${PIDASH_CONSOLE_VT:-8}"
BINARY=""
while [ $# -gt 0 ]; do
  case "$1" in
    --autostart) AUTOSTART=1 ;;
    --console-apps) CONSOLE_APPS=1 ;;
    --binary) BINARY="${2:?--binary needs a path}"; shift ;;
    -h|--help) sed -n '2,19p' "$0"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 1 ;;
  esac
  shift
done

if [ -z "$BINARY" ]; then
  for c in "$ROOT/pidash-arm64" "$ROOT/bin/pidash-arm64" "$ROOT/bin/pidash"; do
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
missing=()
command -v chromium >/dev/null 2>&1 || command -v chromium-browser >/dev/null 2>&1 || missing+=(chromium)
command -v cec-ctl >/dev/null 2>&1 || missing+=(v4l-utils)
command -v curl >/dev/null 2>&1 || missing+=(curl)
fc-list 2>/dev/null | grep -qi '/inter' || missing+=(fonts-inter)
# The YouTube app runs in Firefox (for uBlock Origin).
command -v firefox >/dev/null 2>&1 || command -v firefox-esr >/dev/null 2>&1 || missing+=(firefox)
# Netflix only plays with the Widevine DRM module, which Raspberry Pi OS packages for Chromium.
if ! dpkg -s libwidevinecdm0 >/dev/null 2>&1 && apt-cache show libwidevinecdm0 >/dev/null 2>&1; then
  missing+=(libwidevinecdm0)
fi
if [ "$CONSOLE_APPS" -eq 1 ]; then
  command -v chvt >/dev/null 2>&1 || missing+=(kbd)
fi
if [ ${#missing[@]} -gt 0 ]; then
  echo "    installing: ${missing[*]} (needs sudo)"
  sudo apt-get update -qq
  sudo apt-get install -y --no-install-recommends "${missing[@]}"
else
  echo "    nothing to install"
fi

echo "==> Installing files to $APP_DIR"
mkdir -p "$APP_DIR" "$CONF_DIR" "$UNIT_DIR" "$MENU_DIR"
# Install to a temp name and rename, so a running backend is replaced atomically.
install -m 0755 "$BINARY" "$APP_DIR/pidash.new"
mv -f "$APP_DIR/pidash.new" "$APP_DIR/pidash"
install -m 0755 "$HERE/kiosk.sh" "$HERE/cec-setup.sh" "$APP_DIR/"
if [ -f "$HERE/uninstall.sh" ]; then install -m 0755 "$HERE/uninstall.sh" "$APP_DIR/"; fi
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
install -m 0644 "$HERE/pidash.desktop" "$MENU_DIR/pidash.desktop"
systemctl --user daemon-reload
systemctl --user enable pidash.service >/dev/null
systemctl --user restart pidash.service

if [ "$AUTOSTART" -eq 1 ]; then
  mkdir -p "$AUTOSTART_DIR"
  install -m 0644 "$HERE/pidash.desktop" "$AUTOSTART_DIR/pidash.desktop"
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

if ! id -nG | tr ' ' '\n' | grep -qx video; then
  echo "NOTE: you are not in the 'video' group, so the TV remote (CEC) will not work."
  echo "      Fix with: sudo usermod -aG video $USER   (then log out and in again)"
fi

IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
cat <<MSG

PiDash is installed.

  Show on TV       : open "PiDash" from the desktop menu, or
                     systemctl --user start pidash-kiosk
  Back to desktop  : hold Back on the TV remote for 2 seconds, press Alt+F4,
                     or use the phone remote
  Phone remote     : http://${IP:-<pi-ip>}:8080/remote
  Apps             : press OK on the dashboard; add them under [[apps]] in the settings
  Background photos: set [photos] in the settings, then pick folders at
                     http://${IP:-<pi-ip>}:8080/photos
  Settings         : $CONF_DIR/config.toml  (then: systemctl --user restart pidash)
  Logs             : journalctl --user -u pidash -u pidash-kiosk -f
  Uninstall        : bash $APP_DIR/uninstall.sh
MSG
