import { LANGUAGES, setLanguage, t } from "./i18n";
import { ask, enterPIN, highlight, onTV, startTVNav } from "./tvnav";
import { startBluetooth } from "./bluetooth";
import { startSound } from "./sound";
import { setSounds } from "./sfx";
// The settings page: weather, background photos and apps. Made for a phone,
// and equally usable on the TV with a mouse and keyboard.

interface AppEntry {
  kind: "preset" | "web" | "file" | "desktop";
  id?: string;
  name: string;
  preset?: string;
  /** How much larger a website is drawn; 0 or absent is normal size. */
  zoom?: number;
  /** The menu entry of an installed program, for kind "desktop". */
  desktop?: string;
  /** The program is not installed any more. */
  missing?: boolean;
  url?: string;
  browser?: string;
  adblock?: boolean;
  arrows?: boolean;
  userAgent?: string;
  detail?: string;
}

/** A calendar link: one the Pi already has (key, host), or a new one (url). */
interface CalendarLink {
  key?: string;
  url?: string;
  host?: string;
}

interface Station {
  name: string;
  url: string;
  detail?: string;
}

interface HomeEntity {
  id: string;
  name: string;
  text: string;
}

interface Preset {
  id: string;
  name: string;
  description: string;
  /** Which heading it is offered under: video, music, games or own. */
  group?: string;
  needsUrl: boolean;
  urlHint?: string;
  console: boolean;
  web: boolean;
}

interface Info {
  weather: { location: string; name: string; units: string; coordinates: boolean; radar?: boolean };
  photos: {
    source: string;
    intervalSeconds: number;
    motion: string;
    pairPortraits: boolean;
    onThisDay: boolean;
    videos: boolean;
    map: boolean;
    skipOdd?: boolean;
    art?: boolean;
    synology: { url: string; username: string; passwordSet: boolean; insecureTls: boolean; quality: string };
    immich?: { url: string; keySet: boolean; insecureTls: boolean; quality: string };
    share?: { host: string; name: string; folder: string; username: string; passwordSet: boolean };
    folderPath: string;
  };
  screen: { nightStart: string; nightEnd: string; language: string; clockPosition: string; clockSize: string; clockStyle?: string; eveningDim?: string; view: string; caption?: string; weatherDetail?: string; lockView?: boolean; background: string; performance?: string; tiles?: string; backgroundFps?: number; sounds?: boolean };
  calendar: { links: CalendarLink[] };
  departures?: { stop: string; walk: number };
  home: { url: string; tokenSet: boolean; entities: string[] };
  system: { autoUpdate: boolean; cooling?: boolean; soundFollows?: boolean; spotifySpeaker?: boolean; airplaySpeaker?: boolean; speakerName?: string; resolution?: string; idleOffMinutes?: number; bluetoothOff?: boolean; games?: boolean; gamesPage?: boolean; gamesOnController?: boolean };
  hiddenTiles?: string[];
  lock: { set: boolean };
  wifi?: { name: string; passwordSet?: boolean; open?: boolean; hidden?: boolean };
  videos?: { source: string; folders: string[] };
  tvInputs?: TVInput[];
  radio: Station[];
  apps: AppEntry[];
  presets: Preset[];
  /** Programs installed on the Pi, as its application menu lists them. */
  installed: { id: string; name: string; comment?: string }[];
  emulators?: { id: string; name: string; systems: string; description: string; install: string; desktop?: string }[];
  consoleReady: boolean;
  pinRequired: boolean;
  version: string;
  keyboardProblem?: string;
}

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;
const statusEl = $("status");
const saveBtn = $<HTMLButtonElement>("save");
const summary = $("summary");

if (onTV) {
  const back = $<HTMLAnchorElement>("back");
  back.href = "/";
  back.setAttribute("aria-label", "Back to the dashboard");
  $<HTMLAnchorElement>("p-folders").href = "/photos?from=tv";
  $("tv-help").hidden = false;
}

// The tiles of PiDash's own: ticked means shown.
const ownTiles = [...document.querySelectorAll<HTMLInputElement>("input.own-tile")];

let info: Info;
let apps: AppEntry[] = [];
let calendars: CalendarLink[] = [];
let stations: Station[] = [];
interface TVInput {
  name: string;
  port?: number;
  address?: string;
}
let tvInputs: TVInput[] = [];
let stationsFound: Station[] = [];
let removeLock = false;
let homeTiles: string[] = [];
/** Names of Home Assistant's devices, once it has been asked. */
let homeKnown: HomeEntity[] = [];
let savedState = "";
let busy = false;

// --- Talking to the Pi, with the PIN when it wants one ---------------------------

const PIN_KEY = "pidash-pin";
let pin = "";
// A phone or computer remembers the PIN. The TV does not: there the PIN is
// what keeps the settings from being changed by whoever holds the remote.
if (!onTV) {
  try {
    pin = localStorage.getItem(PIN_KEY) ?? "";
  } catch {
    /* private browsing */
  }
}

async function send(method: string, url: string, body: unknown): Promise<Response> {
  for (;;) {
    const res = await fetch(url, {
      method,
      headers: { "Content-Type": "application/json", ...(pin ? { "X-PiDash-PIN": pin } : {}) },
      body: JSON.stringify(body),
    });
    if (res.status !== 401) return res;
    const question = pin ? "Wrong PIN. Enter the PIN:" : "Enter the PIN:";
    const entered = onTV ? await enterPIN(question) : window.prompt(question);
    if (!entered) return res;
    pin = entered.trim();
    if (!onTV) {
      try {
        localStorage.setItem(PIN_KEY, pin);
      } catch {
        /* not stored */
      }
    }
  }
}

function setStatus(text: string, kind: "" | "ok" | "error" = "") {
  statusEl.textContent = text;
  statusEl.className = kind;
}

function setResult(el: HTMLElement, text: string, kind: "" | "ok" | "error" = "") {
  el.textContent = text;
  el.className = "result " + kind;
}

// --- The form <-> one plain object ------------------------------------------------

const val = (id: string) => $<HTMLInputElement>(id).value;

