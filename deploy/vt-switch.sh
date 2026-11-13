#!/bin/sh
# Switches the screen to a virtual terminal for a console-mode app and back
# again afterwards. Installed root-owned to /usr/local/lib/pidash/vt-switch.sh
# and run only by pidash-console@.service.
#
#   vt-switch.sh enter 8   remember the current terminal, then show terminal 8
#   vt-switch.sh leave 8   return to the remembered terminal
set -eu

STATE=/run/pidash-console.prev-vt
action="${1:-}"
target="${2:-8}"

case "$target" in
  ''|*[!0-9]*) echo "vt-switch: bad terminal number: $target" >&2; exit 2 ;;
esac

case "$action" in
  enter)
    cur="$(fgconsole 2>/dev/null || echo "")"
    # Only remember where we came from if it is somewhere else; this keeps the
    # original value when a second app is started while one is still closing.
    if [ -n "$cur" ] && [ "$cur" != "$target" ]; then
      echo "$cur" > "$STATE"
    fi
    chvt "$target"
    ;;
  leave)
    prev="$(cat "$STATE" 2>/dev/null || echo "")"
    rm -f "$STATE"
    case "$prev" in
      ''|*[!0-9]*) prev=7 ;;   # Raspberry Pi OS runs the desktop on terminal 7
    esac
    # Do not yank the screen away if the user already switched somewhere else.
    cur="$(fgconsole 2>/dev/null || echo "$target")"
    if [ "$cur" = "$target" ]; then
      chvt "$prev"
    fi
    ;;
  *)
    echo "usage: vt-switch.sh enter|leave <terminal>" >&2
    exit 2
    ;;
esac
