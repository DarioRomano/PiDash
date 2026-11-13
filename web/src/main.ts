import { Ambient, iconPalette } from "./ambient";
import { Clock } from "./clock";
import { Help } from "./help";
import { Input } from "./input";
import { Rail, type RailItem } from "./rail";
import { Slideshow } from "./slideshow";
import { connect } from "./sse";
import type { Action, AppsStatus, ClientConfig, PhotoItem, PhotosStatus, StateEntry, SystemState, WeatherState } from "./types";
import { WeatherView } from "./weather";

const $ = <T extends HTMLElement>(sel: string) => document.querySelector<T>(sel)!;

const fit = $("#fit");
const stage = $("#stage");
const exitHint = $("#exit-hint");
const toastEl = $("#toast");
const railHint = $("#rail-hint");
const overlay = $("#overlay");
const conn = $("#conn");
const stale = $("#stale");
const debug = $("#debug");

const DENSITIES = ["photo", "compact", "detailed"] as const;
let density = 1;

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
let lanAddress = "";

function showCaption(item: PhotoItem | null) {
  if (!item) {
    captionEl.textContent = "";
    return;
  }
  // "Holiday · June 2024": the innermost folder and when the photo was taken.
  const folder = item.folder.split(" / ").pop() ?? "";
  const taken = new Date(item.taken);
  const when = isNaN(taken.getTime()) || taken.getFullYear() < 1990
    ? ""
    : new Intl.DateTimeFormat(dateLocale, { month: "long", year: "numeric" }).format(taken);
  captionEl.textContent = [folder, when].filter(Boolean).join("  ·  ");
}

// Without a photo on screen, the background is a slow play of colours.
const ambient = new Ambient($<HTMLCanvasElement>("#ambient"), $("#grain"));
let photoShowing = false;
function updateAmbient() {
  ambient.setActive(!photoShowing && appsStatus.running === "" && document.visibilityState === "visible" && !tvAsleep);
}

const slideshow = new Slideshow($("#backdrop"), (item) => {
  photoShowing = item !== null;
  updateAmbient();
  showCaption(item);
});
let photosHintShown = false;

function onPhotos(st: PhotosStatus) {
  slideshow.setStatus(st);
  // First run: a source is configured but no folders have been chosen yet.
  if (st.enabled && !st.indexing && st.count === 0 && st.selection.folders.length === 0 && !st.error && !photosHintShown) {
    photosHintShown = true;
    const where = lanAddress ? `http://${lanAddress}:${location.port || "80"}/photos` : "the phone remote";
    toast(`Choose the folders for the background photos: open ${where}`, 15000);
  }
}

let tvAsleep = false;

function updateSuspended() {
  // Do not load photos while an app covers the dashboard, the page is hidden,
  // or the TV has been switched off for the night.
  slideshow.setSuspended(appsStatus.running !== "" || document.visibilityState !== "visible" || tvAsleep);
  updateAmbient();
}

// --- Weather ------------------------------------------------------------------

const WEATHER_ID = "__weather";
const weatherEl = $("#weather");
const weather: WeatherView = new WeatherView(weatherEl, $("#weather-panel"), (open) =>
  stage.classList.toggle("panel-open", open || help.isOpen),
);
// With a mouse or touch screen, clicking the summary opens the details too.
weatherEl.addEventListener("click", (e) => {
  e.stopPropagation();
  weather.open();
});

// --- Help ---------------------------------------------------------------------

const help = new Help($("#help"), (open) => stage.classList.toggle("panel-open", open || weather.isOpen));

function openHelp() {
  const url = lanAddress ? `http://${lanAddress}:${location.port || "80"}/remote` : "";
  help.open(input.keyMap, url);
}

// --- Settings -------------------------------------------------------------------
// Settings need typing, so the TV points to the phone, and offers to open the
// page right here for those with a mouse and keyboard on the Pi.

const SETTINGS_ID = "__settings";
const GEAR_GLYPH =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">' +
  '<circle cx="12" cy="12" r="3.2"/><path d="M12 2.8v2.6M12 18.6v2.6M2.8 12h2.6M18.6 12h2.6M5.5 5.5l1.8 1.8M16.7 16.7l1.8 1.8M18.5 5.5l-1.8 1.8M7.3 16.7l-1.8 1.8"/></svg>';
const settingsPanel = $("#settings-panel");
let settingsOpen = false;

function openSettingsPanel() {
  rail.close();
  const address = lanAddress ? `http://${lanAddress}:${location.port || "80"}/settings` : "the PiDash address followed by /settings";
  const h = document.createElement("h2");
  h.textContent = "Settings";
  const p1 = document.createElement("p");
  p1.textContent = "Weather, background photos and apps are set up on your phone or computer. Open:";
  const url = document.createElement("p");
  url.className = "sheet-url";
  url.textContent = address;
  const p2 = document.createElement("p");
  p2.textContent = "With a mouse and keyboard connected to the Pi, press OK or click to open the settings right here.";
  const foot = document.createElement("footer");
  foot.className = "help-foot";
  foot.textContent = "OK or click: open here  ·  Back: close";
  settingsPanel.replaceChildren(h, p1, url, p2, foot);
  settingsOpen = true;
  settingsPanel.classList.add("open");
  stage.classList.add("panel-open");
}