function readForm() {
  const source = val("p-source");
  return {
    weather: { location: val("w-location").trim(), name: info.weather.name, units: val("w-units"), radar: $<HTMLInputElement>("w-radar").checked },
    photos: {
      source,
      intervalSeconds: Math.max(5, Math.round(Number(val("p-interval")) || 30)),
      motion: val("p-motion") || "off",
      pairPortraits: $<HTMLInputElement>("p-pairs").checked,
      onThisDay: $<HTMLInputElement>("p-today").checked,
      videos: $<HTMLInputElement>("p-videos").checked,
      map: $<HTMLInputElement>("p-map").checked,
      skipOdd: $<HTMLInputElement>("p-odd").checked,
      art: $<HTMLInputElement>("p-art").checked,
      synology: {
        url: val("s-url").trim(),
        username: val("s-user").trim(),
        password: val("s-pass"),
        insecureTls: $<HTMLInputElement>("s-tls").checked,
        quality: val("s-quality"),
      },
      immich: { url: val("i-url").trim(), apiKey: val("i-key").trim(), insecureTls: $<HTMLInputElement>("i-tls").checked, quality: val("i-quality") },
      share: { host: val("sh-host").trim(), name: val("sh-name").trim(), folder: val("sh-folder").trim(), username: val("sh-user").trim(), password: val("sh-pass") },
    },
    // Night mode needs both times; "Off" in the first switches it off.
    screen: {
      ...(val("n-start") ? { nightStart: val("n-start"), nightEnd: val("n-end") || "07:00" } : { nightStart: "", nightEnd: "" }),
      language: val("d-language"),
      clockPosition: val("d-position"),
      clockSize: val("d-size"),
      clockStyle: val("d-style"),
      eveningDim: val("d-dim"),
      caption: val("d-caption"),
      weatherDetail: val("d-weather"),
      lockView: $<HTMLInputElement>("d-lock").checked,
      background: val("d-background"),
      performance: val("d-performance"),
      tiles: val("d-tiles"),
      backgroundFps: Number(val("d-fps")) || 0,
      sounds: $<HTMLInputElement>("d-sounds").checked,
    },
    apps: apps.map(({ detail: _detail, missing: _missing, ...rest }) => rest),
    calendar: { links: calendars.map((l) => (l.url ? { url: l.url } : { key: l.key })) },
    departures: { stop: val("dep-stop").trim(), walk: Number(val("dep-walk")) || 0 },
    home: { url: val("h-url").trim(), token: val("h-token").trim(), entities: homeTiles },
    hiddenTiles: ownTiles.filter((b) => !b.checked).map((b) => b.dataset.tile ?? ""),
    system: {
      autoUpdate: $<HTMLInputElement>("sys-auto").checked,
      cooling: $<HTMLInputElement>("sys-cool").checked,
      soundFollows: $<HTMLInputElement>("snd-follow").checked,
      spotifySpeaker: $<HTMLInputElement>("spk-spotify").checked,
      airplaySpeaker: $<HTMLInputElement>("spk-airplay").checked,
      speakerName: val("spk-name").trim(),
      resolution: val("sys-resolution"),
      idleOffMinutes: Number(val("tv-idle")) || 0,
      bluetoothOff: $<HTMLInputElement>("bt-off").checked,
      games: $<HTMLInputElement>("sys-games").checked,
      gamesPage: $<HTMLInputElement>("sys-games-page").checked,
      gamesOnController: $<HTMLInputElement>("sys-games-pad").checked,
    },
    // An empty password keeps the one that is stored.
    wifi: { name: val("wf-name").trim(), password: val("wf-pass"), open: $<HTMLInputElement>("wf-open").checked, hidden: $<HTMLInputElement>("wf-hidden").checked },
    tvInputs,
    videos: { source: val("v-source"), folders: val("v-folders").split(";").map((f) => f.trim()).filter(Boolean) },
    // Empty leaves the PIN as it is.
    lock: { pin: val("l-pin").trim(), remove: removeLock },
    radio: stations.map(({ name, url }) => ({ name, url })),
  };
}

function refresh() {
  const source = val("p-source");
  $("p-synology").hidden = source !== "synology";
  $("p-immich").hidden = source !== "immich";
  $("p-share").hidden = source !== "share";
  $("p-folder").hidden = source !== "folder";
  $("p-interval-row").hidden = source === "";
  $("p-motion-row").hidden = source === "";
  $("p-pairs-row").hidden = source === "";
  $("p-today-row").hidden = source === "";
  $("p-videos-row").hidden = source === "";
  $<HTMLSelectElement>("n-end").disabled = val("n-start") === "";
  const motion = val("p-motion");
  $("p-motion-hint").hidden = source === "" || motion === "off";
  $("p-motion-hint").textContent =
    motion === "whole"
      ? "Photos are shown a little smaller, on a soft blurred copy of themselves, so nothing is cut off while they move. They are prepared again after saving, which takes a moment per photo."
      : "Photos fill the screen as far as they can. While they move, up to about a tenth along the edges can be out of view.";
  const dirty = JSON.stringify(readForm()) !== savedState;
  // Folders can be chosen once the source in use is the one shown here.
  $("p-folders").hidden = source === "" || source !== info.photos.source || dirty;
  saveBtn.disabled = !dirty || busy;
  saveBtn.textContent = busy ? "Saving…" : "Save";
  summary.textContent = dirty ? "Changes not saved yet" : "";
}

