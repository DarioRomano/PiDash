import { play } from "./sfx";
import type { Action } from "./types";

export interface MenuItem {
  label: string;
  /** A line under the label that says what it does. */
  hint?: string;
}

function el<K extends keyof HTMLElementTagNameMap>(tag: K, className: string, text = ""): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  e.className = className;
  e.textContent = text;
  return e;
}

/**
 * A short list to choose from, in the middle of the screen: Up and Down move,
 * OK chooses, Back leaves. For the few things on the dashboard that need a
 * question answered before they happen.
 */
export class Menu {
  private opened = false;
  private items: MenuItem[] = [];
  private at = 0;
  private choose: (index: number) => void = () => {};

  constructor(
    private root: HTMLElement,
    private onToggle: (open: boolean) => void,
  ) {}

  get isOpen() {
    return this.opened;
  }

  /** The item the highlight is on. */
  get selected() {
    return this.at;
  }

  /** Shows the list with one item selected; onChoose gets the index of the one chosen. */
  open(title: string, items: MenuItem[], selected: number, onChoose: (index: number) => void) {
    this.items = items;
    this.at = Math.max(0, Math.min(items.length - 1, selected));
    this.choose = onChoose;
    const list = el("div", "menu-list");
    items.forEach((item, i) => {
      const row = el("div", "menu-item");
      row.append(el("div", "menu-label", item.label));
      if (item.hint) row.append(el("div", "menu-hint", item.hint));
      row.addEventListener("click", (e) => {
        e.stopPropagation();
        this.pick(i);
      });
      list.appendChild(row);
    });
    const box = el("div", "menu-box");
    box.append(el("h2", "", title), list, el("div", "menu-foot", "▲ ▼ choose  ·  OK confirm  ·  Back leave"));
    this.root.replaceChildren(box);
    this.mark();
    if (!this.opened) {
      this.opened = true;
      this.root.classList.add("open");
      this.root.setAttribute("aria-hidden", "false");
      this.onToggle(true);
    }
  }

  close() {
    if (!this.opened) return;
    this.opened = false;
    this.root.classList.remove("open");
    this.root.setAttribute("aria-hidden", "true");
    this.onToggle(false);
  }

  private mark() {
    const rows = this.root.querySelectorAll(".menu-item");
    rows.forEach((r, i) => r.classList.toggle("focused", i === this.at));
  }

  private pick(index: number) {
    const choose = this.choose;
    // Closed first: what was chosen may open the menu again with a new question.
    this.close();
    choose(index);
  }

  /** Handles a button while open; returns true when it was open. */
  handle(action: Action): boolean {
    if (!this.opened) return false;
    switch (action) {
      case "up":
      case "down": {
        const to = this.at + (action === "up" ? -1 : 1);
        if (to < 0 || to >= this.items.length) {
          play("edge");
          break;
        }
        play("move");
        this.at = to;
        this.mark();
        break;
      }
      case "select":
        play("select");
        this.pick(this.at);
        break;
      case "back":
      case "menu":
      case "home":
        play("back");
        this.close();
        break;
    }
    return true;
  }
}
