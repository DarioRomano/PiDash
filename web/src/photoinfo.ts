import { t } from "./i18n";
import { play } from "./sfx";
import type { Action } from "./types";
import { layMap, PIN, TILE } from "./map";

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
// The other way to draw it, chosen with OK: each piece twice as large and
// from one step further away. The same stretch of country, softer, but with
// lettering twice the size, for reading place names from the sofa.
const LARGE_KEY = "pidash-map-large";

function coordinates(lat: number, lon: number): string {
  const part = (v: number, pos: string, neg: string) => `${Math.abs(v).toFixed(3)}° ${v >= 0 ? pos : neg}`;
  return `${part(lat, "N", "S")}, ${part(lon, "E", "W")}`;
}

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
  /** Of two photos, the one whose place the map shows. */
  private chosen = 0;
  /** Large lettering instead of a sharp map; remembered on this TV. */
  private large = false;

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
    this.chosen = 0;
    try {
      this.large = localStorage.getItem(LARGE_KEY) === "1";
    } catch {
      this.large = false;
    }
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
    if (action === "select" && this.map) {
      // Sharp, or with large lettering.
      play("select");
      this.large = !this.large;
      try {
        localStorage.setItem(LARGE_KEY, this.large ? "1" : "0");
      } catch {
        /* then only for now */
      }
      this.drawMap();
      return true;
    }
    if (action === "left" || action === "right") {
      // Of two photos, the left or the right one: the map goes to where it was taken.
      const photos = this.info?.photos ?? [];
      const to = action === "left" ? 0 : 1;
      if (!this.map || photos.length < 2 || !photos[to]?.located || to === this.chosen) play("edge");
      else {
        play("move");
        this.chosen = to;
        this.mark();
        this.drawMap();
      }
      return true;
    }
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
    // The map starts with the first photo that knows where it was taken.
    this.chosen = Math.max(0, info.photos.findIndex((d) => d.located));
    let foot = "Press any key or click to close";
    if (info.map && located) {
      this.map = el("div", "pi-map");
      body.appendChild(this.map);
      foot = info.photos.filter((d) => d.located).length > 1 ? "◀ ▶ the left or the right photo  ·  ▲ ▼ closer or further away  ·  OK larger lettering or a sharper map  ·  Back to close" : "▲ ▼ closer or further away  ·  OK larger lettering or a sharper map  ·  Back to close";
    } else {
      body.classList.add("no-map");
      if (info.map) words.append(el("p", "pi-nowhere", "This photo does not say where it was taken."));
    }
    this.root.replaceChildren(el("h2", "", "About this photo"), body, el("footer", "help-foot", foot));
    this.mark();
    this.drawMap();
  }

  /** Marks, of two photos, the one the map is about. */
  private mark() {
    const blocks = this.root.querySelectorAll(".pi-photo");
    blocks.forEach((b, i) => b.classList.toggle("chosen", !!this.map && blocks.length > 1 && i === this.chosen));
  }

  /** Lays the pieces of map around the first photo's place, with a pin on every photo. */
  private drawMap() {
    const box = this.map;
    const photos = this.info?.photos ?? [];
    const spots = photos.filter((d) => d.located);
    if (!box || spots.length === 0) return;
    // The photo that was chosen is the middle of the map; the other keeps a fainter pin.
    const middle = photos[this.chosen]?.located ? photos[this.chosen] : spots[0];
    // Large lettering: pieces from one step further away, drawn at twice their size.
    const zoom = ZOOMS[this.zoom] - (this.large ? 1 : 0);
    const laid = layMap(box, middle.latitude, middle.longitude, zoom, this.large ? TILE * 2 : TILE);
    const pins = spots.map((d) => {
      const at = laid.spot(d.latitude, d.longitude);
      const pin = el("div", d === middle ? "pi-pin" : "pi-pin other");
      pin.innerHTML = PIN;
      pin.style.left = `${at.x}px`;
      pin.style.top = `${at.y}px`;
      return pin;
    });
    box.replaceChildren(laid.note, laid.layer, ...pins, laid.credit);
    box.dataset.zoom = String(zoom);
    box.dataset.lettering = this.large ? "large" : "sharp";
  }
}
