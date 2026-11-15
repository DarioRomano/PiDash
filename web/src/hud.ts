import type { Action } from "./types";

const ICONS: Record<string, string> = {
  left: '<path d="M15 5l-7 7 7 7"/>',
  right: '<path d="M9 5l7 7-7 7"/>',
  pause: '<path d="M8 5v14M16 5v14"/>',
  play: '<path d="M7 5l12 7-12 7z"/>',
  menu: '<rect x="4" y="4" width="6.5" height="6.5" rx="1.5"/><rect x="13.5" y="4" width="6.5" height="6.5" rx="1.5"/><rect x="4" y="13.5" width="6.5" height="6.5" rx="1.5"/><rect x="13.5" y="13.5" width="6.5" height="6.5" rx="1.5"/>',
  weather: '<path d="M7 18h10a4 4 0 0 0 .5-7.97 6 6 0 0 0-11.6 1.2A3.5 3.5 0 0 0 7 18z"/>',
  settings: '<circle cx="12" cy="12" r="3.2"/><path d="M12 2.8v2.6M12 18.6v2.6M2.8 12h2.6M18.6 12h2.6M5.5 5.5l1.8 1.8M16.7 16.7l1.8 1.8M18.5 5.5l-1.8 1.8M7.3 16.7l-1.8 1.8"/>',
  help: '<circle cx="12" cy="12" r="9"/><path d="M9.6 9.3a2.5 2.5 0 1 1 3.6 2.3c-.8.5-1.2 1-1.2 1.9M12 16.8v.2"/>',
  exit: '<rect x="3" y="4" width="18" height="12" rx="1.5"/><path d="M8 20h8M12 16v4"/>',
};
const icon = (name: string) =>
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">${ICONS[name]}</svg>`;

/** What the bar needs to know to show the right buttons. */
export interface HudState {
  photos: boolean;
  paused: boolean;
  weather: boolean;
}

/**
 * For a mouse, a touchpad or a touch screen: the pointer and a small control
 * bar appear when the mouse moves and fade away again, so the ambient screen
 * stays clean. Everything the bar does goes through the same actions as the
 * keys.
 */
export class Hud {
  private buttons: Partial<Record<string, HTMLButtonElement>> = {};
  private showsPaused = false;
  private timer = 0;
  private travel = 0;

  constructor(
    private root: HTMLElement,
    private emit: (action: Action) => void,
    private state: () => HudState,
    /** The pointer moved or pressed: someone is there. */
    private onActive: () => void,
    /** Where the pointer is, from -1 to 1 across and down the window. */
    onPoint: (x: number, y: number) => void,
  ) {
    this.button("left", "left", "", "Previous photo");
    this.button("playpause", "pause", "", "Hold or resume the slideshow");
    this.button("right", "right", "", "Next photo");
    root.appendChild(Object.assign(document.createElement("span"), { className: "hud-sep" }));
    this.button("menu", "menu", "Apps", "App row");
    this.button("weather", "weather", "Weather", "Weather details");
    this.button("settings", "settings", "", "Settings");
    this.button("help", "help", "Help", "All controls");
    this.button("exit", "exit", "Desktop", "Return to the desktop");

    window.addEventListener("pointermove", (e) => {
      onPoint((e.clientX / window.innerWidth) * 2 - 1, (e.clientY / window.innerHeight) * 2 - 1);
      // Ignore the jitter of a mouse lying still and the event browsers fire on load.
      this.travel += Math.abs(e.movementX) + Math.abs(e.movementY);
      if (e.pointerType !== "mouse" || this.travel > 6) {
        this.travel = 0;
        this.active();
      }
    });
    window.addEventListener("pointerdown", () => this.active());
  }

  private button(action: Action, iconName: string, label: string, title: string) {
    const b = document.createElement("button");
    b.type = "button";
    b.title = title;
    b.setAttribute("aria-label", title);
    b.innerHTML = icon(iconName) + (label ? `<span>${label}</span>` : "");
    // Never take the keyboard focus: Enter must keep meaning "OK" on the dashboard.
    b.addEventListener("pointerdown", (e) => e.preventDefault());
    b.addEventListener("click", (e) => {
      e.stopPropagation();
      this.emit(action);
    });
    this.root.appendChild(b);
    this.buttons[action] = b;
  }

  /** Shows the buttons that have something to do right now. */
  refresh() {
    const st = this.state();
    for (const a of ["left", "playpause", "right"]) this.buttons[a]!.hidden = !st.photos;
    this.root.querySelector<HTMLElement>(".hud-sep")!.hidden = !st.photos;
    this.buttons.weather!.hidden = !st.weather;
    // Redraw the icon only when it changes: replacing it while the mouse button
    // is down would swallow the click.
    if (st.paused !== this.showsPaused) {
      this.showsPaused = st.paused;
      this.buttons.playpause!.innerHTML = icon(this.showsPaused ? "play" : "pause");
    }
  }

  private active() {
    this.onActive();
    this.refresh();
    document.documentElement.classList.add("pointer-active");
    clearTimeout(this.timer);
    this.timer = window.setTimeout(() => {
      // Stay while the pointer rests on the bar.
      if (this.root.matches(":hover")) this.active();
      else document.documentElement.classList.remove("pointer-active");
    }, 3500);
  }
}
