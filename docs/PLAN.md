# PiDash — Plan

A fast, good-looking dashboard for a TV, driven by a Raspberry Pi 5 and controlled
with the TV's own remote (HDMI-CEC) or a phone.

## Decisions

| Area        | Choice                                                  | Why                                                                 |
|-------------|---------------------------------------------------------|---------------------------------------------------------------------|
| Hardware    | Raspberry Pi 5, HDMI0 to the TV                         | Enough GPU/CPU for smooth Chromium at 1080p; HDMI0 carries CEC.     |
| OS          | The existing Raspberry Pi OS desktop, untouched         | No reflashing; the Pi stays a normal desktop computer.              |
| Display     | Chromium `--kiosk` as an app in the desktop session     | Fullscreen over the desktop; exit returns to it. Own browser profile.|
| Frontend    | Vite + vanilla TypeScript, fixed 1920×1080 layout       | Small, no framework overhead, easy to keep off the hot path.        |
| Backend     | Go, single static binary, frontend embedded via `embed` | One file to deploy, low memory, cross-compiles from any laptop.     |
| Transport   | Server-Sent Events for push, HTTP POST for input        | Stdlib only, `EventSource` reconnects by itself.                    |
| Config      | One `config.toml`                                       | Human-editable on the Pi over SSH.                                  |
| Remote      | HDMI-CEC (primary) + phone web remote (fallback)        | No extra hardware; the phone covers text entry and TVs without CEC. |
| First panels| Clock & date, weather (Open-Meteo), photo slideshow     | No API keys required.                                               |
| Photos      | Synology Photos API or a local folder; folders chosen in a phone-friendly picker | The photos already live on the NAS; choosing folders should not need a config file. |
| Apps        | Launcher with two modes: desktop, and console (own VT)  | Makes the dashboard the TV's home screen; console mode gives Moonlight/Kodi direct display access. |

## Experience

The default view is an **ambient screen**: a full-bleed photo slideshow with a
soft gradient scrim and the clock, date and current weather overlaid. It should
look like a piece of art on the wall, not a control panel.

Remote actions (the only input vocabulary the app understands):

| Action      | TV remote key     | Behaviour                                                  |
|-------------|-------------------|------------------------------------------------------------|
| `left/right`| ◀ ▶               | Previous / next photo                                      |
| `up/down`   | ▲ ▼               | Cycle overlay density: photo only → compact → detailed     |
| `select`    | OK                | Open the panel rail (clock, weather detail, photos, system) |
| `back`      | Back / Exit       | Close rail, return to ambient. **Hold 2 s: exit to desktop** |
| `exit`      | F10 (mappable)    | Close the dashboard and return to the desktop              |
| `0-9`       | Number keys       | Jump straight to a panel                                   |
| `play/pause`| ⏯                 | Pause / resume slideshow                                   |

Every panel is a self-contained widget: a **provider** on the backend that
produces a JSON state, and a **view** on the frontend that renders it. Adding
a widget later (calendar, RSS, Home Assistant) means adding one of each.

## Performance rules

These keep the Pi 5 GPU and CPU idle most of the time.

- **Designed at 1080p, scaled to the desktop resolution.** The layout is a fixed
  1920×1080 stage with one uniform `transform: scale()`. Recommend setting the
  desktop to 1080p on a 4K TV: cheaper to render, and the TV upscales for free.
- **Animate only `transform` and `opacity`.** No `backdrop-filter`, `filter: blur`,
  or large `box-shadow`. Scrims are static gradients.
- **Photo crossfade** = two stacked `<img>` elements, opacity transition, next
  image preloaded. Photos are **pre-resized to 1920×1080 by the backend** and
  cached on disk, so the browser never decodes a 20 MP JPEG.
- **No render loop.** The clock updates one text node once a second; everything
  else updates only when an SSE event arrives.
- **No third-party requests from the browser.** Fonts are self-hosted; all data
  comes from the local backend.
- **Pixel shift**: nudge the whole layer by a few pixels every few minutes to
  protect OLED/plasma panels.
- **Stale-tolerant**: the backend caches the last good result for every provider.
  When the internet drops the dashboard keeps showing data with a small "stale"
  marker instead of an error.
- Chromium flags live in `deploy/kiosk.sh` (GPU rasterisation, zero-copy, no
  pinch/overscroll, separate `--user-data-dir`, DevTools on localhost:9222 for
  profiling on the real device through an SSH tunnel).
- **Wake lock**: the page holds a screen wake lock, so the desktop's screen
  blanking is inhibited only while the dashboard is showing.

