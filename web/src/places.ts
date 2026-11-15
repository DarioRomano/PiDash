import { t } from "./i18n";
import { layMap, PIN } from "./map";
import { play } from "./sfx";
import type { Action } from "./types";

/** One line of the list: a place, or a trip. */
export interface Spot {
  id: string;
  name: string;
  /** The second line: how many photos, and from when. */
  sub: string;
  latitude: number;
  longitude: number;
  /** A photo from there, to show beside the list. */
  key: string;
  v: string;
}

/** What the page is to show: the places, or the trips. */
export interface SpotList {
  title: string;
  /** What to say when there is nothing to list. */
  empty: string;
  /** What the arrows choose, for the line at the bottom: "a place", "a trip". */
  what: string;
  load(): Promise<{ spots: Spot[]; map: boolean }>;
  onChoose(spot: Spot): void;
}

function el<K extends keyof HTMLElementTagNameMap>(tag: K, className: string, text = ""): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  e.className = className;
  e.textContent = text;
  return e;
}

// How close the map looks: the world, a continent, a country, a region, a town.
const ZOOMS = [2, 4, 6, 9, 12];
// Moving down the list does not fetch a map for every place passed on the way.
const SETTLE_MS = 220;
// Left alone, the page closes again: the TV goes back to being a picture frame.
const IDLE_MS = 10 * 60_000;

/**
 * A page with a list beside a map: every place the photos were taken, or
 * every trip, with a pin on each. Choosing one shows the photos from there.
 */
export class Places {
  private opened = false;
  private places: Spot[] = [];
  private showing: SpotList | null = null;
  private withMap = false;
  private at = 0;
  private zoom = 2;
  private list: HTMLElement | null = null;
  private map: HTMLElement | null = null;
  private picture: HTMLImageElement | null = null;
  private settle = 0;
  private idle = 0;

  constructor(
    private root: HTMLElement,
    private onToggle: (open: boolean) => void,
  ) {
    // A click beside the list closes the page, and is not also one on the dashboard behind.
    root.addEventListener("click", (e) => {
      e.stopPropagation();
      this.close();
    });
  }

  get isOpen() {
    return this.opened;
  }

  open(what: SpotList) {
    if (this.opened) return;
    this.opened = true;
    this.showing = what;
    this.places = [];
    this.list = this.map = null;
    this.root.replaceChildren(el("h2", "", what.title), el("p", "grid-note", "Loading…"));
    this.root.classList.add("open");
    this.root.setAttribute("aria-hidden", "false");
    this.onToggle(true);
    this.stir();
    void this.load();
  }

  private stir() {
    clearTimeout(this.idle);
    this.idle = window.setTimeout(() => this.close(), IDLE_MS);
  }

  close() {
    if (!this.opened) return;
    this.opened = false;
    clearTimeout(this.settle);
    clearTimeout(this.idle);
    this.root.classList.remove("open");
    this.root.setAttribute("aria-hidden", "true");
    this.onToggle(false);
  }

  handle(action: Action): boolean {
    if (!this.opened) return false;
    this.stir();
    const n = this.places.length;
    switch (action) {
      case "up":
      case "down": {
        const to = this.at + (action === "up" ? -1 : 1);
        if (to < 0 || to >= n) {
          play("edge");
          break;
        }
        play("move");
        this.at = to;
        this.mark();
        // The map follows once the list has come to rest.
        clearTimeout(this.settle);
        this.settle = window.setTimeout(() => this.drawMap(), SETTLE_MS);
        break;
      }
      case "left":
      case "right": {
        const to = this.zoom + (action === "right" ? 1 : -1);
        if (!this.map || to < 0 || to >= ZOOMS.length) {
          play("edge");
          break;
        }
        play("move");
        this.zoom = to;
        clearTimeout(this.settle);
        this.drawMap();
        break;
      }
      case "select": {
        const place = this.places[this.at];
        if (!place) {
          play("edge");
          break;
        }
        play("select");
        this.showing?.onChoose(place);
        break;
      }
      case "back":
      case "menu":
      case "home":
        play("back");
        this.close();
        break;
      default:
        break;
    }
    return true;
  }

