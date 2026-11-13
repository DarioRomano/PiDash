#!/bin/sh
# Registers the Pi as a CEC "Playback Device" so the TV forwards remote-control
# key presses to it. With the KMS driver the kernel turns those into ordinary
# keyboard events, which Chromium receives inside cage.
#
# Pi 5 has two HDMI ports and therefore /dev/cec0 and /dev/cec1. We pick the
# adapter whose port actually has a TV on it (a valid physical address).
set -eu

if ! command -v cec-ctl >/dev/null 2>&1; then
  echo "cec-ctl missing (apt install v4l-utils)" >&2
  exit 1
fi

dev=""
for d in /dev/cec0 /dev/cec1; do
  [ -e "$d" ] || continue
  pa=$(cec-ctl -d "$d" -s 2>/dev/null | sed -n 's/.*Physical Address *: *\([0-9a-f.]*\).*/\1/p' | head -n1)
  if [ -n "$pa" ] && [ "$pa" != "f.f.f.f" ]; then dev="$d"; break; fi
done

if [ -z "$dev" ]; then
  echo "no CEC adapter with a connected TV found (is the TV on and CEC enabled in its menu?)" >&2
  exit 1
fi

echo "using $dev"
# Clear any previous logical address, then claim Playback Device with our OSD name.
cec-ctl -d "$dev" --clear >/dev/null 2>&1 || true
cec-ctl -d "$dev" --playback --osd-name PiDash -S
# Tell the TV we are the active source so it routes remote keys to us.
pa=$(cec-ctl -d "$dev" -s | sed -n 's/.*Physical Address *: *\([0-9a-f.]*\).*/\1/p' | head -n1)
cec-ctl -d "$dev" --to 0 --active-source phys-addr="$pa" || true
