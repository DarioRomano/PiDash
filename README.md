# PiDash

A fast, good-looking dashboard for your TV, running on a Raspberry Pi 5 and
controlled with the TV's own remote (HDMI-CEC) or your phone.

- **Ambient screen**: photo slideshow with clock, date and weather overlaid.
- **Zero extra hardware**: the TV remote works over HDMI-CEC. A phone remote is built in.
- **Single binary**: Go backend with the web UI embedded. Deploy with one command.
- **Stays smooth**: 1080p render, GPU-only animations, no polling in the browser.

See [docs/PLAN.md](docs/PLAN.md) for the architecture and roadmap.

## Status

Working today: fullscreen dashboard with a clock on top of the existing desktop,
start/exit controls, an app launcher (Moonlight, Kodi, Steam Link, …),
remote-control input mapping with on-screen key debugging, phone remote, system
stats. Photos and weather are next.

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
| Only with `--console-apps` | three system files, see [Apps](#apps-moonlight-kodi-steam-link-and-others) |

## Using it

| To… | Do this |
|-----|---------|
| Show the dashboard on the TV | Open **PiDash** from the desktop menu, tap **Show on TV** on the phone remote, or run `systemctl --user start pidash-kiosk` |
| Launch an app | Press **OK** on the dashboard to open the app row, choose with ◀ ▶ and press OK. Or tap the app on the phone remote |
| Get back from an app | Quit the app, or tap **Back to dashboard** on the phone remote |
| Go back to the desktop | **Hold Back for 2 seconds** on the TV remote, press **Alt+F4** or **F10** on a keyboard, or tap **Exit to desktop** on the phone remote |
| Use the phone remote | Open `http://<pi-ip>:8080/remote` on any device on your network |
| Change settings | Edit `~/.config/pidash/config.toml`, then `systemctl --user restart pidash` |
| See logs | `journalctl --user -u pidash -u pidash-kiosk -f` |
| Uninstall | `bash ~/.local/share/pidash/uninstall.sh` (add `--purge` to delete settings too) |

The screen stays awake while the dashboard is showing; your normal screen
blanking applies again once you exit. The layout scales to whatever resolution
the desktop uses. For the smoothest result on a 4K TV, set the desktop to
1920x1080 in *Preferences → Screen Configuration*.

### Apps: Moonlight, Kodi, Steam Link and others

Add the programs you want on the TV to `~/.config/pidash/config.toml`, one block
each, then restart the backend:

```toml
[[apps]]
id = "moonlight"
name = "Moonlight"
command = "moonlight-qt"
mode = "console"

[[apps]]
id = "steamlink"
name = "Steam Link"
command = "steamlink"
```

Each app has a `mode`:

- **`desktop`** (the default) runs the program on the desktop, as if you had
  started it from the menu. No extra setup.
- **`console`** runs it on its own screen outside the desktop, the one you would
  reach with Ctrl+Alt+F8. The app then drives the display directly, which is
  what Moonlight and Kodi want: lower latency, HDR, and switching the refresh
  rate to match the content. When the app ends you are returned to where you
  were, and the desktop is untouched underneath.

Console mode needs a one-time setup, because starting something on another
screen requires system permissions:

```sh
make install PI_HOST=pi@raspberrypi.local INSTALL_FLAGS=--console-apps
# or, on the Pi:  bash deploy/install.sh --console-apps
```

This is the only system-wide part of PiDash. It adds three files: a systemd
unit, a small script that switches screens, and a rule that lets your user start
and stop that unit without a password. Apps still run as you, not as root. The
uninstaller removes all three.

While a console app is on screen, the dashboard cannot see the TV remote. Use the
app's own quit shortcut, or **Back to dashboard** on the phone remote, which
always works. An optional `icon = "/path/to/icon.svg"` puts a picture on the tile.

If an app does not start, the TV shows a message. Details are in the logs:

```sh
journalctl --user -u 'pidash-app-*' -e        # desktop-mode apps
journalctl -u 'pidash-console@*' -e           # console-mode apps
```

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
internal/apps/       app launcher (desktop and console modes)
deploy/              install.sh, uninstall.sh, systemd user units, kiosk and CEC scripts
```

## License

MIT — see [LICENSE](LICENSE).
