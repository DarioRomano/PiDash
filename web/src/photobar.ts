import { play } from "./sfx";
import type { RailItem } from "./rail";
import type { Action } from "./types";

/** How long the bar stays without a button being pressed. */
const IDLE_MS = 12_000;

/**
 * What can be done with the photo on screen, as a small row of round buttons
 * at the bottom of the picture: opened with Up, moved along with Left and
 * Right, and closed by anything else. It is out of the way of the apps, and
 * one press from the photo it is about.
 */
export class PhotoBar {
  private opened = false;
  private items: RailItem[] = [];
  private at = 0;
  private idle = 0;

  constructor(
    private root: HTMLElement,
    private onChoose: (item: RailItem) => void,
    private onToggle: (open: boolean) => void,
  ) {}

  get isOpen() {
    return this.opened;
  }

  /** The buttons; the highlight stays on the one it was on, by its id. */
  setItems(items: RailItem[]) {
    const on = this.items[this.at]?.id;
    this.items = items;
    const same = items.findIndex((i) => i.id === on);
    this.at = same >= 0 ? same : Math.min(this.at, Math.max(0, items.length - 1));
    if (this.opened && items.length === 0) this.close();
    else if (this.opened) this.render();
  }

  /** Opens the bar; false when there is nothing to offer. */
  open(): boolean {
    if (this.items.length === 0) return false;
    if (!this.opened) {
      this.opened = true;
      this.at = 0;
      this.root.classList.add("open");
      this.root.setAttribute("aria-hidden", "false");
      this.onToggle(true);
    }
    this.render();
    this.stay();
    return true;
  }

  close() {
    if (!this.opened) return;
    this.opened = false;
    clearTimeout(this.idle);
    this.root.classList.remove("open");
    this.root.setAttribute("aria-hidden", "true");
    this.onToggle(false);
  }

  private stay() {
    clearTimeout(this.idle);
    this.idle = window.setTimeout(() => this.close(), IDLE_MS);
  }

  private render() {
    const label = document.createElement("div");
    label.className = "pb-label";
    const row = document.createElement("div");
    row.className = "pb-row";
    this.items.forEach((item, i) => {
      const b = document.createElement("div");
      b.className = "pb-item" + (i === this.at ? " focused" : "") + (item.running ? " on" : "");
      b.dataset.id = item.id;
      b.setAttribute("role", "button");
      b.setAttribute("aria-label", item.label);
      b.innerHTML = item.glyph ?? "";
      b.addEventListener("click", (e) => {
        e.stopPropagation();
        this.at = i;
        this.choose();
      });
      row.appendChild(b);
    });
    const current = this.items[this.at];
    // What the highlighted button does, in words; a second line may say more.
    const name = document.createElement("span");
    name.textContent = current?.label ?? "";
    label.appendChild(name);
    if (current?.sub) {
      const sub = document.createElement("small");
      sub.textContent = current.sub;
      sub.dataset.noTranslate = "";
      label.appendChild(sub);
    }
    this.root.replaceChildren(label, row);
  }

  private choose() {
    const item = this.items[this.at];
    if (!item) return;
    this.stay();
    this.onChoose(item);
  }

  /** Handles a button while open; returns true when it was open. */
  handle(action: Action): boolean {
    if (!this.opened) return false;
    switch (action) {
      case "left":
      case "right": {
        const to = this.at + (action === "left" ? -1 : 1);
        if (to < 0 || to >= this.items.length) {
          play("edge");
          break;
        }
        play("move");
        this.at = to;
        this.render();
        this.stay();
        break;
      }
      case "select":
        play("select");
        this.choose();
        break;
      default:
        // Up, Down, Back and everything else: away again.
        play("back");
        this.close();
    }
    return true;
  }
}