## Remote control details

### HDMI-CEC (TV remote)

On Pi 5 with the KMS driver the kernel exposes `/dev/cec0`. Steps:

1. Install `v4l-utils` for `cec-ctl`.
2. When the kiosk starts, `deploy/cec-setup.sh` runs
   `cec-ctl --playback --osd-name PiDash` on the adapter that has a TV attached,
   wakes the TV and claims active source, so the TV switches to the Pi and
   forwards remote keys.
3. The kernel's CEC RC passthrough turns keys into normal Linux input events
   (`KEY_UP`, `KEY_OK`, …). The desktop compositor passes them to the focused
   fullscreen Chromium window as keyboard events.
4. Frontend maps `keydown` codes to the action vocabulary above. The mapping is
   in `config.toml` because brands disagree on Back/Exit and colour keys.
5. Bonus: the backend can send `cec-ctl --to 0 --standby` / `--image-view-on`
   to turn the TV off at night and on in the morning on a schedule.

### Phone remote

The backend serves `/remote`, a touch page with a d-pad, numbers, play/pause
and a text field. Buttons `POST /api/input {"action":"left"}`; the backend
broadcasts the action on the SSE stream and the dashboard handles it exactly
like a key press. Bound to the LAN only.

## Architecture

```
┌──────────── Raspberry Pi 5, normal desktop session ────────────┐
│ systemd --user                                                 │
│  ├─ pidash.service         Go binary :8080 (always on, tiny)   │
│  └─ pidash-kiosk.service   chromium --kiosk  ──────────────────┼──► HDMI ──► TV
│        started on demand: menu entry / phone / autostart       │       ▲ CEC keys
│        stopped: hold Back / Alt+F4 / phone  → back to desktop  │◄──────┘
│                                                                │
│  Go binary                                                     │
│   ├─ embedded web/dist (dashboard, /remote)                    │
│   ├─ GET  /events        SSE: config + state + input           │
│   ├─ POST /api/input     from phone remote                     │
│   ├─ GET/POST /api/kiosk start/stop the fullscreen browser     │
│   ├─ /api/apps           list, start, stop launcher apps       │
│   ├─ GET  /photos/{key}  one photo as a 1920x1080 JPEG, cached │
│   ├─ /api/photos         status, folder tree, selection, list  │
│   ├─ GET  /photos        folder picker page (phone)            │
│   └─ providers: system, weather (Open-Meteo)                   │
└────────────────────────────────────────────────────────────────┘
          ▲ phone on LAN  ▲ laptop: make deploy (ssh)
```

## Repository layout

```
cmd/pidash/            main.go — flags, config, server start
internal/config/       TOML loading + defaults + validation
internal/server/       HTTP mux, SSE hub, input endpoint, static embed
internal/providers/    weather/ (Open-Meteo), photos/ (scan, resize, cache), system/
internal/kiosk/        start/stop the fullscreen browser via its systemd user unit
internal/apps/         app launcher: manager, systemd backend, session env detection
internal/photos/       slideshow: sources (Synology Photos, folder, mock), index, render, cache
internal/cec/          optional, later: schedule TV standby/wake via cec-ctl
web/                   Vite + TS: src/panels/*, src/input.ts, src/sse.ts, remote/
deploy/                install.sh, uninstall.sh, systemd user units, kiosk.sh, cec-setup.sh
config.example.toml
Makefile               dev / build / deploy / lint
docs/PLAN.md           this file
```

## Weather

A provider like any other: polled on an interval, last good value kept and
marked stale when a refresh fails. A failed fetch is retried after 15 seconds,
doubling up to the normal interval, so the forecast appears soon after boot even
though the backend starts before the network is up.

- **Source**: Open-Meteo forecast API, no key. One request returns current
  conditions, 24 hours and 7 days. A place name is resolved once through
  Open-Meteo's geocoding API and remembered on disk; coordinates can be given
  instead. Metric or imperial units are requested from the API, not converted.
- **Times** are sent with the location's UTC offset and its IANA zone name, and
  the browser formats them in that zone, so sunrise is right even if the Pi's
  own time zone differs.
- **Backend sends numbers and WMO weather codes only.** Icons and wording live in
  the frontend: one small set of SVG icons composed from shared parts (sun,
  moon, cloud, drops, flakes, bolt), with day and night variants.
- **Ambient summary** (bottom right): icon, temperature, conditions, high/low;
  the detailed overlay level adds place, feels-like and chance of rain.
