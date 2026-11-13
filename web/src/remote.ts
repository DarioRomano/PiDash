// Phone remote: posts actions to the backend, which forwards them to the TV.

const statusDot = document.getElementById("status")!;

async function send(action: string) {
  // While an app is open these buttons press real keys, which may need the PIN.
  statusDot.classList.toggle("ok", await inputRequest("/api/input", { action }));
}

document.querySelectorAll<HTMLButtonElement>("button[data-action]").forEach((btn) => {
  btn.addEventListener("click", () => {
    if (navigator.vibrate) navigator.vibrate(10);
    send(btn.dataset.action!);
  });
});

const text = document.getElementById("text") as HTMLInputElement;

// --- Show on TV / Exit to desktop -------------------------------------------

const kioskRow = document.getElementById("kiosk")!;
const startBtn = document.getElementById("kiosk-start")!;
const stopBtn = document.getElementById("kiosk-stop")!;

function showKiosk(s: { available: boolean; active: boolean }) {
  kioskRow.hidden = !s.available;
  startBtn.classList.toggle("current", s.active);
  stopBtn.classList.toggle("current", !s.active);
}

async function refreshKiosk() {
  try {
    const res = await fetch("/api/kiosk");
    if (res.ok) showKiosk(await res.json());
  } catch {
    /* backend unreachable; the status dot already shows it */
  }
}

async function setKiosk(action: "start" | "stop") {
  try {
    const res = await fetch("/api/kiosk", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action }),
    });
    statusDot.classList.toggle("ok", res.ok);
    if (res.ok) showKiosk(await res.json());
  } catch {
    statusDot.classList.remove("ok");
  }
  // The browser starts asynchronously; confirm the real state shortly after.
  window.setTimeout(refreshKiosk, 3000);
}

startBtn.addEventListener("click", () => setKiosk("start"));
stopBtn.addEventListener("click", () => setKiosk("stop"));
refreshKiosk();
document.addEventListener("visibilitychange", () => {
  if (document.visibilityState === "visible") refreshKiosk();
});

// --- Apps: launch on the TV, and get back to the dashboard -------------------

interface AppsStatus {
  apps: { id: string; name: string; mode: string; hasIcon: boolean }[];
  running: string;
  consoleReady: boolean;
  error?: string;
}

const appsEl = document.getElementById("apps")!;
const appList = document.getElementById("app-list")!;
const runningRow = document.getElementById("running")!;
const runningName = document.getElementById("running-name")!;
const appMsg = document.getElementById("app-msg")!;

function showApps(st: AppsStatus) {
  appsEl.hidden = st.apps.length === 0;
  const running = st.apps.find((a) => a.id === st.running);
  runningRow.hidden = !running;
  runningName.textContent = running ? `${running.name} is on the TV` : "";
  appMsg.hidden = !st.error;
  appMsg.textContent = st.error ?? "";
  appList.replaceChildren(
    ...st.apps.map((a) => {
      const b = document.createElement("button");
      if (a.hasIcon) {
        const img = document.createElement("img");
        img.src = `/api/apps/${encodeURIComponent(a.id)}/icon`;
        img.alt = "";
        img.addEventListener("error", () => img.remove());
        b.appendChild(img);
      }
      b.append(a.name);
      b.classList.toggle("current", a.id === st.running);
      b.addEventListener("click", () => appRequest("/api/apps/start", { id: a.id }));
      return b;
    }),
  );
}

async function appRequest(url: string, body: unknown) {
  try {
    const res = await fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    statusDot.classList.toggle("ok", res.ok);
    if (res.ok) showApps(await res.json());
  } catch {
    statusDot.classList.remove("ok");
  }
}

document.getElementById("app-stop")!.addEventListener("click", () => appRequest("/api/apps/stop", {}));

// Live updates, so the phone notices when an app is closed with the controller.
const events = new EventSource("/events");
events.addEventListener("apps", (e) => showApps(JSON.parse((e as MessageEvent).data)));
// The folder picker for the background is offered once a photo source is set up.
events.addEventListener("photos", (e) => {
  document.getElementById("photos-link")!.hidden = !JSON.parse((e as MessageEvent).data).enabled;
});
events.addEventListener("open", () => statusDot.classList.add("ok"));
events.addEventListener("error", () => statusDot.classList.remove("ok"));

// --- TV power ------------------------------------------------------------------

async function tvPower(power: "on" | "off") {
  try {
    const res = await fetch("/api/tv", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ power }),
    });
    statusDot.classList.toggle("ok", res.ok);
  } catch {
    statusDot.classList.remove("ok");
  }
}
document.getElementById("tv-on")!.addEventListener("click", () => tvPower("on"));
document.getElementById("tv-off")!.addEventListener("click", () => tvPower("off"));

// --- Touchpad, keys and typing: the phone as mouse and keyboard ---------------------

const padBox = document.getElementById("pad-box")!;
const pad = document.getElementById("pad")!;
const padNote = document.getElementById("pad-note")!;
const textForm = document.getElementById("textform") as HTMLFormElement;
const dragBtn = document.getElementById("drag")!;

const PIN_KEY = "pidash-pin";
let pin = "";
try {
  pin = localStorage.getItem(PIN_KEY) ?? "";
} catch {
  /* private browsing: ask every time */
}
let askingPin = false;