function fill(i: Info) {
  info = i;
  $<HTMLInputElement>("w-location").value = i.weather.location;
  $<HTMLSelectElement>("w-units").value = i.weather.units || "metric";
  $<HTMLInputElement>("w-radar").checked = i.weather.radar ?? true;
  $("w-coords").hidden = !i.weather.coordinates;

  const source = $<HTMLSelectElement>("p-source");
  source.replaceChildren(
    new Option("No photos", ""),
    new Option("Synology Photos on my NAS", "synology"),
    new Option("Immich on my server", "immich"),
    new Option("A shared folder on the network", "share"),
  );
  if (i.photos.folderPath) {
    source.add(new Option(t("Folder {path}", { path: i.photos.folderPath }), "folder"));
    $("p-folder").textContent = t("The folder is set in config.toml on the Pi: {path}", { path: i.photos.folderPath });
  }
  source.value = i.photos.source;
  $<HTMLInputElement>("s-url").value = i.photos.synology.url;
  $<HTMLInputElement>("s-user").value = i.photos.synology.username;
  const pass = $<HTMLInputElement>("s-pass");
  pass.value = "";
  pass.placeholder = i.photos.synology.passwordSet ? "Saved. Leave empty to keep it" : "";
  $<HTMLInputElement>("s-tls").checked = i.photos.synology.insecureTls;
  $<HTMLSelectElement>("s-quality").value = i.photos.synology.quality || "original";
  $<HTMLInputElement>("i-url").value = i.photos.immich?.url ?? "";
  const key = $<HTMLInputElement>("i-key");
  key.value = "";
  key.placeholder = i.photos.immich?.keySet ? "Saved. Leave empty to keep it" : "";
  $<HTMLInputElement>("i-tls").checked = !!i.photos.immich?.insecureTls;
  $<HTMLSelectElement>("i-quality").value = i.photos.immich?.quality || "original";
  $<HTMLInputElement>("sh-host").value = i.photos.share?.host ?? "";
  $<HTMLInputElement>("sh-name").value = i.photos.share?.name ?? "";
  $<HTMLInputElement>("sh-folder").value = i.photos.share?.folder ?? "";
  $<HTMLInputElement>("sh-user").value = i.photos.share?.username ?? "";
  const sharePass = $<HTMLInputElement>("sh-pass");
  sharePass.value = "";
  sharePass.placeholder = i.photos.share?.passwordSet ? "Saved. Leave empty to keep it" : "";
  $<HTMLInputElement>("p-interval").value = String(i.photos.intervalSeconds || 30);
  $<HTMLSelectElement>("p-motion").value = i.photos.motion || "off";
  $<HTMLInputElement>("p-pairs").checked = !!i.photos.pairPortraits;
  $<HTMLInputElement>("p-today").checked = !!i.photos.onThisDay;
  $<HTMLInputElement>("p-map").checked = !!i.photos.map;
  $<HTMLInputElement>("p-odd").checked = i.photos.skipOdd ?? true;
  $<HTMLInputElement>("p-art").checked = i.photos.art ?? true;
  $<HTMLInputElement>("p-videos").checked = !!i.photos.videos;
  const language = $<HTMLSelectElement>("d-language");
  language.replaceChildren(...Object.entries(LANGUAGES).map(([code, name]) => new Option(name, code)));
  language.value = i.screen.language || "en";
  setLanguage(language.value);
  $<HTMLSelectElement>("d-position").value = i.screen.clockPosition || "bottom-left";
  $<HTMLSelectElement>("d-size").value = i.screen.clockSize || "large";
  $<HTMLSelectElement>("d-style").value = i.screen.clockStyle || "bold";
  // Settings from before these existed said it with one word, "view".
  $<HTMLSelectElement>("d-caption").value = i.screen.caption || (i.screen.view === "details" ? "on" : "off");
  $<HTMLSelectElement>("d-weather").value = i.screen.weatherDetail || (i.screen.view === "details" ? "full" : "short");
  $<HTMLInputElement>("d-lock").checked = !!i.screen.lockView;
  $<HTMLSelectElement>("d-background").value = i.screen.background || "moving";
  $<HTMLSelectElement>("d-performance").value = i.screen.performance || "balanced";
  $<HTMLSelectElement>("d-tiles").value = i.screen.tiles || "glow";
  const fps = $<HTMLSelectElement>("d-fps");
  const rate = String(i.screen.backgroundFps ?? 0);
  // A rate set by hand in config.toml that is not on the list is offered as it is.
  if (![...fps.options].some((o) => o.value === rate)) fps.add(new Option(rate, rate));
  fps.value = rate;
  describePerformance();
  $<HTMLInputElement>("d-sounds").checked = i.screen.sounds ?? true;
  setSounds(i.screen.sounds ?? true);
  fillTimes($<HTMLSelectElement>("n-start"), i.screen.nightStart, "Off", "start");
  fillTimes($<HTMLSelectElement>("n-end"), i.screen.nightEnd || "07:00", undefined, "end");
  $<HTMLSelectElement>("d-dim").value = i.screen.eveningDim || "off";
  void showHidden();

  apps = i.apps.map((a) => ({ ...a }));
  renderApps();

  calendars = i.calendar.links.map((l) => ({ ...l }));
  $<HTMLInputElement>("dep-stop").value = i.departures?.stop ?? "";
  const walk = $<HTMLSelectElement>("dep-walk");
  const toStop = String(i.departures?.walk ?? 0);
  // A number set by hand in config.toml that is not on the list is offered as it is.
  if (![...walk.options].some((o) => o.value === toStop)) walk.add(new Option(toStop, toStop));
  walk.value = toStop;
  renderCalendars();
  $<HTMLInputElement>("h-url").value = i.home.url;
  const token = $<HTMLInputElement>("h-token");
  token.value = "";
  token.placeholder = i.home.tokenSet ? "Saved. Leave empty to keep it" : "";
  homeTiles = [...i.home.entities];
  renderHomeTiles();
  $<HTMLInputElement>("sys-auto").checked = i.system.autoUpdate;
  $<HTMLInputElement>("sys-cool").checked = i.system.cooling ?? true;
  $<HTMLInputElement>("snd-follow").checked = i.system.soundFollows ?? true;
  $<HTMLInputElement>("spk-spotify").checked = !!i.system.spotifySpeaker;
  $<HTMLInputElement>("spk-airplay").checked = !!i.system.airplaySpeaker;
  $<HTMLInputElement>("spk-name").value = i.system.speakerName ?? "";
  void showSpeakers();
  $<HTMLSelectElement>("sys-resolution").value = i.system.resolution || "desktop";
  const idle = $<HTMLSelectElement>("tv-idle");
  const minutes = String(i.system.idleOffMinutes ?? 0);
  // A time set by hand in config.toml that is not on the list is offered as it is.
  if (![...idle.options].some((o) => o.value === minutes)) idle.add(new Option(t("{n} minutes", { n: minutes }), minutes));
  idle.value = minutes;
  $<HTMLInputElement>("bt-off").checked = i.system.bluetoothOff ?? true;
  for (const box of ownTiles) box.checked = !(i.hiddenTiles ?? []).includes(box.dataset.tile ?? "");
  $<HTMLInputElement>("sys-games").checked = i.system.games ?? true;
  $<HTMLInputElement>("sys-games-page").checked = i.system.gamesPage ?? true;
  $<HTMLInputElement>("sys-games-pad").checked = i.system.gamesOnController ?? true;
  stations = i.radio.map((s) => ({ ...s }));
  tvInputs = (i.tvInputs ?? []).map((x) => ({ ...x }));
  renderTVInputs();
  renderStations();
  removeLock = false;
  $<HTMLSelectElement>("v-source").value = i.videos?.source ?? "";
  $<HTMLInputElement>("v-folders").value = (i.videos?.folders ?? []).join("; ");
  showVideoFields();
  $<HTMLInputElement>("wf-name").value = i.wifi?.name ?? "";
  const wifiPass = $<HTMLInputElement>("wf-pass");
  wifiPass.value = "";
  wifiPass.placeholder = i.wifi?.passwordSet ? "A password is set. Leave empty to keep it" : "";
  $<HTMLInputElement>("wf-open").checked = !!i.wifi?.open;
  $<HTMLInputElement>("wf-hidden").checked = !!i.wifi?.hidden;
  $("wf-pass-row").hidden = !!i.wifi?.open;
  const lockPin = $<HTMLInputElement>("l-pin");
  lockPin.value = "";
  lockPin.placeholder = i.lock.set ? "A PIN is set. Leave empty to keep it" : "";
  $("l-remove").hidden = !i.lock.set;
  $("l-remove").textContent = "Remove the lock";
  void loadUpdate();

  const kb = $("kb-problem");
  kb.hidden = !i.keyboardProblem;
  kb.textContent = i.keyboardProblem
    ? t("The TV remote's OK and Back buttons cannot be passed on to apps yet: {why}", { why: i.keyboardProblem })
    : "";
  $("version").textContent = i.version ? `PiDash ${i.version}` : "";

  savedState = JSON.stringify(readForm());
  refresh();
}

$("l-remove").addEventListener("click", () => {
  removeLock = !removeLock;
  $("l-remove").textContent = removeLock ? "The lock will be removed when you save" : "Remove the lock";
  refresh();
});

// --- Radio ----------------------------------------------------------------------------------

function renderStations() {
  $("r-list").replaceChildren(
    ...stations.map((s, i) =>
      row(s.name, "", () => {
        stations.splice(i, 1);
        renderStations();
      }),
    ),
  );
  const free = stationsFound.filter((f) => !stations.some((s) => s.url === f.url));
  $<HTMLSelectElement>("r-pick").replaceChildren(...free.map((f) => new Option(f.detail ? `${f.name} (${f.detail})` : f.name, f.url)));
  $("r-add").hidden = free.length === 0;
}

$("r-search").addEventListener("click", async () => {
  const out = $("r-result");
  setResult(out, "Searching…");
  try {
    const res = await fetch(`/api/radio/search?q=${encodeURIComponent(val("r-query"))}`);
    if (!res.ok) return setResult(out, (await res.text()).trim(), "error");
    stationsFound = (await res.json()).stations;
    setResult(out, stationsFound.length ? t("{n} stations found.", { n: stationsFound.length }) : "No station with that name was found.", stationsFound.length ? "ok" : "error");
    renderStations();
  } catch {
    setResult(out, "The Pi did not answer.", "error");
  }
});

$("r-add-btn").addEventListener("click", () => {
  const found = stationsFound.find((f) => f.url === val("r-pick"));
  if (!found) return;
  if (stations.length >= 12) return setResult($("r-result"), "The app row has room for 12 stations.", "error");
  stations.push({ name: found.name, url: found.url });
  renderStations();
  refresh();
});

// --- Other devices on the TV --------------------------------------------------------------------

/** Where a device is plugged in, in words: "HDMI 2", or its address behind a receiver. */
const socket = (x: TVInput) => (x.address ? x.address : `HDMI ${x.port ?? "?"}`);

function renderTVInputs() {
  $("ti-list").replaceChildren(
    ...tvInputs.map((x, i) =>
      row(x.name, socket(x), () => {
        tvInputs.splice(i, 1);
        renderTVInputs();
      }),
    ),
  );
}

function addTVInput(input: TVInput) {
  const out = $("ti-result");
  if (!input.name.trim()) return setResult(out, "Give it a name, like PlayStation.", "error");
  if (tvInputs.length >= 8) return setResult(out, "The app row has room for 8 devices.", "error");
  if (tvInputs.some((x) => socket(x) === socket(input))) return setResult(out, t("{where} already has a tile.", { where: socket(input) }), "error");
  tvInputs.push({ ...input, name: input.name.trim() });
  setResult(out, "");
  renderTVInputs();
  refresh();
}

