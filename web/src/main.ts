import { Clock } from "./clock";
import { Input } from "./input";
import { connect } from "./sse";
import type { ClientConfig, StateEntry, SystemState } from "./types";

const $ = <T extends HTMLElement>(sel: string) => document.querySelector<T>(sel)!;

const stage = $("#stage");
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
  switch (action) {
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
    // left/right/select/back/playpause/digits are wired up with the photo
    // slideshow and the panel rail in the next milestone.
  }
});

// --- Connect ----------------------------------------------------------------

connect("/events", {
  onConfig: applyConfig,
  onState,
  onInput: (ev) => input.fromRemote(ev),
  onStatus: (ok) => conn.classList.toggle("ok", ok),
});
