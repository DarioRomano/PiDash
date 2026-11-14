import { t } from "./i18n";
import { play } from "./sfx";
import type { Action } from "./types";

/** One photo as GET /api/photos/info describes it. */
interface Detail {
  key: string;
  name: string;
  folder: string;
  taken: string;
  /** The time is the one on the camera's clock: shown as it stands. */
  wallClock: boolean;
  place?: string;
  located: boolean;
  latitude: number;
  longitude: number;
  favourite: boolean;
}

interface Info {
  photos: Detail[];
  map: boolean;
}

function el<K extends keyof HTMLElementTagNameMap>(tag: K, className: string, text = ""): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  e.className = className;
  e.textContent = text;
  return e;
}

/** Text that is a name or a number, and so not for translating. */
function plain(className: string, text: string): HTMLElement {
  const e = el("div", className, text);
  e.dataset.noTranslate = "";
  return e;
}

// How close the map looks: a continent, a country, a region, a town. A piece
// of map is drawn at its own size, not enlarged: enlarging is what made the
// map look soft. The same stretch of country therefore takes four times as
// many pieces, each from one step closer.
const ZOOMS = [5, 8, 11, 14];
const TILE = 256;

/** Where a spot lies on the map of the whole world at a zoom level, in tiles. */
function tileOf(lat: number, lon: number, zoom: number): { x: number; y: number } {
  const n = 2 ** zoom;
  const rad = (Math.max(-85, Math.min(85, lat)) * Math.PI) / 180;
  return { x: ((lon + 180) / 360) * n, y: ((1 - Math.asinh(Math.tan(rad)) / Math.PI) / 2) * n };
}

function coordinates(lat: number, lon: number): string {
  const part = (v: number, pos: string, neg: string) => `${Math.abs(v).toFixed(3)}° ${v >= 0 ? pos : neg}`;
  return `${part(lat, "N", "S")}, ${part(lon, "E", "W")}`;
}

const PIN =
  '<svg viewBox="0 0 48 64" aria-hidden="true"><path d="M24 2C11.800 2 2 11.800 2 24c0 16 22 38 22 38s22-22 22-38C46 11.800 36.200 2 24 2z" fill="#e5484d" stroke="#fff" stroke-width="3"/>' +
  '<circle cx="24" cy="24" r="8" fill="#fff"/></svg>';

/**
 * A full-screen page about the photo on screen: when it was taken, where,
 * which album it is from, and a map when it knows its place.
 */
export class PhotoInfo {
  private opened = false;
  private locale: string | undefined;
  private hour24 = true;
  private info: Info | null = null;
  private zoom = 2;
  private map: HTMLElement | null = null;

  constructor(
    private root: HTMLElement,
    private onToggle: (open: boolean) => void,
  ) {}

  get isOpen() {
    return this.opened;
  }

  configure(hour24: boolean, locale: string | undefined) {
    this.hour24 = hour24;
    this.locale = locale;
  }

  /** Opens the page for a slide: one photo, or two side by side ("a+b"). */
  open(key: string, version: string) {
    if (this.opened) return;
    this.opened = true;
    this.info = null;
    this.zoom = 2;
    this.root.replaceChildren(el("h2", "", "About this photo"), el("p", "pi-note", "Loading…"));
    this.root.classList.add("open");
    this.root.setAttribute("aria-hidden", "false");
    this.onToggle(true);
    void this.load(key, version);
  }

  close() {
    if (!this.opened) return;
    this.opened = false;
    this.root.classList.remove("open");
    this.root.setAttribute("aria-hidden", "true");
    this.map = null;
    this.onToggle(false);
  }

  /** While open: Up and Down look closer or from further away, anything else closes. */
  handle(action: Action): boolean {
    if (!this.opened) return false;
    if ((action === "up" || action === "down") && this.map) {
      const to = this.zoom + (action === "up" ? 1 : -1);
      if (to < 0 || to >= ZOOMS.length) {
        play("edge");
        return true;
      }
      play("move");
      this.zoom = to;
      this.drawMap();
      return true;
    }
    if (action === "left" || action === "right") return true;
    play("back");
    this.close();
    return true;
  }

