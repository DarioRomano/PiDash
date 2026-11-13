#!/bin/bash
# Removes PiDash for the current user. Your settings are kept unless --purge is given.
# apt packages installed by install.sh (chromium, v4l-utils, curl, fonts-inter) are left alone.
set -uo pipefail

if [ "$(id -u)" -eq 0 ]; then
  echo "Run this as your normal desktop user, without sudo." >&2
  exit 1
fi
: "${HOME:?HOME is not set}"

PURGE=0
[ "${1:-}" = "--purge" ] && PURGE=1

systemctl --user disable --now pidash-kiosk.service pidash.service >/dev/null 2>&1

rm -f "$HOME/.config/systemd/user/pidash.service" \
      "$HOME/.config/systemd/user/pidash-kiosk.service" \
      "$HOME/.local/share/applications/pidash.desktop" \
      "$HOME/.config/autostart/pidash.desktop"
systemctl --user daemon-reload >/dev/null 2>&1

rm -f "$HOME/.local/share/pidash/pidash" \
      "$HOME/.local/share/pidash/kiosk.sh" \
      "$HOME/.local/share/pidash/cec-setup.sh" \
      "$HOME/.local/share/pidash/uninstall.sh"
rmdir "$HOME/.local/share/pidash" 2>/dev/null

if [ "$PURGE" -eq 1 ]; then
  rm -f "$HOME/.config/pidash/config.toml"
  rmdir "$HOME/.config/pidash" 2>/dev/null
  rm -rf "$HOME/.cache/pidash-chromium"
  echo "PiDash removed, including settings and the browser profile."
else
  echo "PiDash removed. Settings kept in ~/.config/pidash (use --purge to delete them too)."
fi
