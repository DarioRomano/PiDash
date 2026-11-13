import type { Action } from "./types";

export interface RailItem {
  id: string;
  label: string;
  /** Second line under the label, e.g. "Running". */
  sub?: string;
  /** Image URL; falls back to the first letter of the label. */
  icon?: string;
  /** Inline SVG markup used instead of an image or letter. */
  glyph?: string;
  running?: boolean;
  /** An app: its place in the row can be changed with the remote. */
  movable?: boolean;
}

const TILE_PITCH = 284; // tile width + gap, in design pixels
const VIEW_WIDTH = 1920;

/**
 * The panel rail: a row of large tiles along the bottom of the screen, driven
 * entirely by left / right / OK / Back so it works with any TV remote.
 */
export class Rail {
  private items: RailItem[] = [];
  private focus = 0;
  private opened = false;
  private announced: string | null = null;

  /** Called when another tile becomes the selected one; null when the rail closes. */
  onFocus: (item: RailItem | null) => void = () => {};

  // Moving an app: Up picks the selected tile up, Left and Right carry it
  // along, OK puts it down, Back puts it back where it was.
  /** The movable tiles' ids as they were when one was picked up; null while none is. */
  private before: string[] | null = null;
  /** Called when a tile is picked up or put down. */
  onMoving: (moving: boolean) => void = () => {};
  /** Called with the movable tiles' ids in their new order when one was put down elsewhere. */
  onReorder: (ids: string[]) => void = () => {};

  get isMoving() {
    return this.before !== null;
  }

  private movableIds(items = this.items): string[] {
    return items.filter((i) => i.movable).map((i) => i.id);
  }

  /** Puts the movable tiles of items in the given order; the others keep their places. */
  private arranged(items: RailItem[], order: string[]): RailItem[] | null {
    const byId = new Map(items.filter((i) => i.movable).map((i) => [i.id, i]));
    if (byId.size !== order.length || order.some((id) => !byId.has(id))) return null;
    let next = 0;
    return items.map((i) => (i.movable ? byId.get(order[next++])! : i));
  }

  private pickUp() {
    if (!this.items[this.focus]?.movable || this.movableIds().length < 2) return;
    this.before = this.movableIds();
    this.onMoving(true);
    this.updateFocus();
  }

  /** Ends moving: keep=false returns the tile to where it was picked up. */
  private putDown(keep: boolean) {
    const before = this.before;
    if (!before) return;
    this.before = null;
    const now = this.movableIds();
    const changed = now.join("\n") !== before.join("\n");
    if (!keep && changed) {
      const id = this.items[this.focus]?.id;
      this.items = this.arranged(this.items, before) ?? this.items;
      this.focus = Math.max(0, this.items.findIndex((i) => i.id === id));
      if (this.opened) this.render();
    } else this.updateFocus();
    this.onMoving(false);
    if (keep && changed) this.onReorder(now);
  }

  private carry(delta: number) {
    const to = this.focus + delta;
    if (!this.items[to]?.movable) return; // the apps end here
    [this.items[this.focus], this.items[to]] = [this.items[to], this.items[this.focus]];
    this.focus = to;
    this.render();
  }

  constructor(
    private root: HTMLElement,
    private strip: HTMLElement,
    private onActivate: (item: RailItem) => void,
    private onToggle: (open: boolean) => void,
  ) {}

  get isOpen() {
    return this.opened;
  }

  open() {
    if (this.opened) return;
    this.opened = true;
    // Always open on the running app if there is one, otherwise on the first tile.
    this.focus = Math.max(0, this.items.findIndex((i) => i.running));
    this.root.classList.add("open");
    this.root.setAttribute("aria-hidden", "false");
    this.onToggle(true);
    this.render();
  }

  close() {
    if (!this.opened) return;
    this.putDown(false);
    this.opened = false;
    this.root.classList.remove("open");
    this.root.setAttribute("aria-hidden", "true");
    this.onToggle(false);
    this.announce();
  }

  private announce() {
    const item = this.opened ? (this.items[this.focus] ?? null) : null;
    // The icon is part of the comparison: an app's tile can gain one later.
    const key = item ? `${item.id}|${item.icon ?? ""}` : null;
    if (key === this.announced) return;
    this.announced = key;
    this.onFocus(item);
  }

