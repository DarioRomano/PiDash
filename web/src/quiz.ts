import { t } from "./i18n";
import { play } from "./sfx";
import type { Action } from "./types";

interface Question {
  key: string;
  v: string;
  kind: "year" | "place";
  answers: string[];
  right: number;
}

function el<K extends keyof HTMLElementTagNameMap>(tag: K, className: string, text = ""): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  e.className = className;
  e.textContent = text;
  return e;
}

// How long the right answer is shown before the next photo comes.
const SHOWN_MS = 2200;

/**
 * A quiz about your own photos: "Which year was this?" or "Where was this?",
 * with four answers to choose from, ten photos a round.
 */
export class Quiz {
  private opened = false;
  private questions: Question[] = [];
  private at = 0; // which question
  private on = 0; // which answer the highlight is on
  private right = 0;
  /** The answer that was given to the question on screen; -1 while it is open. */
  private given = -1;
  private over = false;
  private timer = 0;
  private answers: HTMLElement | null = null;

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

  open() {
    if (this.opened) return;
    this.opened = true;
    this.root.classList.add("open");
    this.root.setAttribute("aria-hidden", "false");
    this.onToggle(true);
    void this.start();
  }

  close() {
    if (!this.opened) return;
    this.opened = false;
    clearTimeout(this.timer);
    this.root.classList.remove("open");
    this.root.setAttribute("aria-hidden", "true");
    this.onToggle(false);
  }

  /** A new round. */
  private async start() {
    clearTimeout(this.timer);
    this.questions = [];
    this.at = this.right = 0;
    this.over = false;
    this.answers = null;
    this.root.replaceChildren(el("h2", "", "Photo quiz"), el("p", "grid-note", "Loading…"));
    try {
      const res = await fetch("/api/photos/quiz?n=10", { cache: "no-store" });
      if (!res.ok) throw new Error(String(res.status));
      this.questions = ((await res.json()) as { questions: Question[] }).questions;
    } catch {
      if (this.opened) this.root.replaceChildren(el("h2", "", "Photo quiz"), el("p", "grid-note", "The PiDash backend is not reachable."));
      return;
    }
    if (!this.opened) return;
    if (this.questions.length === 0) {
      this.root.replaceChildren(
        el("h2", "", "Photo quiz"),
        el("p", "grid-note", "There is not enough to ask about yet. The quiz needs photos from several years, or from several places."),
        el("footer", "help-foot", "Press any key or click to close"),
      );
      return;
    }
    this.ask();
  }

  /** Shows the question that is next. */
  private ask() {
    const q = this.questions[this.at];
    this.given = -1;
    this.on = 0;
    const picture = el("div", "qz-picture");
    const img = document.createElement("img");
    img.alt = "";
    img.src = `/photos/${encodeURIComponent(q.key)}?v=${encodeURIComponent(q.v)}`;
    picture.appendChild(img);

    const side = el("div", "qz-side");
    side.append(el("div", "qz-count", t("Photo {n} of {all}", { n: this.at + 1, all: this.questions.length })));
    side.append(el("div", "qz-question", q.kind === "year" ? "Which year was this?" : "Where was this?"));
    const answers = el("div", "qz-answers");
    q.answers.forEach((a, i) => {
      const row = el("div", "qz-answer");
      row.append(el("span", "qz-number", String(i + 1)));
      const text = el("span", "qz-text", a);
      text.dataset.noTranslate = "";
      row.append(text);
      row.addEventListener("click", (e) => {
        e.stopPropagation();
        this.answer(i);
      });
      answers.appendChild(row);
    });
    this.answers = answers;
    side.append(answers, el("div", "qz-score", ""));
    const body = el("div", "qz-body");
    body.append(picture, side);
    this.root.replaceChildren(el("h2", "", "Photo quiz"), body, el("footer", "help-foot", "▲ ▼ choose  ·  OK answer  ·  Back close"));
    this.mark();
  }

  private mark() {
    const rows = this.answers?.children;
    if (!rows) return;
    const q = this.questions[this.at];
    for (let i = 0; i < rows.length; i++) {
      rows[i].classList.toggle("focused", this.given < 0 && i === this.on);
      rows[i].classList.toggle("right", this.given >= 0 && i === q.right);
      rows[i].classList.toggle("wrong", this.given >= 0 && i === this.given && i !== q.right);
    }
    const score = this.root.querySelector(".qz-score");
    if (score) score.textContent = this.given < 0 ? "" : this.given === q.right ? t("Right!") : t("It was {answer}.", { answer: q.answers[q.right] });
  }

  private answer(i: number) {
    if (this.given >= 0 || this.over) return;
    const q = this.questions[this.at];
    this.given = i;
    if (i === q.right) {
      this.right++;
      play("select");
    } else play("edge");
    this.mark();
    this.timer = window.setTimeout(() => this.next(), SHOWN_MS);
  }

  private next() {
    clearTimeout(this.timer);
    if (++this.at < this.questions.length) return this.ask();
    this.over = true;
    this.answers = null;
    const all = this.questions.length;
    const words = this.right === all ? "Every one of them. You know your photos." : this.right * 2 >= all ? "Well remembered." : "Time to look at the photos again.";
    const result = el("div", "qz-result", t("{n} of {all} right", { n: this.right, all }));
    result.dataset.noTranslate = "";
    this.root.replaceChildren(el("h2", "", "Photo quiz"), result, el("p", "grid-note", words), el("footer", "help-foot", "OK another round  ·  Back close"));
  }

  handle(action: Action): boolean {
    if (!this.opened) return false;
    if (action === "back" || action === "menu" || action === "home") {
      play("back");
      this.close();
      return true;
    }
    if (this.over) {
      if (action === "select") {
        play("select");
        void this.start();
      }
      return true;
    }
    if (this.questions.length === 0) {
      // Nothing to ask, or still loading: any button leaves.
      if (!this.root.querySelector(".help-foot")) return true;
      play("back");
      this.close();
      return true;
    }
    if (this.given >= 0) {
      // The answer is on screen: OK goes on without waiting.
      if (action === "select") this.next();
      return true;
    }
    const n = this.questions[this.at].answers.length;
    if (action === "up" || action === "down") {
      const to = this.on + (action === "up" ? -1 : 1);
      if (to < 0 || to >= n) play("edge");
      else {
        play("move");
        this.on = to;
        this.mark();
      }
    } else if (action === "select") this.answer(this.on);
    else if (/^[1-9]$/.test(action) && Number(action) <= n) this.answer(Number(action) - 1);
    return true;
  }
}