  private async load() {
    const what = this.showing;
    if (!what) return;
    let data: { spots: Spot[]; map: boolean };
    try {
      data = await what.load();
    } catch {
      if (this.opened) this.root.replaceChildren(el("h2", "", what.title), el("p", "grid-note", "The PiDash backend is not reachable."));
      return;
    }
    if (!this.opened || this.showing !== what) return;
    this.places = data.spots;
    this.withMap = data.map;
    this.at = 0;
    this.zoom = 2;
    if (this.places.length === 0) {
      this.root.replaceChildren(el("h2", "", what.title), el("p", "grid-note", what.empty), el("footer", "help-foot", "Press any key or click to close"));
      return;
    }

    const list = el("div", "pl-list");
    this.places.forEach((p, i) => {
      const row = el("div", "pl-row");
      const name = el("div", "pl-name", p.name);
      name.dataset.noTranslate = "";
      const sub = el("div", "pl-sub", p.sub);
      sub.dataset.noTranslate = "";
      row.append(name, sub);
      row.addEventListener("click", (e) => {
        e.stopPropagation();
        if (this.at === i) return what.onChoose(p);
        this.at = i;
        this.mark();
        this.drawMap();
      });
      list.appendChild(row);
    });
    this.list = list;

    const side = el("div", "pl-side");
    const frame = el("div", "pl-picture");
    this.picture = document.createElement("img");
    this.picture.alt = "";
    frame.appendChild(this.picture);
    side.append(list, frame);

    const body = el("div", "pl-body");
    body.appendChild(side);
    let foot = t("▲ ▼ choose {what}  ·  OK show its photos  ·  Back close", { what: t(what.what) });
    if (this.withMap) {
      this.map = el("div", "pi-map pl-map");
      this.map.addEventListener("click", (e) => e.stopPropagation());
      body.appendChild(this.map);
      foot = t("▲ ▼ choose {what}  ·  ◀ ▶ further away or closer  ·  OK show its photos  ·  Back close", { what: t(what.what) });
    } else {
      body.classList.add("no-map");
    }
    this.root.replaceChildren(el("h2", "", what.title), body, el("footer", "help-foot", foot));
    this.mark();
    this.drawMap();
  }

  /** Moves the highlight, and shows a photo from the place it is on. */
  private mark() {
    const rows = this.list?.children;
    if (!rows) return;
    for (let i = 0; i < rows.length; i++) rows[i].classList.toggle("focused", i === this.at);
    (rows[this.at] as HTMLElement | undefined)?.scrollIntoView({ block: "nearest" });
    const place = this.places[this.at];
    if (place && this.picture) this.picture.src = `/photos/${place.key}?v=${encodeURIComponent(place.v)}`;
  }

  /** The map around the place the highlight is on, with a pin on every place in sight. */
  private drawMap() {
    const box = this.map;
    const here = this.places[this.at];
    if (!box || !here) return;
    const zoom = ZOOMS[this.zoom];
    const laid = layMap(box, here.latitude, here.longitude, zoom);
    const width = box.offsetWidth, height = box.offsetHeight;
    const pins: HTMLElement[] = [];
    for (const p of this.places) {
      const at = laid.spot(p.latitude, p.longitude);
      if (at.x < -30 || at.y < -10 || at.x > width + 30 || at.y > height + 80) continue;
      const pin = el("div", p === here ? "pi-pin" : "pi-pin other");
      pin.innerHTML = PIN;
      pin.style.left = `${at.x}px`;
      pin.style.top = `${at.y}px`;
      pins.push(pin);
    }
    box.replaceChildren(laid.note, laid.layer, ...pins, laid.credit);
    box.dataset.zoom = String(zoom);
    box.dataset.pins = String(pins.length);
  }
}