/** Sends a mouse/keyboard request, asking for the PIN when the Pi wants one. */
async function inputRequest(url: string, body: unknown): Promise<boolean> {
  if (askingPin) return false;
  for (let attempt = 0; attempt < 2; attempt++) {
    let res: Response;
    try {
      res = await fetch(url, {
        method: "POST",
        headers: { "Content-Type": "application/json", ...(pin ? { "X-PiDash-PIN": pin } : {}) },
        body: JSON.stringify(body),
      });
    } catch {
      statusDot.classList.remove("ok");
      return false;
    }
    if (res.status === 401) {
      askingPin = true;
      const entered = window.prompt(pin ? "Wrong PIN. Enter the PiDash PIN:" : "Enter the PiDash PIN:");
      askingPin = false;
      if (!entered) return false;
      pin = entered.trim();
      try {
        localStorage.setItem(PIN_KEY, pin);
      } catch {
        /* not stored */
      }
      continue;
    }
    if (!res.ok) {
      padNote.hidden = false;
      padNote.textContent = (await res.text()).trim();
    } else {
      padNote.hidden = true;
    }
    return res.ok;
  }
  return false;
}

// Movement is collected and sent as one request at a time, so a slow network
// delays the pointer slightly instead of building a queue.
const pending = { dx: 0, dy: 0, scrollX: 0, scrollY: 0 };
let sending = false;

async function flush() {
  if (sending) return;
  const dx = Math.trunc(pending.dx), dy = Math.trunc(pending.dy);
  const scrollX = Math.trunc(pending.scrollX), scrollY = Math.trunc(pending.scrollY);
  if (!dx && !dy && !scrollX && !scrollY) return;
  pending.dx -= dx; pending.dy -= dy; pending.scrollX -= scrollX; pending.scrollY -= scrollY;
  sending = true;
  await inputRequest("/api/pointer", { dx, dy, scrollX, scrollY });
  sending = false;
  void flush();
}

const touches = new Map<number, { x: number; y: number }>();
let gesture = { start: 0, moved: 0, fingers: 0 };
const TAP_MS = 280;
const TAP_SLOP = 10;
const SCROLL_STEP = 22; // pixels of two-finger drag per wheel notch

pad.addEventListener("pointerdown", (e) => {
  pad.setPointerCapture(e.pointerId);
  if (touches.size === 0) gesture = { start: performance.now(), moved: 0, fingers: 0 };
  touches.set(e.pointerId, { x: e.clientX, y: e.clientY });
  gesture.fingers = Math.max(gesture.fingers, touches.size);
  pad.classList.add("active");
});

pad.addEventListener("pointermove", (e) => {
  const last = touches.get(e.pointerId);
  if (!last) return;
  const dx = e.clientX - last.x, dy = e.clientY - last.y;
  touches.set(e.pointerId, { x: e.clientX, y: e.clientY });
  gesture.moved += Math.abs(dx) + Math.abs(dy);
  if (touches.size >= 2) {
    // Content follows the fingers. Each finger reports its own movement, so halve it.
    pending.scrollY += dy / 2 / SCROLL_STEP;
    pending.scrollX -= dx / 2 / SCROLL_STEP;
  } else {
    // Slow movements stay precise; fast swipes cross the screen.
    const speed = Math.hypot(dx, dy);
    const gain = 1.4 + Math.min(2.6, speed / 9);
    pending.dx += dx * gain;
    pending.dy += dy * gain;
  }
  void flush();
});

function endTouch(e: PointerEvent) {
  if (!touches.delete(e.pointerId)) return;
  if (touches.size > 0) return;
  pad.classList.remove("active");
  const quick = performance.now() - gesture.start < TAP_MS && gesture.moved < TAP_SLOP * gesture.fingers;
  if (quick) {
    if (navigator.vibrate) navigator.vibrate(8);
    void inputRequest("/api/pointer", { click: gesture.fingers >= 2 ? "right" : "left" });
  }
}
pad.addEventListener("pointerup", endTouch);
pad.addEventListener("pointercancel", endTouch);

document.getElementById("click-left")!.addEventListener("click", () => inputRequest("/api/pointer", { click: "left" }));
document.getElementById("click-right")!.addEventListener("click", () => inputRequest("/api/pointer", { click: "right" }));

// "Hold" keeps the left button pressed, to drag sliders or select text.
let dragging = false;
dragBtn.addEventListener("click", async () => {
  const next = !dragging;
  if (await inputRequest("/api/pointer", { button: "left", down: next })) {
    dragging = next;
    dragBtn.setAttribute("aria-pressed", String(dragging));
  }
});

document.querySelectorAll<HTMLButtonElement>("button[data-key]").forEach((btn) => {
  btn.addEventListener("click", () => inputRequest("/api/type", { key: btn.dataset.key }));
});

textForm.addEventListener("submit", async (e) => {
  e.preventDefault();
  const value = text.value;
  if (!value) return;
  if (await inputRequest("/api/type", { text: value })) text.value = "";
});

async function loadRemoteInfo() {
  try {
    const info = await (await fetch("/api/remote")).json();
    document.getElementById("tv-row")!.hidden = !info.tv.available;
    padBox.hidden = !info.input.available;
    textForm.hidden = !info.input.typing;
    // Switched on in the settings but not usable yet: say what is missing.
    padNote.hidden = !(info.input.enabled && !info.input.available);
    padNote.textContent = info.input.problem ?? "";
  } catch {
    /* the status dot shows the backend is away */
  }
}
void loadRemoteInfo();

// Keyboard on a laptop works too.
const keymap: Record<string, string> = {
  ArrowUp: "up", ArrowDown: "down", ArrowLeft: "left", ArrowRight: "right",
  Enter: "select", Escape: "back", Backspace: "back",
};
window.addEventListener("keydown", (e) => {
  if (document.activeElement === text) return;
  const a = keymap[e.code] ?? (/^Digit\d$/.test(e.code) ? e.code.slice(5) : undefined);
  if (a) { e.preventDefault(); send(a); }
});

fetch("/healthz").then((r) => statusDot.classList.toggle("ok", r.ok)).catch(() => {});
