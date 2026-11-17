## In a container

A container has no desktop, no HDMI cable and no sound of its own. In one,
PiDash serves **the dashboard for any browser**: the photos, the clock, the
weather with the rain radar, the calendar, places, trips, the photo quiz, art
mode, the radio and the settings page. Open it on a tablet on the wall, in a
smart TV's browser, or in a kiosk browser on some other machine. Apps, the
TV's remote over HDMI, sound outputs, Bluetooth and videos are not part of
it: they need PiDash installed on the computer that is plugged into the TV.

```sh
docker run -d --name pidash --restart unless-stopped \
  -p 8080:8080 \
  -e TZ=Europe/Zurich \
  -v pidash-data:/data \
  -v /path/to/your/pictures:/photos:ro \
  ghcr.io/darioromano/pidash:latest
```

Then open `http://<that computer>:8080`. The image is built for x86-64 and
for 64-bit and 32-bit ARM, so it runs on a NAS, a home server or a Pi alike.
[compose.example.yml](https://github.com/DarioRomano/PiDash/blob/main/compose.example.yml) is the same as a Compose file.

| | |
|---|---|
| `/photos` | Pictures mounted here are shown, with nothing to set up. PiDash only reads them. Photos from Synology Photos, Immich or a shared folder on the network are set up on the settings page instead, as on a Pi |
| `/data` | Everything PiDash keeps: what is saved on the settings page (`settings.json`), its caches, and a `config.toml` if you put one there. Without one the built-in settings apply |
| `TZ` | Your time zone, for the times of the night mode and "on this day" |
| Port 8080 | The dashboard at `/`, the settings at `/settings` |

In the browser, the arrow keys, Enter and Escape do what the remote's buttons
do on a TV; a smart TV's browser sends them from its own remote.

**Updating** is pulling the new image (`docker compose pull && docker compose
up -d`); the settings page says which version runs. A version can be pinned
with its tag, such as `ghcr.io/darioromano/pidash:v0.12.0`.

**Building it yourself**: `docker build -t pidash .` in a checkout. The
container runs as an unprivileged user (65532) without a shell; if you mount a
folder of your own at `/data`, it must be writable by that user, or run the
container with `--user "$(id -u):$(id -g)"`.

PiDash has no login of its own, in a container as on a Pi: it is meant for the
home network, not for a port opened to the internet. A settings PIN
([Lock](Settings#lock)) keeps people at home from changing things.

The same mode without Docker is `pidash -standalone`.

### On a Synology NAS

A Synology NAS with **Container Manager** (DSM 7.2 and newer; install it
from Package Center) runs the container beside the photos it shows. In
Container Manager, create a **Project**, give it a folder such as
`docker/pidash`, and let it create the `compose.yaml` from this:

```yaml
services:
  pidash:
    image: ghcr.io/darioromano/pidash:latest
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      TZ: Europe/Zurich
    volumes:
      - pidash-data:/data

volumes:
  pidash-data:
```

Start the project and open `http://<your NAS>:8080` on the tablet, the
smart TV's browser or whatever is to show the dashboard. A fresh PiDash
asks its [three questions](Installation#the-first-start); for the photos there are two
ways:

- **Synology Photos** (the easier one): choose it under **Background
  photos** and enter the address of the NAS itself, `http://<your NAS>:5000`,
  with an account that may see the photos. Nothing needs mounting, and the
  folder picker offers your folders and albums.
- **A shared folder of pictures**, read as files: add it to the project,

  ```yaml
      volumes:
        - ./data:/data
        - /volume1/photo:/photos:ro
      user: "1026:100"
  ```

  The container runs as a user without rights on the NAS, and a shared
  folder lets only its own users in. `user:` runs it as one of them
  instead: the number of a NAS user who may read the folder, and of that
  user's group (the first user made on a NAS is usually 1026, the group
  `users` 100; `id <name>` over SSH says). The same user must be able to
  write PiDash's own folder, which is why `./data`, a folder inside the
  project's folder, takes the place of the volume here: create it in File
  Station as that user before the first start.

Port 8080 must be free on the NAS and, if its firewall is on, allowed for
the home network. A new version of PiDash is a new image: fetch
`ghcr.io/darioromano/pidash:latest` again under **Image** (or **Registry**)
and build the project anew; what is under `/data` stays.
