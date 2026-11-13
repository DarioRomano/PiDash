import { isAction, type Action, type InputEvent } from "./types";

type Listener = (action: Action, ev: InputEvent) => void;

/** Holding Back this long shows the "keep holding" hint… */
const HOLD_HINT_MS = 600;
/** …and this long exits to the desktop. */
const HOLD_EXIT_MS = 2000;

/**
 * Normalises every input source (keyboard / CEC key presses in the browser,
 * phone remote via SSE) into the small Action vocabulary and fans it out.
 */
export class Input {
  private keys: Record<string, string> = {};
  private listeners: Listener[] = [];
  private debugEl: HTMLElement | null = null;
  private debugTimer = 0;
  private holdCode = "";
  private holdTimers: number[] = [];
  private holdListener: (holding: boolean) => void = () => {};

  setKeyMap(keys: Record<string, string>) {
    this.keys = keys;
  }

  setDebugElement(el: HTMLElement | null) {
    this.debugEl = el;
  }

  on(l: Listener) {
    this.listeners.push(l);
  }

  /** Called with true while Back is being held long enough to hint at exiting. */
  onHold(l: (holding: boolean) => void) {
    this.holdListener = l;
  }

  attachKeyboard(target: Window = window) {
    target.addEventListener("keydown", (e) => {
      if (e.repeat) return;
      // Prefer `code` (layout-independent); some CEC/RC keymaps only set `key`.
      const mapped = this.keys[e.code] ?? this.keys[e.key];
      this.showDebug(`${e.code || e.key}${mapped ? " → " + mapped : ""}`);
      if (mapped && isAction(mapped)) {
        e.preventDefault();
        this.dispatch(mapped, { action: mapped, source: "key" });
        if (mapped === "back") this.startHold(e.code || e.key);
      }
    });
    target.addEventListener("keyup", (e) => {
      if ((e.code || e.key) === this.holdCode) this.cancelHold();
    });
    target.addEventListener("blur", () => this.cancelHold());
  }

  // Holding Back for two seconds is the TV-remote way out of the dashboard,
  // since a TV remote has no Alt+F4.
  private startHold(code: string) {
    this.cancelHold();
    this.holdCode = code;
    this.holdTimers = [
      window.setTimeout(() => this.holdListener(true), HOLD_HINT_MS),
      window.setTimeout(() => {
        this.cancelHold();
        this.dispatch("exit", { action: "exit", source: "key" });
      }, HOLD_EXIT_MS),
    ];
  }

  private cancelHold() {
    for (const t of this.holdTimers) clearTimeout(t);
    this.holdTimers = [];
    this.holdCode = "";
    this.holdListener(false);
  }

  /** Called for input events received from the backend (phone remote). */
  fromRemote(ev: InputEvent) {
    this.showDebug(`remote → ${ev.action}`);
    if (isAction(ev.action)) this.dispatch(ev.action, ev);
  }

  private dispatch(action: Action, ev: InputEvent) {
    for (const l of this.listeners) l(action, ev);
  }

  private showDebug(text: string) {
    const el = this.debugEl;
    if (!el) return;
    el.textContent = text;
    el.hidden = false;
    clearTimeout(this.debugTimer);
    this.debugTimer = window.setTimeout(() => (el.hidden = true), 4000);
  }
}