  /** Replaces the tiles, keeping the focus on the same item where possible. */
  setItems(items: RailItem[]) {
    const focusedId = this.items[this.focus]?.id;
    // Tiles are replaced all the time (the weather, a timer). A tile that is
    // being carried stays where it was carried to; if the apps themselves
    // changed meanwhile, the move is off.
    if (this.before) {
      const kept = this.arranged(items, this.movableIds());
      if (kept) items = kept;
      else {
        this.before = null;
        this.onMoving(false);
      }
    }
    this.items = items;
    const idx = items.findIndex((i) => i.id === focusedId);
    this.focus = idx >= 0 ? idx : Math.min(this.focus, Math.max(0, items.length - 1));
    // The tiles change often (the weather, a timer counting down). While the
    // row is closed nobody sees them, so they are only built when it opens.
    if (this.opened) this.render();
  }

  /** Handles an action while open. Returns true when the action was consumed. */
  handle(action: Action): boolean {
    if (!this.opened) return false;
    if (this.before) {
      switch (action) {
        case "left":
          this.carry(-1);
          break;
        case "right":
          this.carry(1);
          break;
        case "select":
        case "up":
        case "down":
          this.putDown(true);
          break;
        case "back":
        case "menu":
        case "home":
          this.putDown(false);
          break;
      }
      return true; // nothing else happens while a tile is in the air
    }
    switch (action) {
      case "left":
        this.move(-1);
        return true;
      case "right":
        this.move(1);
        return true;
      case "select":
        this.activate(this.focus);
        return true;
      case "back":
      case "menu":
      case "home":
        this.close();
        return true;
      case "up":
        this.pickUp();
        return true;
      case "down":
        return true; // nothing below the rail
      default:
        if (/^[1-9]$/.test(action)) {
          this.activate(Number(action) - 1);
          return true;
        }
        return false;
    }
  }

  private move(delta: number) {
    const next = Math.max(0, Math.min(this.items.length - 1, this.focus + delta));
    if (next === this.focus) return;
    this.focus = next;
    this.updateFocus();
  }

  private activate(index: number) {
    const item = this.items[index];
    if (!item) return;
    this.focus = index;
    this.updateFocus();
    this.onActivate(item);
  }

  private render() {
    this.strip.replaceChildren(
      ...this.items.map((item, i) => {
        const tile = document.createElement("div");
        tile.className = "tile";
        tile.classList.toggle("running", !!item.running);

        const art = document.createElement("div");
        art.className = "tile-art";
        if (item.glyph) {
          art.innerHTML = item.glyph;
        } else if (item.icon) {
          const img = document.createElement("img");
          img.src = item.icon;
          img.alt = "";
          // A broken icon falls back to the letter.
          img.addEventListener("error", () => (art.textContent = item.label.charAt(0).toUpperCase()));
          art.appendChild(img);
        } else {
          art.textContent = item.label.charAt(0).toUpperCase();
        }

        const label = document.createElement("div");
        label.className = "tile-label";
        label.textContent = item.label;

        const sub = document.createElement("div");
        sub.className = "tile-sub";
        sub.textContent = item.sub ?? "";

        const num = document.createElement("div");
        num.className = "tile-num";
        num.textContent = i < 9 ? String(i + 1) : "";

        tile.append(num, art, label, sub);
        // Mouse and touch: hovering focuses a tile, clicking chooses it.
        tile.addEventListener("pointerenter", (e) => {
          if (e.pointerType !== "mouse" || this.focus === i || this.before) return;
          this.focus = i;
          this.updateFocus(false);
        });
        tile.addEventListener("click", (e) => {
          e.stopPropagation();
          if (this.before) return this.putDown(true);
          this.activate(i);
        });
        return tile;
      }),
    );
    this.updateFocus();
  }

  /** scroll=false keeps the row still, so tiles do not slide away under the mouse. */
  private updateFocus(scroll = true) {
    const tiles = this.strip.children;
    for (let i = 0; i < tiles.length; i++) {
      tiles[i].classList.toggle("focused", i === this.focus);
      tiles[i].classList.toggle("moving", i === this.focus && this.before !== null);
    }
    this.announce();

    if (!scroll) return;
    // Keep the focused tile on screen when there are more tiles than fit.
    const avail = this.root.clientWidth || VIEW_WIDTH;
    const total = this.items.length * TILE_PITCH;
    let offset = 0;
    if (total > avail) {
      const centre = this.focus * TILE_PITCH + TILE_PITCH / 2;
      offset = Math.max(0, Math.min(total - avail, centre - avail / 2));
    }
    this.strip.style.transform = `translateX(${-offset}px)`;
  }
}