$("ti-add").addEventListener("click", () => {
  addTVInput({ name: val("ti-name"), port: Number(val("ti-port")) });
  $<HTMLInputElement>("ti-name").value = "";
});

// The TV's own list of what is plugged in: every device that answers gets a tile.
$("ti-find").addEventListener("click", async () => {
  const out = $("ti-result");
  setResult(out, "Asking the TV. This takes a few seconds…");
  try {
    const res = await send("POST", "/api/tv/devices", {});
    if (!res.ok) return setResult(out, (await res.text()).trim(), "error");
    const found: { address: string; name: string }[] = (await res.json()).devices;
    let added = 0;
    for (const d of found) {
      // "2.0.0.0" is the TV's second socket; anything else sits behind a receiver.
      const port = /^[1-9a-f]\.0\.0\.0$/.test(d.address) ? parseInt(d.address, 16) : 0;
      const input: TVInput = port ? { name: d.name || `HDMI ${port}`, port } : { name: d.name || d.address, address: d.address };
      if (tvInputs.length >= 8 || tvInputs.some((x) => socket(x) === socket(input))) continue;
      tvInputs.push(input);
      added++;
    }
    renderTVInputs();
    if (added > 0) refresh();
    setResult(
      out,
      found.length === 0 ? "No other device answered. One that is switched off at the wall cannot; add it by hand above." : t(added === 1 ? "1 device added. Remove what you do not want, then save." : "{n} devices added. Remove what you do not want, then save.", { n: added }),
      found.length === 0 ? "error" : "ok",
    );
  } catch {
    setResult(out, "The Pi did not answer.", "error");
  }
});

// --- Backup -----------------------------------------------------------------------------------

$("b-buttons").hidden = onTV;
$("b-tv").hidden = !onTV;

$("b-save").addEventListener("click", async () => {
  const out = $("b-result");
  try {
    const res = await send("POST", "/api/settings/backup", {});
    if (!res.ok) return setResult(out, res.status === 401 ? "The PIN is needed for a backup." : (await res.text()).trim(), "error");
    const link = document.createElement("a");
    link.href = URL.createObjectURL(await res.blob());
    link.download = "pidash-backup.json";
    link.click();
    URL.revokeObjectURL(link.href);
    setResult(out, "Saved as pidash-backup.json.", "ok");
  } catch {
    setResult(out, "The Pi did not answer.", "error");
  }
});

$("b-load").addEventListener("click", () => $<HTMLInputElement>("b-file").click());
$("b-file").addEventListener("change", async () => {
  const out = $("b-result");
  const file = $<HTMLInputElement>("b-file").files?.[0];
  $<HTMLInputElement>("b-file").value = "";
  if (!file) return;
  let content: unknown;
  try {
    content = JSON.parse(await file.text());
  } catch {
    return setResult(out, "This is not a PiDash backup file.", "error");
  }
  if (!window.confirm(t("Replace all settings on this Pi with those from {file}?", { file: file.name }))) return;
  try {
    const before = await startedAt();
    const res = await send("POST", "/api/settings/restore", content);
    if (!res.ok) return setResult(out, res.status === 401 ? "The PIN is needed to load a backup." : (await res.text()).trim(), "error");
    setResult(out, "Loaded. Applying…");
    for (let i = 0; i < 40; i++) {
      await new Promise((r) => setTimeout(r, 400));
      try {
        if ((await startedAt()) !== before) break;
      } catch {
        /* restarting */
      }
    }
    await load();
    setResult($("b-result"), "The backup is loaded.", "ok");
  } catch {
    setResult(out, "The Pi did not answer.", "error");
  }
});

// --- The Pi as a speaker for phones ---------------------------------------------------------

interface SpeakerState {
  on: boolean;
  installed: boolean;
  running: boolean;
  problem?: string;
}

/** Says under each of the two kinds how it is doing: running, not installed, or what went wrong. */
async function showSpeakers() {
  let status: { name: string; spotify: SpeakerState; airplay: SpeakerState } | null = null;
  try {
    const res = await fetch("/api/speakers", { cache: "no-store" });
    if (res.ok) status = await res.json();
  } catch {
    /* said below */
  }
  const say = (id: string, st: SpeakerState | undefined, program: string) => {
    const out = $(id);
    let text = "";
    if (!st) text = "";
    else if (!st.installed) text = t("Its program, {program}, is not on the Pi yet. The guide says how to install it: Speaker for phones.", { program });
    else if (st.on && st.running) text = t("Running, as “{name}”.", { name: status?.name ?? "" });
    else if (st.on) text = t("Not running: {problem}", { problem: st.problem || t("it is being started") });
    out.textContent = text;
    out.hidden = text === "";
    out.dataset.noTranslate = "";
  };
  say("spk-spotify-state", status?.spotify, "librespot");
  say("spk-airplay-state", status?.airplay, "shairport-sync");
}

// --- Updates -----------------------------------------------------------------------------

interface UpdateStatus {
  current: string;
  latest?: string;
  available: boolean;
  possible: boolean;
  reason?: string;
  installing: boolean;
  error?: string;
}

function showUpdate(st: UpdateStatus) {
  let text = t("This is PiDash {version}.", { version: st.current });
  if (!st.possible) text += " " + t(st.reason ?? "");
  else if (st.installing) text += " " + t("An update is being installed; PiDash restarts by itself.");
  else if (st.available) text += " " + t("{version} is available.", { version: st.latest ?? "" });
  else if (st.latest) text += " " + t("That is the newest version.");
  $("up-status").textContent = text;
  for (const id of ["up-auto-row", "up-auto-hint", "up-buttons"]) $(id).hidden = !st.possible;
  $("up-install").hidden = !st.available || st.installing;
  setResult($("up-result"), st.error ?? "", st.error ? "error" : "");
}

async function loadUpdate() {
  try {
    const res = await fetch("/api/update", { cache: "no-store" });
    if (res.ok) showUpdate(await res.json());
  } catch {
    /* the card keeps its last text */
  }
}

$("up-check").addEventListener("click", async () => {
  setResult($("up-result"), "Looking…");
  try {
    const res = await send("POST", "/api/update/check", {});
    if (res.ok) showUpdate(await res.json());
  } catch {
    setResult($("up-result"), "The Pi did not answer.", "error");
  }
});

$("up-install").addEventListener("click", async () => {
  setResult($("up-result"), "Starting the update…");
  try {
    const res = await send("POST", "/api/update/install", {});
    if (!res.ok) return setResult($("up-result"), (await res.text()).trim(), "error");
    showUpdate(await res.json());
    // The page comes back by itself once the new version runs.
    const before = await startedAt();
    for (let i = 0; i < 150; i++) {
      await new Promise((r) => setTimeout(r, 2000));
      try {
        if ((await startedAt()) !== before) return location.reload();
      } catch {
        /* restarting */
      }
    }
  } catch {
    setResult($("up-result"), "The Pi did not answer.", "error");
  }
});

// --- Calendar and smart home ----------------------------------------------------------

/** One row of a list: a text, a small second line, and a button to remove it. */
function row(text: string, small: string, remove: () => void): HTMLLIElement {
  const li = document.createElement("li");
  const what = document.createElement("div");
  what.className = "what";
  what.textContent = text;
  if (small) {
    const s = document.createElement("small");
    s.textContent = small;
    what.append(s);
  }
  const b = document.createElement("button");
  b.type = "button";
  b.textContent = "✕";
  b.title = "Remove";
  b.setAttribute("aria-label", t("Remove {name}", { name: text }));
  b.addEventListener("click", () => {
    remove();
    refresh();
  });
  li.append(what, b);
  return li;
}

