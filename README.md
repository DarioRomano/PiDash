# PiDash

A fast, good-looking dashboard for your TV, running on a Raspberry Pi 5 and
controlled with the TV's own remote (HDMI-CEC) or your phone.

- **Ambient screen**: photo slideshow with clock, date and weather overlaid.
- **Zero extra hardware**: the TV remote works over HDMI-CEC. A phone remote is built in.
- **Single binary**: Go backend with the web UI embedded. Deploy with one command.
- **Stays smooth**: 1080p render, GPU-only animations, no polling in the browser.

See [docs/PLAN.md](docs/PLAN.md) for the architecture and roadmap.

## Status

Milestone 1: boots into a kiosk with a clock, remote-control input mapping with
on-screen key debugging, phone remote, system stats. Photos and weather are next.

## Quick start on the Pi

Requirements: Raspberry Pi 5 with Raspberry Pi OS Lite (Bookworm, 64-bit), SSH
enabled, connected to the TV's HDMI0 port. On your laptop: Go 1.24+, Node 20+.

```sh
# 1. Build for arm64 and install everything on the Pi (packages, systemd units, boot config).
make install PI_HOST=pi@pidash.local

# 2. Reboot the Pi. It boots straight into the dashboard.
ssh pi@pidash.local sudo reboot

# 3. Later updates: rebuild and restart in a few seconds.
make deploy PI_HOST=pi@pidash.local
```

Phone remote: open `http://<pi-ip>:8080/remote` on any device on your network.

Configuration lives at `/etc/pidash/config.toml` on the Pi; see
[config.example.toml](config.example.toml). After editing: `sudo systemctl restart pidash`.

### Mapping your TV remote

Enable CEC on the TV (Samsung "Anynet+", LG "Simplink", Sony "Bravia Sync",
Philips "EasyLink", etc.). With `display.debug_keys = true` (the default) the
dashboard shows the raw key code of every press in the top-right corner. Put the
codes you see into `[input.keys]` in the config, mapped to actions
`up down left right select back menu home playpause 0-9`.

Check CEC from the Pi:

```sh
systemctl status pidash-cec     # should say "using /dev/cec0"
cec-ctl -d /dev/cec0 -S         # show the TV in the topology
sudo journalctl -u pidash-kiosk -f
```

### Debugging the browser on the TV

Chromium exposes remote DevTools on port 9222. On your laptop open
`chrome://inspect`, add `<pi-ip>:9222` under "Discover network targets" and
inspect the dashboard page to profile paint and layout on the real device.

## Development

```sh
make dev            # Go backend on :8080 with mock data + Vite dev server on :5173
make build          # native binary in bin/pidash (frontend embedded)
make lint test
```

Arrow keys, Enter, Escape and digits stand in for the remote during development.

## Layout

```
cmd/pidash/          entry point
internal/config/     TOML config, defaults, validation
internal/server/     HTTP, SSE hub, input API, static files
internal/providers/  provider interface + runner, system/ stats
web/                 Vite + TypeScript frontend (dashboard + phone remote)
deploy/              install.sh, systemd units, kiosk and CEC scripts
```

## License

MIT — see [LICENSE](LICENSE).
