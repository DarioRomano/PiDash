## Development

How PiDash is put together, and why, is in
[docs/PLAN.md](https://github.com/DarioRomano/PiDash/blob/main/docs/PLAN.md).

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

**Tests.** There are three kinds, and all of them run on every push and
before every release.

- `make test` runs the Go tests, with the race detector. Where a test needs
  a real program and finds it, it uses it: with `mpv` and `ffmpeg` installed,
  a real video is played and frames are taken out of it.
- `make e2e` starts PiDash with sample data and drives the real pages in a
  browser with the buttons of a TV remote: the dashboard, the app row, the
  settings and their on-screen keyboard. Every button press is tried the
  three ways it can arrive on a Pi: read from the remote by the backend, as
  a key in the browser, and both at once, which must count as one press. It
  needs `make build` first and Playwright's Chromium (`cd web && npm install
  --no-save playwright && npx playwright install chromium`).
- `make integration` tries PiDash as it is shipped (`test/integration`). It
  builds the release's packages and has the real installer install one into
  a throwaway home, with stand-ins only for what would change the machine
  (`sudo`, `systemctl`); updates it through the running backend, as the
  settings page's button does; checks that a damaged package is refused;
  and removes it again. It builds the container image and runs it, and it
  starts a Samba server to read a share from. The last two need Docker and
  are skipped without it.

What these cannot try is the hardware: a TV on an HDMI cable, a remote, a
sound card. For those there are stand-ins that behave as the real things are
documented to, which is not the same as the real things.

Two kinds of files guard updates. `internal/settings/testdata` holds
settings as earlier versions saved them, and `internal/config/testdata` the
settings file as earlier versions installed it; every version must still
read them all. A release that changes how something is stored adds its own
file there; the old ones are never edited. And a fault that was found gets
a test that fails without its fix, named after what went wrong.

To try a change on your own Pi straight from the checkout, without making a
release:

```sh
make install PI_HOST=pi@raspberrypi.local     # first time
make deploy  PI_HOST=pi@raspberrypi.local     # afterwards: rebuild, copy, restart
```

Add `INSTALL_FLAGS="--autostart --console-apps --phone-input"` to `make install`
as needed. On the Pi itself, `make install-local` does the same from a checkout
there.

## The guide

The README is what someone sees first, and is kept short: what PiDash is,
what it looks like, how to install it. Everything else is this guide. Its
pages are the files in `docs/wiki`; the workflow `.github/workflows/wiki.yml`
publishes them to the project's wiki whenever they change on the main
branch, so a page is changed there, in a commit like any other, and never
on the wiki itself. `go test ./docs` checks that every link between the
pages, and from the README, the project page and the installers into them,
leads to a page and a heading that exist.

The wiki has to exist before anything can be published to it: once, under
the repository's **Settings, Features**, tick **Wikis**, and save a first
page on the **Wiki** tab (GitHub only then creates the place the pages are
published to; what the page says does not matter, it is replaced). The
wiki keeps no history of its own: each time, it becomes a single commit made
by the workflow, so the hand-saved first page, with the name and address of
whoever saved it, does not stay in it.

## The project page

The page at [darioromano.github.io/PiDash](https://darioromano.github.io/PiDash/)
is the `site/` folder: one HTML file, one stylesheet, and screenshots. A push
to `main` that changes it publishes it (`.github/workflows/pages.yml`); for
that, the repository's **Settings → Pages → Source** must be set to **GitHub
Actions**, once.

The screenshots are taken from the real pages, driven like a TV remote drives
them, with sample pictures that are drawn for the purpose:

```sh
make build && make site-shots
```

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

A second job then builds the [container image](Container#in-a-container) for x86-64 and
ARM and pushes it to `ghcr.io/darioromano/pidash`, tagged with the version
and, unless it is a pre-release, `latest`. The release does not wait for it.
The first time, GitHub creates the package as private: make it public once
under the repository's **Packages**, in the package's settings, so that
`docker pull` works without a login.

- Versions follow `vMAJOR.MINOR.PATCH`. A suffix such as `v0.3.0-rc.1` makes a
  pre-release, which the installer skips unless asked for by name.
- `make dist` builds the same packages locally into `dist/`, to inspect them or
  to install from a file: unpack one and run `bash deploy/install.sh` inside it.
- Every push and pull request runs the same checks and a package build in
  GitHub Actions (`.github/workflows/ci.yml`), and builds the container image
  and starts it.
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
internal/sound/      sound output and volume, through pactl
internal/playing/    what music is playing, from the players on the session bus
internal/games/      the games: a PC's from Moonlight's settings, RetroArch's from its lists
internal/videos/     folders of videos, on the Pi or a Synology NAS, and the player
internal/radar/      the rain radar's pictures, fetched from RainViewer
internal/maps/       the map beside a photo: pieces fetched from OpenStreetMap and kept
internal/power/      restarting and shutting down the Pi
internal/wifi/       the code a phone scans to join the Wi-Fi for visitors
deploy/              the installer run on the Pi, uninstaller, systemd units, kiosk launcher
install.sh           one-line installer and updater: downloads a release and runs deploy/install.sh
scripts/dist.sh      builds the release packages
docs/wiki/           the guide: the pages of the project's wiki
test/integration/    PiDash tried as it is shipped: installed, updated, as a container
Dockerfile           the container image: the dashboard alone, for any browser
compose.example.yml  the same as a Compose file
.github/workflows/   CI on every push, and the release build and container image on a version tag
```
