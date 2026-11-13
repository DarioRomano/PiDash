#!/bin/sh
# Registers the Pi as a CEC "Playback Device" so the TV forwards remote-control
# key presses to it, and tells the TV to switch to the Pi's HDMI input.
# With the KMS driver the kernel turns the remote keys into ordinary keyboard
# events, which the fullscreen browser receives like any other key press.
#
# Pi 5 has two HDMI ports and therefore /dev/cec0 and /dev/cec1. We pick the
# adapter whose port actually has a TV on it (a valid physical address).
# Needs the user to be in the "video" group (the default Pi user is).
set -eu

if ! command -v cec-ctl >/dev/null 2>&1; then
  echo "cec-ctl missing: sudo apt install v4l-utils" >&2
  exit 1
fi

phys_addr() {
  cec-ctl -d "$1" -s 2>/dev/null | sed -n 's/.*Physical Address *: *\([0-9a-f.]*\).*/\1/p' | head -n1
}

dev=""
for d in /dev/cec0 /dev/cec1; do
  [ -e "$d" ] || continue
  pa="$(phys_addr "$d")"
  if [ -n "$pa" ] && [ "$pa" != "f.f.f.f" ]; then dev="$d"; break; fi
done

if [ -z "$dev" ]; then
  echo "no CEC adapter with a connected TV found (is the TV on and CEC enabled in its menu?)" >&2
  exit 1
fi

echo "using $dev (physical address $pa)"
# Claim "Playback Device" with our on-screen name; keeps an existing claim if present.
cec-ctl -d "$dev" --playback --osd-name PiDash >/dev/null
# Wake the TV and make the Pi the active source so remote keys are routed to us.
cec-ctl -d "$dev" --to 0 --image-view-on >/dev/null 2>&1 || true
cec-ctl -d "$dev" --to 0 --active-source phys-addr="$pa" >/dev/null 2>&1 || true
