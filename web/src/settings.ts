export {};
// The settings page: weather, background photos and apps. Made for a phone,
// and equally usable on the TV with a mouse and keyboard.

interface AppEntry {
  kind: "preset" | "web" | "file";
  id?: string;
  name: string;
  preset?: string;
  url?: string;
  browser?: string;
  adblock?: boolean;
  userAgent?: string;
  detail?: string;
}

interface Preset {
  id: string;
  name: string;
  description: string;
  needsUrl: boolean;
  urlHint?: string;
  console: boolean;
}

interface Info {
  weather: { location: string; name: string; units: string; coordinates: boolean };
  photos: {
    source: string;
    intervalSeconds: number;
    synology: { url: string; username: string; passwordSet: boolean; insecureTls: boolean; quality: string };
    folderPath: string;
  };
  apps: AppEntry[];
  presets: Preset[];
  consoleReady: boolean;
  pinRequired: boolean;
  version: string;
  keyboardProblem?: string;
}

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;
const statusEl = $("status");
const saveBtn = $<HTMLButtonElement>("save");
const summary = $("summary");

const fromTV = new URLSearchParams(location.search).get("from") === "tv";
if (fromTV) {
  const back = $<HTMLAnchorElement>("back");
  back.href = "/";
  back.setAttribute("aria-label", "Back to the dashboard");
}

let info: Info;
let apps: AppEntry[] = [];
let savedState = "";
let busy = false;

// --- Talking to the Pi, with the PIN when it wants one ---------------------------

const PIN_KEY = "pidash-pin";
let pin = "";
try {
  pin = localStorage.getItem(PIN_KEY) ?? "";
} catch {
  /* private browsing */
}

async function send(method: string, url: string, body: unknown): Promise<Response> {
  for (;;) {
    const res = await fetch(url, {
      method,
      headers: { "Content-Type": "application/json", ...(pin ? { "X-PiDash-PIN": pin } : {}) },
      body: JSON.stringify(body),
    });
    if (res.status !== 401) return res;
    const entered = window.prompt(pin ? "Wrong PIN. Enter the PiDash PIN:" : "Enter the PiDash PIN:");
    if (!entered) return res;
    pin = entered.trim();
    try {
      localStorage.setItem(PIN_KEY, pin);
    } catch {
      /* not stored */
    }
  }
}

function setStatus(text: string, kind: "" | "ok" | "error" = "") {
  statusEl.textContent = text;
  statusEl.className = kind;
}

function setResult(el: HTMLElement, text: string, kind: "" | "ok" | "error" = "") {
  el.textContent = text;
  el.className = "result " + kind;
}

// --- The form <-> one plain object ------------------------------------------------

const val = (id: string) => $<HTMLInputElement>(id).value;

function readForm() {
  const source = val("p-source");
  return {
    weather: { location: val("w-location").trim(), name: info.weather.name, units: val("w-units") },
    photos: {
      source,
      intervalSeconds: Math.max(5, Math.round(Number(val("p-interval")) || 30)),
      synology: {
        url: val("s-url").trim(),
        username: val("s-user").trim(),
        password: val("s-pass"),
        insecureTls: $<HTMLInputElement>("s-tls").checked,
        quality: val("s-quality"),
      },
    },
    apps: apps.map(({ detail: _detail, ...rest }) => rest),
  };
}

function refresh() {
  const source = val("p-source");
  $("p-synology").hidden = source !== "synology";
  $("p-folder").hidden = source !== "folder";
  $("p-interval-row").hidden = source === "";
  const dirty = JSON.stringify(readForm()) !== savedState;
  // Folders can be chosen once the source in use is the one shown here.
  $("p-folders").hidden = source === "" || source !== info.photos.source || dirty;
  saveBtn.disabled = !dirty || busy;
  saveBtn.textContent = busy ? "Saving…" : "Save";
  summary.textContent = dirty ? "Changes not saved yet" : "";
}