- **Detail panel**, opened from the Weather tile at the start of the app row:
  a hero temperature, six stat tiles, a 12-column hourly strip, and the week as
  aligned low-to-high range bars on one shared scale in a single hue, with a
  marker for the current temperature. Every value is printed, because a TV has
  no hover. While open it replaces the ambient layer and takes all keys.

## Background photos

The slideshow has three parts.

**Sources** implement one small interface: list the folders inside a folder,
list the photos in a folder, fetch a photo. Implementations: Synology Photos, a
local folder, and a generator of sample pictures for `-mock`.

- *Synology Photos* talks to DSM 7's web API (`/webapi/entry.cgi`): sign in with
  `SYNO.API.Auth`, browse with `SYNO.Foto.Browse.Folder` / `.Item` (Personal
  Space) and `SYNO.FotoTeam.*` (Shared Space), fetch with `SYNO.Foto.Thumbnail`
  (ready-made 1280 px preview) or `SYNO.Foto.Download` (full file). An expired
  session is renewed transparently. Login problems are translated into plain
  messages (wrong password, 2-step verification, not a NAS, unreachable).
  This API is not officially documented; it is implemented from community
  documentation and covered by tests against a fake NAS, and still needs to be
  confirmed against real hardware.

**Service** (`internal/photos`): keeps the user's selection (a list of folders
plus "include subfolders") in a small JSON file, walks those folders into an
in-memory index, and re-reads them every few hours and whenever the selection
changes. If the source cannot be reached, the previous index is kept and the
error is reported in the status. It hands out shuffled playlists and renders
photos on demand. Only photos in the index can be requested.

**Rendering**: each photo becomes a 1920×1080 JPEG, once, cached on disk with
least-recently-used eviction under a size limit.

- EXIF orientation is read from the file and applied.
- If filling the screen would crop away at most about a quarter (so 4:3 and 3:2
  landscape photos), the photo fills the screen. Otherwise (portraits,
  panoramas) it is shown whole on a blurred, darkened copy of itself. The blur
  is made by shrinking the photo to a few pixels and enlarging it again, so it
  costs nothing at display time and needs no CSS filter.
- JPEG/PNG are rendered from the full file; other formats (HEIC, RAW) and any
  full file that fails to decode fall back to the source's preview.
- One render at a time, and concurrent requests for the same photo share it.

**Frontend**: two stacked `<img>` elements crossfaded by opacity; the incoming
photo fades in above the outgoing one so brightness never dips. The next photo
is requested as soon as the current one is shown, so its render is ready in
time. The slideshow stops fetching while an app covers the dashboard.

**Folder picker** (`/photos`, linked from the phone remote): a lazily loaded
folder tree with checkboxes, the list of chosen folders, and live status while
the Pi re-reads the library.

## Apps (launcher)

Pressing OK on the dashboard opens the panel rail: a row of large tiles, one per
configured app plus "Desktop". Apps are `[[apps]]` entries in `config.toml` with
an id, name, command and a launch mode.

| Mode      | Where it runs                           | Use for                          | Setup |
|-----------|-----------------------------------------|----------------------------------|-------|
| `desktop` | In the desktop session, like the menu   | Steam Link, anything windowed    | None  |
| `console` | On its own virtual terminal (tty8)      | Moonlight, Kodi: direct display access for low latency, HDR, refresh-rate switching | `install.sh --console-apps` |

An app is either a **program** (`command`) or a **website** (`url`). A website
app is opened with `--kiosk` in Chromium or Firefox, in its own browser profile
under `~/.local/share/pidash/profiles/<id>`, so logins persist and stay separate
from the user's normal browser. Options: `browser`, `user_agent`, and `adblock`,
which downloads uBlock Origin from addons.mozilla.org into the Firefox profile on
first launch and enables it through profile preferences (no system-wide Firefox
policy). The default config ships YouTube (Firefox, uBlock, TV interface via a TV
user agent) and Netflix (Chromium, Widevine).

Tile icons need no configuration: an explicit `icon` wins, then built-in icons
for YouTube, Netflix and Synology Photos, then the installed program's own icon
resolved through its `.desktop` entry and the hicolor icon theme (including
Flatpak exports), then a globe or the first letter.

How it works:

- Both modes run the app as a systemd unit whose command is
  `pidash -exec-app <id>`. That looks the command up in the config and replaces
  itself with it, so the command lives in one place and never passes through a
  privileged component.
- **Desktop mode** uses a transient *user* unit (`systemd-run --user`,
  `pidash-app-<id>.service`). `-exec-app` locates the Wayland socket or X display.
