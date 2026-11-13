#!/bin/bash
# One-shot installer for a fresh Raspberry Pi OS Lite (Bookworm, 64-bit) on a Pi 5.
# Run from a directory containing: pidash-arm64, deploy/, config.example.toml
#   sudo bash deploy/install.sh
# Re-running is safe; it only updates what changed.
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then echo "run with sudo" >&2; exit 1; fi

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
# The desktop user that will run the kiosk and the backend.
KIOSK_USER="${KIOSK_USER:-${SUDO_USER:-pi}}"
id "$KIOSK_USER" >/dev/null 2>&1 || { echo "user $KIOSK_USER does not exist" >&2; exit 1; }

echo "==> Installing packages"
apt-get update -qq
# chromium is the Bookworm package name; chromium-browser exists on some images.
apt-get install -y --no-install-recommends cage v4l-utils curl fonts-inter seatd \
  $(apt-cache show chromium >/dev/null 2>&1 && echo chromium || echo chromium-browser)

echo "==> Installing files"
install -d -m 0755 /opt/pidash /etc/pidash
if [ -f "$ROOT/pidash-arm64" ]; then
  install -m 0755 "$ROOT/pidash-arm64" /opt/pidash/pidash
elif [ -f "$ROOT/bin/pidash-arm64" ]; then
  install -m 0755 "$ROOT/bin/pidash-arm64" /opt/pidash/pidash
else
  echo "pidash-arm64 binary not found next to deploy/ — run 'make build-pi' first" >&2; exit 1
fi
install -m 0755 "$HERE/kiosk.sh" "$HERE/cec-setup.sh" /opt/pidash/
if [ ! -f /etc/pidash/config.toml ]; then
  install -m 0644 "$ROOT/config.example.toml" /etc/pidash/config.toml
  echo "    wrote /etc/pidash/config.toml (edit it, then: sudo systemctl restart pidash)"
fi

echo "==> Installing systemd units for user '$KIOSK_USER'"
for u in pidash.service pidash-kiosk.service pidash-cec.service; do
  sed "s/@USER@/$KIOSK_USER/g" "$HERE/$u" > "/etc/systemd/system/$u"
done
# cage needs the user in these groups to open the GPU and input devices via logind/seatd.
for g in video render input; do getent group "$g" >/dev/null && usermod -aG "$g" "$KIOSK_USER"; done
systemctl daemon-reload
systemctl set-default graphical.target
systemctl disable --now getty@tty1.service >/dev/null 2>&1 || true
systemctl enable pidash.service pidash-kiosk.service pidash-cec.service
systemctl restart pidash.service

echo "==> Boot config"
CMDLINE=/boot/firmware/cmdline.txt
[ -f "$CMDLINE" ] || CMDLINE=/boot/cmdline.txt
if [ -f "$CMDLINE" ]; then
  # Force 1080p60 on HDMI0: cheaper to render than 4K and the TV upscales for free.
  if ! grep -q 'video=HDMI-A-1' "$CMDLINE"; then
    sed -i '1 s/$/ video=HDMI-A-1:1920x1080M@60D/' "$CMDLINE"
    echo "    added video=HDMI-A-1:1920x1080M@60D to $CMDLINE"
  fi
  # Quieter boot.
  for opt in quiet loglevel=3 vt.global_cursor_default=0 consoleblank=0; do
    grep -q "$opt" "$CMDLINE" || sed -i "1 s/$/ $opt/" "$CMDLINE"
  done
fi
CONFIG=/boot/firmware/config.txt
[ -f "$CONFIG" ] || CONFIG=/boot/config.txt
if [ -f "$CONFIG" ] && ! grep -q '^disable_splash=1' "$CONFIG"; then
  echo 'disable_splash=1' >> "$CONFIG"
fi

echo
echo "Done. Reboot to start the kiosk:  sudo reboot"
echo "Then on another machine:  http://$(hostname -I | awk '{print $1}'):8080/remote"
