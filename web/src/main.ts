import { Ambient, iconPalette } from "./ambient";
import { setLanguage, t } from "./i18n";
import { Clock } from "./clock";
import { Help } from "./help";
import { StatusPanel } from "./status";
import { play, setSounds } from "./sfx";
import { Input } from "./input";
import { Rail, type RailItem } from "./rail";
import { Slideshow } from "./slideshow";
import { connect } from "./sse";
import type { Action, AppsStatus, CalendarState, ClientConfig, HomeEntity, PhotoItem, RadioStation, PhotosStatus, StateEntry, SystemState, WeatherState } from "./types";
import { WeatherView } from "./weather";

const $ = <T extends HTMLElement>(sel: string) => document.querySelector<T>(sel)!;

const fit = $("#fit");
const stage = $("#stage");
/** The part of the stage drawn over the photo; see pixel_shift. */
const ui = $("#ui");
const exitHint = $("#exit-hint");
const toastEl = $("#toast");
const railHint = $("#rail-hint");
const overlay = $("#overlay");
const conn = $("#conn");
const stale = $("#stale");
const debug = $("#debug");

const DENSITIES = ["photo", "compact", "detailed"] as const;
let density = 1;
/** True once Up or Down was used: the configured start view no longer applies. */
let viewChosen = false;

const clock = new Clock($("#hm"), $("#ampm"), $("#date"));
clock.start();

const input = new Input();
input.attachKeyboard();

// --- Fit the 1920x1080 design to the actual screen ----------------------------

function fitToScreen() {
  const scale = Math.min(window.innerWidth / 1920, window.innerHeight / 1080);
  fit.style.setProperty("--scale", scale.toFixed(4));
}
fitToScreen();
window.addEventListener("resize", fitToScreen);

// --- Keep the screen awake while the dashboard is showing ---------------------
// Uses the desktop's idle-inhibit mechanism, so screen blanking only applies
// again once the dashboard is closed.

let wakeLock: WakeLockSentinel | null = null;
async function keepAwake() {
  if (!("wakeLock" in navigator) || wakeLock) return;
  try {
    wakeLock = await navigator.wakeLock.request("screen");
    wakeLock.addEventListener("release", () => (wakeLock = null));
  } catch {
    /* not allowed right now; retried on the next visibility change */
  }
}
keepAwake();
document.addEventListener("visibilitychange", () => {
  if (document.visibilityState === "visible") keepAwake();
  updateSuspended();
});

// --- Exit to desktop ----------------------------------------------------------

input.onHold((holding) => exitHint.classList.toggle("visible", holding));

async function exitToDesktop() {
  exitHint.textContent = "Returning to the desktop…";
  exitHint.classList.add("visible");
  try {
    const res = await fetch("/api/kiosk", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "stop" }),
    });
    if (!res.ok) throw new Error(await res.text());
  } catch {
    // Not running as the kiosk unit (e.g. a normal browser tab): try closing the tab.
    window.close();
    exitHint.textContent = "Press Alt+F4 to return to the desktop";
    window.setTimeout(() => {
      exitHint.classList.remove("visible");
      exitHint.textContent = "Keep holding to exit to the desktop…";
    }, 4000);
  }
}

// --- Toast --------------------------------------------------------------------

let toastTimer = 0;
function toast(text: string, ms = 4000) {
  toastEl.textContent = text;
  toastEl.classList.add("visible");
  clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => toastEl.classList.remove("visible"), ms);
}

// --- Background photos --------------------------------------------------------

const captionEl = $("#caption");
let dateLocale: string | undefined;
let clock24h = true;
let lanAddress = "";

function showCaption(item: PhotoItem | null) {
  if (!item) {
    captionEl.textContent = "";
    return;
  }
  // "Holiday · Sintra, Portugal · 14 June 2024": the innermost folder, the
  // place if the photo carries one, and the day it was taken.
  const folder = item.folder.split(" / ").pop() ?? "";
  const taken = new Date(item.taken);
  const when = isNaN(taken.getTime()) || taken.getFullYear() < 1990
    ? ""
    : new Intl.DateTimeFormat(dateLocale, { day: "numeric", month: "long", year: "numeric" }).format(taken);
  captionEl.textContent = [folder, item.place, when].filter(Boolean).join("  ·  ");
}

// Without a photo on screen, the background is a slow play of colours.
const ambient = new Ambient($<HTMLCanvasElement>("#ambient"), $("#grain"));
let photoShowing = false;
function updateAmbient() {
  ambient.setActive(!photoShowing && appsStatus.running === "" && document.visibilityState === "visible" && !tvAsleep && !nightNow);
}

let shownPhoto: PhotoItem | null = null;
const slideshow = new Slideshow($("#backdrop"), (item) => {
  photoShowing = item !== null;
  shownPhoto = item;
  updateAmbient();
  showCaption(item);
  rail.setItems(railItems()); // the "hide this photo" tile follows the photo
});
let photosHintShown = false;

