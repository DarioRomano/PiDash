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

**Automatic updates.** On the settings page under **Updates** you can see
which version runs and whether a newer one exists, install it with one
button, or switch on **Install new versions automatically**. PiDash then
looks for a release a few times a day and installs it between three and five
at night, never while an app is open. It runs the same updater as above.
Steps that need administrator rights only run where `sudo` works without a
password, as it does by default on Raspberry Pi OS.

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
| Move an app in the app row | Select it, press ▲, carry it with ◀ ▶, press **OK**. **Back** leaves it where it was |
| See how PiDash is doing | Choose **Status** in the app row. See [Status page](#status-page) |
| Pair a Bluetooth controller, headphones or a speaker | **Settings** in the app row, then **Bluetooth**. See [Bluetooth](#bluetooth) |
| Use a game controller | Plug it in or pair it. Switching it on also switches the TV on. See [Game controller](#game-controller) |
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

Weather, background photos, the calendar, night hours and apps are set up on
the settings page.

- **On the TV:** choose **Settings** in the app row, or press **S**. The page
  opens on the TV and works with the remote alone: arrows move, OK presses a
  button, opens a list, or opens an on-screen keyboard for text, and Back
  leaves, offering to save first.
- **On a phone or computer:** open `http://<pi-ip>:8080/settings`. Long
  entries such as an address are quicker to type there.

What it covers:

- **Weather:** your town and the units. **Check town** shows which place the
  weather service found before you save.
- **Background photos:** where photos come from, the connection to Synology
  Photos with a **Test connection** button, the picture quality, how long
  each photo stays, whether photos move, and whether two upright photos
  share the screen (see below). After saving, **Choose folders and albums**
  takes you to the picker.
- **Calendar:** links of the calendars whose next events the TV shows, with
  a **Check link** button.
- **Smart home:** the connection to Home Assistant and which of its devices
  appear as tiles in the app row.
- **Radio:** internet radio stations for the app row, found by name.
- **Screen:** the language, where the clock sits and how large it is, and
  what the dashboard shows when it starts.
- **Night:** hours in which the TV shows only a dim clock on black. Any button
  brings the dashboard back for a minute.
- **Apps:** the app row. Websites have a **size** next to them: 150% or 200%
  makes Netflix, Plex or Synology Photos readable on a 4K screen.
- **Apps, adding:** Add from a list of ready-made apps (YouTube, Netflix,
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

- **Lock:** a PIN without which settings can be looked at but not changed.
- **Backup:** one file with everything set up here, to save or load from a
  computer.
- **Updates:** which version runs, whether a newer one exists, a button to
  install it, and a switch to have that done automatically at night.

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

### Calendar

PiDash shows your next events at the bottom right, grouped under Today,
Tomorrow and the days after. It reads calendars through their link, which
every common calendar offers:

| Calendar | Where the link is |
|---|---|
| Google Calendar | Settings of the calendar, "Secret address in iCal format" |
| iCloud | Share the calendar as a public calendar and copy its link |
| Outlook.com | Settings, Calendar, Shared calendars, publish, the ICS link |
| Synology Calendar | Publish the calendar and copy its link |

Add the link on the settings page under **Calendar**; **Check link** tells you
how many events it finds. Several calendars can be combined. Repeating
events, single moved or cancelled dates and all-day events are understood.
The list is read again every 15 minutes.

Whoever has such a link can read the calendar. PiDash stores it on the Pi in a
file only your user can read, and the settings page never shows it again,
only the name of the server it leads to.

### Smart home

With Home Assistant, lights, plugs, covers, scenes, scripts and sensor values
can sit in the app row as tiles. **OK** switches a light or plug, opens or
closes a cover, or runs a scene or script; a tile for a sensor shows its
value.

1. In Home Assistant, open your profile, then **Security**, and create a
   **long-lived access token**.
2. On the settings page under **Smart home**, enter Home Assistant's address
   and the token, and press **Connect and list devices**.
3. Pick the devices you want from the list, add them, and save.

Locks and alarm panels are never offered: a remote lying on the sofa should
not open the door. Only the tiles you put on the app row can be switched
from PiDash. The token stays on the Pi and is never shown again.

### Language and layout

Under **Screen** on the settings page:

- **Language:** English, German, French or Italian, for the dashboard, the
  settings and the dates. Messages that come from the Pi itself, such as "the
  NAS cannot be reached", stay in English.
- **Clock position and size:** any of the four corners, in three sizes. The
  calendar takes the bottom corner the clock leaves free.
- **Shown at start:** the clock with date and calendar, the same plus weather
  and the photo's caption, or nothing but the photo. Up and Down on the
  remote switch between these three at any time.

The translations live in one table, `scripts/translations.py`; running it
writes the language files.

### Radio

Internet radio stations sit in the app row as tiles. **OK** on a station
plays it while the photos carry on; **OK** again stops it. The remote's play
button pauses and resumes. Opening an app, or the TV being switched off, stops
the radio.

Add stations on the settings page under **Radio**: type part of the name,
press **Search**, pick the station from the results and add it. The search
uses the community directory radio-browser.info. Stations can also be written
into `config.toml` with the address of their stream:

```toml
[[radio]]
name = "Radio Swiss Jazz"
url = "https://stream.srg-ssr.ch/m/rsj/mp3_128"
```

### Sleep timer

The **Sleep timer** tile in the app row switches the TV off after a while.
Each **OK** sets the next length (30, 60, 90 or 120 minutes) and then off
again; the tile shows the time left. A minute before the end the screen says
so. When the time is up, an app that is still playing is closed and the TV
is switched to standby. The tile only appears when the TV can be switched
over HDMI-CEC.

### Bluetooth

**Settings → Bluetooth** manages the Pi's Bluetooth devices with the TV
remote, so pairing a controller does not need a mouse and the desktop:

- The devices the Pi knows are listed with **Connect** / **Disconnect** and
  **Forget**.
- **Look for new devices** searches for a minute. Put the new device into
  pairing mode first (on most controllers: hold the pairing button until the
  light flashes quickly), then press **Pair** on its row.
- A paired device is trusted, so from then on it connects by itself when it is
  switched on. For a controller that also [switches the TV on](#game-controller).
- **Switch Bluetooth off** switches the Pi's Bluetooth off entirely.

Controllers, headphones, speakers, mice and most keyboards pair this way. A
device that insists on a code being typed is paired from the desktop's
Bluetooth menu instead. Headphones and speakers are connected here, but which
output the sound goes to is chosen in the desktop's volume menu.

Things nearby that have no name (other people's phones, beacons) are left out
of the list. With a [lock](#lock) set, changes ask for the PIN.

### Lock

With a PIN set under **Lock**, the settings can still be opened and looked
at, but saving, installing an update and backups ask for the PIN. On the TV
it is typed on a number pad with the remote, and the TV does not remember it.
If you forget it, remove the `lock` entry from
`~/.config/pidash/settings.json` on the Pi and restart PiDash.

### Backup

**Save a backup file** on the settings page, opened on a computer or phone,
downloads one file with everything set up there, the chosen photo folders
and the hidden photos. **Load a backup file** puts it back, on this Pi or
another one. The file contains the passwords and links stored on the Pi, so
keep it to yourself.

### Keeping the Pi cool

A picture that stands still costs the Pi nothing; anything that moves makes
it redraw the screen again and again, and at 4K that is what warms it. PiDash
therefore redraws no more often than the eye can tell: moving photos 15 times
a second, the drifting background 12 times, a reaction to a button 30 times.

It also stops drawing when nobody can see it:

- **While the TV is off.** PiDash hears the TV announce that it is switching
  off, and asks it once a minute whether it is on, both over HDMI-CEC. While
  it is off, photos stop changing and moving, the background stands still and
  the radio stops. A button press, or the TV reporting that it is on again,
  brings everything back. This needs a TV that tells the truth about its
  power state; most do.
- **While an app is open**, and during [night mode](#night-mode).

- **While the Pi is hot.** From 75 °C the background stands still and photos
  stop moving, though they keep changing; at 68 °C everything moves again.
  The seven degrees in between keep it from switching back and forth. The
  switch is under Settings, Screen; the temperature is `cool_above` under
  `[system]` in `config.toml`, and `0` switches it off. A Pi 5 slows itself
  down from 80 °C, so this steps in before that.

If the Pi still runs warm (check with `vcgencmd measure_temp`, or on the
[status page](#status-page)), these settings make the largest difference, most
effective first:

1. **Movement: None** for the photos (Settings, Background photos).
2. **Background without photos: Still picture** (Settings, Screen).
3. No video clips in the slideshow: decoding video is the heaviest work of all.
4. A longer time per photo: every new photo is prepared once on the Pi.

A case with a fan or a large heat sink is worth having for a Pi 5 that drives
a 4K screen all day.

### Status page

**Status** in the app row shows, on the TV, what would otherwise take a
keyboard and a terminal to find out:

- **This Pi**: the PiDash version and whether a newer one exists, temperature,
  memory, load, and the address to open on a phone.
- **TV and remote**: whether the TV answers over HDMI, its timetable, which
  remotes, keyboards and controllers PiDash is listening to, and the **last
  button** it heard, with the device it came from. Press any button while the
  page is open and it appears there within two seconds. A button that does
  not show up never reached the Pi: the TV is not passing it on.
- **Content**: where photos come from and how many there are, and when the
  weather and the calendar were last fetched, or why that failed.
- **Recent problems**: the latest warnings and errors since PiDash started,
  the same lines `journalctl --user -u pidash` would show.

Arrows do nothing on this page, so they can be tried out. **Back** or **OK**
closes it. It shows no passwords and no calendar addresses.

### Night mode

Between two times of day, set on the settings page under **Night**, the TV
shows only a dim clock on black: no photos, no weather, no calendar. Any
button brings the dashboard back for a minute; the press that wakes it does
nothing else. This is separate from the [TV power schedule](#tv-power-schedule),
which switches the TV itself off.

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
- **They can stand still.** Under **Screen** on the settings page,
  **Background without photos** can be set to a still picture. It keeps its
  look and still takes the colours of the selected app, but is painted once
  instead of continuously, which keeps the Pi cooler.
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

**More from this album.** While a photo is showing, the **More from this
album** tile in the app row makes the slideshow stay with that folder or
album: the next up to 30 pictures come from it, in the order they were taken.
Then the usual mix returns.

**Video clips.** With **Also show video clips, without sound**
(`videos = true`), clips from the chosen folders and albums take their turn in
the slideshow. A clip plays once, for at most 45 seconds, always muted. Clips
over 90 MB are left out, and so are clips the Pi's browser cannot play, which
includes many phone videos in the HEVC format: the first failed attempt is
remembered and the clip is not tried again. Live photos are shown as stills.

**On this day.** With **Start with photos taken on this day in earlier
years** (`on_this_day = true`), each round of the slideshow begins with the
photos from today's date in past years, when there are any.

**Captions.** Press Up on the dashboard for the caption: the folder or album,
the place where the photo was taken when the NAS knows it, and the date.

**Hiding a photo.** While a photo is showing, the app row has a **Hide this
photo** tile (for two side by side, one tile each). A hidden photo never comes
up again; **Show them again** on the settings page brings all of them back.
Nothing is deleted or changed on the NAS.

**Two upright photos at a time.** With **Show two upright photos side by
side** on the settings page (`pair_portraits = true` under `[photos]`), a
portrait photo no longer stands alone with empty space beside it. It shares
the screen with a second one.

- Partners are neighbours: photos from the same folder or album, in the order
  they were taken. So a pair usually comes from the same occasion, and the
  same two appear together every time.
- A portrait without a partner (an odd one out in its folder) is shown alone,
  as before. Landscape photos are not affected.
- To know which photos are upright, PiDash looks at a small preview of each
  photo once, in the background. On a large library that takes a few minutes
  the first time; pairs start to appear while it is still going.
- A photo close to the shape of half a screen (3:4, 2:3) fills its half and
  loses a little at the top and bottom. A narrower one (9:16 from a phone) is
  shown whole. With **Gentle** movement both are shown whole.

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

To change the order, select an app in the row and press ▲: the tile lifts.
◀ ▶ carry it, **OK** puts it down and saves the order, **Back** returns it to
where it was. With a [lock](#lock) set, apps are moved on the settings page
instead.

The easiest way to add apps is the [settings page](#settings). They can also
be defined in `~/.config/pidash/config.toml`, one `[[apps]]` block each. After
editing, run `systemctl --user restart pidash`. An app is either a website or
a program.

#### Spotify

**Spotify** in the list of ready-made apps opens the Spotify web player in
Chromium. It is made for a mouse: with only the TV remote, OK and the play
button work on whatever is selected, but moving around the page needs a mouse
or the phone's touchpad. Setting its size to 150% or more helps on a 4K
screen.

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

#### Emulators

The settings page offers these emulators by name under **Add an app**. All of
them run well on a Pi 5, except that Dolphin only manages the lighter games
at full speed:

| Emulator | Plays | Install on the Pi |
|----------|-------|-------------------|
| RetroArch | Many classic consoles, from the NES and Mega Drive to the first PlayStation | `sudo apt install retroarch` |
| PPSSPP | PlayStation Portable | `flatpak install flathub org.ppsspp.PPSSPP` |
| Mupen64Plus | Nintendo 64 | `sudo apt install mupen64plus-qt` |
| Dolphin | GameCube and Wii | `flatpak install flathub org.DolphinEmu.dolphin-emu` |
| ScummVM | Point-and-click adventures | `sudo apt install scummvm` |
| DOSBox | DOS games | `sudo apt install dosbox` |
| MAME | Arcade machines | `sudo apt install mame` |

One that is not installed yet is listed with its install command; after
installing it, open the settings page again and add it. PiDash finds an
emulator through its entry in the Pi's application menu, whether it came from
`apt` or from Flatpak, and starts RetroArch, PPSSPP, ScummVM and DOSBox
filling the screen. Any other emulator in the menu can be added with **A
program installed on the Pi…**.

PiDash starts the emulator; the games are yours to supply, and each emulator
has its own way of being told where they are. RetroArch also needs a "core"
per console: `apt search libretro` lists the ones Raspberry Pi OS has.

Inside an emulator the [game controller](#game-controller) belongs to the
game. **Holding its logo button for two seconds** returns to PiDash, as does
holding Back on the TV remote.

#### The TV remote inside apps

A TV remote's **OK** and **Back** buttons are not keyboard keys, so apps such
as YouTube and Netflix would ignore them. PiDash reads the remote itself and
fills the gap:

- **OK** is passed on to the app as Enter, and **Back** as Escape. The number
  buttons are passed on as numbers. Arrows already work by themselves.
- **Holding Back for two seconds** closes the app and returns to the dashboard.
  Holding **Esc** on a keyboard does the same.

Buttons go to whichever window has the keyboard. An app's window gets it when
it opens, but can lose it, for example when the TV is switched off and on
again; the app then looks as if the remote had stopped working. PiDash checks
on every button press that the open app's window has the keyboard and gives
it back if not. This uses the small tool `wlrctl`, which the installer adds,
and works for websites and for programs added from the list of installed
programs. The [status page](#status-page) says whether it is available.

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
work without any setup. PiDash reads all of them from the remote directly,
rather than relying on the browser to receive them as keys: OK and Back are
not keyboard keys at all, and the arrows only reach the browser while its
window has the keyboard, which it can lose.

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

### Game controller

A game controller, wired or over Bluetooth, works as a remote without any
setup:

| On the controller | Does |
|-------------------|------|
| Left stick or direction pad | Arrows |
| Bottom button (A on an Xbox pad, ✕ on a PlayStation pad) | OK |
| Right button (B, ○) | Back |
| Start / Menu / Options | Opens the app row |
| Shoulder buttons | Previous and next photo |
| Logo button | Back to the standard view. **Held for two seconds inside an app: closes it** |

Buttons are named by where they sit, so on a Nintendo pad the bottom button
is B. Inside an app PiDash leaves the controller to the app, apart from the
logo button being held. To change what a button does, name it under
`[input.remote_keys]`, for example `BTN_NORTH = "weather"`; `evtest` shows the
names.

Pair a Bluetooth controller once under [Settings → Bluetooth](#bluetooth). It
shows up under **Listening to** on the [status page](#status-page) when PiDash
has found it.

**Switching the controller on switches the TV on**, the way a games console
does. A Bluetooth controller switches itself off when left alone; when it is
switched on again and the TV is in standby, PiDash switches the TV on and to
the Pi's input over HDMI-CEC. Pressing the logo button of a controller that
is already connected, a wired one for example, does the same. A TV that is on
is left alone, so a controller never pulls the TV away from another input,
and no other button or stick wakes it, so a controller lying on the sofa
does not either. This needs a TV that says over HDMI whether it is on, which
`~/.local/share/pidash/pidash -cec status` shows. To switch it off:

```toml
[tv]
wake_on_controller = false
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
repairs both where it can. The [status page](#status-page) on the TV shows the
same, and the last button PiDash heard.

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

**Tests.** `make test` runs the Go tests. `make e2e` starts PiDash with sample
data and drives the real pages in a browser with the buttons of a TV remote:
the dashboard, the app row, the settings and their on-screen keyboard. Every
button press is tried the three ways it can arrive on a Pi: read from the
remote by the backend, as a key in the browser, and both at once, which must
count as one press. It needs `make build` first and Playwright's Chromium
(`cd web && npm install --no-save playwright && npx playwright install
chromium`). Both run on every push and before every release.

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
internal/bluetooth/  Bluetooth devices: list, search, pair, connect, forget
deploy/              the installer run on the Pi, uninstaller, systemd units, kiosk launcher
install.sh           one-line installer and updater: downloads a release and runs deploy/install.sh
scripts/dist.sh      builds the release packages
.github/workflows/   CI on every push, and the release build on a version tag
```

## License

MIT — see [LICENSE](LICENSE).
