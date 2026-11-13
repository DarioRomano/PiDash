// Driving a settings page with a TV remote.
//
// The settings and photo-picker pages are ordinary forms, made for a phone.
// Opened on the TV (address ending in ?from=tv) they are enlarged, and this
// module makes them usable with nothing but arrows, OK and Back:
//
//   arrows  move a highlight to the nearest control in that direction; on a
//           list with nothing beside it, left and right change its value
//   OK      presses a button, ticks a box, opens a list to choose from, or
//           opens an on-screen keyboard for a text field
//   Back    closes the keyboard or list, otherwise leaves the page
//
// A mouse and a real keyboard keep working as on any page.

export const onTV = new URLSearchParams(location.search).get("from") === "tv";

type Dir = "up" | "down" | "left" | "right";
type Press = Dir | "select" | "back" | "exit";

interface Options {
  /** Back was pressed with nothing else open. */
  onBack: () => void;
}

/** Something that takes over the remote while it is open: a keyboard, a list. */
interface Layer {
  handle(press: Press): void;
}

const CONTROLS = "a[href], button, input, select";
let layer: Layer | null = null;
let current: HTMLElement | null = null;
let options: Options;

export function startTVNav(opts: Options) {
  options = opts;
  const html = document.documentElement;
  html.classList.add("tv");
  // The pages are laid out for a narrow screen; enlarge them to couch size.
  const fit = () => (html.style.zoom = String(Math.max(1, window.innerWidth / 1150)));
  fit();
  window.addEventListener("resize", fit);

  window.addEventListener(
    "keydown",
    (e) => {
      if (e.ctrlKey || e.altKey || e.metaKey) return;
      const press = keyPress(e);
      if (!press) {
        // An arrow that moves the cursor inside a text field. The backend
        // reports the same press; remember it so that copy is dropped.
        const arrow = ({ ArrowLeft: "left", ArrowRight: "right" } as Record<string, Press>)[e.key];
        if (arrow) seen(arrow, "key");
        return;
      }
      e.preventDefault();
      e.stopPropagation();
      if (!e.repeat || press !== "select") dispatch(press, "key");
    },
    true,
  );

  // OK and Back of a TV remote are not keyboard keys; the backend reads them
  // from the remote and passes them on.
  const events = new EventSource("/events");
  events.addEventListener("input", (e) => {
    const ev = JSON.parse((e as MessageEvent).data) as { action: string; source: string };
    if (!["up", "down", "left", "right", "select", "back", "exit"].includes(ev.action)) return;
    const press = ev.action as Press;
    // In a text field, Left and Right belong to the cursor as long as the
    // browser gets the keys itself. Only when it does not (its window has
    // lost the keyboard) are the backend's reports all there is to go by.
    if (ev.source === "evdev" && (press === "left" || press === "right") && !layer && isText(document.activeElement) && document.hasFocus()) {
      seen(press, "evdev"); // pairs up with the key press, whichever came first
      return;
    }
    dispatch(press, ev.source);
  });

  // The first control gets the highlight once the page has filled itself in.
  const first = () => {
    if (!current || !usable(current)) focus(controls()[0] ?? null);
  };
  new MutationObserver(first).observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ["hidden", "disabled"] });
  first();
}

function keyPress(e: KeyboardEvent): Press | null {
  const typing = isText(e.target) && !layer;
  switch (e.key) {
    case "ArrowUp":
      return "up";
    case "ArrowDown":
      return "down";
    // In a text field the arrows move the cursor, and leave the field only
    // from its end.
    case "ArrowLeft":
      return typing && !cursorAt(e.target as HTMLInputElement, "start") ? null : "left";
    case "ArrowRight":
      return typing && !cursorAt(e.target as HTMLInputElement, "end") ? null : "right";
    case "Enter":
      return "select";
    case "Escape":
    case "BrowserBack":
      return "back";
    case "Backspace":
      return isText(e.target) ? null : "back";
  }
  return null;
}

function cursorAt(input: HTMLInputElement, where: "start" | "end"): boolean {
  try {
    const at = where === "start" ? input.selectionStart : input.selectionEnd;
    return at === null || at === (where === "start" ? 0 : input.value.length);
  } catch {
    return true; // number fields have no cursor to ask about
  }
}

// One press of a remote button can arrive twice: as a key in the browser and
// from the backend. The second of such a pair is dropped.
let recent: { press: Press; source: string; at: number }[] = [];