function onPhotos(st: PhotosStatus) {
  slideshow.setStatus(st);
  // First run: a source is configured but no folders have been chosen yet.
  if (st.enabled && !st.indexing && st.count === 0 && st.selection.folders.length === 0 && !st.error && !photosHintShown) {
    photosHintShown = true;
    toast("Choose the photos for the background: press OK, open Settings, then Background photos.", 15000);
  }
}

let tvAsleep = false;

function updateSuspended() {
  // Do not load photos while an app covers the dashboard, the page is hidden,
  // the TV has been switched off for the night, or night mode shows only a clock.
  slideshow.setSuspended(appsStatus.running !== "" || document.visibilityState !== "visible" || tvAsleep || nightNow);
  // An app brings its own sound, and a TV that is off needs none.
  if (appsStatus.running !== "" || tvAsleep) stopRadio();
  updateAmbient();
}

// --- Night mode -----------------------------------------------------------------
// Between two times of day the screen shows only a dim clock on black. Any
// button brings the dashboard back for a minute.

let nightHours: [number, number] | null = null; // minutes after midnight
let nightNow = false;
let awakeUntil = 0;

function minutesOf(clock: string): number {
  const [h, m] = clock.split(":").map(Number);
  return h * 60 + m;
}

function updateNight() {
  let night = false;
  if (nightHours) {
    const d = new Date();
    const now = d.getHours() * 60 + d.getMinutes();
    const [from, to] = nightHours;
    night = from <= to ? now >= from && now < to : now >= from || now < to;
  }
  night = night && Date.now() > awakeUntil;
  if (night === nightNow) return;
  nightNow = night;
  stage.classList.toggle("night", night);
  if (night) {
    rail.close();
    weather.close();
  }
  updateSuspended();
}
window.setInterval(updateNight, 15_000);

/** A button was pressed. Returns true when that press only woke the screen. */
function wakeFromNight(): boolean {
  if (!nightHours) return false;
  const wasNight = nightNow;
  awakeUntil = Date.now() + 60_000;
  updateNight();
  return wasNight;
}

// --- Weather ------------------------------------------------------------------

const WEATHER_ID = "__weather";
const weatherEl = $("#weather");
const weather: WeatherView = new WeatherView(weatherEl, $("#weather-panel"), (open) =>
  stage.classList.toggle("panel-open", open || help.isOpen || statusPanel.isOpen),
);
// With a mouse or touch screen, clicking the summary opens the details too.
weatherEl.addEventListener("click", (e) => {
  e.stopPropagation();
  weather.open();
});

// --- Help ---------------------------------------------------------------------

const help = new Help($("#help"), (open) => stage.classList.toggle("panel-open", open || weather.isOpen || statusPanel.isOpen));

// --- Status ---------------------------------------------------------------------
// How PiDash is doing, readable from the sofa.

const STATUS_ID = "__status";
const STATUS_GLYPH =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">' +
  '<path d="M2.8 12.5h4l2.2-6 4 12 2.6-8 1.4 2h4.2"/></svg>';
let lastSystem: SystemState | null = null;
const statusPanel: StatusPanel = new StatusPanel(
  $("#status-panel"),
  () => lastSystem,
  (open) => stage.classList.toggle("panel-open", open || weather.isOpen || help.isOpen),
);

function openHelp() {
  const url = lanAddress ? `http://${lanAddress}:${location.port || "80"}/remote` : "";
  help.open(input.keyMap, url);
}

// --- Settings -------------------------------------------------------------------
// The settings page opens right on the TV, where it is driven with the remote.

const SETTINGS_ID = "__settings";
const GEAR_GLYPH =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">' +
  '<circle cx="12" cy="12" r="3.2"/><path d="M12 2.8v2.6M12 18.6v2.6M2.8 12h2.6M18.6 12h2.6M5.5 5.5l1.8 1.8M16.7 16.7l1.8 1.8M18.5 5.5l-1.8 1.8M7.3 16.7l-1.8 1.8"/></svg>';

function openSettings() {
  location.href = "/settings?from=tv";
}

// --- Panel rail and apps ------------------------------------------------------

// Smart-home tiles: lights, plugs, scenes and values from Home Assistant.
const HOME_ID = "__home:";
let homeEntities: HomeEntity[] = [];
const svg = (path: string) =>
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">${path}</svg>`;
function homeGlyph(e: HomeEntity): string {
  switch (e.domain) {
    case "light":
      return svg('<path d="M9 18h6M10 21h4M12 3a6 6 0 0 0-3.6 10.8c.6.5 1 1.300 1 2.200h5.200c0-.900.400-1.700 1-2.200A6 6 0 0 0 12 3z"/>');
    case "scene":
    case "script":
      return svg('<path d="M8 5l11 7-11 7z"/>');
    case "cover":
      return svg('<rect x="4" y="3" width="16" height="18" rx="1.5"/><path d="M4 8h16M4 13h16M12 13v5"/>');
    case "sensor":
    case "climate":
    case "weather":
      return svg('<path d="M12 3a2 2 0 0 0-2 2v8.500a4 4 0 1 0 4 0V5a2 2 0 0 0-2-2z"/><path d="M12 9v7"/>');
    case "switch":
    case "input_boolean":
    case "fan":
      return svg('<path d="M12 3v8"/><path d="M6.300 6.800a8 8 0 1 0 11.400 0"/>');
  }
  return svg('<circle cx="12" cy="12" r="7"/>');
}

