When something does not work, start here:

```sh
bash ~/.local/share/pidash/doctor.sh
```

It only reads, changes nothing, and does not print passwords. The
[status page](Performance-and-status#status-page) on the TV says much of the
same.

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
repairs both where it can. The [status page](Performance-and-status#status-page) on the TV shows the
same, and the last button PiDash heard.

**Looking at the services directly:**

```sh
systemctl --user status pidash pidash-kiosk -l --no-pager
```

## If the dashboard freezes

A browser that runs for weeks can end up with a page that has frozen or
crashed while the browser itself is still there. Nothing would restart it,
and the TV would show a stuck clock until someone noticed.

So the dashboard tells PiDash every 20 seconds that it is alive. If nothing
was heard for three minutes while the dashboard should be showing, PiDash
starts the browser again, and the dashboard is back within seconds. This is
not done while an app is open, since a hidden page may be put to sleep by the
browser, and not after **Exit to desktop**. After three restarts within an
hour it stops trying, because a page that will not stay up has a cause that
restarting does not cure; the [status page](Performance-and-status#status-page) and
`journalctl --user -u pidash` then say so. Nothing is restarted before the
dashboard has reported in at least once since PiDash started.

A browser that crashes outright is restarted by its service, as before. To
switch the watching off:

```toml
[kiosk]
watchdog = false
```

## Debugging the browser on the TV

The dashboard's Chromium listens for DevTools on port 9222, on the Pi only. From
your laptop, forward the port and inspect it:

```sh
ssh -L 9222:localhost:9222 pi@raspberrypi.local
```

Then open `chrome://inspect` in Chrome on the laptop and pick the PiDash page to
profile paint and layout on the real device.

## An app does not start

If an app does not start, the TV shows a message. Details are in the logs:

```sh
systemctl --user status 'pidash-app-*' -l --no-pager   # websites and desktop-mode programs
journalctl -u 'pidash-console@*' -e                    # console-mode programs
```