/** Notes a press; returns true when it is the second report of one already seen. */
function seen(press: Press, source: string): boolean {
  if (source !== "key" && source !== "evdev") return false;
  const now = performance.now();
  recent = recent.filter((r) => now - r.at < 300);
  const other = source === "key" ? "evdev" : "key";
  const twin = recent.findIndex((r) => r.press === press && r.source === other);
  if (twin >= 0) {
    recent.splice(twin, 1);
    return true;
  }
  recent.push({ press, source, at: now });
  return false;
}

function dispatch(press: Press, source: string) {
  if (seen(press, source)) return;
  if (press === "exit") {
    // Back held for two seconds: out to the desktop, as on the dashboard.
    void fetch("/api/kiosk", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ action: "stop" }) });
    return;
  }
  if (layer) layer.handle(press);
  else page(press);
}

// --- Moving around the page --------------------------------------------------------

function isText(el: EventTarget | null): el is HTMLInputElement {
  return el instanceof HTMLInputElement && !["checkbox", "radio", "button", "submit", "range"].includes(el.type);
}

function usable(el: HTMLElement): boolean {
  return el.isConnected && el.getClientRects().length > 0 && !(el as HTMLButtonElement).disabled && !el.closest("[data-tv-layer]");
}

function controls(): HTMLElement[] {
  return [...document.querySelectorAll<HTMLElement>(CONTROLS)].filter(usable);
}

function focus(el: HTMLElement | null) {
  current?.classList.remove("tv-focus");
  current = el;
  if (!el) return;
  el.classList.add("tv-focus");
  el.focus({ preventScroll: true });
  if (isText(el)) {
    // The cursor at the end: Right then goes on to what is beside the field.
    try {
      el.setSelectionRange(el.value.length, el.value.length);
    } catch {
      /* number fields */
    }
  }
  el.scrollIntoView({ block: "center", behavior: "smooth" });
}

/**
 * The nearest of the candidates in a direction: it must lie entirely on that
 * side, and of those the closest wins, with a penalty for being off to the side.
 */
function nearest(from: DOMRect, dir: Dir, candidates: HTMLElement[]): HTMLElement | null {
  const cx = from.left + from.width / 2;
  const cy = from.top + from.height / 2;
  let best: HTMLElement | null = null;
  let bestScore = Infinity;
  for (const el of candidates) {
    const r = el.getBoundingClientRect();
    let gap: number, aside: number;
    if (dir === "down" || dir === "up") {
      gap = dir === "down" ? r.top - from.bottom : from.top - r.bottom;
      // Overlapping sideways counts as straight above or below.
      aside = r.right < from.left || r.left > from.right ? Math.abs(r.left + r.width / 2 - cx) : 0;
    } else {
      gap = dir === "right" ? r.left - from.right : from.left - r.right;
      aside = r.bottom < from.top || r.top > from.bottom ? Math.abs(r.top + r.height / 2 - cy) * 4 : 0;
    }
    if (gap < -2) continue;
    const score = gap + aside * 0.6;
    if (score < bestScore) {
      best = el;
      bestScore = score;
    }
  }
  return best;
}

function page(press: Press) {
  if (press === "back") return options.onBack();
  if (press === "exit") return;
  const all = controls();
  if (!current || !usable(current)) {
    focus(all[0] ?? null);
    return;
  }
  const el = current;

  if (press === "select") return activate(el);

  // The bar at the bottom of the screen (Save) is where the page ends: it is
  // reached by going down from the last control, not from wherever it happens
  // to be nearest.
  const inBar = (c: HTMLElement) => !!c.closest("footer");
  const others = all.filter((c) => c !== el);
  let next: HTMLElement | null;
  if (inBar(el)) {
    next = press === "up" ? (all.filter((c) => !inBar(c)).pop() ?? null) : nearest(el.getBoundingClientRect(), press, others.filter(inBar));
  } else {
    next = nearest(el.getBoundingClientRect(), press, others.filter((c) => !inBar(c)));
    if (!next && press === "down") next = all.find(inBar) ?? null;
  }
  if (next) focus(next);
  // A list with nothing beside it takes Left and Right as "previous" and "next".
  else if ((press === "left" || press === "right") && el instanceof HTMLSelectElement) step(el, press === "right" ? 1 : -1);
}