// "More from this album": the slideshow stays with the album of the photo on screen.
const MORE_ID = "__more";
const MORE_GLYPH = svg('<rect x="3" y="6" width="13" height="13" rx="1.500"/><path d="M7 6V4.500A1.500 1.500 0 0 1 8.500 3H19.500A1.500 1.500 0 0 1 21 4.500V15.500A1.500 1.500 0 0 1 19.500 17H16"/>');

// --- Radio ----------------------------------------------------------------------
// Stations play in the background while the photos carry on. The play button
// of the remote pauses and resumes the station; opening an app stops it.

const RADIO_ID = "__radio:";
const RADIO_GLYPH = svg('<rect x="3" y="9" width="18" height="11" rx="2"/><path d="M7 9l9-5"/><circle cx="8.500" cy="14.500" r="2.500"/><path d="M14 13h4M14 16h4"/>');
let radioStations: RadioStation[] = [];
let radioPlaying = -1;
const radio = new Audio();
radio.preload = "none";
const radioLabel = document.createElement("div");
radioLabel.id = "radio";
radioLabel.hidden = true;
ui.appendChild(radioLabel);

function showRadio() {
  const st = radioStations[radioPlaying];
  radioLabel.hidden = !st;
  radioLabel.textContent = st ? `♪  ${st.name}${radio.paused ? "  ·  " + t("paused") : ""}` : "";
  rail.setItems(railItems());
}

function stopRadio() {
  if (radioPlaying < 0) return;
  radioPlaying = -1;
  radio.pause();
  radio.removeAttribute("src");
  radio.load(); // drops the connection to the station
  showRadio();
}

function toggleRadio(index: number) {
  if (radioPlaying === index) return stopRadio();
  const st = radioStations[index];
  if (!st) return;
  radioPlaying = index;
  radio.src = st.url;
  radio.play().catch((err: unknown) => {
    // Pausing before the station has started is not a failure.
    if (radioPlaying !== index || (err instanceof DOMException && err.name === "AbortError")) return;
    toast(t("{name} cannot be played right now.", { name: st.name }), 6000);
    stopRadio();
  });
  showRadio();
}
radio.addEventListener("error", () => {
  const st = radioStations[radioPlaying];
  if (!st) return;
  toast(t("{name} cannot be played right now.", { name: st.name }), 6000);
  stopRadio();
});
radio.addEventListener("pause", showRadio);
radio.addEventListener("playing", showRadio);

const SLEEP_ID = "__sleep";
const SLEEP_GLYPH = svg('<path d="M20 14.500A8 8 0 0 1 9.500 4a8 8 0 1 0 10.500 10.500z"/>');
let tvAvailable = false;
let sleepAt = 0; // when the TV will be switched off, in milliseconds; 0 when not set
let sleepMinutes = 0;
let sleepWarned = 0;
// The tile counts down, and a minute before the end the screen says so.
window.setInterval(() => {
  if (!sleepAt) return;
  rail.setItems(railItems());
  if (sleepAt - Date.now() < 75_000 && sleepWarned !== sleepAt) {
    sleepWarned = sleepAt;
    toast("The TV switches off in a minute. The sleep timer in the app row changes that.", 12000);
  }
}, 30_000);

const HIDE_ID = "__hide:";
const HIDE_GLYPH =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">' +
  '<path d="M3 3l18 18M10.6 6.2A9.8 9.8 0 0 1 12 6c5 0 8.5 4.5 9.5 6-.4.7-1.300 1.900-2.600 3.100M6.300 7.700C4.300 9 3 10.900 2.500 12c1 1.500 4.500 6 9.500 6 1.300 0 2.500-.300 3.600-.800M9.900 9.900a3 3 0 0 0 4.200 4.200"/></svg>';
const EXIT_ID = "__desktop";
const EXIT_GLYPH =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">' +
  '<rect x="3" y="4" width="18" height="12" rx="1.5"/><path d="M8 20h8M12 16v4"/></svg>';

let appsStatus: AppsStatus = { apps: [], running: "", consoleReady: true };
let appsLoaded = false;

const rail = new Rail(
  $("#rail"),
  $("#rail-strip"),
  activateRailItem,
  (open) => {
    stage.classList.toggle("rail-open", open);
    ambient.setLifted(open);
    railActivity();
  },
);

// Each tile glows in the main colour of its icon.
rail.tintOf = async (item) => {
  if (!item.icon) return null;
  const palette = await iconPalette(item.icon);
  return palette?.[0] ? palette[0].map((v) => Math.round(v)).join(", ") : null;
};

// What the buttons do in the app row, under the tiles.
function showRailHint() {
  const n = appsStatus.apps.length;
  railHint.textContent = rail.isMoving
    ? "◀ ▶ move  ·  OK put down  ·  Back leave it where it was"
    : n > 1
      ? "◀ ▶ choose  ·  OK open  ·  ▲ move an app  ·  Back close"
      : n === 1
        ? "◀ ▶ choose  ·  OK open  ·  Back close"
        : "Add apps under Settings  ·  Back close";
}
rail.onMoving = showRailHint;

