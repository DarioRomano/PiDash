import { t } from "./i18n";
import { layMap, layTiles, PIN, TILE } from "./map";
import { play } from "./sfx";
import type { Action } from "./types";

function el<K extends keyof HTMLElementTagNameMap>(tag: K, className: string, text = ""): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  e.className = className;
  e.textContent = text;
  return e;
}

// From a country to a region; closer than this the radar's pictures are not offered.
const ZOOMS = [5, 6, 7];
// The last hour, a picture every ten minutes.
const FRAMES = 7;
// How long a picture stays while they run; how long the newest one does,
// where the film goes over from what was to what is expected; and how long
// the last one does, before it starts again.
const STEP_MS = 650;
const NOW_MS = 1300;
const REST_MS = 1800;

/** Where a place is, and how its time is written. */
export interface RadarPlace {
  name: string;
  latitude: number;
  longitude: number;
  timezone?: string;
}

/** The pictures to come: the newest one, moved on as the rain moves. */
interface Ahead {
  from: number;
  times: number[];
  motion: { x: number; y: number };
}

/**
 * The rain radar: where it rained in the last hour, over a map, as pictures
 * that run one after the other up to now, and on into the next hour, where
 * the rain is moved on the way it moved last.
 */
export class Radar {
  private opened = false;
  private place: RadarPlace | null = null;
  private times: number[] = [];
  /** How many of the times are measured; the ones after them are expected. */
  private measured = 0;
  private ahead: Ahead | null = null;
  private line: HTMLElement | null = null;
  private sheets: HTMLElement[] = [];
  private at = 0;
  private zoom = 1;
  private running = true;
  private timer = 0;
  private box: HTMLElement | null = null;
  private clock: HTMLElement | null = null;
  private hour24 = true;
  private locale: string | undefined;

  constructor(
    private root: HTMLElement,
    private onToggle: (open: boolean) => void,
  ) {
    root.addEventListener("click", (e) => {
      e.stopPropagation();
      this.close();
    });
  }

  get isOpen() {
    return this.opened;
  }

  configure(hour24: boolean, locale: string | undefined) {
    this.hour24 = hour24;
    this.locale = locale;
  }

  open(place: RadarPlace) {
    if (this.opened) return;
    this.opened = true;
    this.place = place;
    this.zoom = 1;
    this.running = true;
    this.root.replaceChildren(el("h2", "", "Rain radar"), el("p", "grid-note", "Loading…"));
    this.root.classList.add("open");
    this.root.setAttribute("aria-hidden", "false");
    this.onToggle(true);
    void this.load();
  }

  close() {
    if (!this.opened) return;
    this.opened = false;
    clearTimeout(this.timer);
    this.root.classList.remove("open");
    this.root.setAttribute("aria-hidden", "true");
    // The pictures are let go of: they are an hour of radar, of no use later.
    this.root.replaceChildren();
    this.sheets = [];
    this.box = null;
    this.onToggle(false);
  }

  private async load() {
    try {
      const where = this.place ? `?lat=${this.place.latitude.toFixed(3)}&lon=${this.place.longitude.toFixed(3)}` : "";
      const res = await fetch("/api/radar" + where, { cache: "no-store" });
      if (!res.ok) throw new Error(String(res.status));
      const data = (await res.json()) as { frames: { time: number }[]; ahead?: Ahead };
      this.times = data.frames.map((f) => f.time).slice(-FRAMES);
      this.measured = this.times.length;
      // Only from the picture that is shown as the newest.
      this.ahead = data.ahead && data.ahead.from === this.times[this.measured - 1] ? data.ahead : null;
      if (this.ahead) this.times.push(...this.ahead.times);
    } catch {
      this.times = [];
      this.ahead = null;
    }
    if (!this.opened) return;
    if (this.times.length === 0) {
      this.root.replaceChildren(el("h2", "", "Rain radar"), el("p", "grid-note", "The rain radar cannot be reached right now."), el("footer", "help-foot", "Press any key or click to close"));
      return;
    }
    const title = el("h2", "", t("Rain radar"));
    title.dataset.noTranslate = "";
    const place = el("span", "rd-place", this.place?.name ?? "");
    title.append(" ", place);
    const box = el("div", "pi-map rd-map");
    box.addEventListener("click", (e) => e.stopPropagation());
    this.box = box;
    this.clock = el("div", "rd-clock");
    this.clock.dataset.noTranslate = "";
    // The film as a line: a mark for every picture, the newest one larger.
    this.line = el("div", "rd-line");
    this.line.replaceChildren(...this.times.map((_, i) => el("i", i === this.measured - 1 ? "now" : i < this.measured ? "" : "ahead")));
    const body = el("div", "rd-body");
    body.append(box, this.clock, this.line);
    this.root.replaceChildren(title, body, el("footer", "help-foot", "◀ ▶ earlier or later  ·  OK run or hold  ·  ▲ ▼ closer or further away  ·  Back close"));
    this.at = this.measured - 1;
    this.draw();
  }

