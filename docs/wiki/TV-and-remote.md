## Switching the TV off when nobody is there

Settings, Screen and night: *Switch the TV off when no button was pressed
for* five minutes up to four hours (`idle_off` under `[tv]`). It counts only
while the dashboard shows: not during an app, a game or a video, and not
while music or the radio plays. A minute before, the TV says so, and any
button keeps it on. It needs a TV that can be switched over HDMI-CEC.

**While the TV shows something else, it is left on.** Someone watching
another input, or one of the TV's own apps, presses no buttons that reach
the Pi; that must not look like nobody being there. A TV says over HDMI when
it turns to another input, and most say when they turn to their own
programme or apps. From then on the time does not count, a notice that was
running is taken back, and the nightly switch-off (`off` under `[tv]`) waits
too, as it does for an app on the Pi. It all starts again when the TV comes
back to the Pi, which it says as well, and which any button of its remote
arriving at the Pi shows in any case. A sleep timer set by hand still
counts: that one means "off in half an hour, whatever is on".

The [status page](Performance-and-status#status-page) says under **TV shows** what the TV has
said: PiDash, or something else and since when, and the last time it showed
something else. If you watch one of the TV's apps for a while and that line
never mentions it, your TV keeps this to itself; nothing can then tell the
two apart, and the idle switch-off is better left on "Never".

When the TV goes off, for this reason or any other, game controllers,
headphones and speakers connected over Bluetooth are disconnected, so that
they switch themselves off instead of running their batteries down.
Keyboards and mice stay. Settings, Sound and Bluetooth has the switch
(`disconnect_with_tv` under `[bluetooth]`).

## Sleep timer

The **Sleep timer** tile in the app row switches the TV off after a while.
Each **OK** sets the next length (30, 60, 90 or 120 minutes) and then off
again; the tile shows the time left. A minute before the end the screen says
so. When the time is up, an app that is still playing is closed and the TV
is switched to standby. The same button is at the end of the bar that ▲
opens, where it can be pressed several times in a row. It only appears when
the TV can be switched over HDMI-CEC.

## TV power schedule

PiDash can switch the TV off at night and on in the morning over HDMI-CEC:

```toml
[tv]
off = "23:30"
on = "06:30"
```

Either time can be left out. With only `off`, the TV is never switched on by
itself.

**What switches the TV on, and what does not.** The TV is switched on by the
schedule above, by **TV on** or **Show on TV** on the phone remote, and by a
[game controller being switched on](#game-controller). The dashboard starting
does not switch it on: that also happens after a fault, after an update, or
while someone works on the Pi from afar (a remote shell, screen sharing with
Raspberry Pi Connect), none of which means anyone wants to watch. A TV that is
on when the dashboard starts is switched to the Pi's input; a TV in standby
stays in standby. Likewise a controller that a program creates (game streams
and remote desktops do) does not count as one being switched on. If you want
the old behaviour, where starting the Pi switches the TV on:

```toml
[tv]
wake_on_start = true
```

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

## Other devices on the TV

A games console, a set-top box, a player: each can have a tile in the app
row that has the TV show it, so that the dashboard is where everything on
the TV starts. Set them up under Settings, Apps and radio, *Other devices
on the TV*: a name and the HDMI socket of the TV it is plugged into, or
*Ask the TV what is connected*, which adds every device that answers, by
the name it gives itself. In `config.toml`:

```toml
[[tv.inputs]]
name = "PlayStation"
port = 2                # the TV's HDMI 2

[[tv.inputs]]
name = "Set-top box"
address = "1.2.0.0"     # for a device behind a receiver or soundbar
```

**OK** on the tile tells the TV, over HDMI-CEC, to show that device; a
console that listens to the TV usually wakes up for it. To come back to the
dashboard, choose the Pi's input with the TV's own remote: while the TV
shows something else, its remote's buttons do not reach the Pi. **Show on
TV** on the phone remote brings it back as well.

The tiles are there when the TV can be reached over HDMI-CEC. How willingly
a TV switches when asked differs from one make to the next; if yours does
not, the TV's own input button is all that is left.

## The TV remote on the dashboard

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

## Game controller

A game controller, wired or over Bluetooth, works as a remote without any
setup. While it is what was pressed last, the lines on screen that say what
the buttons do name its buttons instead of OK and Back: ✕ and ○ for a
PlayStation controller, Ⓐ and Ⓑ for an Xbox one, Ⓑ and Ⓐ for a Nintendo one,
whose letters sit the other way round. A press on the TV's remote brings OK
and Back back.

A controller that switches the TV on (see below) opens the
[Games](Games-and-videos#games) page, the way a games console starts with its games.
Settings, Apps has the switch (`open_on_controller` under `[games]`).

| On the controller | Does |
|-------------------|------|
| Left stick or direction pad | Arrows |
| Bottom button (A on an Xbox pad, ✕ on a PlayStation pad) | OK |
| Right button (B, ○) | Back |
| Start / Menu / Options | Opens the app row |
| Shoulder buttons | Previous and next photo |
| Logo button | Back to the standard view. **Held for two seconds inside an app: closes it** |
| Logo button held, and up or down | TV volume |

**Volume.** With the logo button held, up and down on the direction pad or
the stick turn the TV's volume up and down, anywhere: on the dashboard, in a
website, in a game. PiDash presses the volume buttons of the TV's remote on
its behalf, over HDMI. If a soundbar or receiver on HDMI sets the volume, put
`volume_to = "audio"` under `[tv]`; `controller_volume = false` switches this
off. The game sees the logo button being pressed too, since PiDash only
listens in; inside Steam that button plus a direction can have a meaning of
its own.

Buttons are named by where they sit, so on a Nintendo pad the bottom button
is B. To change what a button does on the dashboard, name it under
`[input.remote_keys]`, for example `BTN_NORTH = "weather"`; `evtest` shows the
names.

Inside an app it depends on the app:

- **Websites** (YouTube, Netflix, Plex, …) know nothing of controllers, so
  PiDash presses keys for it: stick and direction pad are the arrows, the
  bottom button is Enter, the right one Escape, Start is play/pause. A held
  direction keeps scrolling. If a website reacts to the controller by itself
  and everything happens twice, set `controller_in_websites = false` under
  `[input]`.
- **Programs** (emulators, Steam Link, Moonlight) get the controller to
  themselves.
- Everywhere, **holding the logo button for two seconds** returns to PiDash.

Pair a Bluetooth controller once under [Settings → Bluetooth](Music-and-sound#bluetooth). It
shows up under **Listening to** on the [status page](Performance-and-status#status-page) when PiDash
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

## Phone as mouse and keyboard

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
