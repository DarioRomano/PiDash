import { LANGUAGES, setLanguage, t } from "./i18n";
import { ask, enterPIN, onTV, startTVNav } from "./tvnav";
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
  needsUrl: boolean;
  urlHint?: string;
  console: boolean;
  web: boolean;
}

interface Info {
  weather: { location: string; name: string; units: string; coordinates: boolean };
  photos: {
    source: string;
    intervalSeconds: number;
    motion: string;
    pairPortraits: boolean;
    onThisDay: boolean;
    videos: boolean;
    synology: { url: string; username: string; passwordSet: boolean; insecureTls: boolean; quality: string };
    folderPath: string;
  };
  screen: { nightStart: string; nightEnd: string; language: string; clockPosition: string; clockSize: string; view: string; background: string; sounds?: boolean };
  calendar: { links: CalendarLink[] };
  home: { url: string; tokenSet: boolean; entities: string[] };
  system: { autoUpdate: boolean; cooling?: boolean };
  lock: { set: boolean };
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

let info: Info;
let apps: AppEntry[] = [];
let calendars: CalendarLink[] = [];
let stations: Station[] = [];
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
    weather: { location: val("w-location").trim(), name: info.weather.name, units: val("w-units") },
    photos: {
      source,
      intervalSeconds: Math.max(5, Math.round(Number(val("p-interval")) || 30)),
      motion: val("p-motion") || "off",
      pairPortraits: $<HTMLInputElement>("p-pairs").checked,
      onThisDay: $<HTMLInputElement>("p-today").checked,
      videos: $<HTMLInputElement>("p-videos").checked,
      synology: {
        url: val("s-url").trim(),
        username: val("s-user").trim(),
        password: val("s-pass"),
        insecureTls: $<HTMLInputElement>("s-tls").checked,
        quality: val("s-quality"),
      },
    },
    // Night mode needs both times; "Off" in the first switches it off.
    screen: {
      ...(val("n-start") ? { nightStart: val("n-start"), nightEnd: val("n-end") || "07:00" } : { nightStart: "", nightEnd: "" }),
      language: val("d-language"),
      clockPosition: val("d-position"),
      clockSize: val("d-size"),
      view: val("d-view"),
      background: val("d-background"),
      sounds: $<HTMLInputElement>("d-sounds").checked,
    },
    apps: apps.map(({ detail: _detail, missing: _missing, ...rest }) => rest),
    calendar: { links: calendars.map((l) => (l.url ? { url: l.url } : { key: l.key })) },
    home: { url: val("h-url").trim(), token: val("h-token").trim(), entities: homeTiles },
    system: { autoUpdate: $<HTMLInputElement>("sys-auto").checked, cooling: $<HTMLInputElement>("sys-cool").checked },
    // Empty leaves the PIN as it is.
    lock: { pin: val("l-pin").trim(), remove: removeLock },
    radio: stations.map(({ name, url }) => ({ name, url })),
  };
}

