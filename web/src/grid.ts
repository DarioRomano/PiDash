import { play } from "./sfx";
import type { Action } from "./types";

/** One thing to choose on a page: a game with its cover, a folder, a film. */
export interface Card {
  id: string;
  title: string;
  /** A second line: what it is, or what state it is in. */
  sub?: string;
  /** The address of a picture: a cover. */
  image?: string;
  /** Inline SVG where there is no picture. */
  glyph?: string;
  /** Marked: running, or begun. */
  on?: boolean;
  /** A heading over this card and the ones after it, which then start a row of their own. */
  heading?: string;
}

export interface GridOptions {
  title: string;
  cards: Card[];
  /** "covers" are upright pictures in six columns; "wide" ones are film frames in four; "rows" are names in two. */
  shape: "covers" | "wide" | "rows";
  /** The id of the card to start on. */
  selected?: string;
  /** What to say where there is nothing to choose. */
  empty?: string;
  foot?: string;
  onChoose: (card: Card) => void;
  /** Back was pressed; true when that was dealt with and the page stays open. */
  onBack?: () => boolean;
}

function el<K extends keyof HTMLElementTagNameMap>(tag: K, className: string, text = ""): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  e.className = className;
  e.textContent = text;
  return e;
}

// Left alone, the page closes again: the TV goes back to being a picture frame.
const IDLE_MS = 10 * 60_000;

/**
 * A page that fills the screen with things to choose from: the arrows move,
 * OK chooses, Back leaves. For the games and for the videos.
 */
export class Grid {
  private opened = false;
  private opt: GridOptions | null = null;
  private at = 0;
  private columns = 6;
  private cardEls: HTMLElement[] = [];
  private idle = 0;

  constructor(
    private root: HTMLElement,
    private onToggle: (open: boolean) => void,
    /** True while the page should stay although nobody touches it: a video is playing. */
    private stay: () => boolean = () => false,
  ) {
    // A click beside the cards closes the page.
    // It ends there: the click is not also one on the dashboard behind.
    root.addEventListener("click", (e) => {
      e.stopPropagation();
      this.close();
    });
  }

  get isOpen() {
    return this.opened;
  }

  /** What the page is showing: so that others can tell whose page it is. */
  get title() {
    return this.opened ? (this.opt?.title ?? "") : "";
  }

  /** The card the highlight is on. */
  get selected(): Card | null {
    return this.opt?.cards[this.at] ?? null;
  }

  /** Opens the page, or shows something else on the page that is open. */
  open(opt: GridOptions) {
    this.opt = opt;
    this.columns = opt.shape === "covers" ? 6 : opt.shape === "wide" ? 4 : 2;
    this.at = Math.max(0, opt.cards.findIndex((c) => c.id === opt.selected));
    this.draw();
    this.stir();
    if (this.opened) return;
    this.opened = true;
    this.root.classList.add("open");
    this.root.setAttribute("aria-hidden", "false");
    this.onToggle(true);
  }

  /** New cards for the page that is open; the highlight stays on the same one. */
  setCards(cards: Card[]) {
    if (!this.opened || !this.opt) return;
    const id = this.selected?.id;
    this.opt = { ...this.opt, cards };
    const again = cards.findIndex((c) => c.id === id);
    this.at = again >= 0 ? again : Math.min(this.at, Math.max(0, cards.length - 1));
    this.draw();
  }

  close() {
    if (!this.opened) return;
    this.opened = false;
    clearTimeout(this.idle);
    this.root.classList.remove("open");
    this.root.setAttribute("aria-hidden", "true");
    this.onToggle(false);
  }

  private stir() {
    clearTimeout(this.idle);
    this.idle = window.setTimeout(() => (this.stay() ? this.stir() : this.close()), IDLE_MS);
  }