- **Console mode** uses a *system* template unit `pidash-console@<id>.service`.
  It opens a real login session for the desktop user on tty8 (`PAMName=login`),
  so the app gets the display, input and audio devices like a console login.
  A root-owned helper switches to tty8 before the app starts and back to the
  previous terminal when it ends, however it ends. The desktop and dashboard
  stay alive in the background and stop rendering while hidden.
- A polkit rule lets the desktop user start and stop only
  `pidash-console@*.service`. These three files (unit, helper, rule) are the
  only system-wide parts of PiDash, and only exist after `--console-apps`.
- The backend polls the unit state, so it notices when an app is quit with a
  controller, and adopts an app that is already running after a backend restart.
  An app that exits within five seconds of starting is reported as a failed launch.
- One app at a time. Starting another stops the first.
- While a console app owns the screen the dashboard cannot see TV-remote keys,
  so the **phone remote shows "Back to dashboard"**. The app's own quit shortcut
  works too.

## Input on the dashboard

Every input source ends in the same small action vocabulary (`up`, `select`,
`back`, `weather`, `help`, …), handled in one place, so all sources behave alike.

- **Keys** (TV remote over CEC, keyboards) are mapped by `KeyboardEvent.code`
  through a key map: built-in defaults plus additions from the settings, where
  `"none"` removes one. Key presses with Ctrl, Alt or Meta are ignored so
  desktop shortcuts keep working. Tab and Shift+Tab move along the app row.
- **Mouse and touch**: the pointer and a control bar appear on movement and
  fade after a few seconds. Bar buttons emit actions; they never take keyboard
  focus, so Enter keeps meaning OK. A click on the screen toggles the app row,
  a click on a tile activates it, hovering focuses it, right-click is Back, and
  the wheel is left/right.
- **Phone remote**: actions over the event stream.
- **Help** (`H`, `F1`, the bar): a full-screen overview generated from the live
  key map.

## Controlling apps: phone input and the TV remote

While an app has the screen, the dashboard page receives no keys, so two
mechanisms work below the browser.

**Virtual mouse and keyboard** (`internal/input`, opt-in via
`remote.mouse_keyboard`). The backend creates a device through `/dev/uinput`
with relative axes, wheel, mouse buttons and a small set of keys. To the system
it is real hardware, so it works on Wayland and X11 and in console-mode apps.

- The phone's touchpad page collects movement and posts it one request at a
  time, so a slow network adds a little lag instead of a growing queue.
  Acceleration and gestures (tap, two-finger tap, two-finger scroll) are
  computed on the phone.
- `POST /api/input` presses real keys instead of sending a dashboard action
  whenever an app is running, so the phone's direction pad works in apps.
- **Text** is typed with `wtype` (Wayland) or `xdotool` (X11) rather than as key
  codes, because key codes would come out wrong on non-US keyboard layouts.
- Access to `/dev/uinput` comes from one udev rule installed by
  `install.sh --phone-input`.
- **Security**: this is remote control of the desktop for anyone on the LAN.
  It is off by default, documented as such, and can be protected with a PIN
  (constant-time compare, lockout after five wrong attempts).

**Long press of Back on the TV remote.** The backend reads `/dev/input/event*`
directly, picking devices that can send one of the configured exit keys and
ignoring its own virtual device, and rescans every few seconds for hot-plugged
devices. A per-device hold detector fires when a key stays down for the
configured time; it only acts while an app is running, and then stops the app.
Reading the device directly works whichever virtual terminal is showing. It
needs membership of the `input` group and no system changes.

## TV power

`internal/cec` wraps `cec-ctl`: it finds the adapter that has a TV attached,
registers the Pi as a playback device, and sends image-view-on, active-source,
standby and give-device-power-status. `pidash -cec setup` replaces the earlier
shell script and runs when the kiosk starts.

`internal/tv` runs the daily schedule by checking the clock twice a minute,
which stays correct across daylight saving, clock corrections and suspend.

- A switch-off is deferred while an app is running and dropped when the
  switch-on time arrives first.
- Nothing is sent when the backend starts inside the off period.
- After sending standby it polls the TV's power state. "Asleep" is true only
  while the TV confirms standby, so the dashboard resumes when someone switches
  the TV on by hand, and a TV that does not report its state never leaves the
  dashboard suspended.
- The dashboard stops loading photos while asleep.

## Install & operations

PiDash is a guest on the existing desktop. It never changes how the Pi boots.

