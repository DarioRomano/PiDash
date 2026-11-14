import { t } from "./i18n";
import type { Action } from "./types";

/** What GET /api/wifi answers. */
interface Network {
  name: string;
  password: string;
  /** The code: rows of "1" for dark and "0" for light squares. */
  code: string[];
}

function el<K extends keyof HTMLElementTagNameMap>(tag: K, className: string, text = ""): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  e.className = className;
  e.textContent = text;
  return e;
}

/**
 * A full-screen page with the Wi-Fi for visitors: a code to point a phone's
 * camera at, and the name and password in letters for whoever prefers to type.
 */
export class WifiPanel {
  private opened = false;

  constructor(
    private root: HTMLElement,
    private onToggle: (open: boolean) => void,
  ) {}

  get isOpen() {
    return this.opened;
  }

  open() {
    if (this.opened) return;
    this.opened = true;
    this.root.replaceChildren(el("h2", "", "Wi-Fi for visitors"), el("p", "wifi-note", "Loading…"));
    this.root.classList.add("open");
    this.root.setAttribute("aria-hidden", "false");
    this.onToggle(true);
    void this.load();
  }

  close() {
    if (!this.opened) return;
    this.opened = false;
    this.root.classList.remove("open");
    this.root.setAttribute("aria-hidden", "true");
    // The password does not stay in the page once nobody is looking at it.
    this.root.replaceChildren();
    this.onToggle(false);
  }

  /** While open, any button closes it. */
  handle(_action: Action): boolean {
    if (!this.opened) return false;
    this.close();
    return true;
  }

  private async load() {
    let net: Network;
    try {
      const res = await fetch("/api/wifi", { cache: "no-store" });
      if (!res.ok) throw new Error(String(res.status));
      net = (await res.json()) as Network;
    } catch {
      if (this.opened) this.root.replaceChildren(el("h2", "", "Wi-Fi for visitors"), el("p", "wifi-note", "The PiDash backend is not reachable."));
      return;
    }
    if (!this.opened) return;

    // The code as one drawing: a white sheet with a margin four squares
    // wide, which a camera needs to find the code, and a dark square for
    // every "1".
    const n = net.code.length;
    const quiet = 4;
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("viewBox", `0 0 ${n + 2 * quiet} ${n + 2 * quiet}`);
    svg.setAttribute("shape-rendering", "crispEdges");
    svg.setAttribute("role", "img");
    svg.setAttribute("aria-label", t("Code for joining the Wi-Fi"));
    let path = "";
    net.code.forEach((row, y) => {
      for (let x = 0; x < row.length; x++) if (row[x] === "1") path += `M${x + quiet} ${y + quiet}h1v1h-1z`;
    });
    const sheet = document.createElementNS("http://www.w3.org/2000/svg", "rect");
    sheet.setAttribute("width", "100%");
    sheet.setAttribute("height", "100%");
    sheet.setAttribute("fill", "#fff");
    const dark = document.createElementNS("http://www.w3.org/2000/svg", "path");
    dark.setAttribute("d", path);
    dark.setAttribute("fill", "#000");
    svg.append(sheet, dark);
    const code = el("div", "wifi-code");
    code.appendChild(svg);

    // Names and passwords are not for translating.
    const value = (text: string) => {
      const v = el("div", "wifi-value", text);
      v.dataset.noTranslate = "";
      return v;
    };
    const words = el("div", "wifi-words");
    words.append(el("p", "wifi-how", "Point your phone's camera at the code and tap the note that appears."), el("div", "wifi-label", "Network"), value(net.name));
    if (net.password) words.append(el("div", "wifi-label", "Password"), value(net.password));
    else words.append(el("div", "wifi-label", "Password"), el("div", "wifi-value", "None needed"));

    const body = el("div", "wifi-body");
    body.append(code, words);
    this.root.replaceChildren(el("h2", "", "Wi-Fi for visitors"), body, el("footer", "help-foot", "Press any key or click to close"));
  }
}
