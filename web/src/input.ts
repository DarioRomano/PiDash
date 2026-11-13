import { isAction, type Action, type InputEvent } from "./types";

type Listener = (action: Action, ev: InputEvent) => void;

/** Holding Back this long shows the "keep holding" hint… */
const HOLD_HINT_MS = 600;
/** …and this long exits to the desktop. */
const HOLD_EXIT_MS = 2000;
/** A release shorter than this, followed by a new press, is still one hold. */
const RELEASE_GRACE_MS = 600;

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
  private releaseTimer = 0;
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
      // Leave shortcuts of the desktop and the browser alone (Alt+F4, Ctrl+R, …).
      if (e.ctrlKey || e.altKey || e.metaKey) return;
      // Prefer `code` (layout-independent); some CEC/RC keymaps only set `key`.
      let mapped = this.keys[e.code] ?? this.keys[e.key];
      // Tab moves through the tiles like in any other program.
      if (e.code === "Tab") mapped = e.shiftKey ? "left" : "right";
      this.showDebug(`${e.code || e.key}${mapped ? " → " + mapped : ""}`);
      if (mapped && isAction(mapped)) {
        e.preventDefault();
        this.dispatch(mapped, { action: mapped, source: "key" });
        if (mapped === "back") this.startHold(e.code || e.key);
      }
    });
    target.addEventListener("keyup", (e) => {
      // Some TVs report a held button as rapid presses: only believe the
      // release if no new press follows shortly.
      if ((e.code || e.key) !== this.holdCode) return;
      clearTimeout(this.releaseTimer);
      this.releaseTimer = window.setTimeout(() => this.cancelHold(), RELEASE_GRACE_MS);
    });
    target.addEventListener("blur", () => this.cancelHold());
  }

  // Holding Back for two seconds is the TV-remote way out of the dashboard,
  // since a TV remote has no Alt+F4.
  private startHold(code: string) {
    clearTimeout(this.releaseTimer);
    if (this.holdCode === code && this.holdTimers.length > 0) return; // same hold continues
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
    clearTimeout(this.releaseTimer);
    for (const t of this.holdTimers) clearTimeout(t);
    this.holdTimers = [];
    this.holdCode = "";
    this.holdListener(false);
  }

  /** Performs an action chosen with the mouse or a touch screen. */
  emit(action: Action) {
    this.showDebug(`pointer → ${action}`);
    this.dispatch(action, { action, source: "pointer" });
  }

  /** The current key map, for the help screen. */
  get keyMap(): Readonly<Record<string, string>> {
    return this.keys;
  }

  /** Called for input events received from the backend: the phone remote, or
   *  a TV remote button the backend read itself. */
  fromRemote(ev: InputEvent) {
    this.showDebug(`${ev.source === "evdev" ? "tv remote" : "phone"} → ${ev.action}${ev.repeat ? " (held)" : ""}`);
    if (!isAction(ev.action)) return;
    // A held button's repeats come from the backend only (the keyboard's own
    // are ignored above), so there is no second report to pair them with.
    if (ev.repeat) for (const l of this.listeners) l(ev.action, ev);
    else this.dispatch(ev.action, ev);
  }

  private dispatch(action: Action, ev: InputEvent) {
    if (this.isDuplicate(action, ev.source)) return;
    for (const l of this.listeners) l(action, ev);
  }

  // One press of a remote button can arrive twice: once as a key event in the
  // browser and once from the backend, which reads the remote directly. The
  // second of such a pair, arriving within a moment of the first, is dropped.
  private recent: { action: Action; source: string; at: number }[] = [];

  private isDuplicate(action: Action, source: string): boolean {
    if (source !== "key" && source !== "evdev") return false;
    const now = performance.now();
    this.recent = this.recent.filter((r) => now - r.at < 300);
    const other = source === "key" ? "evdev" : "key";
    const twin = this.recent.findIndex((r) => r.action === action && r.source === other);
    if (twin >= 0) {
      this.recent.splice(twin, 1);
      return true;
    }
    this.recent.push({ action, source, at: now });
    return false;
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