  /** Lays the map and, over it, the radar at each moment. */
  private draw() {
    const box = this.box, place = this.place;
    if (!box || !place) return;
    clearTimeout(this.timer);
    const zoom = ZOOMS[this.zoom];
    const map = layMap(box, place.latitude, place.longitude, zoom);
    const ahead = this.ahead;
    this.sheets = this.times.map((time, i) => {
      const made = i >= this.measured && ahead ? `?from=${ahead.from}&mx=${ahead.motion.x}&my=${ahead.motion.y}` : "";
      const sheet = layTiles(box, place.latitude, place.longitude, zoom, TILE, (z, x, y) => `/api/radar/${time}/${z}/${x}/${y}${made}`).layer;
      sheet.classList.add("rd-sheet");
      if (made) sheet.classList.add("rd-ahead");
      return sheet;
    });
    const pin = el("div", "pi-pin");
    pin.innerHTML = PIN;
    const at = map.spot(place.latitude, place.longitude);
    pin.style.left = `${at.x}px`;
    pin.style.top = `${at.y}px`;
    const credit = el("div", "pi-credit", "© OpenStreetMap contributors  ·  RainViewer");
    credit.dataset.noTranslate = "";
    box.replaceChildren(map.note, map.layer, ...this.sheets, pin, credit);
    box.dataset.zoom = String(zoom);
    this.show();
  }

  /** Shows the picture of one moment, and goes on to the next while they run. */
  private show() {
    clearTimeout(this.timer);
    this.sheets.forEach((s, i) => s.classList.toggle("shown", i === this.at));
    if (this.box) this.box.dataset.frame = String(this.at);
    this.line?.querySelectorAll("i").forEach((mark, i) => mark.classList.toggle("on", i === this.at));
    if (this.clock) {
      const when = new Date(this.times[this.at] * 1000);
      let zone = {};
      try {
        new Intl.DateTimeFormat(undefined, { timeZone: this.place?.timezone });
        zone = { timeZone: this.place?.timezone };
      } catch {
        /* a zone this browser does not know: the Pi's own time */
      }
      const time = new Intl.DateTimeFormat(this.locale, { hour: "2-digit", minute: "2-digit", hour12: !this.hour24, ...zone }).format(when);
      const newest = this.at === this.measured - 1;
      const minutes = Math.round((when.getTime() - Date.now()) / 60_000);
      let what: string;
      if (this.at >= this.measured) {
        // Counted from the newest picture, which is "now" on this film.
        const lead = Math.round((this.times[this.at] - this.times[this.measured - 1]) / 60);
        what = `${t("in {n} minutes", { n: lead })}  ·  ${t("expected")}`;
      } else what = newest && minutes > -20 ? t("the latest picture") : t("{n} minutes ago", { n: Math.max(0, -minutes) });
      this.clock.textContent = `${time}  ·  ${what}${this.running ? "" : "  ·  ❚❚"}`;
      this.clock.classList.toggle("ahead", this.at >= this.measured);
    }
    if (!this.running) return;
    const last = this.at === this.times.length - 1;
    const newest = this.at === this.measured - 1;
    this.timer = window.setTimeout(() => {
      this.at = (this.at + 1) % this.times.length;
      this.show();
    }, last ? REST_MS : newest ? NOW_MS : STEP_MS);
  }

  handle(action: Action): boolean {
    if (!this.opened) return false;
    if (action === "back" || action === "menu" || action === "home") {
      play("back");
      this.close();
      return true;
    }
    if (this.sheets.length === 0) {
      // Nothing to show, or still loading.
      if (this.root.querySelector(".help-foot")) {
        play("back");
        this.close();
      }
      return true;
    }
    switch (action) {
      case "left":
      case "right": {
        // By hand: the pictures stop running.
        const to = this.at + (action === "right" ? 1 : -1);
        this.running = false;
        if (to < 0 || to >= this.times.length) play("edge");
        else {
          play("move");
          this.at = to;
        }
        this.show();
        break;
      }
      case "select":
      case "playpause":
        play("select");
        this.running = !this.running;
        this.show();
        break;
      case "up":
      case "down": {
        const to = this.zoom + (action === "up" ? 1 : -1);
        if (to < 0 || to >= ZOOMS.length) {
          play("edge");
          break;
        }
        play("move");
        this.zoom = to;
        this.draw();
        break;
      }
      default:
        break;
    }
    return true;
  }
}
