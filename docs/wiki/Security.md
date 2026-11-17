## Security

PiDash is made for a home network, and trusts the people on it: anyone who
can reach the Pi can open the phone remote, start an app or switch the TV off.
What it guards against, and what it does not:

- **Web pages cannot give it orders.** A page open in a browser at home, or
  inside a website app on the Pi itself, could otherwise quietly send PiDash
  requests. Every request that changes something must come from PiDash's own
  pages, and PiDash only answers when addressed by the Pi's address or a home
  network's kind of name (`raspberrypi.local`, `pi.lan`, a single word). If
  you reach it by a name of your own, allow it:

  ```toml
  [server]
  names = ["tv.example.org"]
  ```
- **The settings can be locked with a PIN** (see [Lock](Settings#lock)). Wrong PINs
  lock everything PIN-protected for a minute, then two, four and so on up to
  an hour. The PIN covers the settings, typing and the mouse from the phone,
  Bluetooth, the backup, updates and shutting down. It does not cover the
  everyday remote: starting apps, switching the TV, choosing photo folders.
- **Passwords stay on the Pi.** The settings page never shows a saved
  password, the files that hold them can be read by your user only, and the
  Wi-Fi password for visitors is handed out to the TV alone.
- **Nothing is encrypted on the way.** Like most things on a home network
  PiDash speaks plain HTTP, so a password or PIN typed into the settings page
  on a phone crosses your network readable. Type it on the TV itself if that
  matters to you.
- **Do not put it on the internet.** No port forwarding, no public address:
  the points above are for a network of people you live with.
- **Updates** are downloaded from this project's releases on GitHub over
  HTTPS and checked against the release's checksums before anything is
  installed.
- **What the Pi fetches from outside your home**, and from whom: the weather
  from Open-Meteo, the map beside photos from OpenStreetMap, the rain radar
  from RainViewer, departures from transport.opendata.ch, the paintings of
  art mode from the Art Institute of Chicago, the cover of what a phone
  plays through Spotify from Spotify, radio stations from the stations
  themselves, and updates from GitHub. Each learns the Pi's address on the internet and what was asked
  for (a region, not your photos), and each has a switch in the settings.
  The dashboard's own pages load nothing from anywhere but the Pi.
- **Videos** are listed, by name, to anyone on the home network who asks the
  Pi, like the photo folders. A video on the NAS is passed on only to the
  player on the Pi itself, through an address PiDash signs; the NAS
  password and session never leave PiDash.

To report a security problem, open an issue on GitHub.
