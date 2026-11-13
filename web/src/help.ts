import type { Action } from "./types";

/** What each action does, in the order shown on the help screen. */
const ACTIONS: [Action | "digits", string][] = [
  ["select", "Open the app row, choose a tile"],
  ["back", "Close, go back. Hold 2 seconds: exit to the desktop"],
  ["left", "Previous photo, or move left in the app row"],
  ["right", "Next photo, or move right in the app row"],
  ["up", "Show more information"],
  ["down", "Show less information"],
  ["menu", "App row"],
  ["weather", "Weather details"],
  ["playpause", "Hold or resume the slideshow"],
  ["digits", "Choose a tile in the app row by number"],
  ["home", "Back to the standard view"],
  ["help", "This overview"],
  ["exit", "Return to the desktop"],
];

const MOUSE: [string, string][] = [
  ["Move", "Show the control bar"],
  ["Click", "Open or close the app row"],
  ["Click a tile", "Start that app"],
  ["Click the weather", "Weather details"],
  ["Right-click", "Close, go back"],
  ["Wheel", "Previous or next photo; move along the app row"],
];

const NAMES: Record<string, string> = {
  ArrowUp: "↑", ArrowDown: "↓", ArrowLeft: "←", ArrowRight: "→",
  Escape: "Esc", NumpadEnter: "Enter", Slash: "/", ContextMenu: "Menu key",
  MediaPlayPause: "Play/Pause", PageUp: "Page Up", PageDown: "Page Down", BrowserBack: "Browser Back",
};

/** Turns a key code like "KeyW" into what is printed on the key. */
function keyLabel(code: string): string | null {
  if (NAMES[code]) return NAMES[code];
  if (/^Key[A-Z]$/.test(code)) return code.slice(3);
  if (/^Digit\d$/.test(code)) return null; // summarised as "1 – 9"
  if (code === "MediaPlay" || code === "MediaPause") return null; // covered by Play/Pause
  return code;
}

function el<K extends keyof HTMLElementTagNameMap>(tag: K, className: string, text = ""): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  e.className = className;
  e.textContent = text;
  return e;
}

/**
 * A full-screen overview of every way to control the dashboard. The keys are
 * read from the live key map, so it always matches the settings.
 */
export class Help {
  private opened = false;

  constructor(
    private root: HTMLElement,
    private onToggle: (open: boolean) => void,
  ) {}

  get isOpen() {
    return this.opened;
  }

  open(keys: Readonly<Record<string, string>>, remoteURL: string) {
    this.render(keys, remoteURL);
    this.opened = true;
    this.root.classList.add("open");
    this.root.setAttribute("aria-hidden", "false");
    this.onToggle(true);
  }

  close() {
    if (!this.opened) return;
    this.opened = false;
    this.root.classList.remove("open");
    this.root.setAttribute("aria-hidden", "true");
    this.onToggle(false);
  }

  /** While open, any key closes it. */
  handle(_action: Action): boolean {
    if (!this.opened) return false;
    this.close();
    return true;
  }

  private render(keys: Readonly<Record<string, string>>, remoteURL: string) {
    const byAction = new Map<string, string[]>();
    for (const [code, action] of Object.entries(keys)) {
      const label = keyLabel(code);
      if (!label) continue;
      const list = byAction.get(action) ?? [];
      if (!list.includes(label)) list.push(label);
      byAction.set(action, list);
    }
    // Built in, not part of the key map.
    byAction.set("right", [...(byAction.get("right") ?? []), "Tab"]);
    byAction.set("left", [...(byAction.get("left") ?? []), "Shift+Tab"]);

    const row = (keysText: string[], what: string) => {
      const r = el("div", "help-row");
      const k = el("div", "help-keys");
      for (const label of keysText) k.appendChild(el("kbd", "", label));
      r.append(k, el("div", "help-what", what));
      return r;
    };

    const keyboard = el("section", "help-col");
    keyboard.appendChild(el("h3", "", "Keyboard and TV remote"));
    for (const [action, what] of ACTIONS) {
      const labels = action === "digits" ? ["1 – 9"] : (byAction.get(action) ?? []);
      if (labels.length === 0) continue;
      // Short labels first reads best; cap the list so rows stay on one line.
      keyboard.appendChild(row(labels.sort((a, b) => a.length - b.length).slice(0, 5), what));
    }

    const side = el("section", "help-col");
    side.appendChild(el("h3", "", "Mouse or touchpad"));
    for (const [gesture, what] of MOUSE) side.appendChild(row([gesture], what));
    side.appendChild(el("h3", "help-gap", "Phone"));
    side.appendChild(el("p", "help-text", remoteURL ? `Open ${remoteURL} on a phone in your home network.` : "Open the PiDash address followed by /remote on your phone."));

    const grid = el("div", "help-grid");
    grid.append(keyboard, side);
    this.root.replaceChildren(el("h2", "", "Controls"), grid, el("footer", "help-foot", "Press any key or click to close"));
  }
}