// An app was put down in a new place: remember the order on the Pi.
rail.onReorder = (ids) => {
  // Shown at once; the Pi confirms with the same order a moment later.
  const byId = new Map(appsStatus.apps.map((a) => [a.id, a]));
  const apps = ids.map((id) => byId.get(id)).filter((a): a is NonNullable<typeof a> => !!a);
  if (apps.length === appsStatus.apps.length) appsStatus = { ...appsStatus, apps };
  rail.setItems(railItems());
  void (async () => {
    let problem = "";
    try {
      const res = await postJSON("/api/apps/order", { ids });
      if (res.status === 401 || res.status === 429) problem = "The settings are locked, so apps can only be moved there: Settings, Apps.";
      else if (!res.ok) problem = (await res.text()).trim() || "The new order could not be saved.";
    } catch {
      problem = "The new order could not be saved.";
    }
    if (!problem) return;
    toast(problem, 8000);
    // Back to the order the Pi has.
    try {
      onApps((await (await fetch("/api/apps")).json()) as AppsStatus);
    } catch {
      /* the next update from the Pi brings it */
    }
  })();
};

// The background takes on the colours of the selected app's icon, and returns
// to its own when the row closes or a tile without an icon is selected.
let paletteRequest = 0;
rail.onFocus = (item) => {
  const request = ++paletteRequest;
  if (!item?.icon) {
    ambient.setPalette(null);
    return;
  }
  void iconPalette(item.icon).then((palette) => {
    if (request === paletteRequest) ambient.setPalette(palette);
  });
};

// Left alone, the app row closes again, so the TV goes back to being a clock
// and a picture frame instead of showing a menu all evening.
let railTimeoutMs = 30_000;
let railTimer = 0;
function railActivity() {
  clearTimeout(railTimer);
  if (!rail.isOpen || railTimeoutMs <= 0) return;
  railTimer = window.setTimeout(() => {
    // Not under an app that is starting or running: the row is not visible
    // then, and should be where it was when the app closes.
    if (appsStatus.running !== "" || document.visibilityState !== "visible") railActivity();
    else rail.close();
  }, railTimeoutMs);
}

function railItems(): RailItem[] {
  const items: RailItem[] = [];
  const w = weather.tile;
  if (w) items.push({ id: WEATHER_ID, label: "Weather", glyph: w.glyph, sub: w.sub });
  for (const a of appsStatus.apps) {
    const running = appsStatus.running === a.id;
    items.push({
      id: a.id,
      label: a.name,
      icon: a.hasIcon ? `/api/apps/${encodeURIComponent(a.id)}/icon` : undefined,
      movable: true,
      running,
      sub: running ? "Running · OK to stop" : "",
    });
  }
  radioStations.forEach((st, i) => {
    const playing = radioPlaying === i;
    items.push({ id: RADIO_ID + i, label: st.name, glyph: RADIO_GLYPH, running: playing, sub: playing ? "Playing · OK to stop" : "Radio" });
  });
  // The sleep timer: each OK sets it to the next length, then off again.
  if (tvAvailable) {
    const left = Math.max(1, Math.round((sleepAt - Date.now()) / 60_000));
    items.push({ id: SLEEP_ID, label: "Sleep timer", glyph: SLEEP_GLYPH, running: sleepAt > 0, sub: sleepAt > 0 ? t("TV off in {n} min", { n: left }) : "Off" });
  }
  for (const e of homeEntities) {
    items.push({ id: HOME_ID + e.id, label: e.name, sub: e.text, glyph: homeGlyph(e), running: e.on });
  }
  // A photo that should not come up again can be hidden; of two side by
  // side, either one. Or the slideshow can stay with its album for a while.
  if (shownPhoto) {
    items.push({ id: MORE_ID, label: "More from this album", glyph: MORE_GLYPH, sub: shownPhoto.folder.split(" / ").pop() });
    const keys = shownPhoto.key.split("+");
    if (keys.length === 2) {
      items.push({ id: HIDE_ID + keys[0], label: "Hide left photo", glyph: HIDE_GLYPH });
      items.push({ id: HIDE_ID + keys[1], label: "Hide right photo", glyph: HIDE_GLYPH });
    } else {
      items.push({ id: HIDE_ID + keys[0], label: "Hide this photo", glyph: HIDE_GLYPH });
    }
  }
  items.push({ id: STATUS_ID, label: "Status", glyph: STATUS_GLYPH });
  items.push({ id: SETTINGS_ID, label: "Settings", glyph: GEAR_GLYPH });
  items.push({ id: EXIT_ID, label: "Desktop", glyph: EXIT_GLYPH });
  return items;
}

function onApps(st: AppsStatus) {
  const previousError = appsStatus.error;
  appsStatus = st;
  rail.setItems(railItems());
  updateSuspended();
  showRailHint();
  // Do not replay an old problem when the page (re)connects.
  if (appsLoaded && st.error && st.error !== previousError) toast(st.error, 9000);
}
onApps(appsStatus);

