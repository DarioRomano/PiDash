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
left it. It installs into your home directory, plus one small system rule for
the TV remote, and can be removed again.

Requirements: a Raspberry Pi 5 running Raspberry Pi OS with desktop (Bookworm or
newer), connected to the TV and to the internet.

Open a terminal on the Pi, or connect with SSH, and run this as your normal
user:

```sh
curl -fsSL https://raw.githubusercontent.com/DarioRomano/PiDash/main/install.sh | bash
```

That downloads the latest release for your Pi, checks it against the release's
checksums, and installs it. Nothing is compiled on the Pi. When it finishes it
prints how to start the dashboard and the address of the phone remote.

Options go after `bash -s --`:

```sh
curl -fsSL https://raw.githubusercontent.com/DarioRomano/PiDash/main/install.sh | bash -s -- --autostart --phone-input
```

| Option | What it adds |
|--------|--------------|
| `--autostart` | The dashboard opens by itself whenever the desktop starts. Without it, the Pi boots to your normal desktop and the dashboard appears when you ask for it |
| `--console-apps` | Lets apps such as Moonlight and Kodi run outside the desktop, see [Apps](#apps) |
| `--phone-input` | Lets the phone act as mouse and keyboard, see [Phone as mouse and keyboard](#phone-as-mouse-and-keyboard) |

If you prefer to look before you run: download `install.sh` from this
repository, read it, and run `bash install.sh`. A specific version can be
installed with `PIDASH_VERSION=v0.2.0` in front of `bash`.

### Updating

```sh
bash ~/.local/share/pidash/update.sh
```

This fetches the latest release and installs it over the old one. Your
settings, chosen photo folders, app logins and the options you installed with
are kept. The dashboard on the TV reloads by itself when the new version
starts. The phone remote shows the installed version at the bottom.

### Uninstalling

```sh
bash ~/.local/share/pidash/uninstall.sh            # keeps your settings
bash ~/.local/share/pidash/uninstall.sh --purge    # removes them too
```

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
| One system rule, so the TV remote's buttons reach apps | `/etc/udev/rules.d/70-pidash-uinput.rules` |
| What you save on the settings page | `~/.config/pidash/settings.json` |
| Only with `--phone-input` | `wtype`/`xdotool` for typing, see [Phone as mouse and keyboard](#phone-as-mouse-and-keyboard) |

## Using it

| To… | Do this |
|-----|---------|
| Show the dashboard on the TV | Open **PiDash** from the desktop menu, tap **Show on TV** on the phone remote, or run `systemctl --user start pidash-kiosk` |
| See every control | Press **H** or **F1**, or click **Help** on the control bar |
| See the full forecast | Press **OK**, then **OK** again on the **Weather** tile. **Back** closes it |
| Choose the background photos | Open `http://<pi-ip>:8080/photos`, or tap **Background photos** on the phone remote |
| Flip through photos | ◀ ▶ on the dashboard. Play/pause holds the current photo. ▲ shows its folder and date |
| Change settings | Choose **Settings** in the app row, press **S**, or open `http://<pi-ip>:8080/settings` on your phone. See [Settings](#settings) |
| Launch an app | Press **OK** on the dashboard to open the app row, choose with ◀ ▶ and press OK. Or tap the app on the phone remote |
| Get back from an app | **Hold Back for 2 seconds** on the TV remote, hold **Esc** on a keyboard, or tap **Back to dashboard** on the phone remote |
| Use the phone as mouse and keyboard | Switch it on once (see [Phone as mouse and keyboard](#phone-as-mouse-and-keyboard)), then use the touchpad on the phone remote |
| Switch the TV on or off | **TV on** / **TV off** on the phone remote, or a daily schedule (see [TV power schedule](#tv-power-schedule)) |
| Go back to the desktop | **Hold Back for 2 seconds** on the TV remote, press **Alt+F4** or **F10** on a keyboard, or tap **Exit to desktop** on the phone remote |
| Use the phone remote | Open `http://<pi-ip>:8080/remote` on any device on your network |
| Change settings | Edit `~/.config/pidash/config.toml`, then `systemctl --user restart pidash` |
| Find out why something does not work | `bash ~/.local/share/pidash/doctor.sh`, see [Troubleshooting](#troubleshooting) |
| Uninstall | `bash ~/.local/share/pidash/uninstall.sh` (add `--purge` to delete settings too) |

The screen stays awake while the dashboard is showing; your normal screen
blanking applies again once you exit. The layout scales to whatever resolution
the desktop uses. For the smoothest result on a 4K TV, set the desktop to
1920x1080 in *Preferences → Screen Configuration*.

### Keyboard and mouse

The dashboard works with a TV remote, a keyboard, a mouse, an air mouse, a touch
screen, or the phone. Press **H** or **F1** at any time for an overview on the
TV; it lists the keys as they are currently set.

**Keyboard**

| Key | Does |
|-----|------|
| Enter, Space | Open the app row, choose a tile |
| Esc, Backspace | Close, go back. Hold 2 seconds to exit to the desktop |
| ← → | Previous / next photo, or move along the app row |
| Tab, Shift+Tab | Move along the app row |
| ↑ ↓ | Show more / less information |
| A | App row |
| W | Weather details |
| P | Hold or resume the slideshow |
| 1 – 9 | Choose a tile in the app row |
| H, F1, / | Show all controls |
| F10 | Return to the desktop |

Shortcuts of the desktop itself, such as Alt+F4 and anything with Ctrl, are
left alone. To add or change keys, see `[input.keys]` in the settings: your
entries are added to the built-in ones, and `"none"` switches one off.

**Mouse**

The pointer is hidden until the mouse moves. Then a control bar appears at the
top with previous, pause and next for the photos, and Apps, Weather, Help and
Desktop. Both fade away again after a few seconds.

- **Click** anywhere to open or close the app row; click a tile to start it.
- **Right-click** goes back.
- **The wheel** steps through photos, or along the app row while it is open.
- **Click the weather** in the corner for the details; click again to close.

With the [phone as mouse](#phone-as-mouse-and-keyboard), the same works from the
sofa.

### Settings

Weather, background photos and apps are set up on the settings page. It needs
typing, so it is made for your phone or computer:

- **On your phone or computer:** open `http://<pi-ip>:8080/settings`. It is
  also linked at the bottom of the phone remote.
- **On the TV:** choose **Settings** in the app row, or press **S**. The TV
  shows the address to open. With a mouse and keyboard on the Pi, pressing OK
  opens the page right there.

What it covers:

- **Weather:** your town and the units. **Check town** shows which place the
  weather service found before you save.
- **Background photos:** where photos come from, the connection to Synology
  Photos with a **Test connection** button, the picture quality, how long
  each photo stays, and whether photos move (see below). After saving, **Choose folders and albums** takes you to
  the picker.
- **Apps:** the app row. Add from a list of ready-made apps (YouTube, Netflix,
  Synology Photos, Plex, Moonlight, Kodi, Steam Link), any program installed
  on the Pi, or any website by address. Reorder them, and remove them.

**Programs installed on the Pi** are the ones in the Pi's application menu.
Choose **A program installed on the Pi…** under Add, pick it, and save; a
program installed later appears in that list the next time you open the page.
It gets its own icon and starts the way it does from the menu, as a window on
top of the dashboard. A few things to know:

- Most desktop programs are made for a mouse and keyboard. The TV remote sends
  them arrows, Enter and Esc; for the rest use a mouse or the phone's touchpad.
- Holding **Back** closes the program and returns to the dashboard, as for
  every app.
- A program that only hands over to a copy that is already open (some file
  managers and browsers do) is reported as "closed right after starting",
  although its window opens. PiDash then cannot close it for you.
- Text programs that need a terminal window are not offered.
- If you uninstall a program, its tile disappears; the other settings stay.

The settings page can be opened by anyone on your home network, so it only
lets them pick a program by name. The command always comes from the menu entry
on the Pi itself. A program that needs its own command or options still goes
into `config.toml`.

Press **Save**. PiDash restarts itself to apply the changes, which takes a few
seconds, and the TV reloads.

How it fits with `config.toml`:

- What you save is stored in `~/.config/pidash/settings.json`, next to
  `config.toml`, and takes precedence over the same sections there. The page
  never rewrites `config.toml`. Delete `settings.json` to return to exactly
  what `config.toml` says.
- The page can add programs only from its ready-made list. A program with your
  own command still has to be written into `config.toml`; it then appears on
  the page to reorder or remove. This is deliberate: the page is reachable from
  your home network and must not be a way to run commands on the Pi.
- The NAS password is never sent back to the page, and `settings.json` is
  readable by your user only.
- If a PIN is set under `[remote]`, saving and the connection test ask for it.
- Everything else, such as the TV schedule, keys and the phone's mouse mode,
  stays in `config.toml`.

The sections below describe the same settings as they appear in `config.toml`.

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

### Background without photos

While no photo is on screen (no photo source, nothing chosen yet, or the NAS is
away), the background shows broad ribbons of colour that wind slowly across
the screen.

- **They react to you.** Left and right send them flowing that way, up and
  down make them sway, OK sends a swell through them, and opening the app row
  lifts them along with the clock. With a mouse they lean towards the pointer.
  A few seconds later they are back to their slow drift.
- **They take the colours of the selected app.** With the app row open, the
  ribbons turn to the colours of the selected icon: red for YouTube, the
  icon's own mix for anything else.
- **They are sharp without costing much.** The ribbons are drawn at half the
  screen's resolution: 1080p on a 4K screen, which keeps their edges crisp at
  a quarter of the work. If the Pi cannot keep up, the picture steps down to
  a coarser size by itself and looks softer. Drawing stops while a photo or
  an app covers it. If the system is set to reduce motion, the ribbons stand
  still and only change colour.

### Background photos from Synology Photos

PiDash can show a slideshow behind the clock, taken from folders and albums you
choose in Synology Photos.

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
   the phone remote as **Background photos**. Browse your Personal Space,
   Shared Space and Albums, tick what you want, and tap **Use these photos**.
   "Include the folders inside them" decides whether subfolders count. The TV
   switches to the new photos within a few seconds. Until you have chosen, the
   TV shows a message with this address.

You can change the choice at any time on the same page. New photos added to the
chosen folders and albums show up by themselves within six hours, or
immediately when you save the selection again.

**Albums** lists the account's own albums and the albums other people shared
with it. This makes a neat setup: collect the photos for the TV in one album in
your own account, share that album with the PiDash account, and tick it here.
The PiDash account then sees nothing else. If the share only allows viewing,
not downloading, PiDash uses the NAS's previews (1280 pixels on the long side)
instead of the full files.

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

**Moving photos.** Under **Movement** on the settings page, or with `motion`
under `[photos]`, each photo can drift slowly while it is shown: a zoom in, a
zoom out, or a glide to one side, a different one each time.

| Setting | What you see |
|---|---|
| None (`off`) | Photos stand still. This is the default |
| Gentle (`whole`) | The whole photo stays in view the entire time; nothing is ever cut off, not even for landscape photos. To make room for the movement, photos are shown a little smaller (at most 89% of the screen at the widest view), on a blurred copy of themselves |
| Full screen (`fill`) | Photos fill the screen as before and move more freely. While they move, up to about a tenth along the edges can be out of view |

The movement is spread over the time a photo is shown, so a longer time per
photo means slower movement. After changing the setting, photos are prepared
again as they come up. If the system is set to reduce motion, photos stand
still.

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

Press **OK** on the dashboard to open the app row. Left alone for 30 seconds,
the row closes again and the TV returns to the clock; `app_row_timeout` under
`[display]` changes that, and `0` keeps it open.

The easiest way to add apps is the [settings page](#settings). They can also
be defined in `~/.config/pidash/config.toml`, one `[[apps]]` block each. After
editing, run `systemctl --user restart pidash`. An app is either a website or
a program.

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

- **Signing in needs typing once.** Use the phone as keyboard (see
  [Phone as mouse and keyboard](#phone-as-mouse-and-keyboard)) or plug in a
  keyboard for the first login to Netflix or your NAS. The login is remembered.
- **Netflix and Synology Photos expect a mouse.** Their websites are not built
  for arrow-key navigation. The phone's touchpad covers this. YouTube's TV
  interface works with the TV remote alone.
- **Getting back.** Hold Back on the TV remote for two seconds, tap **Back to
  dashboard** on the phone remote, or press Alt+F4 on a keyboard.
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

To leave a console app, hold Back on the TV remote for two seconds, use the
app's own quit shortcut, or tap **Back to dashboard** on the phone remote.

#### The TV remote inside apps

A TV remote's **OK** and **Back** buttons are not keyboard keys, so apps such
as YouTube and Netflix would ignore them. PiDash reads the remote itself and
fills the gap:

- **OK** is passed on to the app as Enter, and **Back** as Escape. The number
  buttons are passed on as numbers. Arrows already work by themselves.
- **Holding Back for two seconds** closes the app and returns to the dashboard.
  Holding **Esc** on a keyboard does the same.

This works in every app, including ones that run outside the desktop. It needs
your user to be in the `input` group, which is the case for the first user on
Raspberry Pi OS, and the one system rule the installer adds. The installer and
`doctor.sh` tell you if either is missing.

Some TVs do not report that a button is being held, or report it as a quick
series of presses. PiDash copes with the second kind. If holding Back still
does nothing, pick a button you do not otherwise need, such as a colour key,
and make it act at once:

```toml
[input]
app_exit_keys = ["KEY_RED"]
app_exit_hold = "0s"
```

`sudo apt install evtest && sudo evtest` shows the name of any button you press.

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
systemctl --user status 'pidash-app-*' -l --no-pager   # websites and desktop-mode programs
journalctl -u 'pidash-console@*' -e                    # console-mode programs
```

### Phone as mouse and keyboard

The phone remote can act as a touchpad and keyboard for whatever is on the TV.
This is what makes Netflix and Synology Photos usable from the sofa, and lets
you sign in without plugging in a keyboard.

**Read this first.** With this switched on, every device on your home network
can move the mouse and type on the Pi's desktop, which amounts to full control
of that desktop. It is off by default. Leave it off on a network you share with
people you do not trust, or set a PIN.

To switch it on, set this in `~/.config/pidash/config.toml` and run
`systemctl --user restart pidash`:

```toml
[remote]
mouse_keyboard = true
pin = "4321"        # optional; the phone asks once and remembers it
```

For typing text from the phone, install the small typing tool once:

```sh
bash ~/.local/share/pidash/update.sh --phone-input
```

On the phone remote you then get:

- **A touchpad.** Drag to move, tap to click, tap with two fingers for a right
  click, drag with two fingers to scroll. **Hold** keeps the button pressed for
  dragging sliders.
- **Keys:** Esc, Tab, Backspace and Enter.
- **Typing.** Write in the text field and tap **Type**; the text is entered
  into whatever has the focus on the TV, in the right characters whatever
  keyboard layout the Pi uses.
- **The direction pad works inside apps.** While an app is open, the arrows, OK
  and Back on the phone press real keys in that app instead of steering the
  dashboard.

After five wrong PINs the phone's mouse and keyboard are locked for a minute.
The PIN protects only the mouse and keyboard; the rest of the phone remote
stays open to your network as before.

### TV power schedule

PiDash can switch the TV off at night and on in the morning over HDMI-CEC:

```toml
[tv]
off = "23:30"
on = "06:30"
```

Either time can be left out. With only `off`, the TV is never switched on by
itself.

- The TV is **not switched off while an app is open**, so a film is never cut
  short. It goes off once you are back on the dashboard.
- If you switch the TV on by hand during the night, PiDash notices and carries
  on normally. It does not switch the TV off a second time.
- While the TV is off, the dashboard stops loading photos from your NAS.
- The phone remote has **TV on** and **TV off** buttons for doing the same by
  hand. **TV on** also switches the TV to the Pi's input.

To try what your TV supports, on the Pi:

```sh
~/.local/share/pidash/pidash -cec status    # asks the TV whether it is on
~/.local/share/pidash/pidash -cec off
~/.local/share/pidash/pidash -cec on
```

### The TV remote on the dashboard

Enable CEC on the TV (Samsung "Anynet+", LG "Simplink", Sony "Bravia Sync",
Philips "EasyLink", etc.). When the dashboard starts it registers the Pi with
the TV and asks it to switch to the Pi's input; from then on the TV forwards
remote key presses to the Pi.

A TV only sends the remote's keys to the device it believes it is showing. So
whenever you switch the TV to the Pi's input, from another input or from one
of the TV's own apps, the TV asks the Pi to confirm, and PiDash answers. It
also registers the Pi if the TV was off when the dashboard started. PiDash
never pulls the TV to the Pi's input by itself; that only happens when the
dashboard starts and with **TV on**.

The arrows, OK, Back, the number buttons, play/pause, menu and channel up/down
work without any setup. OK and Back are not keyboard keys, so PiDash reads them
from the remote directly rather than through the browser.

To give other buttons a job, for example the colour keys, add them by their
Linux key name:

```toml
[input.remote_keys]
KEY_RED = "weather"
KEY_BLUE = "settings"
```

`sudo apt install evtest && sudo evtest` shows the name of any button you
press. With `display.debug_keys = true` (the default) the dashboard also shows
in the top-right corner what each press was understood as.

Check CEC from a terminal on the Pi:

```sh
~/.local/share/pidash/pidash -cec status   # should print "TV power: on"
cec-ctl -d /dev/cec0 -S                    # show the TV in the topology
```

### Debugging the browser on the TV

The dashboard's Chromium listens for DevTools on port 9222, on the Pi only. From
your laptop, forward the port and inspect it:

```sh
ssh -L 9222:localhost:9222 pi@raspberrypi.local
```

Then open `chrome://inspect` in Chrome on the laptop and pick the PiDash page to
profile paint and layout on the real device.

## Troubleshooting

**Nothing happens when I start PiDash.** The menu entry shows an error window
if the dashboard cannot start. For the full picture, run:

```sh
bash ~/.local/share/pidash/doctor.sh
```

It only reads, changes nothing, and does not print passwords. It reports the
PiDash version, your desktop session, the browsers it found, whether the
settings file is valid, whether the backend answers and on which port, which
programs occupy the nearby ports, the state and recent log of both services,
and whether the TV answers over HDMI-CEC. Paste its output when asking for help.

**The TV remote stops working after I used another input or one of the TV's
apps.** PiDash answers the TV when it comes back to the Pi, which is what TVs
wait for before they forward the remote again. If yours still does not, press
**TV on** on the phone remote, which announces the Pi to the TV again. Then
please report what the TV says to the Pi. Run this in a terminal on the Pi,
switch the TV away and back, and copy what it printed:

```sh
systemctl --user stop pidash
~/.local/share/pidash/pidash -v 2>&1 | grep -i cec    # Ctrl+C to end
systemctl --user start pidash
```

**The phone remote is not at port 8080.** If another program on the Pi already
uses port 8080, PiDash takes the next free port instead of failing. It says so
once, on the TV and at the end of the installer, and then remembers that port
and uses it from then on. To pick the port yourself, set it in `config.toml`
and restart:

```toml
[server]
listen = ":8686"
```

**A message about the settings file appears on the TV.** A mistake in
`~/.config/pidash/config.toml` no longer stops PiDash: it runs with its built-in
settings and shows what is wrong. To check the file without restarting:

```sh
~/.local/share/pidash/pidash -config ~/.config/pidash/config.toml -check-config
```

**The TV remote's OK or Back does nothing.** Run `doctor.sh` and look at the
"TV remote and keyboards" part. It shows whether PiDash can read the input
devices, which needs your user in the `input` group, and whether the virtual
keyboard is allowed, which the installer sets up. Running the update again
repairs both where it can.

**Looking at the services directly:**

```sh
systemctl --user status pidash pidash-kiosk -l --no-pager
```

## Development

You need Go 1.22 or newer and Node 18 or newer.

```sh
make dev            # Go backend on :8080 with sample data + Vite dev server on :5173
make build          # native binary in bin/pidash, with the frontend embedded
make lint test      # static checks (Go, TypeScript, shell) and tests
```

`make dev` uses generated sample photos, a made-up forecast and pretend app
launches, so everything can be worked on without a Pi, a NAS or a TV. Arrow
keys, Enter, Escape and the mouse work as on the TV; hold Escape for two
seconds to trigger "exit to desktop".

To try a change on your own Pi straight from the checkout, without making a
release:

```sh
make install PI_HOST=pi@raspberrypi.local     # first time
make deploy  PI_HOST=pi@raspberrypi.local     # afterwards: rebuild, copy, restart
```

Add `INSTALL_FLAGS="--autostart --console-apps --phone-input"` to `make install`
as needed. On the Pi itself, `make install-local` does the same from a checkout
there.

## Releasing

There are two ways to publish a version. Both end in the same Release workflow.

**On GitHub, with a button.** Open the repository's **Actions** tab, choose
**Release**, click **Run workflow**, enter the version, for example `v0.2.0`,
and run it on the main branch. The workflow creates the tag itself. It refuses
a version that already exists.

**From a checkout**, on the main branch with everything committed:

```sh
make release VERSION=v0.2.0
```

This runs the checks, creates the tag and pushes it, which starts the workflow.

Either way, GitHub Actions builds the packages for 64-bit and 32-bit Raspberry
Pi OS and for ordinary PCs, and publishes them as a GitHub release with
generated notes and checksums. From that moment the one-line installer installs
the new version, and `update.sh` on every Pi picks it up.

- Versions follow `vMAJOR.MINOR.PATCH`. A suffix such as `v0.3.0-rc.1` makes a
  pre-release, which the installer skips unless asked for by name.
- `make dist` builds the same packages locally into `dist/`, to inspect them or
  to install from a file: unpack one and run `bash deploy/install.sh` inside it.
- Every push and pull request runs the same checks and a package build in
  GitHub Actions (`.github/workflows/ci.yml`).
- The download address can be redirected with `PIDASH_BASE_URL`, for a mirror
  or for testing a release before it is published.

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
internal/input/      TV remote handling (key map, passing buttons on to apps, hold to exit); phone as mouse and keyboard
internal/settings/   what the settings page saves, laid over config.toml
internal/cec/        HDMI-CEC through cec-ctl: register with the TV, power on/off
internal/tv/         daily TV power schedule
deploy/              the installer run on the Pi, uninstaller, systemd units, kiosk launcher
install.sh           one-line installer and updater: downloads a release and runs deploy/install.sh
scripts/dist.sh      builds the release packages
.github/workflows/   CI on every push, and the release build on a version tag
```

## License

MIT — see [LICENSE](LICENSE).
