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
    this.opened = false;
    this.root.classList.remove("open");
    this.root.setAttribute("aria-hidden", "true");
    this.onToggle(false);
  }

  /** Replaces the tiles, keeping the focus on the same item where possible. */
  setItems(items: RailItem[]) {
    const focusedId = this.items[this.focus]?.id;
    this.items = items;
    const idx = items.findIndex((i) => i.id === focusedId);
    this.focus = idx >= 0 ? idx : Math.min(this.focus, Math.max(0, items.length - 1));
    this.render();
  }

  /** Handles an action while open. Returns true when the action was consumed. */
  handle(action: Action): boolean {
    if (!this.opened) return false;
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
      case "down":
        return true; // nothing above or below the rail yet
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
        return tile;
      }),
    );
    this.updateFocus();
  }

  private updateFocus() {
    const tiles = this.strip.children;
    for (let i = 0; i < tiles.length; i++) tiles[i].classList.toggle("focused", i === this.focus);

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
