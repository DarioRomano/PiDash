import { isAction, type Action, type InputEvent } from "./types";

type Listener = (action: Action, ev: InputEvent) => void;

/**
 * Normalises every input source (keyboard / CEC key presses in the browser,
 * phone remote via SSE) into the small Action vocabulary and fans it out.
 */
export class Input {
  private keys: Record<string, string> = {};
  private listeners: Listener[] = [];
  private debugEl: HTMLElement | null = null;
  private debugTimer = 0;

  setKeyMap(keys: Record<string, string>) {
    this.keys = keys;
  }

  setDebugElement(el: HTMLElement | null) {
    this.debugEl = el;
  }

  on(l: Listener) {
    this.listeners.push(l);
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
      }
    });
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
