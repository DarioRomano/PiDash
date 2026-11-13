# PiDash

A fast, good-looking dashboard for your TV, running on a Raspberry Pi 5 and
controlled with the TV's own remote (HDMI-CEC) or your phone.

- **Ambient screen**: photo slideshow with clock, date and weather overlaid.
- **Zero extra hardware**: the TV remote works over HDMI-CEC. A phone remote is built in.
- **Single binary**: Go backend with the web UI embedded. Deploy with one command.
- **Stays smooth**: 1080p render, GPU-only animations, no polling in the browser.

See [docs/PLAN.md](docs/PLAN.md) for the architecture and roadmap.

## Status

Milestone 1: fullscreen dashboard with a clock on top of the existing desktop,
start/exit controls, remote-control input mapping with on-screen key debugging,
phone remote, system stats. Photos and weather are next.

## Install on your Pi

PiDash runs **on top of your existing Raspberry Pi OS desktop**. Nothing is
flashed and the boot setup is not changed. It is a fullscreen browser window
with its own profile: close it and you are back on your desktop, exactly as you
left it. Everything installs into your home directory and can be removed again.

Requirements: a Raspberry Pi 5 running Raspberry Pi OS with desktop (Bookworm or
newer), connected to the TV.

**Option A: from another computer over SSH** (needs Go 1.22+ and Node 18+ there):

```sh
make install PI_HOST=pi@raspberrypi.local     # first time
make deploy  PI_HOST=pi@raspberrypi.local     # later updates
```

**Option B: directly on the Pi** (needs Go 1.22+ and Node 18+ on the Pi; on
Raspberry Pi OS Trixie: `sudo apt install golang nodejs npm`):

```sh
git clone https://github.com/DarioRomano/PiDash && cd PiDash
make install-local
```

Add `INSTALL_FLAGS=--autostart` to either command if you want the dashboard to
open by itself whenever the desktop starts. Without it, the Pi boots to your
normal desktop and the dashboard only appears when you ask for it.

What the installer does:

| What | Where |
|------|-------|
| Program and scripts | `~/.local/share/pidash/` |
| Settings | `~/.config/pidash/config.toml` |
| Two systemd *user* services (backend, fullscreen browser) | `~/.config/systemd/user/` |
| "PiDash" entry in the desktop menu | `~/.local/share/applications/` |
| Packages, only if missing | `chromium`, `v4l-utils`, `curl`, `fonts-inter` |

## Using it

| To… | Do this |
|-----|---------|
| Show the dashboard on the TV | Open **PiDash** from the desktop menu, tap **Show on TV** on the phone remote, or run `systemctl --user start pidash-kiosk` |
| Go back to the desktop | **Hold Back for 2 seconds** on the TV remote, press **Alt+F4** or **F10** on a keyboard, or tap **Exit to desktop** on the phone remote |
| Use the phone remote | Open `http://<pi-ip>:8080/remote` on any device on your network |
| Change settings | Edit `~/.config/pidash/config.toml`, then `systemctl --user restart pidash` |
| See logs | `journalctl --user -u pidash -u pidash-kiosk -f` |
| Uninstall | `bash ~/.local/share/pidash/uninstall.sh` (add `--purge` to delete settings too) |

The screen stays awake while the dashboard is showing; your normal screen
blanking applies again once you exit. The layout scales to whatever resolution
the desktop uses. For the smoothest result on a 4K TV, set the desktop to
1920x1080 in *Preferences → Screen Configuration*.

### Mapping your TV remote

Enable CEC on the TV (Samsung "Anynet+", LG "Simplink", Sony "Bravia Sync",
Philips "EasyLink", etc.). When the dashboard starts it registers the Pi with
the TV and asks it to switch to the Pi's input; from then on the TV forwards
remote key presses to the Pi.

With `display.debug_keys = true` (the default) the dashboard shows the raw key
code of every press in the top-right corner. Put the codes you see into
`[input.keys]` in the config, mapped to the actions
`up down left right select back menu home playpause exit 0-9`.

Check CEC from a terminal on the Pi:

```sh
~/.local/share/pidash/cec-setup.sh   # should print "using /dev/cec0 ..."
cec-ctl -d /dev/cec0 -S              # show the TV in the topology
```

### Debugging the browser on the TV

The dashboard's Chromium listens for DevTools on port 9222, on the Pi only. From
your laptop, forward the port and inspect it:

```sh
ssh -L 9222:localhost:9222 pi@raspberrypi.local
```

Then open `chrome://inspect` in Chrome on the laptop and pick the PiDash page to
profile paint and layout on the real device.

## Development

```sh
make dev            # Go backend on :8080 with mock data + Vite dev server on :5173
make build          # native binary in bin/pidash (frontend embedded)
make lint test
```

Arrow keys, Enter, Escape and digits stand in for the remote during development.
Hold Escape for two seconds to trigger "exit to desktop".

## Layout

```
cmd/pidash/          entry point
internal/config/     TOML config, defaults, validation
internal/server/     HTTP, SSE hub, input API, static files
internal/providers/  provider interface + runner, system/ stats
web/                 Vite + TypeScript frontend (dashboard + phone remote)
internal/kiosk/      start/stop the fullscreen browser (systemd user unit)
deploy/              install.sh, uninstall.sh, systemd user units, kiosk and CEC scripts
```

## License

MIT — see [LICENSE](LICENSE).
