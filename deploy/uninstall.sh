#!/bin/bash
# Removes PiDash for the current user. Your settings are kept unless --purge is given.
# apt packages installed by install.sh are left alone.
# Without --purge the browser profiles of your web apps (YouTube, Netflix, ... logins) are kept too.
# If console mode was set up (install.sh --console-apps), its three system files
# are removed too, which needs sudo.
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

# System-wide pieces of console mode, present only after `install.sh --console-apps`.
if [ -e "/etc/systemd/system/pidash-console@.service" ] || [ -e /etc/polkit-1/rules.d/50-pidash-console.rules ] || [ -e /usr/local/lib/pidash/vt-switch.sh ]; then
  echo "Removing console mode (needs sudo)"
  sudo systemctl stop 'pidash-console@*.service' >/dev/null 2>&1
  sudo rm -f "/etc/systemd/system/pidash-console@.service" \
             /etc/polkit-1/rules.d/50-pidash-console.rules \
             /usr/local/lib/pidash/vt-switch.sh
  sudo rmdir /usr/local/lib/pidash 2>/dev/null
  sudo systemctl daemon-reload
fi

rm -f "$HOME/.local/share/pidash/pidash" \
      "$HOME/.local/share/pidash/kiosk.sh" \
      "$HOME/.local/share/pidash/cec-setup.sh" \
      "$HOME/.local/share/pidash/uninstall.sh"
rmdir "$HOME/.local/share/pidash" 2>/dev/null

if [ "$PURGE" -eq 1 ]; then
  rm -f "$HOME/.config/pidash/config.toml"
  rmdir "$HOME/.config/pidash" 2>/dev/null
  rm -rf "$HOME/.cache/pidash-chromium" "$HOME/.local/share/pidash/profiles"
  rmdir "$HOME/.local/share/pidash" 2>/dev/null
  echo "PiDash removed, including settings and the browser profile."
else
  echo "PiDash removed. Settings (~/.config/pidash) and app logins (~/.local/share/pidash/profiles) kept; use --purge to delete them too."
fi