function fill(i: Info) {
  info = i;
  $<HTMLInputElement>("w-location").value = i.weather.location;
  $<HTMLSelectElement>("w-units").value = i.weather.units || "metric";
  $("w-coords").hidden = !i.weather.coordinates;

  const source = $<HTMLSelectElement>("p-source");
  source.replaceChildren(new Option("No photos", ""), new Option("Synology Photos on my NAS", "synology"));
  if (i.photos.folderPath) {
    source.add(new Option(`Folder ${i.photos.folderPath}`, "folder"));
    $("p-folder").textContent = `The folder is set in config.toml on the Pi: ${i.photos.folderPath}`;
  }
  source.value = i.photos.source;
  $<HTMLInputElement>("s-url").value = i.photos.synology.url;
  $<HTMLInputElement>("s-user").value = i.photos.synology.username;
  const pass = $<HTMLInputElement>("s-pass");
  pass.value = "";
  pass.placeholder = i.photos.synology.passwordSet ? "Saved. Leave empty to keep it" : "";
  $<HTMLInputElement>("s-tls").checked = i.photos.synology.insecureTls;
  $<HTMLSelectElement>("s-quality").value = i.photos.synology.quality || "original";
  $<HTMLInputElement>("p-interval").value = String(i.photos.intervalSeconds || 30);

  apps = i.apps.map((a) => ({ ...a }));
  renderApps();

  const kb = $("kb-problem");
  kb.hidden = !i.keyboardProblem;
  kb.textContent = i.keyboardProblem
    ? `The TV remote's OK and Back buttons cannot be passed on to apps yet: ${i.keyboardProblem}`
    : "";
  $("version").textContent = i.version ? `PiDash ${i.version}` : "";

  savedState = JSON.stringify(readForm());
  refresh();
}

// --- Apps -----------------------------------------------------------------------

const presetOf = (a: AppEntry) => info.presets.find((p) => p.id === a.preset);

function appDetail(a: AppEntry): string {
  if (a.kind === "preset") {
    const p = presetOf(a);
    if (p?.needsUrl) return a.url ?? "";
    return p?.description ?? "";
  }
  if (a.kind === "web") return (a.url ?? "") + (a.adblock ? "  ·  ads blocked" : "");
  return `From config.toml: ${a.detail ?? a.id ?? ""}`;
}

function renderApps() {
  const list = $("app-list");
  list.replaceChildren(
    ...apps.map((a, i) => {
      const li = document.createElement("li");
      const what = document.createElement("div");
      what.className = "what";
      const name = document.createElement("div");
      name.className = "name";
      name.textContent = a.name || presetOf(a)?.name || a.id || "";
      const detail = document.createElement("div");
      detail.className = "detail";
      detail.textContent = appDetail(a);
      what.append(name, detail);

      const button = (label: string, title: string, disabled: boolean, act: () => void) => {
        const b = document.createElement("button");
        b.type = "button";
        b.textContent = label;
        b.title = title;
        b.setAttribute("aria-label", `${title}: ${name.textContent}`);
        b.disabled = disabled;
        b.addEventListener("click", () => {
          act();
          renderApps();
          refresh();
        });
        return b;
      };
      li.append(
        what,
        button("↑", "Move up", i === 0, () => apps.splice(i - 1, 0, ...apps.splice(i, 1))),
        button("↓", "Move down", i === apps.length - 1, () => apps.splice(i + 1, 0, ...apps.splice(i, 1))),
        button("✕", "Remove", false, () => apps.splice(i, 1)),
      );
      return li;
    }),
  );
  renderAddChoices();
}

const OTHER = "__website";

function renderAddChoices() {
  const kind = $<HTMLSelectElement>("add-kind");
  const previous = kind.value;
  const used = new Set(apps.filter((a) => a.kind === "preset").map((a) => a.preset));
  kind.replaceChildren(
    ...info.presets.filter((p) => !used.has(p.id)).map((p) => new Option(p.name, p.id)),
    new Option("Another website…", OTHER),
  );
  if ([...kind.options].some((o) => o.value === previous)) kind.value = previous;
  showAddFields();
}

function showAddFields() {
  const choice = val("add-kind");
  const p = info.presets.find((x) => x.id === choice);
  const other = choice === OTHER;
  $("add-name-row").hidden = !other;
  $("add-adblock-row").hidden = !other;
  $("add-url-row").hidden = !(other || p?.needsUrl);
  $<HTMLInputElement>("add-url").placeholder = other ? "https://…" : (p?.urlHint ?? "");
  let desc = other ? "Opens the address fullscreen, with its own login." : (p?.description ?? "");
  if (p?.console && !info.consoleReady) {
    desc += " It runs outside the desktop, which needs a one-time setup on the Pi: bash deploy/install.sh --console-apps";
  }
  $("add-desc").textContent = desc;
  setResult($("add-result"), "");
}

