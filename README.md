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
start/exit controls, weather with an hourly and 7-day detail view, a background
photo slideshow from Synology Photos or a local folder with a folder picker on
your phone, an app launcher (YouTube, Netflix, Synology Photos, Moonlight, Kodi,
Steam Link, …), remote-control input mapping with on-screen key debugging, and
a phone remote.

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
| Packages, only if missing | `chromium`, `v4l-utils`, `curl`, `fonts-inter`, `firefox`, `libwidevinecdm0` |
| Browser profiles of website apps (created on first launch) | `~/.local/share/pidash/profiles/` |
| Chosen photo folders, and prepared photos | `~/.local/share/pidash/photos-selection.json`, `~/.cache/pidash/photos/` |
| Only with `--console-apps` | three system files, see [Apps](#apps) |

## Using it

| To… | Do this |
|-----|---------|
| Show the dashboard on the TV | Open **PiDash** from the desktop menu, tap **Show on TV** on the phone remote, or run `systemctl --user start pidash-kiosk` |
| See the full forecast | Press **OK**, then **OK** again on the **Weather** tile. **Back** closes it |
| Choose the background photos | Open `http://<pi-ip>:8080/photos`, or tap **Background photos** on the phone remote |
| Flip through photos | ◀ ▶ on the dashboard. Play/pause holds the current photo. ▲ shows its folder and date |
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

### Weather

Set your town in `~/.config/pidash/config.toml` and restart the backend:

```toml
[weather]
location = "Zurich"
```

The current temperature, conditions and today's high and low then appear in the
bottom right corner of the screen. Pressing ▲ adds the place name, the "feels
like" temperature and the chance of rain.

For more, press **OK** to open the app row and **OK** again on the **Weather**
tile, which is first in the row. The detail view shows:

- the current temperature and conditions, with what it feels like
- wind with direction and gusts, humidity, chance and amount of rain today,
  UV index, sunrise and sunset
- the next 24 hours in two-hour steps, with the chance of rain where it matters
- the next seven days, each as a bar from its low to its high on one shared
  scale, so warm and cold days are easy to compare. The dot on today's bar is
  the temperature right now.

**Back** or **OK** closes it. With a mouse, clicking the weather in the corner
opens it too.

Options:

```toml
[weather]
location = "Portland, Maine"   # add a region or country for ambiguous names
# latitude = 43.66             # or give exact coordinates instead
# longitude = -70.26
# name = "Home"                # label on the TV
units = "metric"               # or "imperial" for °F, mph and inches
interval = "15m"               # how often the forecast is refreshed
```

Weather data comes from [Open-Meteo](https://open-meteo.com), which is free for
personal use and needs no account. If the internet is down, the last forecast
stays on screen, and the detail view says when it was last updated. Times such
as sunrise are shown in the local time of the place you configured.

### Background photos from Synology Photos

PiDash can show a slideshow behind the clock, taken from folders you choose in
Synology Photos.

1. **On the NAS**, decide which account PiDash signs in with. A separate user
   that can only see the photos you want on the TV is the safest choice. The
   account must not use 2-step verification, because PiDash cannot answer the
   code prompt.
2. **On the Pi**, add this to `~/.config/pidash/config.toml`:

   ```toml
   [photos]
   source = "synology"

   [photos.synology]
   url = "http://192.168.1.20:5000"   # the address you open DSM with
   username = "pidash"
   password = "your-password"
   ```

   Then run `systemctl --user restart pidash`.
3. **On your phone**, open `http://<pi-ip>:8080/photos`. It is also linked from
   the phone remote as **Background photos**. Browse your Personal Space and
   Shared Space, tick the folders you want, and tap **Use these folders**.
   "Include the folders inside them" decides whether subfolders count. The TV
   switches to the new photos within a few seconds. Until you have chosen, the
   TV shows a message with this address.

You can change the choice at any time on the same page. New photos added to the
chosen folders show up by themselves within six hours, or immediately when you
save the selection again.

How photos are shown:

- Every photo is prepared once on the Pi as a 1920×1080 picture and kept on
  disk, so the TV never has to load a large file. The first time a photo
  appears, preparing it takes about a second; this happens ahead of time while
  the previous photo is showing.
- Landscape photos fill the screen. Portrait photos and panoramas are shown
  whole, on a blurred, darkened copy of themselves, instead of being cropped.
- Photos are rotated the right way up using the camera's rotation tag.
- JPEG and PNG photos are prepared from the full file for the sharpest result.
  HEIC and RAW photos use the 1280-pixel preview the NAS has already made. Set
  `quality = "preview"` to use previews for everything, which is lighter on the
  NAS and the Pi. Videos are skipped.
- If the NAS is switched off or unreachable, the photos already prepared keep
  showing, and the folder picker says what is wrong.

More settings, with their defaults:

```toml
[photos]
interval = "30s"    # how long each photo stays
refresh = "6h"      # how often the folders are checked for new photos
cache_mb = 2000     # disk space for prepared photos

[photos.synology]
insecure_tls = false   # true accepts a self-signed certificate on an https:// address
quality = "original"   # or "preview"
```

Good to know:

- The NAS password is stored in the config file, which the installer makes
  readable by your user only.
- Like the phone remote, the folder picker is open to every device on your home
  network. Anyone on it can see the folder names and the chosen photos. Using a
  NAS account limited to the photos meant for the TV keeps that harmless.
- To use a folder on the Pi or a mounted network share instead of Synology
  Photos, set `source = "folder"` and `[photos.folder] path = "/home/pi/Pictures"`.
  All its photos are shown; the folder picker can narrow it down.

### Apps

Press **OK** on the dashboard to open the app row. Apps are defined in
`~/.config/pidash/config.toml`, one `[[apps]]` block each. After editing, run
`systemctl --user restart pidash`. An app is either a website or a program.

#### YouTube, Netflix and other websites

A website app opens fullscreen in its own browser profile, so its login is
remembered and kept apart from your normal browser. The default config ships
with these two:

```toml
[[apps]]
id = "youtube"
name = "YouTube"
url = "https://www.youtube.com/tv"
browser = "firefox"
adblock = true
user_agent = "Mozilla/5.0 (PS4; Leanback Shell) Gecko/20100101 Firefox/65.0 LeanbackShell/01.00.01.75 Sony PS4/ (PS4, , no, CH)"

[[apps]]
id = "netflix"
name = "Netflix"
url = "https://www.netflix.com/browse"
```

- **YouTube** runs in Firefox with **uBlock Origin**. The ad blocker is
  downloaded from Mozilla's add-on site into the YouTube profile the first time
  you start the app, so the Pi needs internet then. Firefox keeps it updated.
  The address is YouTube's TV interface, which is built for remote controls:
  the arrow keys and OK on your TV remote work. The `user_agent` line is what
  makes YouTube serve that interface. For the normal website instead, use
  `url = "https://www.youtube.com/"` and delete the `user_agent` line.
- **Netflix** runs in Chromium, because Raspberry Pi OS provides the DRM module
  Netflix requires (`libwidevinecdm0`) for Chromium. The installer adds it if
  it is missing.

#### Synology Photos

Add a block with the address you normally open Synology Photos with in a
browser:

```toml
[[apps]]
id = "synology-photos"
name = "Photos"
url = "http://192.168.1.20:5000/?launchApp=SYNO.Foto.AppInstance"
```

If your NAS uses HTTPS with a self-signed certificate, the browser will show a
warning page first. Use the plain `http://` address on your home network, or a
real certificate.

#### Things to know about website apps

- **Signing in needs a keyboard once.** Plug one in for the first login to
  Netflix or your NAS. After that the login is remembered.
- **Netflix and Synology Photos expect a mouse.** Their websites are not built
  for arrow-key navigation, so browsing them from the sofa works best with a
  wireless keyboard with touchpad. Playback keys such as Space work. YouTube's
  TV interface does not have this problem.
- **Getting back.** Tap **Back to dashboard** on the phone remote, or press
  Alt+F4 on a keyboard.
- **If uBlock Origin is not active** in the YouTube app, Firefox on your system
  did not accept the automatic install. The reliable alternative applies to
  every Firefox profile on the Pi. Create `/etc/firefox/policies/policies.json`:

  ```json
  {"policies": {"ExtensionSettings": {"uBlock0@raymondhill.net": {
    "installation_mode": "force_installed",
    "install_url": "https://addons.mozilla.org/firefox/downloads/latest/ublock-origin/latest.xpi"}}}}
  ```

#### Programs: Moonlight, Kodi, Steam Link and others

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

Each program has a `mode`:

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
always works.

#### Icons

You do not need to configure icons. PiDash picks one for each app in this order:

1. `icon` in the app's block, if you set one: a path to an SVG or PNG.
2. A built-in icon for YouTube, Netflix and Synology Photos. These are simple
   drawings in each service's colours, not the official artwork.
3. For programs, the icon the installed program ships with, found through its
   desktop menu entry. This covers Moonlight, Kodi, Steam Link and anything
   else that appears in the desktop menu, including Flatpak apps.
4. A globe for other websites, or the first letter of the name.

#### Troubleshooting

If an app does not start, the TV shows a message. Details are in the logs:

```sh
journalctl --user -u 'pidash-app-*' -e        # websites and desktop-mode programs
journalctl -u 'pidash-console@*' -e           # console-mode programs
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
internal/providers/  provider interface + runner, system/ stats, weather/ (Open-Meteo)
web/                 Vite + TypeScript frontend (dashboard + phone remote)
internal/kiosk/      start/stop the fullscreen browser (systemd user unit)
internal/apps/       app launcher (desktop and console modes)
internal/photos/     background slideshow: Synology Photos and folder sources, rendering, cache
deploy/              install.sh, uninstall.sh, systemd user units, kiosk and CEC scripts
```

## License

MIT — see [LICENSE](LICENSE).
