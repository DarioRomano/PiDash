## Other computers

PiDash is one program without dependencies of its own, built for 64-bit ARM,
32-bit ARM and x86-64. The installer above picks the right one, and installs
what PiDash uses with `apt`, `dnf` or `pacman`, whichever the system has. What
a system does not offer is named at the end, with what PiDash does without it.

PiDash is developed for the Raspberry Pi 5 on Raspberry Pi OS. The rest of
this section is what is known about other machines; less of it has been tried
on the real thing. `bash ~/.local/share/pidash/doctor.sh` says what a machine
has and lacks.

| Computer | What to expect |
|----------|----------------|
| Raspberry Pi 5 | What PiDash is made for. A 4K TV is fine; see [Performance](Performance-and-status#performance) |
| Raspberry Pi 4, Pi 400 | Everything works the same way. It is two to three times slower than a Pi 5: on a 4K TV choose **1920 × 1080** as the picture sent to the TV, and the **Power saver** preset, both under [Performance](Performance-and-status#performance) |
| Raspberry Pi 3, Zero 2 W | The dashboard with photos, clock and weather runs. With 1 GB of memory or less, website apps such as YouTube do not run well beside it |
| Other ARM boards (Orange Pi, Radxa Rock, Odroid, …) | Needs a Linux with a desktop, systemd and Chromium. The TV's remote works where the board's Linux has an HDMI-CEC driver, which shows as `/dev/cec0`; many vendor images have none. Sites with copy protection (Netflix and the like) need Widevine, which outside Raspberry Pi OS is rarely there for ARM |
| PC or mini PC (x86-64) | Debian, Ubuntu, Fedora, Arch and their relatives. Google Chrome is used when it is installed, Chromium otherwise; Chrome brings Widevine. A PC's HDMI socket has no CEC: see below |

**The TV's remote on a PC.** Graphics cards do not pass HDMI-CEC on, so the
TV's remote does not reach a PC by itself. What works instead:

- a USB CEC adapter that Linux has a driver for, such as the Pulse-Eight one.
  Once it shows as `/dev/cec0`, PiDash uses it like the Pi's own: the remote's
  buttons, switching the TV on and off, the volume;
- a remote with a USB receiver, an "air mouse", or a FLIRC receiver, all of
  which are keyboards to Linux;
- a game controller, a keyboard, or the phone remote.

Without CEC, what is about the TV itself is simply not offered: switching it
on and off, the sleep timer, other devices on the TV.

**Desktops.** PiDash is a fullscreen browser window on the desktop that is
there. Bringing an app's window to the front needs `wlrctl` (Wayland desktops
built on wlroots: labwc, Sway, Wayfire) or `xdotool` (X11); sending the TV a
smaller picture needs `wlr-randr`. GNOME and KDE on Wayland offer neither to
other programs: the dashboard and the apps run there, but those two things do
not, and typing from the phone does not either. An X11 session has all of it.

**Ubuntu's Chromium** is a snap, which may only write inside `~/snap`. PiDash
keeps its browser profiles there for it, and cannot hand it
[extensions](Apps#browser-extensions-for-a-website-app). Installing Google Chrome
avoids both.

**Temperature.** The status page and [Keeping the Pi cool](Performance-and-status#keeping-the-pi-cool)
read the processor's temperature from Linux's thermal zones, whatever the
machine calls them. Where there is none (a virtual machine), neither is shown.
