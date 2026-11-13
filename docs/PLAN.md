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
│   ├─ GET  /photos/{id}   resized, cached            (planned)  │
│   └─ providers: system, weather, photos             (planned)  │
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
internal/cec/          optional, later: schedule TV standby/wake via cec-ctl
web/                   Vite + TS: src/panels/*, src/input.ts, src/sse.ts, remote/
deploy/                install.sh, uninstall.sh, systemd user units, kiosk.sh, cec-setup.sh
config.example.toml
Makefile               dev / build / deploy / lint
docs/PLAN.md           this file
```

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
3. **Ambient screen.** Photo provider with resize cache and crossfade; clock and
   weather overlay; overlay density levels.
4. **Resilience & schedule.** Reconnect handling, TV standby/wake via CEC.
5. **Polish & docs.** Weather detail panel, system panel.

Known gaps in the launcher, candidates for a follow-up:

- **No way back with the TV remote alone.** While an app has the screen the
  dashboard does not receive keys. Fix: have the backend read the remote's
  input device directly and treat a long press of Back as "stop the app".
- **Pointer-driven websites.** Netflix and Synology Photos want a mouse. Fix: a
  touchpad on the phone remote that drives a virtual pointer.
- **Typing into apps.** The phone remote's text field only reaches the dashboard.
- **Synology Photos as the slideshow source**, through its API, instead of only
  opening its website.

## Later ideas (not in scope now)

Calendar (ICS), RSS headlines, Home Assistant entities, accent colour extracted
from the current photo, a dedicated appliance mode (Pi OS Lite + `cage`) for a
Pi that does nothing else.
