// Phone remote: posts actions to the backend, which forwards them to the TV.

const statusDot = document.getElementById("status")!;

async function send(action: string, text?: string) {
  try {
    const res = await fetch("/api/input", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(text === undefined ? { action } : { action, text }),
    });
    statusDot.classList.toggle("ok", res.ok);
  } catch {
    statusDot.classList.remove("ok");
  }
}

document.querySelectorAll<HTMLButtonElement>("button[data-action]").forEach((btn) => {
  btn.addEventListener("click", () => {
    if (navigator.vibrate) navigator.vibrate(10);
    send(btn.dataset.action!);
  });
});

const form = document.getElementById("textform") as HTMLFormElement;
const text = document.getElementById("text") as HTMLInputElement;
form.addEventListener("submit", (e) => {
  e.preventDefault();
  const value = text.value.trim();
  if (!value) return;
  send("text", value);
  text.value = "";
});

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
  apps: { id: string; name: string; mode: string }[];
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
      b.textContent = a.name;
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
events.addEventListener("open", () => statusDot.classList.add("ok"));
events.addEventListener("error", () => statusDot.classList.remove("ok"));

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
