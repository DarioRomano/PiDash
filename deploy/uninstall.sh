#!/bin/bash
# Removes PiDash for the current user. Your settings are kept unless --purge is given.
# apt packages installed by install.sh are left alone.
# Without --purge the browser profiles of your web apps (YouTube, Netflix, ... logins) are kept too.
# The system files added by install.sh --console-apps and --phone-input are
# removed too, which needs sudo.
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

# The udev rule the installer adds for the virtual keyboard.
if [ -e /etc/udev/rules.d/70-pidash-uinput.rules ]; then
  echo "Removing the virtual keyboard permission (needs sudo)"
  sudo rm -f /etc/udev/rules.d/70-pidash-uinput.rules
  sudo udevadm control --reload-rules
fi

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
      "$HOME/.local/share/pidash/update.sh" \
      "$HOME/.local/share/pidash/launch.sh" \
      "$HOME/.local/share/pidash/doctor.sh" \
      "$HOME/.local/share/pidash/install-flags" \
      "$HOME/.local/share/pidash/cec-setup.sh" \
      "$HOME/.local/share/pidash/uninstall.sh"
rmdir "$HOME/.local/share/pidash" 2>/dev/null

if [ "$PURGE" -eq 1 ]; then
  # Everything PiDash kept: the settings file and what was saved on the
  # settings page; what it chose, remembered and logged in to; and what it
  # can make again (prepared photos, covers, the browser's own cache).
  rm -rf "${HOME:?}/.config/pidash" "${HOME:?}/.local/share/pidash" \
         "${HOME:?}/.cache/pidash" "${HOME:?}/.cache/pidash-chromium" \
         "${HOME:?}/snap/chromium/common/pidash" "${HOME:?}/snap/chromium/common/pidash-chromium"
  echo "PiDash removed, including settings, app logins and everything it kept."
else
  echo "PiDash removed. Settings (~/.config/pidash) and app logins (~/.local/share/pidash/profiles) kept; use --purge to delete them too."
fi
