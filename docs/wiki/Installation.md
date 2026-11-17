## Install on your Pi

PiDash runs **on top of your existing Raspberry Pi OS desktop**. Nothing is
flashed and the boot setup is not changed. It is a fullscreen browser window
with its own profile: close it and you are back on your desktop, exactly as you
left it. It installs into your home directory, plus one small system rule for
the TV remote, and can be removed again.

Requirements: a Raspberry Pi 5 or 4 running Raspberry Pi OS with desktop
(Bookworm or newer), connected to the TV and to the internet. For anything
else, see [Other computers](Other-computers#other-computers).

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
| `--console-apps` | Lets apps such as Moonlight and Kodi run outside the desktop, see [Apps](Apps#apps) |
| `--phone-input` | Lets the phone act as mouse and keyboard, see [Phone as mouse and keyboard](TV-and-remote#phone-as-mouse-and-keyboard) |

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

With automatic updates off, the TV points a new version out: the **Settings**
tile in the app row says "Update available", and the first time the app row
is opened after the version appeared, a message says which one. It is not
said at the moment it is found, which is as likely to be three at night.

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
| Only with `--console-apps` | three system files, see [Apps](Apps#apps) |
| One system rule, so the TV remote's buttons reach apps | `/etc/udev/rules.d/70-pidash-uinput.rules` |
| What you save on the settings page | `~/.config/pidash/settings.json` |
| Only with `--phone-input` | `wtype`/`xdotool` for typing, see [Phone as mouse and keyboard](TV-and-remote#phone-as-mouse-and-keyboard) |

## The first start

The first time the dashboard opens on a freshly installed PiDash, it asks
three things, with the TV's remote: which language, where the TV is (for
the weather; a town is enough, and it is looked up at once), and where your
photos are. For photos it only asks which kind (Synology Photos, Immich, a
shared folder on the network) and then opens the settings page at that
kind, where the address and the account go. Each question can be answered
with "Not now", and **Back** on the first one leaves them all for later.
They are asked once: everything they set is on the [settings page](Settings#settings).

A PiDash that already has a place for the weather or photos, or where
anything was ever saved on the settings page, does not ask.