  handle(action: Action): boolean {
    if (!this.opened || !this.opt) return false;
    this.stir();
    const n = this.opt.cards.length;
    const move = (to: number) => {
      // Down from the last full row goes to the last card.
      if (to >= n && Math.floor(this.at / this.columns) < Math.floor((n - 1) / this.columns)) to = n - 1;
      if (to < 0 || to >= n || to === this.at) return play("edge");
      play("move");
      this.at = to;
      this.mark();
    };
    switch (action) {
      case "left":
        move(this.at - 1);
        break;
      case "right":
        move(this.at + 1);
        break;
      case "up":
      case "down": {
        // By where the cards lie: a heading starts a row anew, so rows are
        // not all as long as each other.
        const to = this.above(action === "up" ? -1 : 1);
        if (to < 0) play("edge");
        else move(to);
        break;
      }
      case "select": {
        const card = this.selected;
        if (!card) {
          play("edge");
          break;
        }
        play("select");
        this.opt.onChoose(card);
        break;
      }
      case "back":
      case "menu":
      case "home":
        play("back");
        if (action !== "back" || !this.opt.onBack?.()) this.close();
        break;
      default:
        break; // the other buttons mean nothing here
    }
    return true;
  }

  private draw() {
    const opt = this.opt;
    if (!opt) return;
    const title = el("h2", "", opt.title);
    title.dataset.noTranslate = "";
    const box = el("div", `grid ${opt.shape}`);
    box.style.setProperty("--columns", String(this.columns));
    this.cardEls = [];
    opt.cards.forEach((card, i) => {
      if (card.heading) box.appendChild(el("h3", "grid-heading", card.heading));
      const c = el("div", "card" + (card.on ? " on" : ""));
      this.cardEls.push(c);
      const art = el("div", "card-art");
      if (card.image) {
        const img = document.createElement("img");
        img.alt = "";
        img.loading = "lazy";
        img.decoding = "async";
        // A cover that cannot be loaded leaves its place to the card's
        // drawing, or to the name's first letter.
        img.addEventListener("error", () => {
          if (card.glyph) art.innerHTML = card.glyph;
          else {
            art.classList.add("bare");
            art.textContent = card.title.trim().charAt(0).toUpperCase();
          }
        });
        img.src = card.image;
        art.appendChild(img);
      } else if (card.glyph) {
        art.innerHTML = card.glyph;
      } else {
        // No cover: its first letter, as on a tile without an icon.
        art.classList.add("bare");
        art.textContent = card.title.trim().charAt(0).toUpperCase();
      }
      const words = el("div", "card-words");
      const name = el("div", "card-title", card.title);
      name.dataset.noTranslate = "";
      words.appendChild(name);
      if (card.sub) words.appendChild(el("div", "card-sub", card.sub));
      c.append(art, words);
      c.addEventListener("click", (e) => {
        e.stopPropagation();
        this.at = i;
        this.mark();
        opt.onChoose(card);
      });
      box.appendChild(c);
    });
    const parts: HTMLElement[] = [title];
    if (opt.cards.length === 0) parts.push(el("p", "grid-note", opt.empty ?? "There is nothing here."));
    parts.push(box, el("footer", "help-foot", opt.foot ?? "◀ ▶ ▲ ▼ choose  ·  OK open  ·  Back close"));
    this.root.replaceChildren(...parts);
    this.mark();
  }

  /** Moves the highlight, and the page with it where the card is out of sight. */
  private mark() {
    this.cardEls.forEach((c, i) => c.classList.toggle("focused", i === this.at));
    const here = this.cardEls[this.at];
    if (!here) return;
    // The first card under a heading brings its heading into view with it.
    const before = here.previousElementSibling;
    if (before?.classList.contains("grid-heading")) before.scrollIntoView({ block: "nearest" });
    here.scrollIntoView({ block: "nearest" });
  }

  /**
   * The card in the row above (-1) or below (1) that lies nearest under or
   * over the highlighted one; -1 where there is no such row. Down from a
   * row that is longer than the last goes to the last card.
   */
  private above(dir: -1 | 1): number {
    const here = this.cardEls[this.at]?.getBoundingClientRect();
    if (!here) return -1;
    let best = -1, bestRow = Infinity, bestSide = Infinity;
    this.cardEls.forEach((c, i) => {
      const r = c.getBoundingClientRect();
      const row = dir * (r.top - here.top);
      if (row < here.height / 2) return; // the same row, or the other way
      const side = Math.abs(r.left - here.left);
      if (row < bestRow - 1 || (Math.abs(row - bestRow) <= 1 && side < bestSide)) [best, bestRow, bestSide] = [i, row, side];
    });
    return best;
  }
}