async function postJSON(url: string, body: unknown): Promise<Response> {
  return fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

async function activateRailItem(item: RailItem) {
  if (item.id === EXIT_ID) {
    exitToDesktop();
    return;
  }
  if (item.id === WEATHER_ID) {
    rail.close();
    weather.open();
    return;
  }
  if (item.id === SETTINGS_ID) {
    openSettings();
    return;
  }
  if (item.id === STATUS_ID) {
    rail.close();
    statusPanel.open();
    return;
  }
  if (item.id.startsWith(RADIO_ID)) {
    toggleRadio(Number(item.id.slice(RADIO_ID.length)));
    return;
  }
  if (item.id === MORE_ID && shownPhoto) {
    rail.close();
    try {
      const where = `folder=${encodeURIComponent(shownPhoto.folder)}&after=${encodeURIComponent(shownPhoto.key)}&limit=30`;
      const data: { items: PhotoItem[] } = await (await fetch(`/api/photos/playlist?${where}`)).json();
      slideshow.queue(data.items);
      const album = shownPhoto.folder.split(" / ").pop() ?? "";
      toast(
        data.items.length === 0
          ? t("There are no other photos in “{album}”.", { album })
          : t("Staying with “{album}” for the next {n} pictures.", { album, n: data.items.length }),
        5000,
      );
      if (data.items.length > 0) slideshow.next();
    } catch {
      toast("The PiDash backend is not reachable.", 6000);
    }
    return;
  }
  if (item.id === SLEEP_ID) {
    const steps = [0, 30, 60, 90, 120];
    const next = steps[(steps.indexOf(sleepMinutes) + 1) % steps.length];
    try {
      await postJSON("/api/tv/sleep", { minutes: next });
    } catch {
      toast("The PiDash backend is not reachable.", 6000);
    }
    return;
  }
  if (item.id.startsWith(HOME_ID)) {
    const e = homeEntities.find((x) => HOME_ID + x.id === item.id);
    if (!e?.actionable) {
      toast(e?.available === false ? t("{name} is not available right now.", { name: item.label }) : t("{name} only shows a value.", { name: item.label }), 3000);
      return;
    }
    try {
      const res = await postJSON("/api/home/activate", { id: e.id });
      if (!res.ok) toast((await res.text()).trim(), 6000);
    } catch {
      toast("The PiDash backend is not reachable.", 6000);
    }
    return;
  }
  if (item.id.startsWith(HIDE_ID)) {
    rail.close();
    try {
      const res = await postJSON("/api/photos/hide", { keys: [item.id.slice(HIDE_ID.length)] });
      toast(res.ok ? "Hidden. It can be brought back under Settings, Background photos." : (await res.text()).trim(), 6000);
    } catch {
      toast("The PiDash backend is not reachable.", 6000);
    }
    return;
  }
  const stopping = appsStatus.running === item.id;
  toast(t(stopping ? "Closing {name}…" : "Starting {name}…", { name: item.label }));
  try {
    const res = stopping
      ? await postJSON("/api/apps/stop", {})
      : await postJSON("/api/apps/start", { id: item.id });
    // Launch problems arrive as an "apps" event with an error message, which
    // is shown as a toast; only report what that channel cannot.
    if (!res.ok && res.status >= 500 && !appsStatus.error) toast((await res.text()).trim(), 9000);
    if (res.ok && !stopping) rail.close();
  } catch {
    toast("The PiDash backend is not reachable.", 6000);
  }
}

// --- Config -----------------------------------------------------------------

let pixelShiftTimer = 0;

// While the Pi is hot the page does without everything that moves all the
// time: the flowing background and the movement of photos.
let hot = false;
let stillBackground = false;

let runningVersion = "";
let shownNotice = "";

function applyConfig(cfg: ClientConfig) {
  // After an update the backend restarts with a new version: load the new
  // dashboard too, so the TV never runs an old page against a new backend.
  // The same goes for saved settings, which the backend applies by restarting.
  const run = `${cfg.version}/${cfg.started}`;
  if (runningVersion && run !== runningVersion) {
    location.reload();
    return;
  }
  runningVersion = run;
  // Start-up problems, such as a settings mistake or a port that was taken.
  const notice = (cfg.notices ?? []).join("  ·  ");
  if (notice && notice !== shownNotice) toast(notice, 15000);
  shownNotice = notice;
  const d = cfg.display;
  document.documentElement.style.setProperty("--safe", `${(d.safe_area * 100).toFixed(2)}%`);
  // Dates follow the language unless a locale is set for them.
  setLanguage(d.language || "en");
  const locale = d.locale || (d.language && d.language !== "en" ? d.language : "");
  clock.configure(d.clock_24h, locale);
  clock24h = d.clock_24h;
  setSounds(d.sounds ?? true);
  stillBackground = d.background === "still";
  ambient.setStill(stillBackground || hot);
  stage.dataset.clock = d.clock_position || "bottom-left";
  stage.dataset.clockSize = d.clock_size || "large";
  if (!viewChosen) {
    density = d.view === "details" ? 2 : d.view === "photo" ? 0 : 1;
    overlay.dataset.density = DENSITIES[density];
  }
  weather.configure(d.clock_24h, locale);
  dateLocale = locale || undefined;
  renderAgenda();
  input.setKeyMap(cfg.keys);
  input.setDebugElement(d.debug_keys ? debug : null);
  if (!d.debug_keys) debug.hidden = true;
  const stations = cfg.radio ?? [];
  if (JSON.stringify(stations) !== JSON.stringify(radioStations)) {
    stopRadio();
    radioStations = stations;
    rail.setItems(railItems());
  }
  startPixelShift(d.pixel_shift);
  nightHours = d.night_start && d.night_end ? [minutesOf(d.night_start), minutesOf(d.night_end)] : null;
  updateNight();
  railTimeoutMs = (d.app_row_timeout ?? 30) * 1000;
  railActivity();
  sysBadge.hidden = !d.debug_keys;
}

// Pages that cover the screen reach this far past its edges (see #help in
// the styles, which allows a little more).
const MAX_PIXEL_SHIFT = 20;

function startPixelShift(max: number) {
  clearInterval(pixelShiftTimer);
  stage.style.setProperty("--shift-x", "0px");
  stage.style.setProperty("--shift-y", "0px");
  if (max <= 0) return;
  max = Math.min(max, MAX_PIXEL_SHIFT);
  pixelShiftTimer = window.setInterval(() => {
    const x = Math.round((Math.random() * 2 - 1) * max);
    const y = Math.round((Math.random() * 2 - 1) * max);
    stage.style.setProperty("--shift-x", `${x}px`);
    stage.style.setProperty("--shift-y", `${y}px`);
  }, 3 * 60 * 1000);
}

// --- Calendar ---------------------------------------------------------------------
// The next few events, grouped under "Today", "Tomorrow" or the weekday.

const agendaEl = $("#agenda");
let agendaEvents: CalendarState["events"] = [];
const AGENDA_LINES = 5;

function renderAgenda() {
  const now = new Date();
  const dayStart = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const today = dayStart(now);
  const time = new Intl.DateTimeFormat(dateLocale, { hour: "numeric", minute: "2-digit", hour12: !clock24h });
  const weekday = new Intl.DateTimeFormat(dateLocale, { weekday: "long" });
  const date = new Intl.DateTimeFormat(dateLocale, { weekday: "short", day: "numeric", month: "short" });

  const rows: HTMLElement[] = [];
  let lastDay = "";
  for (const ev of agendaEvents) {
    const start = new Date(ev.start);
    const end = new Date(ev.end);
    if (end.getTime() <= now.getTime() && !(ev.allDay && end.getTime() > today)) continue; // over
    if (rows.filter((r) => r.className === "agenda-event").length >= AGENDA_LINES) break;
    // An event that began earlier and is still going on belongs to today.
    const day = Math.max(dayStart(start), today);
    const days = Math.round((day - today) / 86_400_000);
    const label = days === 0 ? "Today" : days === 1 ? "Tomorrow" : days < 7 ? weekday.format(day) : date.format(day);
    if (label !== lastDay) {
      lastDay = label;
      const h = document.createElement("div");
      h.className = "agenda-day";
      h.textContent = label;
      rows.push(h);
    }
    const row = document.createElement("div");
    row.className = "agenda-event";
    const when = document.createElement("span");
    when.className = "agenda-time";
    when.textContent = ev.allDay || start.getTime() < day ? "" : time.format(start);
    const what = document.createElement("span");
    what.className = "agenda-title";
    what.textContent = ev.title;
    row.append(what, when);
    rows.push(row);
  }
  agendaEl.replaceChildren(...rows);
  agendaEl.hidden = rows.length === 0;
}
// Events pass, and "Tomorrow" becomes "Today", without anything arriving.
window.setInterval(renderAgenda, 60_000);

// --- State ------------------------------------------------------------------

const staleProviders = new Set<string>();

const sysBadge = document.createElement("div");
sysBadge.id = "sys";
sysBadge.hidden = true;
ui.appendChild(sysBadge);

function onState(entry: StateEntry) {
  if (entry.stale) staleProviders.add(entry.provider);
  else staleProviders.delete(entry.provider);
  stale.hidden = staleProviders.size === 0;

  switch (entry.provider) {
    case "system":
      renderSystem(entry.data as SystemState | null);
      break;
    case "home":
      homeEntities = (entry.data as { entities: HomeEntity[] } | null)?.entities ?? homeEntities;
      rail.setItems(railItems());
      break;
    case "calendar":
      agendaEvents = (entry.data as CalendarState | null)?.events ?? [];
      renderAgenda();
      break;
    case "weather":
      weather.update(entry.data as WeatherState | null, entry.stale);
      rail.setItems(railItems()); // the Weather tile shows the current conditions
      break;
  }
}

function renderSystem(s: SystemState | null) {
  if (!s) return;
  lastSystem = s;
  statusPanel.refresh();
  if (!!s.hot !== hot) {
    hot = !!s.hot;
    ambient.setStill(stillBackground || hot);
    slideshow.setCalm(hot);
    stage.dataset.hot = String(hot);
  }
  lanAddress = s.addresses[0] ?? "";
  const up = formatUptime(s.uptimeSeconds);
  const temp = s.cpuTempC == null ? "" : ` · ${s.cpuTempC.toFixed(0)}°C`;
  sysBadge.textContent =
    `${s.hostname} · ${s.addresses.join(", ") || "no network"}\n` +
    `load ${s.load1.toFixed(2)} · ${s.memUsedMB}/${s.memTotalMB} MB${temp} · up ${up}`;
  sysBadge.style.whiteSpace = "pre";
}

function formatUptime(sec: number): string {
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  return d > 0 ? `${d}d ${h}h` : h > 0 ? `${h}h ${m}m` : `${m}m`;
}

// --- Actions ----------------------------------------------------------------

// The background answers to the remote: it flows the way an arrow points and
// swells on a press, so the screen feels alive under the hand.
function stirAmbient(action: Action) {
  switch (action) {
    case "left":
      ambient.nudge(-1, 0);
      break;
    case "right":
      ambient.nudge(1, 0);
      break;
    case "up":
      ambient.nudge(0, -1);
      break;
    case "down":
      ambient.nudge(0, 1);
      break;
    case "select":
      ambient.pulse(1);
      break;
    default:
      ambient.pulse(0.45);
  }
}

input.on((action, ev) => {
  // A held direction runs along the app row. Everywhere else on the dashboard
  // one press is one step: nobody wants to race through photos.
  if (ev.repeat && !(rail.isOpen && (action === "left" || action === "right"))) return;
  // In night mode the first press only brings the dashboard back.
  if (wakeFromNight() && action !== "exit") return;
  railActivity();
  stirAmbient(action);
  if (action === "exit") {
    exitToDesktop();
    return;
  }
  if (help.handle(action)) return play("back");
  if (statusPanel.handle(action)) {
    if (!statusPanel.isOpen) play("back");
    return;
  }
  if (action === "settings") {
    openSettings();
    return;
  }
  if (action === "help") {
    play("open");
    openHelp();
    return;
  }
  if (action === "weather") {
    // A direct shortcut: toggles the details from anywhere.
    if (weather.isOpen) {
      play("back");
      weather.close();
    } else if (weather.hasData) {
      play("open");
      rail.close();
      weather.open();
    } else toast("Weather is not set up. Add a location under [weather] in the settings.", 6000);
    return;
  }
  if (weather.handle(action)) {
    play(weather.isOpen ? "move" : "back");
    return;
  }
  if (rail.handle(action)) return;
  switch (action) {
    case "select":
    case "menu":
      play("open");
      rail.open();
      break;
    case "up":
    case "down": {
      viewChosen = true;
      const to = Math.max(0, Math.min(DENSITIES.length - 1, density + (action === "up" ? 1 : -1)));
      play(to === density ? "edge" : "move");
      density = to;
      overlay.dataset.density = DENSITIES[density];
      break;
    }
    case "home":
      density = 1;
      overlay.dataset.density = DENSITIES[density];
      break;
    case "left":
      play("move");
      slideshow.previous();
      break;
    case "right":
      play("move");
      slideshow.next();
      break;
    case "playpause":
      if (radioPlaying >= 0) {
        // A live station cannot be resumed where it stopped: pausing mutes it.
        if (radio.paused) void radio.play().catch(() => stopRadio());
        else radio.pause();
        break;
      }
      if (slideshow.hasPhotos) toast(slideshow.togglePause() ? "Slideshow paused" : "Slideshow resumed", 2000);
      refreshHud();
      break;
  }
});

// --- Mouse, touchpad and touch screen -------------------------------------------
// The pointer and a small control bar appear when the mouse moves and fade away
// again, so the ambient screen stays clean. Everything the bar does goes
// through the same actions as the keys.

const hud = $("#hud");
const ICONS: Record<string, string> = {
  left: '<path d="M15 5l-7 7 7 7"/>',
  right: '<path d="M9 5l7 7-7 7"/>',
  pause: '<path d="M8 5v14M16 5v14"/>',
  play: '<path d="M7 5l12 7-12 7z"/>',
  menu: '<rect x="4" y="4" width="6.5" height="6.5" rx="1.5"/><rect x="13.5" y="4" width="6.5" height="6.5" rx="1.5"/><rect x="4" y="13.5" width="6.5" height="6.5" rx="1.5"/><rect x="13.5" y="13.5" width="6.5" height="6.5" rx="1.5"/>',
  weather: '<path d="M7 18h10a4 4 0 0 0 .5-7.97 6 6 0 0 0-11.6 1.2A3.5 3.5 0 0 0 7 18z"/>',
  settings: '<circle cx="12" cy="12" r="3.2"/><path d="M12 2.8v2.6M12 18.6v2.6M2.8 12h2.6M18.6 12h2.6M5.5 5.5l1.8 1.8M16.7 16.7l1.8 1.8M18.5 5.5l-1.8 1.8M7.3 16.7l-1.8 1.8"/>',
  help: '<circle cx="12" cy="12" r="9"/><path d="M9.6 9.3a2.5 2.5 0 1 1 3.6 2.3c-.8.5-1.2 1-1.2 1.9M12 16.8v.2"/>',
  exit: '<rect x="3" y="4" width="18" height="12" rx="1.5"/><path d="M8 20h8M12 16v4"/>',
};
const icon = (name: string) =>
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">${ICONS[name]}</svg>`;

const hudButtons: Partial<Record<string, HTMLButtonElement>> = {};
function hudButton(action: Action, iconName: string, label: string, title: string) {
  const b = document.createElement("button");
  b.type = "button";
  b.title = title;
  b.setAttribute("aria-label", title);
  b.innerHTML = icon(iconName) + (label ? `<span>${label}</span>` : "");
  // Never take the keyboard focus: Enter must keep meaning "OK" on the dashboard.
  b.addEventListener("pointerdown", (e) => e.preventDefault());
  b.addEventListener("click", (e) => {
    e.stopPropagation();
    input.emit(action);
  });
  hud.appendChild(b);
  hudButtons[action] = b;
}
hudButton("left", "left", "", "Previous photo");
hudButton("playpause", "pause", "", "Hold or resume the slideshow");
hudButton("right", "right", "", "Next photo");
hud.appendChild(Object.assign(document.createElement("span"), { className: "hud-sep" }));
hudButton("menu", "menu", "Apps", "App row");
hudButton("weather", "weather", "Weather", "Weather details");
hudButton("settings", "settings", "", "Settings");
hudButton("help", "help", "Help", "All controls");
hudButton("exit", "exit", "Desktop", "Return to the desktop");

let hudShowsPaused = false;
function refreshHud() {
  const photos = slideshow.hasPhotos;
  for (const a of ["left", "playpause", "right"]) hudButtons[a]!.hidden = !photos;
  hud.querySelector<HTMLElement>(".hud-sep")!.hidden = !photos;
  hudButtons.weather!.hidden = !weather.hasData;
  // Redraw the icon only when it changes: replacing it while the mouse button
  // is down would swallow the click.
  if (slideshow.isPaused !== hudShowsPaused) {
    hudShowsPaused = slideshow.isPaused;
    hudButtons.playpause!.innerHTML = icon(hudShowsPaused ? "play" : "pause");
  }
}

let pointerTimer = 0;
let pointerTravel = 0;
function pointerActive() {
  railActivity();
  refreshHud();
  document.documentElement.classList.add("pointer-active");
  clearTimeout(pointerTimer);
  pointerTimer = window.setTimeout(() => {
    // Stay while the pointer rests on the bar.
    if (hud.matches(":hover")) pointerActive();
    else document.documentElement.classList.remove("pointer-active");
  }, 3500);
}
window.addEventListener("pointermove", (e) => {
  ambient.pointTo((e.clientX / window.innerWidth) * 2 - 1, (e.clientY / window.innerHeight) * 2 - 1);
  // Ignore the jitter of a mouse lying still and the event browsers fire on load.
  pointerTravel += Math.abs(e.movementX) + Math.abs(e.movementY);
  if (e.pointerType !== "mouse" || pointerTravel > 6) {
    pointerTravel = 0;
    pointerActive();
  }
});
window.addEventListener("pointerdown", pointerActive);

// A click on the screen itself works like OK: it opens the app row, or closes
// whatever is open.
stage.addEventListener("click", (e) => {
  if (help.isOpen) return help.close();
  if (statusPanel.isOpen) return statusPanel.close();
  if (weather.isOpen) return weather.close();
  if ((e.target as Element).closest("#hud")) return;
  if (rail.isOpen) rail.close();
  else rail.open();
});

// Right-click means Back, as in most TV interfaces driven by an air mouse.
window.addEventListener("contextmenu", (e) => {
  e.preventDefault();
  input.emit("back");
});

// The wheel steps through photos, or along the app row while it is open.
let lastWheel = 0;
window.addEventListener(
  "wheel",
  (e) => {
    e.preventDefault();
    if (help.isOpen || weather.isOpen || statusPanel.isOpen) return;
    const now = performance.now();
    if (now - lastWheel < (rail.isOpen ? 110 : 450)) return;
    const delta = Math.abs(e.deltaY) >= Math.abs(e.deltaX) ? e.deltaY : e.deltaX;
    if (delta === 0) return;
    lastWheel = now;
    input.emit(delta > 0 ? "right" : "left");
  },
  { passive: false },
);

// --- Connect ----------------------------------------------------------------

connect("/events", {
  onConfig: applyConfig,
  onState,
  onInput: (ev) => input.fromRemote(ev),
  onApps: (st) => {
    onApps(st);
    appsLoaded = true;
  },
  onPhotos,
  onBattery: (n) => toast(t("{name} is running low: {n} % battery left.", { name: n.name, n: n.percent }), 12000),
  onTV: (st) => {
    tvAsleep = st.asleep;
    tvAvailable = !!st.available;
    sleepAt = st.sleepAt ? new Date(st.sleepAt).getTime() : 0;
    sleepMinutes = st.sleepMinutes ?? 0;
    rail.setItems(railItems());
    updateSuspended();
  },
  onStatus: (ok) => conn.classList.toggle("ok", ok),
});