function renderCalendars() {
  $("cal-list").replaceChildren(
    ...calendars.map((l, i) =>
      row(l.host ?? hostOf(l.url ?? ""), l.url ? "Not saved yet" : "", () => {
        calendars.splice(i, 1);
        renderCalendars();
      }),
    ),
  );
}

function hostOf(link: string): string {
  try {
    return new URL(link.replace(/^webcal:/i, "https:")).host;
  } catch {
    return link;
  }
}

$("dep-test").addEventListener("click", async () => {
  const out = $("dep-result");
  setResult(out, "Looking it up…");
  try {
    const res = await send("POST", "/api/settings/test-departures", { stop: val("dep-stop") });
    if (!res.ok) return setResult(out, (await res.text()).trim(), "error");
    // The name as the timetable has it is what is saved: that is what it answers to.
    const name: string = (await res.json()).stop;
    $<HTMLInputElement>("dep-stop").value = name;
    setResult(out, t("Found: {place}", { place: name }), "ok");
    refresh();
  } catch {
    setResult(out, "The Pi did not answer.", "error");
  }
});

$("cal-test").addEventListener("click", async () => {
  const out = $("cal-result");
  setResult(out, "Reading the calendar…");
  try {
    const res = await send("POST", "/api/settings/test-calendar", { url: val("cal-url") });
    if (!res.ok) return setResult(out, (await res.text()).trim(), "error");
    const r: { events: number; days: number } = await res.json();
    setResult(out, t(r.events === 1 ? "Works: 1 event in the next {days} days." : "Works: {n} events in the next {days} days.", { n: r.events, days: r.days }), "ok");
  } catch {
    setResult(out, "The Pi did not answer.", "error");
  }
});

$("cal-add").addEventListener("click", () => {
  const link = val("cal-url").trim();
  if (!/^(https?|webcal):\/\/\S+/i.test(link)) return setResult($("cal-result"), "Enter the calendar's link, starting with https:// or webcal://", "error");
  calendars.push({ url: link });
  $<HTMLInputElement>("cal-url").value = "";
  setResult($("cal-result"), "");
  renderCalendars();
  refresh();
});

const homeName = (id: string) => homeKnown.find((e) => e.id === id);

function renderHomeTiles() {
  $("h-list").replaceChildren(
    ...homeTiles.map((id, i) =>
      row(homeName(id)?.name ?? id, homeName(id) ? id : "", () => {
        homeTiles.splice(i, 1);
        renderHomeTiles();
      }),
    ),
  );
  const pick = $<HTMLSelectElement>("h-pick");
  const free = homeKnown.filter((e) => !homeTiles.includes(e.id));
  pick.replaceChildren(...free.map((e) => new Option(e.text ? `${e.name} (${e.text})` : e.name, e.id)));
  $("h-add").hidden = free.length === 0;
}

$("h-connect").addEventListener("click", async () => {
  const out = $("h-result");
  setResult(out, "Connecting to Home Assistant…");
  try {
    const res = await send("POST", "/api/settings/home-entities", { url: val("h-url").trim(), token: val("h-token").trim() });
    if (!res.ok) return setResult(out, (await res.text()).trim(), "error");
    homeKnown = (await res.json()).entities;
    setResult(out, t("Connected. {n} devices and values can be shown.", { n: homeKnown.length }), "ok");
    renderHomeTiles();
  } catch {
    setResult(out, "The Pi did not answer.", "error");
  }
});

$("h-add-btn").addEventListener("click", () => {
  const id = val("h-pick");
  if (!id || homeTiles.includes(id)) return;
  if (homeTiles.length >= 16) return setResult($("h-result"), "The app row has room for 16 of these.", "error");
  homeTiles.push(id);
  renderHomeTiles();
  refresh();
});

// --- Night hours and hidden photos ------------------------------------------------

/** Fills a list with the times of day in half-hour steps, keeping an odd saved time. */
/** Times that follow the sun, offered before the times of the clock. */
const SUN_TIMES: Record<"start" | "end", [string, string][]> = {
  start: [
    ["sunset", "At sunset"],
    ["sunset+60", "1 hour after sunset"],
    ["sunset+120", "2 hours after sunset"],
    ["sunset+180", "3 hours after sunset"],
  ],
  end: [
    ["sunrise-60", "1 hour before sunrise"],
    ["sunrise", "At sunrise"],
    ["sunrise+60", "1 hour after sunrise"],
  ],
};

function fillTimes(sel: HTMLSelectElement, value: string, off?: string, sun?: "start" | "end") {
  const times: string[] = [];
  for (let m = 0; m < 24 * 60; m += 30) times.push(`${String(Math.floor(m / 60)).padStart(2, "0")}:${String(m % 60).padStart(2, "0")}`);
  const padded = value && /^\d:\d\d$/.test(value) ? "0" + value : value;
  const sunTimes = sun ? SUN_TIMES[sun] : [];
  // A time set by hand in config.toml that is not on the list, such as
  // "sunset+45", is offered as it is.
  const known = times.includes(padded) || sunTimes.some(([v]) => v === padded);
  if (padded && !known && /^\d\d:\d\d$/.test(padded)) times.push(padded);
  times.sort();
  sel.replaceChildren(
    ...(off ? [new Option(off, "")] : []),
    ...sunTimes.map(([v, label]) => new Option(label, v)),
    ...(padded && !known && !/^\d\d:\d\d$/.test(padded) ? [new Option(padded, padded)] : []),
    ...times.map((t) => new Option(t, t)),
  );
  sel.value = padded;
}

async function showHidden() {
  try {
    const st: { hidden?: number } = await (await fetch("/api/photos", { cache: "no-store" })).json();
    const n = st.hidden ?? 0;
    $("p-hidden-row").hidden = n === 0;
    $("p-hidden").textContent = n === 1 ? "1 photo is hidden from the slideshow." : t("{n} photos are hidden from the slideshow.", { n });
  } catch {
    $("p-hidden-row").hidden = true;
  }
}

$("p-unhide").addEventListener("click", async () => {
  await send("POST", "/api/photos/unhide", {});
  await showHidden();
});

// --- Apps -----------------------------------------------------------------------

const presetOf = (a: AppEntry) => info.presets.find((p) => p.id === a.preset);

function appDetail(a: AppEntry): string {
  if (a.kind === "preset") {
    const p = presetOf(a);
    if (p?.needsUrl) return a.url ?? "";
    return p?.description ?? "";
  }
  if (a.kind === "web") return (a.url ?? "") + (a.adblock ? "  ·  ads blocked" : a.arrows ? "  ·  " + t("arrows move around the page") : "");
  if (a.kind === "desktop") {
    if (a.missing) return "Not installed on the Pi any more. Its tile is hidden until it is back.";
    return info.installed.find((e) => e.id === a.desktop)?.comment || "Installed on the Pi";
  }
  return t("From config.toml: {what}", { what: a.detail ?? a.id ?? "" });
}