- Everything is per-user: files in `~/.local/share/pidash`, settings in
  `~/.config/pidash/config.toml`, two systemd **user** units, one menu entry.
  `deploy/uninstall.sh` removes it all.
- `pidash.service` (backend) starts with the desktop session and idles at a few MB.
- `pidash-kiosk.service` (fullscreen Chromium) runs only on demand. It finds the
  running Wayland or X11 session itself, uses a dedicated browser profile, and
  uses `Restart=on-failure` so crashes recover but a deliberate exit stays exited.
- Optional autostart (`install.sh --autostart`) adds an XDG autostart entry so
  the dashboard opens whenever the desktop starts. Off by default.
- `make install` / `make deploy` from a laptop over SSH, or `make install-local`
  on the Pi itself.
- Overscan: set the TV to "Just Scan" / "Screen Fit"; the layout also has a
  configurable safe-area inset as a fallback.
- Later: night schedule in config (`display.off = "23:30"`, `display.on = "06:30"`)
  using CEC standby/wake.

## Shipping

The goal: a Pi owner needs one command and no build tools; a maintainer needs
one command to publish.

- **Packages** (`scripts/dist.sh`): one tarball per platform (arm64, 32-bit ARM,
  amd64) holding the static binary with the frontend embedded, the installer
  and its files, and the example settings. Asset names carry no version, so
  `releases/latest/download/…` is a stable address. A `SHA256SUMS` file sits
  next to them.
- **Publishing**: the Release workflow builds the packages and creates the
  GitHub release with generated notes. It starts either from a pushed tag
  (`make release VERSION=vX.Y.Z`, which checks, tags and pushes) or from the
  "Run workflow" button on GitHub with a version number, in which case it
  creates the tag on the default branch itself and refuses an existing
  version. Versions with a suffix become pre-releases.
- **Installing and updating** (`install.sh` at the repository root, installed
  on the Pi as `update.sh`): detects the platform, downloads the package and
  checksums, verifies, unpacks and runs the packaged installer. The options
  used at install time are saved and reused on update. Settings and data are
  never overwritten.
- **After an update** the backend restarts under systemd; the dashboard sees a
  new version in its first event and reloads, so the TV never runs an old page
  against a new backend. Shutdown closes open event streams, so restarts are
  immediate.
- **Failing visibly.** The first install on real hardware found the backend
  crash-looping on a port that was already taken, with nothing on screen. Since
  then: the backend moves to the next free port and records its address in
  `$XDG_RUNTIME_DIR/pidash/url`, which the launcher reads instead of assuming
  a port; a broken settings file makes it run on defaults and say so on the TV;
  the menu entry shows an error window when the dashboard does not come up;
  the installer verifies the backend started and prints the real port; and
  `doctor.sh` gathers a read-only diagnostic report.
- **CI** runs lint (Go, TypeScript, shellcheck), tests with the race detector
  and a package build on every push and pull request.

## Dev workflow

- `make dev` runs the Go backend with a `--mock` flag (canned weather, sample
  photos) and the Vite dev server with HMR. Arrow keys on the laptop stand in for
  the remote.
- `make build` builds the frontend and embeds it into the Go binary.
- Profile on the Pi by pointing laptop Chrome at `chrome://inspect` with the
  Pi's IP and port 9222.

## Milestones

1. **Clock on the TV, and a way back.** *(done)* User-level installer, backend,
   fullscreen kiosk on the desktop with start/exit controls, key mapping with
   on-screen debug to confirm CEC key events reach the browser, phone remote.
2. **Panel rail and apps.** *(done)* Rail navigation with the remote, app
   launcher with desktop and console modes, launch feedback on the TV, app
   controls and "Back to dashboard" on the phone remote.
3. **Background photos.** *(done)* Synology Photos and local-folder sources,
   folder picker on the phone, smart fill/fit rendering with disk cache,
   crossfading slideshow with previous/next/pause and captions.
4. **Weather.** *(done)* Open-Meteo provider with place-name lookup, summary on
   the ambient screen, and a detail panel opened from the app row.
5. **Remote control of apps and TV power.** *(done)* Phone as touchpad and
   keyboard, long press of Back on the TV remote to leave any app, daily TV
   off/on schedule and manual TV power over HDMI-CEC.
6. **Polish.** System panel, more panels (calendar, headlines).

## Later ideas (not in scope now)

Choosing photos by album, person or date in Synology Photos in addition to
folders. Calendar (ICS), RSS headlines, Home Assistant entities, accent colour extracted
from the current photo, a dedicated appliance mode (Pi OS Lite + `cage`) for a
Pi that does nothing else.