function refresh() {
  const source = val("p-source");
  $("p-synology").hidden = source !== "synology";
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
  $("w-coords").hidden = !i.weather.coordinates;

  const source = $<HTMLSelectElement>("p-source");
  source.replaceChildren(new Option("No photos", ""), new Option("Synology Photos on my NAS", "synology"));
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
  $<HTMLInputElement>("p-interval").value = String(i.photos.intervalSeconds || 30);
  $<HTMLSelectElement>("p-motion").value = i.photos.motion || "off";
  $<HTMLInputElement>("p-pairs").checked = !!i.photos.pairPortraits;
  $<HTMLInputElement>("p-today").checked = !!i.photos.onThisDay;
  $<HTMLInputElement>("p-videos").checked = !!i.photos.videos;
  const language = $<HTMLSelectElement>("d-language");
  language.replaceChildren(...Object.entries(LANGUAGES).map(([code, name]) => new Option(name, code)));
  language.value = i.screen.language || "en";
  setLanguage(language.value);
  $<HTMLSelectElement>("d-position").value = i.screen.clockPosition || "bottom-left";
  $<HTMLSelectElement>("d-size").value = i.screen.clockSize || "large";
  $<HTMLSelectElement>("d-view").value = i.screen.view || "clock";
  $<HTMLSelectElement>("d-background").value = i.screen.background || "moving";
  $<HTMLInputElement>("d-sounds").checked = i.screen.sounds ?? true;
  setSounds(i.screen.sounds ?? true);
  fillTimes($<HTMLSelectElement>("n-start"), i.screen.nightStart, "Off");
  fillTimes($<HTMLSelectElement>("n-end"), i.screen.nightEnd || "07:00");
  void showHidden();

  apps = i.apps.map((a) => ({ ...a }));
  renderApps();

  calendars = i.calendar.links.map((l) => ({ ...l }));
  renderCalendars();
  $<HTMLInputElement>("h-url").value = i.home.url;
  const token = $<HTMLInputElement>("h-token");
  token.value = "";
  token.placeholder = i.home.tokenSet ? "Saved. Leave empty to keep it" : "";
  homeTiles = [...i.home.entities];
  renderHomeTiles();
  $<HTMLInputElement>("sys-auto").checked = i.system.autoUpdate;
  $<HTMLInputElement>("sys-cool").checked = i.system.cooling ?? true;
  stations = i.radio.map((s) => ({ ...s }));
  renderStations();
  removeLock = false;
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
function fillTimes(sel: HTMLSelectElement, value: string, off?: string) {
  const times: string[] = [];
  for (let m = 0; m < 24 * 60; m += 30) times.push(`${String(Math.floor(m / 60)).padStart(2, "0")}:${String(m % 60).padStart(2, "0")}`);
  const padded = value && value.length === 4 ? "0" + value : value;
  if (padded && !times.includes(padded)) times.push(padded);
  times.sort();
  sel.replaceChildren(...(off ? [new Option(off, "")] : []), ...times.map((t) => new Option(t, t)));
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
  if (a.kind === "web") return (a.url ?? "") + (a.adblock ? "  ·  ads blocked" : "");
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

function renderAddChoices() {
  const kind = $<HTMLSelectElement>("add-kind");
  const previous = kind.value;
  const used = new Set(apps.filter((a) => a.kind === "preset").map((a) => a.preset));
  // Emulators are listed whether installed or not: choosing one that is not
  // says how to install it.
  const inRow = new Set(apps.filter((a) => a.kind === "desktop").map((a) => a.desktop));
  kind.replaceChildren(
    ...info.presets.filter((p) => !used.has(p.id)).map((p) => new Option(p.name, p.id)),
    ...(info.emulators ?? []).filter((em) => !em.desktop || !inRow.has(em.desktop)).map((em) => new Option(`${em.name} · ${t(em.systems)}`, EMULATOR + em.id)),
    new Option("A program installed on the Pi…", INSTALLED),
    new Option("Another website…", OTHER),
  );
  if ([...kind.options].some((o) => o.value === previous)) kind.value = previous;

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

function showAddFields() {
  const choice = val("add-kind");
  const p = info.presets.find((x) => x.id === choice);
  const other = choice === OTHER;
  const installed = choice === INSTALLED;
  $("add-installed-row").hidden = !installed;
  $("add-name-row").hidden = !other;
  $("add-adblock-row").hidden = !other;
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
  $("add-desc").textContent = desc;
  setResult($("add-result"), "");
}

function emulatorChosen() {
  const choice = val("add-kind");
  return choice.startsWith(EMULATOR) ? (info.emulators ?? []).find((x) => x.id === choice.slice(EMULATOR.length)) : undefined;
}

$("add-kind").addEventListener("change", showAddFields);
$("add-installed").addEventListener("change", showAddFields);

$("add-btn").addEventListener("click", () => {
  const choice = val("add-kind");
  const url = val("add-url").trim();
  const result = $("add-result");
  if (choice === OTHER) {
    const name = val("add-name").trim();
    if (!name) return setResult(result, "Give it a name.", "error");
    if (!/^https?:\/\/\S+\.\S+|^https?:\/\/[^\s/]+/.test(url)) return setResult(result, "Enter the full address, starting with http:// or https://", "error");
    apps.push({ kind: "web", name, url, adblock: $<HTMLInputElement>("add-adblock").checked });
  } else if (choice.startsWith(EMULATOR)) {
    const em = emulatorChosen();
    if (!em) return;
    if (!em.desktop) return setResult(result, t("{name} is not installed on the Pi yet.", { name: em.name }), "error");
    apps.push({ kind: "desktop", desktop: em.desktop, name: em.name });
  } else if (choice === INSTALLED) {
    const e = info.installed.find((x) => x.id === val("add-installed"));
    if (!e) return setResult(result, "Choose a program first.", "error");
    apps.push({ kind: "desktop", desktop: e.id, name: e.name });
  } else {
    const p = info.presets.find((x) => x.id === choice);
    if (!p) return;
    if (p.needsUrl && !/^https?:\/\/[^\s/]+/.test(url)) return setResult(result, "Enter the address you open it with in a browser, starting with http:// or https://", "error");
    apps.push({ kind: "preset", preset: p.id, name: p.name, ...(p.needsUrl ? { url } : {}) });
  }
  $<HTMLInputElement>("add-name").value = "";
  $<HTMLInputElement>("add-url").value = "";
  $<HTMLInputElement>("add-adblock").checked = false;
  renderApps();
  refresh();
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

for (const id of ["w-location", "w-units", "p-source", "s-url", "s-user", "s-pass", "s-tls", "s-quality", "p-interval", "p-motion", "p-pairs", "p-today", "p-videos", "n-start", "n-end", "h-url", "h-token", "sys-auto", "sys-cool", "l-pin", "d-language", "d-position", "d-size", "d-view", "d-background", "d-sounds"]) {
  $(id).addEventListener("input", refresh);
  $(id).addEventListener("change", refresh);
}

async function load() {
  const res = await fetch("/api/settings", { cache: "no-store" });
  if (!res.ok) throw new Error(await res.text());
  fill(await res.json());
  $("main").hidden = false;
  $("footer").hidden = false;
}

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