function step(sel: HTMLSelectElement, by: number) {
  const i = sel.selectedIndex + by;
  if (i < 0 || i >= sel.options.length) return;
  sel.selectedIndex = i;
  changed(sel);
}

function changed(el: HTMLElement) {
  el.dispatchEvent(new Event("input", { bubbles: true }));
  el.dispatchEvent(new Event("change", { bubbles: true }));
}

function activate(el: HTMLElement) {
  if (el instanceof HTMLSelectElement) {
    const label = el.closest("label")?.querySelector("span")?.textContent ?? "";
    void choose(label, [...el.options].map((o) => o.text), el.selectedIndex).then((i) => {
      if (i === null || i === el.selectedIndex) return;
      el.selectedIndex = i;
      changed(el);
    });
  } else if (isText(el)) {
    openKeyboard(el);
  } else {
    el.click();
  }
}

// --- A list to choose from -----------------------------------------------------------

/** Shows a list; resolves to the chosen index, or null when Back was pressed. */
export function choose(title: string, items: string[], selected = 0): Promise<number | null> {
  return new Promise((resolve) => {
    const box = document.createElement("div");
    box.className = "tv-layer tv-chooser";
    box.dataset.tvLayer = "";
    const h = document.createElement("h2");
    h.textContent = title;
    const list = document.createElement("div");
    list.className = "tv-list";
    let at = Math.max(0, Math.min(items.length - 1, selected));
    const rows = items.map((text, i) => {
      const row = document.createElement("div");
      row.className = "tv-row";
      row.textContent = text;
      row.addEventListener("click", () => done(i));
      return row;
    });
    list.append(...rows);
    const foot = document.createElement("p");
    foot.className = "tv-foot";
    foot.textContent = "OK: choose  ·  Back: cancel";
    box.append(h, list, foot);
    document.body.append(box);

    const show = () => {
      rows.forEach((r, i) => r.classList.toggle("tv-focus", i === at));
      rows[at]?.scrollIntoView({ block: "center" });
    };
    const done = (result: number | null) => {
      box.remove();
      layer = null;
      resolve(result);
    };
    layer = {
      handle(press) {
        if (press === "up") at = Math.max(0, at - 1);
        else if (press === "down") at = Math.min(items.length - 1, at + 1);
        else if (press === "select") return done(at);
        else if (press === "back") return done(null);
        show();
      },
    };
    show();
  });
}

/** Asks a question with two answers; resolves to true for the first. */
export async function ask(question: string, yes: string, no: string): Promise<boolean | null> {
  const i = await choose(question, [yes, no], 0);
  return i === null ? null : i === 0;
}

// --- The on-screen keyboard ----------------------------------------------------------

const LETTERS = ["1234567890", "qwertyuiop", "asdfghjkl-", "zxcvbnm./:", "@_?=&%+#!~"];
const SYMBOLS = ["1234567890", "!\"#$%&'()*", "+,;<=>?[]^", "{|}~\\`€£°§", "@_-./:    "];