function renderApps() {
  const list = $("app-list");
  list.replaceChildren(
    ...apps.map((a, i) => {
      const li = document.createElement("li");
      const what = document.createElement("div");
      what.className = "what";
      const name = document.createElement("div");
      name.className = "name";
      name.textContent = a.name || presetOf(a)?.name || a.id || "";
      const detail = document.createElement("div");
      detail.className = "detail";
      detail.textContent = appDetail(a);
      what.append(name, detail);

      const button = (label: string, title: string, disabled: boolean, act: () => void) => {
        const b = document.createElement("button");
        b.type = "button";
        b.textContent = label;
        b.title = title;
        b.setAttribute("aria-label", `${t(title)}: ${name.textContent}`);
        b.disabled = disabled;
        b.addEventListener("click", () => {
          act();
          renderApps();
          refresh();
        });
        return b;
      };
      // Websites can be drawn larger, for a 4K screen seen from the sofa.
      const isWeb = a.kind === "web" || (a.kind === "preset" && !!presetOf(a)?.web);
      if (isWeb) {
        const size = document.createElement("select");
        size.className = "size";
        size.title = "Size";
        size.setAttribute("aria-label", t("Size of {name}", { name: name.textContent ?? "" }));
        for (const z of [1, 1.25, 1.5, 1.75, 2, 2.5, 3]) size.add(new Option(`${Math.round(z * 100)}%`, String(z)));
        size.value = String(a.zoom && a.zoom !== 1 ? a.zoom : 1);
        size.addEventListener("change", () => {
          const z = Number(size.value);
          if (z === 1) delete a.zoom;
          else a.zoom = z;
          refresh();
        });
        li.append(what, size);
      } else {
        li.append(what);
      }
      li.append(
        button("↑", "Move up", i === 0, () => apps.splice(i - 1, 0, ...apps.splice(i, 1))),
        button("↓", "Move down", i === apps.length - 1, () => apps.splice(i + 1, 0, ...apps.splice(i, 1))),
        button("✕", "Remove", false, () => apps.splice(i, 1)),
      );
      return li;
    }),
  );
  renderAddChoices();
}

const OTHER = "__website";
const INSTALLED = "__installed";
/** Followed by the emulator's id. */
const EMULATOR = "__emulator:";

/** What is being added, where that takes more than choosing it: one of the above, or a ready-made app's id. */
let addChoice = "";

const GLOBE = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.800 3 2.800 15 0 18M12 3c-2.800 3-2.800 15 0 18"/></svg>';
const WINDOW = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4.500" width="18" height="15" rx="2.500"/><path d="M3 9h18"/></svg>';
const PAD = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.500" stroke-linecap="round" stroke-linejoin="round"><path d="M7 8h10a4.500 4.500 0 0 1 4.400 5.500l-.600 2.800a2.300 2.300 0 0 1-4 1L15 15.500H9l-1.800 1.800a2.300 2.300 0 0 1-4-1l-.600-2.800A4.500 4.500 0 0 1 7 8z"/><path d="M8 11v3M6.500 12.500h3"/><circle cx="16" cy="12.500" r="0.800" fill="currentColor"/></svg>';

/** One thing that can be added: a card with its picture and its name. */
function pick(id: string, name: string, art: { icon?: string; glyph?: string }, opts: { sub?: string; added?: boolean; about?: string } = {}): HTMLButtonElement {
  const b = document.createElement("button");
  b.type = "button";
  b.className = "pick";
  b.dataset.add = id;
  b.dataset.noTranslate = ""; // names of apps stay as they are; what is beside them is translated here
  const picture = document.createElement("span");
  picture.className = "pick-art";
  const letter = () => {
    picture.classList.add("bare");
    picture.textContent = name.charAt(0);
  };
  if (art.glyph) picture.innerHTML = art.glyph;
  else if (art.icon) {
    const img = document.createElement("img");
    img.alt = "";
    img.loading = "lazy";
    // A program that is not installed has no picture yet: its first letter.
    img.addEventListener("error", letter);
    img.src = art.icon;
    picture.append(img);
  } else letter();
  const label = document.createElement("span");
  label.className = "pick-name";
  label.textContent = name;
  b.append(picture, label);
  if (opts.sub) {
    const sub = document.createElement("small");
    sub.textContent = opts.sub;
    b.append(sub);
  }
  if (opts.about) b.title = opts.about;
  if (opts.added) {
    b.disabled = true;
    b.classList.add("added");
    b.setAttribute("aria-label", `${name}: ${t("in the list")}`);
    const tick = document.createElement("span");
    tick.className = "pick-tick";
    tick.textContent = "✓";
    b.append(tick);
  }
  b.addEventListener("click", () => chooseToAdd(id));
  return b;
}

/** Whether a ready-made app can be added with one press: nothing to enter, nothing to be told first. */
const addsAtOnce = (p: Preset) => !p.needsUrl && !(p.console && !info.consoleReady);

function renderAddChoices() {
  const grid = $("add-grid");
  // The card that had the highlight keeps it, or hands it to its neighbour.
  const had = grid.contains(document.activeElement) ? (document.activeElement as HTMLElement).dataset.add : undefined;
  const used = new Set(apps.filter((a) => a.kind === "preset").map((a) => a.preset));
  // Emulators are listed whether installed or not: choosing one that is not
  // says how to install it.
  const inRow = new Set(apps.filter((a) => a.kind === "desktop").map((a) => a.desktop));
  const section = (title: string, cards: HTMLElement[]) => {
    if (cards.length === 0) return [];
    const h = document.createElement("h4");
    h.textContent = t(title);
    const row = document.createElement("div");
    row.className = "picks";
    row.append(...cards);
    return [h, row];
  };
  const ready = (group: string) =>
    info.presets
      .filter((p) => (p.group || "own") === group)
      .map((p) => pick(p.id, p.name, { icon: `/api/presets/${encodeURIComponent(p.id)}/icon` }, { added: used.has(p.id), about: t(p.description) }));
  const emulators = (info.emulators ?? []).map((em) =>
    pick(EMULATOR + em.id, em.name, { glyph: PAD }, { sub: t(em.systems), added: !!em.desktop && inRow.has(em.desktop), about: t(em.description) }),
  );
  grid.replaceChildren(
    ...section("Films, series and TV", ready("video")),
    ...section("Music", ready("music")),
    ...section("Games", [...ready("games"), ...emulators]),
    ...section("From your own server", ready("own")),
    ...section("Something else", [
      pick(OTHER, t("Another website"), { glyph: GLOBE }, { about: t("Opens the address fullscreen, with its own login.") }),
      pick(INSTALLED, t("A program installed on the Pi"), { glyph: WINDOW }),
    ]),
  );
  if (had) {
    const cards = [...grid.querySelectorAll<HTMLButtonElement>(".pick")];
    const at = cards.findIndex((c) => c.dataset.add === had);
    const next = [...cards.slice(Math.max(0, at)), ...cards.slice(0, Math.max(0, at)).reverse()].find((c) => !c.disabled);
    if (next) {
      if (onTV) highlight(next);
      else next.focus({ preventScroll: true });
    }
  }

  // Installed programs that are not in the list yet. Two with the same name
  // (say, a system and a Flatpak copy) are told apart by their entry's id.
  const program = $<HTMLSelectElement>("add-installed");
  const previousProgram = program.value;
  const added = new Set(apps.filter((a) => a.kind === "desktop").map((a) => a.desktop));
  const free = info.installed.filter((e) => !added.has(e.id));
  const twice = new Set(free.map((e) => e.name).filter((n, i, all) => all.indexOf(n) !== i));
  program.replaceChildren(...free.map((e) => new Option(twice.has(e.name) ? `${e.name} (${e.id})` : e.name, e.id)));
  if ([...program.options].some((o) => o.value === previousProgram)) program.value = previousProgram;
  showAddFields();
}

/** A card was pressed: add it at once where that is all there is to it, otherwise ask for the rest. */
function chooseToAdd(id: string) {
  const p = info.presets.find((x) => x.id === id);
  if (p && addsAtOnce(p)) {
    addChoice = "";
    apps.push({ kind: "preset", preset: p.id, name: p.name });
    renderApps();
    refresh();
    setResult($("add-result"), `${t("Added: {name}.", { name: p.name })} ${t(p.description)}.`, "ok");
    return;
  }
  addChoice = id;
  showAddFields();
  setResult($("add-result"), "");
  // On to what is asked for: the first thing to fill in, or the button.
  const first = [...$("add-detail").querySelectorAll<HTMLElement>("input, select, button")].find((el) => el.getClientRects().length > 0);
  if (first) {
    if (onTV) highlight(first);
    else {
      first.scrollIntoView({ block: "center", behavior: "smooth" });
      first.focus({ preventScroll: true });
    }
  }
}