$("add-kind").addEventListener("change", showAddFields);

$("add-btn").addEventListener("click", () => {
  const choice = val("add-kind");
  const url = val("add-url").trim();
  const result = $("add-result");
  if (choice === OTHER) {
    const name = val("add-name").trim();
    if (!name) return setResult(result, "Give it a name.", "error");
    if (!/^https?:\/\/\S+\.\S+|^https?:\/\/[^\s/]+/.test(url)) return setResult(result, "Enter the full address, starting with http:// or https://", "error");
    apps.push({ kind: "web", name, url, adblock: $<HTMLInputElement>("add-adblock").checked });
  } else {
    const p = info.presets.find((x) => x.id === choice);
    if (!p) return;
    if (p.needsUrl && !/^https?:\/\/[^\s/]+/.test(url)) return setResult(result, "Enter the address you open it with in a browser, starting with http:// or https://", "error");
    apps.push({ kind: "preset", preset: p.id, name: p.name, ...(p.needsUrl ? { url } : {}) });
  }
  $<HTMLInputElement>("add-name").value = "";
  $<HTMLInputElement>("add-url").value = "";
  $<HTMLInputElement>("add-adblock").checked = false;
  renderApps();
  refresh();
});

// --- Checks before saving ---------------------------------------------------------

$("w-test").addEventListener("click", async () => {
  const out = $("w-result");
  setResult(out, "Looking it up…");
  try {
    const res = await send("POST", "/api/settings/test-weather", { location: val("w-location") });
    if (!res.ok) return setResult(out, (await res.text()).trim(), "error");
    setResult(out, `Found: ${(await res.json()).place}`, "ok");
  } catch {
    setResult(out, "The Pi did not answer.", "error");
  }
});

$("s-test").addEventListener("click", async () => {
  const out = $("s-result");
  setResult(out, "Connecting to the NAS…");
  try {
    const res = await send("POST", "/api/settings/test-synology", readForm().photos.synology);
    if (!res.ok) return setResult(out, (await res.text()).trim(), "error");
    const libs: string[] = (await res.json()).libraries;
    setResult(out, `Connected. Found: ${libs.join(", ")}`, "ok");
  } catch {
    setResult(out, "The Pi did not answer.", "error");
  }
});

// --- Saving -----------------------------------------------------------------------

async function startedAt(): Promise<number> {
  const res = await fetch("/api/config", { cache: "no-store" });
  return (await res.json()).started;
}

saveBtn.addEventListener("click", async () => {
  busy = true;
  refresh();
  try {
    const before = await startedAt();
    const res = await send("PUT", "/api/settings", readForm());
    if (!res.ok) {
      setStatus(res.status === 401 ? "The PIN is needed to save." : (await res.text()).trim(), "error");
      return;
    }
    // PiDash restarts to apply the settings: wait until it is back.
    setStatus("Saved. Applying…");
    for (let i = 0; i < 40; i++) {
      await new Promise((r) => setTimeout(r, 400));
      try {
        if ((await startedAt()) !== before) break;
      } catch {
        /* still restarting */
      }
    }
    await load();
    setStatus("Saved.", "ok");
  } catch {
    setStatus("The Pi did not answer. Nothing was saved.", "error");
  } finally {
    busy = false;
    refresh();
  }
});

for (const id of ["w-location", "w-units", "p-source", "s-url", "s-user", "s-pass", "s-tls", "s-quality", "p-interval"]) {
  $(id).addEventListener("input", refresh);
  $(id).addEventListener("change", refresh);
}

async function load() {
  const res = await fetch("/api/settings", { cache: "no-store" });
  if (!res.ok) throw new Error(await res.text());
  fill(await res.json());
  $("main").hidden = false;
  $("footer").hidden = false;
}

load()
  .then(() => setStatus("Changes take effect a few seconds after saving."))
  .catch(() => setStatus("The PiDash backend is not reachable.", "error"));