function openKeyboard(input: HTMLInputElement) {
  const box = document.createElement("div");
  box.className = "tv-layer tv-keyboard";
  box.dataset.tvLayer = "";
  const label = document.createElement("div");
  label.className = "tv-kb-label";
  label.textContent = input.closest("label")?.querySelector("span")?.textContent ?? "";
  const value = document.createElement("div");
  value.className = "tv-kb-value";
  const keys = document.createElement("div");
  keys.className = "tv-keys";
  box.append(label, value, keys);
  document.body.append(box);

  let caps = false;
  let symbols = false;
  let focused: HTMLElement | null = null;
  const secret = input.type === "password";

  const showValue = () => {
    // A password shows only what was typed last, as phones do.
    const v = input.value;
    value.textContent = (secret ? "•".repeat(Math.max(0, v.length - 1)) + v.slice(-1) : v) || input.placeholder || " ";
    value.classList.toggle("empty", v === "");
  };
  const type = (text: string) => {
    if (input.type === "number" && !/^[0-9.]$/.test(text)) return;
    input.value += text;
    changed(input);
    showValue();
  };
  const key = (text: string, act: () => void, wide = "") => {
    const b = document.createElement("div");
    b.className = "tv-key " + wide;
    b.textContent = text;
    b.addEventListener("click", act);
    return b;
  };
  const close = () => {
    box.remove();
    layer = null;
    input.removeEventListener("input", showValue);
  };
  const draw = () => {
    const keep = focused?.textContent;
    keys.replaceChildren();
    for (const line of symbols ? SYMBOLS : LETTERS) {
      const row = document.createElement("div");
      row.className = "tv-key-row";
      for (const ch of line) {
        if (ch === " ") continue;
        const shown = caps && !symbols ? ch.toUpperCase() : ch;
        row.append(key(shown, () => type(shown)));
      }
      keys.append(row);
    }
    const last = document.createElement("div");
    last.className = "tv-key-row";
    last.append(
      key(caps ? "abc" : "ABC", () => ((caps = !caps), draw()), "wide"),
      key(symbols ? "abc" : "#+=", () => ((symbols = !symbols), draw()), "wide"),
      key("space", () => type(" "), "wider"),
      key("⌫", () => ((input.value = input.value.slice(0, -1)), changed(input), showValue()), "wide"),
      key("Done", close, "wide done"),
    );
    keys.append(last);
    const all = [...keys.querySelectorAll<HTMLElement>(".tv-key")];
    setFocus(all.find((k) => k.textContent === keep) ?? all[14] ?? all[0]);
  };
  const setFocus = (el: HTMLElement | null) => {
    if (!el) return;
    focused?.classList.remove("tv-focus");
    focused = el;
    el.classList.add("tv-focus");
  };

  layer = {
    handle(press) {
      if (press === "back") return close();
      if (press === "select") return focused?.click();
      if (press === "exit" || !focused) return;
      const others = [...keys.querySelectorAll<HTMLElement>(".tv-key")].filter((k) => k !== focused);
      setFocus(nearest(focused.getBoundingClientRect(), press, others));
    },
  };
  // A real keyboard, if there is one, types straight into the field.
  input.addEventListener("input", showValue);
  input.focus({ preventScroll: true });
  showValue();
  draw();
}

// --- A number pad, for the PIN -------------------------------------------------------

/** Asks for a PIN; resolves to the digits, or null when Back was pressed. */
export function enterPIN(title: string): Promise<string | null> {
  return new Promise((resolve) => {
    const box = document.createElement("div");
    box.className = "tv-layer tv-keyboard tv-pin";
    box.dataset.tvLayer = "";
    const label = document.createElement("div");
    label.className = "tv-kb-label";
    label.textContent = title;
    const value = document.createElement("div");
    value.className = "tv-kb-value";
    const keys = document.createElement("div");
    keys.className = "tv-keys";
    box.append(label, value, keys);
    document.body.append(box);

    let pin = "";
    let focused: HTMLElement | null = null;
    const show = () => (value.textContent = "•".repeat(pin.length) || " ");
    const done = (result: string | null) => {
      box.remove();
      layer = null;
      window.removeEventListener("keydown", typed, true);
      resolve(result);
    };
    const key = (text: string, act: () => void, cls = "") => {
      const b = document.createElement("div");
      b.className = "tv-key " + cls;
      b.textContent = text;
      b.addEventListener("click", act);
      return b;
    };
    for (const line of ["123", "456", "789"]) {
      const row = document.createElement("div");
      row.className = "tv-key-row";
      for (const d of line) row.append(key(d, () => ((pin += d), show())));
      keys.append(row);
    }
    const last = document.createElement("div");
    last.className = "tv-key-row";
    last.append(
      key("⌫", () => ((pin = pin.slice(0, -1)), show())),
      key("0", () => ((pin += "0"), show())),
      key("OK", () => done(pin), "done"),
    );
    keys.append(last);
    const all = () => [...keys.querySelectorAll<HTMLElement>(".tv-key")];
    const setFocus = (el: HTMLElement | null) => {
      if (!el) return;
      focused?.classList.remove("tv-focus");
      focused = el;
      el.classList.add("tv-focus");
    };
    // A real keyboard, and the number buttons of a remote, type directly.
    const typed = (e: KeyboardEvent) => {
      if (/^[0-9]$/.test(e.key)) {
        pin += e.key;
        show();
        e.preventDefault();
        e.stopPropagation();
      }
    };
    window.addEventListener("keydown", typed, true);
    layer = {
      handle(press) {
        if (press === "back") return done(null);
        if (press === "select") return focused?.click();
        if (press === "exit" || !focused) return;
        setFocus(nearest(focused.getBoundingClientRect(), press, all().filter((k) => k !== focused)));
      },
    };
    show();
    setFocus(all()[4]);
  });
}