function showAddFields() {
  const choice = addChoice;
  for (const card of $("add-grid").querySelectorAll<HTMLElement>(".pick")) card.classList.toggle("chosen", card.dataset.add === choice);
  $("add-detail").hidden = choice === "";
  if (choice === "") return;
  const p = info.presets.find((x) => x.id === choice);
  const other = choice === OTHER;
  const installed = choice === INSTALLED;
  $("add-installed-row").hidden = !installed;
  $("add-name-row").hidden = !other;
  $("add-adblock-row").hidden = !other;
  $("add-arrows-row").hidden = !other;
  $("add-url-row").hidden = !(other || p?.needsUrl);
  $<HTMLInputElement>("add-url").placeholder = other ? "https://…" : (p?.urlHint ?? "");
  let desc = other ? "Opens the address fullscreen, with its own login." : (p?.description ?? "");
  if (installed) {
    const e = info.installed.find((x) => x.id === val("add-installed"));
    desc = e
      ? (e.comment ? e.comment.replace(/\.$/, "") + ". " : "") +
        "It starts the way it does from the Pi's menu. Programs made for a mouse need one: the phone remote has a touchpad."
      : "No more programs were found in the Pi's application menu.";
  }
  const em = emulatorChosen();
  if (em) {
    desc = t(em.description) + " ";
    desc += em.desktop
      ? t("Holding the controller's logo button, or Back on the remote, for two seconds returns to PiDash.")
      : t("It is not installed yet. In a terminal on the Pi: {command}  Then open this page again.", { command: em.install });
  }
  if (p?.console && !info.consoleReady) {
    desc += " It runs outside the desktop, which needs a one-time setup on the Pi: bash deploy/install.sh --console-apps";
  }
  const what = $("add-what");
  what.dataset.noTranslate = "";
  what.textContent = p?.name ?? em?.name ?? t(other ? "Another website" : "A program installed on the Pi");
  $("add-desc").textContent = desc;
  setResult($("add-problem"), "");
}

function emulatorChosen() {
  return addChoice.startsWith(EMULATOR) ? (info.emulators ?? []).find((x) => x.id === addChoice.slice(EMULATOR.length)) : undefined;
}

$("add-installed").addEventListener("change", showAddFields);

$("add-btn").addEventListener("click", () => {
  const choice = addChoice;
  const url = val("add-url").trim();
  const result = $("add-problem");
  let name = "";
  if (choice === OTHER) {
    name = val("add-name").trim();
    if (!name) return setResult(result, "Give it a name.", "error");
    if (!/^https?:\/\/\S+\.\S+|^https?:\/\/[^\s/]+/.test(url)) return setResult(result, "Enter the full address, starting with http:// or https://", "error");
    const adblock = $<HTMLInputElement>("add-adblock").checked;
    // Moving with the arrows is Chromium's; the ad blocker opens Firefox.
    apps.push({ kind: "web", name, url, adblock, ...(!adblock && $<HTMLInputElement>("add-arrows").checked ? { arrows: true } : {}) });
  } else if (choice.startsWith(EMULATOR)) {
    const em = emulatorChosen();
    if (!em) return;
    if (!em.desktop) return setResult(result, t("{name} is not installed on the Pi yet.", { name: em.name }), "error");
    name = em.name;
    apps.push({ kind: "desktop", desktop: em.desktop, name: em.name });
  } else if (choice === INSTALLED) {
    const e = info.installed.find((x) => x.id === val("add-installed"));
    if (!e) return setResult(result, "Choose a program first.", "error");
    name = e.name;
    apps.push({ kind: "desktop", desktop: e.id, name: e.name });
  } else {
    const p = info.presets.find((x) => x.id === choice);
    if (!p) return;
    if (p.needsUrl && !/^https?:\/\/[^\s/]+/.test(url)) return setResult(result, "Enter the address you open it with in a browser, starting with http:// or https://", "error");
    name = p.name;
    apps.push({ kind: "preset", preset: p.id, name: p.name, ...(p.needsUrl ? { url } : {}) });
  }
  $<HTMLInputElement>("add-name").value = "";
  $<HTMLInputElement>("add-url").value = "";
  $<HTMLInputElement>("add-adblock").checked = false;
  $<HTMLInputElement>("add-arrows").checked = false;
  // Back to the cards, on the one that was added.
  const was = addChoice;
  addChoice = "";
  renderApps();
  refresh();
  setResult($("add-result"), t("Added: {name}.", { name }), "ok");
  const card = [...$("add-grid").querySelectorAll<HTMLButtonElement>(".pick")].find((c) => c.dataset.add === was && !c.disabled) ?? $("add-grid").querySelector<HTMLButtonElement>(".pick:not(:disabled)");
  if (card) {
    if (onTV) highlight(card);
    else card.focus({ preventScroll: true });
  }
});

// --- Checks before saving ---------------------------------------------------------

$("w-test").addEventListener("click", async () => {
  const out = $("w-result");
  setResult(out, "Looking it up…");
  try {
    const res = await send("POST", "/api/settings/test-weather", { location: val("w-location") });
    if (!res.ok) return setResult(out, (await res.text()).trim(), "error");
    setResult(out, t("Found: {place}", { place: (await res.json()).place }), "ok");
  } catch {
    setResult(out, "The Pi did not answer.", "error");
  }
});

$("s-test").addEventListener("click", async () => {
  const out = $("s-result");
  setResult(out, "Connecting to the NAS…");
  try {
    const res = await send("POST", "/api/settings/test-synology", readForm().photos.synology);
    if (!res.ok) return setResult(out, (await res.text()).trim(), "error");
    const libs: string[] = (await res.json()).libraries;
    setResult(out, t("Connected. Found: {list}", { list: libs.map((l) => t(l)).join(", ") }), "ok");
  } catch {
    setResult(out, "The Pi did not answer.", "error");
  }
});

$("i-test").addEventListener("click", async () => {
  const out = $("i-result");
  setResult(out, "Connecting to the server…");
  try {
    const res = await send("POST", "/api/settings/test-immich", readForm().photos.immich);
    if (!res.ok) return setResult(out, (await res.text()).trim(), "error");
    setResult(out, t("Connected, as {name}.", { name: (await res.json()).user }), "ok");
  } catch {
    setResult(out, "The Pi did not answer.", "error");
  }
});

$("sh-test").addEventListener("click", async () => {
  const out = $("sh-result");
  setResult(out, "Looking into the share…");
  try {
    const res = await send("POST", "/api/settings/test-share", readForm().photos.share);
    if (!res.ok) return setResult(out, (await res.text()).trim(), "error");
    const n: number = (await res.json()).entries;
    setResult(out, t(n === 1 ? "Connected. There is 1 folder or file in it." : "Connected. There are {n} folders and files in it.", { n }), "ok");
  } catch {
    setResult(out, "The Pi did not answer.", "error");
  }
});

// --- Saving -----------------------------------------------------------------------

async function startedAt(): Promise<number> {
  const res = await fetch("/api/config", { cache: "no-store" });
  return (await res.json()).started;
}

saveBtn.addEventListener("click", async () => {
  busy = true;
  refresh();
  try {
    const before = await startedAt();
    const res = await send("PUT", "/api/settings", readForm());
    if (!res.ok) {
      setStatus(res.status === 401 ? "The PIN is needed to save." : (await res.text()).trim(), "error");
      return;
    }
    // PiDash restarts to apply the settings: wait until it is back.
    setStatus("Saved. Applying…");
    for (let i = 0; i < 40; i++) {
      await new Promise((r) => setTimeout(r, 400));
      try {
        if ((await startedAt()) !== before) break;
      } catch {
        /* still restarting */
      }
    }
    await load();
    setStatus("Saved.", "ok");
  } catch {
    setStatus("The Pi did not answer. Nothing was saved.", "error");
  } finally {
    busy = false;
    refresh();
  }
});

