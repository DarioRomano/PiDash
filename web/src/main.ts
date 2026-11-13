import { Clock } from "./clock";
import { Input } from "./input";
import { Rail, type RailItem } from "./rail";
import { connect } from "./sse";
import type { AppsStatus, ClientConfig, StateEntry, SystemState } from "./types";

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
  (open) => stage.classList.toggle("rail-open", open),
);

function railItems(): RailItem[] {
  const items: RailItem[] = appsStatus.apps.map((a) => {
    const running = appsStatus.running === a.id;
    return {
      id: a.id,
      label: a.name,
      icon: a.hasIcon ? `/api/apps/${encodeURIComponent(a.id)}/icon` : undefined,
      running,
      sub: running ? "Running · OK to stop" : "",
    };
  });
  items.push({ id: EXIT_ID, label: "Desktop", glyph: EXIT_GLYPH });
  return items;
}

function onApps(st: AppsStatus) {
  const previousError = appsStatus.error;
  appsStatus = st;
  rail.setItems(railItems());
  railHint.textContent = st.apps.length
    ? "◀ ▶ choose  ·  OK open  ·  Back close"
    : "Add apps under [[apps]] in ~/.config/pidash/config.toml  ·  Back close";
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

function applyConfig(cfg: ClientConfig) {
  const d = cfg.display;
  document.documentElement.style.setProperty("--safe", `${(d.safe_area * 100).toFixed(2)}%`);
  clock.configure(d.clock_24h, d.locale);
  input.setKeyMap(cfg.keys);
  input.setDebugElement(d.debug_keys ? debug : null);
  if (!d.debug_keys) debug.hidden = true;
  startPixelShift(d.pixel_shift);
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
  }
}

function renderSystem(s: SystemState | null) {
  if (!s) return;
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

input.on((action) => {
  if (action === "exit") {
    exitToDesktop();
    return;
  }
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
    // left/right/playpause are wired up with the photo slideshow.
  }
});

// --- Connect ----------------------------------------------------------------

connect("/events", {
  onConfig: applyConfig,
  onState,
  onInput: (ev) => input.fromRemote(ev),
  onApps: (st) => {
    onApps(st);
    appsLoaded = true;
  },
  onStatus: (ok) => conn.classList.toggle("ok", ok),
});
