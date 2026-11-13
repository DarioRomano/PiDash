# PiDash — Plan

A fast, good-looking dashboard for a TV, driven by a Raspberry Pi 5 and controlled
with the TV's own remote (HDMI-CEC) or a phone.

## Decisions

| Area        | Choice                                                  | Why                                                                 |
|-------------|---------------------------------------------------------|---------------------------------------------------------------------|
| Hardware    | Raspberry Pi 5, HDMI0 to the TV                         | Enough GPU/CPU for smooth Chromium at 1080p; HDMI0 carries CEC.     |
| OS          | Raspberry Pi OS Lite (Bookworm, 64-bit)                 | No desktop to fight with; boots straight into the kiosk.            |
| Display     | `cage` (single-app Wayland compositor) + Chromium kiosk | Hardware-accelerated compositing, trivially restartable.            |
| Frontend    | Vite + vanilla TypeScript, fixed 1920×1080 layout       | Small, no framework overhead, easy to keep off the hot path.        |
| Backend     | Go, single static binary, frontend embedded via `embed` | One file to deploy, low memory, cross-compiles from any laptop.     |
| Transport   | Server-Sent Events for push, HTTP POST for input        | Stdlib only, `EventSource` reconnects by itself.                    |
| Config      | One `config.toml`                                       | Human-editable on the Pi over SSH.                                  |
| Remote      | HDMI-CEC (primary) + phone web remote (fallback)        | No extra hardware; the phone covers text entry and TVs without CEC. |
| First panels| Clock & date, weather (Open-Meteo), photo slideshow     | No API keys required.                                               |

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
| `back`      | Back / Exit       | Close rail, return to ambient                              |
| `0-9`       | Number keys       | Jump straight to a panel                                   |
| `play/pause`| ⏯                 | Pause / resume slideshow                                   |

Every panel is a self-contained widget: a **provider** on the backend that
produces a JSON state, and a **view** on the frontend that renders it. Adding
a widget later (calendar, RSS, Home Assistant) means adding one of each.

## Performance rules

These keep the Pi 5 GPU and CPU idle most of the time.

- **Render at 1080p**, even if the TV is 4K. Set the kiosk output mode to 1920×1080.
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
- Chromium flags: `--kiosk --noerrdialogs --disable-infobars --ozone-platform=wayland
  --enable-gpu-rasterization --enable-zero-copy --disable-pinch --overscroll-history-navigation=0
  --autoplay-policy=no-user-gesture-required --remote-debugging-port=9222`
  (the last one lets you open DevTools from your laptop to profile on the real device).

## Remote control details

### HDMI-CEC (TV remote)

On Pi 5 with the KMS driver the kernel exposes `/dev/cec0`. Steps:

1. Install `v4l-utils` for `cec-ctl`.
2. A oneshot systemd unit at boot runs
   `cec-ctl --playback --osd-name PiDash -d /dev/cec0` so the TV sees the Pi as a
   playback device and forwards remote keys.
3. The kernel's CEC RC passthrough turns keys into normal Linux input events
   (`KEY_UP`, `KEY_OK`, …). `cage` passes them to Chromium as keyboard events.
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
┌──────────────── Raspberry Pi 5 ────────────────┐
│ systemd                                        │
│  ├─ pidash-kiosk.service  cage → chromium ─────┼──► HDMI0 ──► TV
│  ├─ pidash.service        Go binary :8080      │        ▲  CEC keys
│  └─ pidash-cec.service    cec-ctl --playback   │◄───────┘
│                                                │
│  Go binary                                     │
│   ├─ embedded web/dist (dashboard, /remote)    │
│   ├─ GET  /events        SSE: state + input    │
│   ├─ POST /api/input     from phone remote     │
│   ├─ GET  /photos/{id}   resized, cached       │
│   └─ providers: clock, weather, photos, system │
└────────────────────────────────────────────────┘
          ▲ phone on LAN  ▲ laptop: make deploy (ssh)
```

## Repository layout

```
cmd/pidash/            main.go — flags, config, server start
internal/config/       TOML loading + defaults + validation
internal/server/       HTTP mux, SSE hub, input endpoint, static embed
internal/providers/    weather/ (Open-Meteo), photos/ (scan, resize, cache), system/
internal/cec/          optional: schedule TV standby/wake via cec-ctl
web/                   Vite + TS: src/panels/*, src/input.ts, src/sse.ts, remote/
deploy/                install.sh, systemd units, chromium flags, cage launcher
config.example.toml
Makefile               dev / build / deploy / lint
docs/PLAN.md           this file
```

## Boot & operations

- `pidash-kiosk.service` runs as the `pi` user on `tty1` with `PAMName=login` so
  logind grants seat access; it starts `cage -- chromium <flags> http://localhost:8080`.
  `Restart=always` for both it and `pidash.service`.
- `deploy/install.sh` turns a fresh Pi OS Lite image into a working dashboard:
  installs `cage chromium v4l-utils`, copies units, enables them, writes a default
  config.
- `make deploy` cross-compiles for `linux/arm64`, copies the binary and config over
  SSH and restarts the service. Builds take seconds; no toolchain on the Pi.
- Overscan: set the TV to "Just Scan" / "Screen Fit"; the layout also has a
  configurable safe-area inset as a fallback.
- Night schedule in config: `display.off = "23:30"`, `display.on = "06:30"`.

## Dev workflow

- `make dev` runs the Go backend with a `--mock` flag (canned weather, sample
  photos) and the Vite dev server with HMR. Arrow keys on the laptop stand in for
  the remote.
- `make build` builds the frontend and embeds it into the Go binary.
- Profile on the Pi by pointing laptop Chrome at `chrome://inspect` with the
  Pi's IP and port 9222.

## Milestones

1. **Boots into a clock.** Install script, kiosk unit, Go binary serving a static
   page with a ticking clock. Confirm CEC key events reach the browser
   (`console.log(e.code)`).
2. **Ambient screen.** Photo provider with resize cache and crossfade; clock and
   weather overlay; overlay density levels; pixel shift.
3. **Input.** Action vocabulary, key mapping from config, panel rail, phone
   remote page.
4. **Resilience & schedule.** Stale caching, reconnect handling, TV standby/wake
   via CEC, service hardening.
5. **Polish & docs.** Weather detail panel, system panel, README with a 10-minute
   setup guide.

## Later ideas (not in scope now)

Calendar (ICS), RSS headlines, Home Assistant entities, accent colour extracted
from the current photo, read-only root filesystem with overlay for SD-card
longevity, 4K output mode for Pi 5 once 1080p is proven smooth.