for (const id of ["w-location", "w-units", "w-radar", "p-source", "s-url", "s-user", "s-pass", "s-tls", "s-quality", "i-url", "i-key", "i-tls", "i-quality", "sh-host", "sh-name", "sh-folder", "sh-user", "sh-pass", "p-interval", "p-motion", "p-pairs", "p-today", "p-videos", "p-map", "n-start", "n-end", "h-url", "h-token", "sys-auto", "tv-idle", "bt-off", "p-odd", "p-art", "sys-resolution", "sys-cool", "snd-follow", "spk-spotify", "spk-airplay", "spk-name", "dep-stop", "dep-walk", "sys-games", "sys-games-page", "sys-games-pad", "v-source", "v-folders", "l-pin", "wf-name", "wf-pass", "wf-open", "wf-hidden", "d-language", "d-position", "d-size", "d-style", "d-dim", "d-caption", "d-weather", "d-lock", "d-background", "d-performance", "d-tiles", "d-fps", "d-sounds"]) {
  $(id).addEventListener("input", refresh);
  $(id).addEventListener("change", refresh);
}

for (const box of ownTiles) box.addEventListener("change", () => refresh());

// What a performance preset does, in a sentence under the list.
const PERFORMANCE_HINTS: Record<string, string> = {
  smooth: "The moving background and moving photos are drawn 30 times a second. Most work for the Pi.",
  balanced: "The moving background is drawn 12 times a second, moving photos 15 times.",
  saver: "The moving background is drawn 5 times a second and less finely, photos stand still instead of drifting, and video clips are left out.",
};
function describePerformance() {
  $("d-performance-hint").textContent = t(PERFORMANCE_HINTS[val("d-performance")] ?? "");
}
$("d-performance").addEventListener("change", describePerformance);

// --- Categories --------------------------------------------------------------------
// The settings are sorted into a few categories, listed in a bar: at the
// left on a TV or computer, along the top on a phone. One is shown at a time.

const groupButtons = [...$("groups").querySelectorAll<HTMLButtonElement>("button")];
const cards = [...document.querySelectorAll<HTMLElement>("#main section.card")];
let group = "";

function showGroup(name: string) {
  const offered = groupButtons.filter((b) => !b.hidden);
  if (!offered.some((b) => b.dataset.group === name)) name = offered[0]?.dataset.group ?? "";
  if (!name || name === group) return;
  group = name;
  for (const c of cards) c.classList.toggle("other-group", c.dataset.group !== name);
  $("version").classList.toggle("other-group", name !== "system");
  for (const b of groupButtons) {
    const on = b.dataset.group === name;
    b.classList.toggle("current", on);
    if (on) {
      b.setAttribute("aria-current", "true");
      // On a phone the bar scrolls sideways: keep the chosen one in view.
      b.scrollIntoView({ block: "nearest", inline: "center" });
    } else b.removeAttribute("aria-current");
  }
  // Kept in the address, so that coming back, or loading the page again
  // after saving, returns to the same category.
  history.replaceState(null, "", location.pathname + location.search + "#" + name);
  window.scrollTo(0, 0);
}

/** A category all of whose settings are missing here, such as sound on a Pi without any, is not offered. */
function offerGroups() {
  for (const b of groupButtons) b.hidden = !cards.some((c) => c.dataset.group === b.dataset.group && !c.hidden);
  const offered = (name: string) => groupButtons.some((b) => b.dataset.group === name && !b.hidden);
  // The category the page was opened for may only now have something to
  // show: whether the Pi has sound and Bluetooth is known a moment later.
  const to = asked && offered(asked) ? asked : offered(group) ? group : "";
  if (to === group) return;
  const inBar = !!document.querySelector("#groups .tv-focus");
  group = "";
  showGroup(to || wantedGroup());
  // On the TV the highlight goes along, if it is on the bar.
  if (inBar) highlight(groupButtons.find((b) => b.classList.contains("current")) ?? null);
}

/** The category the address asks for: by its own name, or by something in it ("#bt-card"). */
function wantedGroup(): string {
  const wanted = decodeURIComponent(location.hash.slice(1));
  if (groupButtons.some((b) => b.dataset.group === wanted)) return wanted;
  const inside = wanted ? document.getElementById(wanted)?.closest<HTMLElement>("section.card") : null;
  return inside?.dataset.group ?? groupButtons[0].dataset.group ?? "";
}

for (const b of groupButtons) {
  b.addEventListener("click", () => {
    asked = ""; // from here on the person chooses
    showGroup(b.dataset.group ?? "");
  });
}
new MutationObserver(offerGroups).observe($("main"), { subtree: true, attributes: true, attributeFilter: ["hidden"] });
window.addEventListener("hashchange", () => showGroup(wantedGroup()));
/** The category the address asked for when the page was opened. */
let asked = wantedGroup();
showGroup(asked);

/**
 * Where only the dashboard is served (a container), what is about the
 * machine itself is not offered: its apps, its TV, its videos.
 */
async function leaveOutTheMachine() {
  try {
    const cfg = await (await fetch("/api/config", { cache: "no-store" })).json();
    if (!cfg.standalone) return;
    for (const el of document.querySelectorAll<HTMLElement>("[data-device]")) el.hidden = true;
  } catch {
    /* everything stays on offer */
  }
}

/** The kind of photos named in the address was chosen once; after that the page is the person's. */
let sourceTaken = false;

async function load() {
  const res = await fetch("/api/settings", { cache: "no-store" });
  if (!res.ok) throw new Error(await res.text());
  fill(await res.json());
  // Sent here to set up a kind of photos (from the questions of the first
  // start): that kind is chosen already, and its fields are open.
  const wanted = new URLSearchParams(location.search).get("source");
  const sources = $<HTMLSelectElement>("p-source");
  if (wanted && !sourceTaken && [...sources.options].some((o) => o.value === wanted)) {
    sourceTaken = true;
    sources.value = wanted;
    refresh();
  }
  await leaveOutTheMachine();
  $("groups").hidden = false;
  $("main").hidden = false;
  $("footer").hidden = false;
  offerGroups();
  groupButtons.find((b) => b.classList.contains("current"))?.scrollIntoView({ block: "nearest", inline: "center" });
}

// An open network has no password to type.
/** Shows what the chosen place of the videos needs to know. */
function showVideoFields() {
  const source = val("v-source");
  $("v-folders-row").hidden = source === "";
  $("v-hint-folder").hidden = source !== "folder";
  $("v-hint-synology").hidden = source !== "synology";
  $("v-folders-label").textContent = source === "synology" ? "Shared folder on the NAS" : "Folder";
  $<HTMLInputElement>("v-folders").placeholder = source === "synology" ? "/video" : "/home/pi/Videos";
}
$("v-source").addEventListener("change", showVideoFields);
$("wf-open").addEventListener("change", () => ($("wf-pass-row").hidden = $<HTMLInputElement>("wf-open").checked));

startSound();
startBluetooth(send);

// On the TV: Back leaves for the dashboard, after offering to save.
if (onTV) {
  startTVNav({
    onBack: async () => {
      if (!saveBtn.disabled) {
        const save = await ask("Save the changes?", "Save", "Leave without saving");
        if (save === null) return;
        if (save) {
          saveBtn.click();
          return; // stay to see whether it worked; Back again leaves
        }
      }
      location.href = "/";
    },
  });
}

load()
  .then(() => setStatus("Changes take effect a few seconds after saving."))
  .catch(() => setStatus("The PiDash backend is not reachable.", "error"));