function closeSettingsPanel() {
  if (!settingsOpen) return;
  settingsOpen = false;
  settingsPanel.classList.remove("open");
  stage.classList.toggle("panel-open", weather.isOpen || help.isOpen);
}

/** While the panel is open, OK opens the page here; anything else closes it. */
function handleSettingsPanel(action: Action): boolean {
  if (!settingsOpen) return false;
  if (action === "select") location.href = "/settings?from=tv";
  else closeSettingsPanel();
  return true;
}

// --- Panel rail and apps ------------------------------------------------------

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
      running,
      sub: running ? "Running · OK to stop" : "",
    });
  }
  items.push({ id: SETTINGS_ID, label: "Settings", glyph: GEAR_GLYPH });
  items.push({ id: EXIT_ID, label: "Desktop", glyph: EXIT_GLYPH });
  return items;
}

function onApps(st: AppsStatus) {
  const previousError = appsStatus.error;
  appsStatus = st;
  rail.setItems(railItems());
  updateSuspended();
  railHint.textContent = st.apps.length
    ? "◀ ▶ choose  ·  OK open  ·  Back close"
    : "Add apps under Settings  ·  Back close";
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
    openSettingsPanel();
    return;
  }
  const stopping = appsStatus.running === item.id;
  toast(stopping ? `Closing ${item.label}…` : `Starting ${item.label}…`);
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
  clock.configure(d.clock_24h, d.locale);
  weather.configure(d.clock_24h, d.locale);
  dateLocale = d.locale || undefined;
  input.setKeyMap(cfg.keys);
  input.setDebugElement(d.debug_keys ? debug : null);
  if (!d.debug_keys) debug.hidden = true;
  startPixelShift(d.pixel_shift);
  railTimeoutMs = (d.app_row_timeout ?? 30) * 1000;
  railActivity();
  sysBadge.hidden = !d.debug_keys;
}

function startPixelShift(max: number) {
  clearInterval(pixelShiftTimer);
  stage.style.setProperty("--shift-x", "0px");
  stage.style.setProperty("--shift-y", "0px");
  if (max <= 0) return;
  pixelShiftTimer = window.setInterval(() => {
    const x = Math.round((Math.random() * 2 - 1) * max);
    const y = Math.round((Math.random() * 2 - 1) * max);
    stage.style.setProperty("--shift-x", `${x}px`);
    stage.style.setProperty("--shift-y", `${y}px`);
  }, 3 * 60 * 1000);
}

// --- State ------------------------------------------------------------------

const staleProviders = new Set<string>();

const sysBadge = document.createElement("div");
sysBadge.id = "sys";
sysBadge.hidden = true;
stage.appendChild(sysBadge);

function onState(entry: StateEntry) {
  if (entry.stale) staleProviders.add(entry.provider);
  else staleProviders.delete(entry.provider);
  stale.hidden = staleProviders.size === 0;

  switch (entry.provider) {
    case "system":
      renderSystem(entry.data as SystemState | null);
      break;
    case "weather":
      weather.update(entry.data as WeatherState | null, entry.stale);
      rail.setItems(railItems()); // the Weather tile shows the current conditions
      break;
  }
}

function renderSystem(s: SystemState | null) {
  if (!s) return;
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

input.on((action) => {
  railActivity();
  stirAmbient(action);
  if (action === "exit") {
    exitToDesktop();
    return;
  }
  if (help.handle(action)) return;
  if (handleSettingsPanel(action)) return;
  if (action === "settings") {
    openSettingsPanel();
    return;
  }
  if (action === "help") {
    openHelp();
    return;
  }
  if (action === "weather") {
    // A direct shortcut: toggles the details from anywhere.
    if (weather.isOpen) weather.close();
    else if (weather.hasData) {
      rail.close();
      weather.open();
    } else toast("Weather is not set up. Add a location under [weather] in the settings.", 6000);
    return;
  }
  if (weather.handle(action)) return;
  if (rail.handle(action)) return;
  switch (action) {
    case "select":
    case "menu":
      rail.open();
      break;
    case "up":
      density = Math.min(DENSITIES.length - 1, density + 1);
      overlay.dataset.density = DENSITIES[density];
      break;
    case "down":
      density = Math.max(0, density - 1);
      overlay.dataset.density = DENSITIES[density];
      break;
    case "home":
      density = 1;
      overlay.dataset.density = DENSITIES[density];
      break;
    case "left":
      slideshow.previous();
      break;
    case "right":
      slideshow.next();
      break;
    case "playpause":
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
  if (settingsOpen) {
    location.href = "/settings?from=tv";
    return;
  }
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
    if (help.isOpen || weather.isOpen || settingsOpen) return;
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
  onTV: (st) => {
    tvAsleep = st.asleep;
    updateSuspended();
  },
  onStatus: (ok) => conn.classList.toggle("ok", ok),
});