  private async load(key: string, version: string) {
    let info: Info;
    try {
      const res = await fetch(`/api/photos/info?key=${encodeURIComponent(key)}`, { cache: "no-store" });
      if (!res.ok) throw new Error(String(res.status));
      info = (await res.json()) as Info;
    } catch {
      if (this.opened) this.root.replaceChildren(el("h2", "", "About this photo"), el("p", "pi-note", "Nothing is known about this photo any more."));
      return;
    }
    if (!this.opened) return;
    this.info = info;

    const picture = el("div", "pi-picture");
    const img = document.createElement("img");
    img.src = `/photos/${key}?v=${encodeURIComponent(version)}`;
    img.alt = "";
    picture.appendChild(img);

    const words = el("div", "pi-words");
    info.photos.forEach((d, i) => {
      const block = el("div", "pi-photo");
      if (info.photos.length > 1) block.append(el("div", "pi-which", i === 0 ? "Left photo" : "Right photo"));
      const taken = new Date(d.taken);
      const known = !isNaN(taken.getTime()) && taken.getUTCFullYear() >= 1990;
      if (known) {
        const zone = d.wallClock ? { timeZone: "UTC" } : {};
        block.append(plain("pi-date", new Intl.DateTimeFormat(this.locale, { weekday: "long", day: "numeric", month: "long", year: "numeric", ...zone }).format(taken)));
        const time = new Intl.DateTimeFormat(this.locale, { hour: "2-digit", minute: "2-digit", hour12: !this.hour24, ...zone }).format(taken);
        const now = new Date();
        let years = now.getFullYear() - (d.wallClock ? taken.getUTCFullYear() : taken.getFullYear());
        const month = d.wallClock ? taken.getUTCMonth() : taken.getMonth();
        const day = d.wallClock ? taken.getUTCDate() : taken.getDate();
        if (now.getMonth() < month || (now.getMonth() === month && now.getDate() < day)) years--;
        const ago = years > 1 ? t("{n} years ago", { n: years }) : years === 1 ? t("A year ago") : "";
        block.append(plain("pi-line", [time, ago].filter(Boolean).join("  ·  ")));
      } else {
        block.append(el("div", "pi-date", "Date unknown"));
      }
      const place = d.place || (d.located ? coordinates(d.latitude, d.longitude) : "");
      if (place) block.append(el("div", "pi-label", "Place"), plain("pi-value", place));
      block.append(el("div", "pi-label", "Album"), plain("pi-value", d.folder));
      block.append(plain("pi-file", (d.favourite ? "★  " : "") + d.name));
      words.appendChild(block);
    });

    const body = el("div", "pi-body");
    const left = el("div", "pi-left");
    left.append(picture, words);
    body.appendChild(left);

    const located = info.photos.some((d) => d.located);
    let foot = "Press any key or click to close";
    if (info.map && located) {
      this.map = el("div", "pi-map");
      body.appendChild(this.map);
      foot = "▲ ▼ closer or further away  ·  Back to close";
    } else {
      body.classList.add("no-map");
      if (info.map) words.append(el("p", "pi-nowhere", "This photo does not say where it was taken."));
    }
    this.root.replaceChildren(el("h2", "", "About this photo"), body, el("footer", "help-foot", foot));
    this.drawMap();
  }

  /** Lays the pieces of map around the first photo's place, with a pin on every photo. */
  private drawMap() {
    const box = this.map;
    const spots = this.info?.photos.filter((d) => d.located) ?? [];
    if (!box || spots.length === 0) return;
    const zoom = ZOOMS[this.zoom];
    const n = 2 ** zoom;
    const width = box.offsetWidth, height = box.offsetHeight;
    const centre = tileOf(spots[0].latitude, spots[0].longitude, zoom);

    const note = el("div", "pi-map-note", "Loading the map…");
    const layer = el("div", "pi-tiles");
    let loaded = 0;
    const x0 = Math.floor(centre.x - width / 2 / TILE), x1 = Math.floor(centre.x + width / 2 / TILE);
    const y0 = Math.floor(centre.y - height / 2 / TILE), y1 = Math.floor(centre.y + height / 2 / TILE);
    for (let ty = y0; ty <= y1; ty++) {
      if (ty < 0 || ty >= n) continue; // beyond the poles there is no map
      for (let tx = x0; tx <= x1; tx++) {
        const img = document.createElement("img");
        img.alt = "";
        img.style.left = `${Math.round(width / 2 + (tx - centre.x) * TILE)}px`;
        img.style.top = `${Math.round(height / 2 + (ty - centre.y) * TILE)}px`;
        img.addEventListener("load", () => {
          loaded++;
          img.classList.add("loaded");
        });
        img.addEventListener("error", () => {
          img.remove();
          if (loaded === 0) note.textContent = t("The map could not be loaded.");
        });
        // The world goes round: east of the last piece comes the first again.
        img.src = `/api/map/${zoom}/${((tx % n) + n) % n}/${ty}`;
        layer.appendChild(img);
      }
    }
    const pins = spots.map((d) => {
      const at = tileOf(d.latitude, d.longitude, zoom);
      const pin = el("div", "pi-pin");
      pin.innerHTML = PIN;
      pin.style.left = `${width / 2 + (at.x - centre.x) * TILE}px`;
      pin.style.top = `${height / 2 + (at.y - centre.y) * TILE}px`;
      return pin;
    });
    // Whoever draws the map is to be named on it.
    box.replaceChildren(note, layer, ...pins, plain("pi-credit", "© OpenStreetMap contributors"));
    box.dataset.zoom = String(zoom);
  }
}
