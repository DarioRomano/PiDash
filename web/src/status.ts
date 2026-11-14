import { t } from "./i18n";
import type { Action, SystemState } from "./types";

/** What GET /api/status answers. */
interface StatusReport {
  version: string;
  started: number;
  update?: { latest?: string; available: boolean; auto: boolean; possible: boolean; installing: boolean; error?: string };
  tv?: { control: boolean; asleep: boolean; off?: string; on?: string; sleepAt?: string };
  remote?: { devices: string[]; last?: { device: string; key: string; action?: string; at: string }; keys?: string; focus: boolean };
  photos?: { source: string; count: number; indexing: boolean; hidden: number; error?: string };
  sources: { name: string; updatedAt: string; stale: boolean; error?: string }[];
  apps: { count: number; running?: string; error?: string };
  bluetooth?: { name: string; kind: string; battery?: number }[];
  notices: string[];
  problems: { time: string; level: "warn" | "error"; text: string; count: number }[];
}

type Mark = "ok" | "warn" | "none";

const SOURCE_NAMES: Record<string, string> = { weather: "Weather", calendar: "Calendar", home: "Home Assistant" };

function el<K extends keyof HTMLElementTagNameMap>(tag: K, className: string, text = ""): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  e.className = className;
  e.textContent = text;
  return e;
}

/** "12 s", "5 min", "3 h" or "2 d": how long ago something happened. */
function ago(from: number, now = Date.now()): string {
  const sec = Math.max(0, Math.round((now - from) / 1000));
  if (sec < 90) return t("{n} s ago", { n: sec });
  if (sec < 5400) return t("{n} min ago", { n: Math.round(sec / 60) });
  if (sec < 48 * 3600) return t("{n} h ago", { n: Math.round(sec / 3600) });
  return t("{n} days ago", { n: Math.round(sec / 86400) });
}

function span(sec: number): string {
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  return d > 0 ? `${d} d ${h} h` : h > 0 ? `${h} h ${m} min` : `${m} min`;
}

/**
 * A full-screen page that says how PiDash is doing: what the Pi measures,
 * whether the TV and the remote are heard, where photos and weather come
 * from, and what went wrong lately. It is the place to look before reaching
 * for a keyboard and a terminal.
 */
export class StatusPanel {
  private opened = false;
  private timer = 0;
  private report: StatusReport | null = null;
  private failed = false;

  constructor(
    private root: HTMLElement,
    private system: () => SystemState | null,
    private onToggle: (open: boolean) => void,
  ) {}

  get isOpen() {
    return this.opened;
  }

  open() {
    if (this.opened) return;
    this.opened = true;
    this.render();
    this.root.classList.add("open");
    this.root.setAttribute("aria-hidden", "false");
    this.onToggle(true);
    void this.load();
    // Live, so pressing a button on the remote shows up as "last button".
    this.timer = window.setInterval(() => void this.load(), 2000);
  }

  close() {
    if (!this.opened) return;
    this.opened = false;
    clearInterval(this.timer);
    this.root.classList.remove("open");
    this.root.setAttribute("aria-hidden", "true");
    this.onToggle(false);
  }

  /**
   * While open, Back, OK and Menu close it. The other buttons do nothing, so
   * that pressing them can be watched arriving under "Last button".
   */
  handle(action: Action): boolean {
    if (!this.opened) return false;
    if (action === "back" || action === "select" || action === "menu" || action === "home") this.close();
    return true;
  }

  /** New readings from the Pi arrived. */
  refresh() {
    if (this.opened) this.render();
  }

  private async load() {
    try {
      const res = await fetch("/api/status");
      if (!res.ok) throw new Error(String(res.status));
      this.report = (await res.json()) as StatusReport;
      this.failed = false;
    } catch {
      this.failed = true;
    }
    if (this.opened) this.render();
  }

  private render() {
    const r = this.report;
    const sys = this.system();
    const section = (title: string) => {
      const s = el("section", "status-section");
      s.appendChild(el("h3", "", title));
      return s;
    };
    // Values are written in the page's language here, or are names and
    // messages that have none: the automatic translation leaves them alone.
    const row = (into: HTMLElement, label: string, value: string, mark: Mark = "none") => {
      const line = el("div", "status-row");
      const v = el("div", `status-value mark-${mark}`, value);
      v.dataset.noTranslate = "";
      line.append(el("div", "status-label", label), v);
      into.appendChild(line);
    };

    const pi = section("This Pi");
    if (r) {
      const up = r.update;
      let version = r.version;
      let mark: Mark = "none";
      if (up?.installing) version += " · " + t("installing {version}", { version: up.latest ?? "" });
      else if (up?.available) {
        version += " · " + t("{version} is available.", { version: up.latest ?? "" });
        mark = "warn";
      } else if (up?.latest) {
        version += " · " + t("up to date");
        mark = "ok";
      }
      row(pi, "PiDash", version, mark);
      if (up?.error) row(pi, "Updates", up.error, "warn");
    }
    if (sys) {
      if (sys.cpuTempC != null) {
        const temp = `${sys.cpuTempC.toFixed(0)} °C`;
        row(pi, "Temperature", sys.hot ? `${temp} · ${t("hot: movement on screen is paused")}` : temp, sys.hot ? "warn" : "ok");
      }
      const share = sys.memTotalMB > 0 ? sys.memUsedMB / sys.memTotalMB : 0;
      row(pi, "Memory", t("{used} of {total} MB in use", { used: sys.memUsedMB, total: sys.memTotalMB }), share > 0.9 ? "warn" : "ok");
      row(pi, "Load", sys.load1.toFixed(2), sys.load1 > 4 ? "warn" : "ok");
      row(pi, "Switched on for", span(sys.uptimeSeconds));
      row(pi, "Address", sys.addresses.length > 0 ? sys.addresses.map((a) => `${a}:${location.port || "80"}`).join(", ") : t("no network"), sys.addresses.length > 0 ? "ok" : "warn");
    }

    const tv = section("TV and remote");
    if (r?.tv) {
      row(tv, "TV", r.tv.control ? (r.tv.asleep ? t("in standby") : t("answers over HDMI")) : t("does not answer over HDMI, so PiDash cannot switch it on or off"), r.tv.control ? "ok" : "warn");
      if (r.tv.off || r.tv.on) row(tv, "Timetable", [r.tv.off ? t("off at {time}", { time: r.tv.off }) : "", r.tv.on ? t("on at {time}", { time: r.tv.on }) : ""].filter(Boolean).join(" · "));
      if (r.tv.sleepAt) row(tv, "Sleep timer", t("TV off in {n} min", { n: Math.max(1, Math.round((Date.parse(r.tv.sleepAt) - Date.now()) / 60000)) }));
    }
    if (r?.remote) {
      const rem = r.remote;
      row(tv, "Listening to", rem.devices.length > 0 ? rem.devices.join(", ") : t("nothing: no remote, keyboard or controller was found"), rem.devices.length > 0 ? "ok" : "warn");
      const last = rem.last;
      row(tv, "Last button", last ? `${last.key}${last.action ? ` (${last.action})` : ""} · ${last.device} · ${ago(Date.parse(last.at))}` : t("none yet: press one to try"));
      if (rem.keys) row(tv, "Buttons in apps", rem.keys, "warn");
      row(tv, "Keyboard for apps", rem.focus ? t("PiDash hands it to the app that is open") : t("this desktop does not let PiDash choose the window"), rem.focus ? "ok" : "warn");
    }

    for (const d of r?.bluetooth ?? []) {
      const low = d.battery != null && d.battery <= 15;
      row(tv, "Bluetooth", d.battery != null ? `${d.name} · ${t("battery")} ${d.battery} %` : d.name, low ? "warn" : "ok");
    }

    const content = section("Content");
    if (r?.photos) {
      const p = r.photos;
      const source = t(p.source);
      const text = p.error ?? (p.indexing && p.count === 0 ? `${source} · ${t("looking for photos…")}` : t("{source} · {n} photos in the slideshow", { source, n: p.count }));
      row(content, "Photos", text, p.error ? "warn" : p.count > 0 ? "ok" : "none");
    } else if (r) row(content, "Photos", t("not set up"));
    for (const s of r?.sources ?? []) {
      const name = SOURCE_NAMES[s.name] ?? s.name;
      const when = Date.parse(s.updatedAt);
      const fresh = when > 0 ? t("updated {when}", { when: ago(when) }) : t("waiting for the first answer");
      row(content, name, s.error ? `${s.error} · ${fresh}` : fresh, s.error || s.stale ? "warn" : "ok");
    }
    if (r) row(content, "Apps", r.apps.error ?? t("{n} in the app row", { n: r.apps.count }), r.apps.error ? "warn" : "ok");

    const trouble = section("Recent problems");
    const lines = [...(r?.notices ?? []).map((text) => ({ when: "", text })), ...(r?.problems ?? []).slice(0, 7).map((p) => ({ when: ago(Date.parse(p.time)), text: p.count > 1 ? `${p.text} (×${p.count})` : p.text }))];
    if (this.failed && !r) trouble.appendChild(el("p", "status-none", "The PiDash backend is not reachable."));
    else if (lines.length === 0) trouble.appendChild(el("p", "status-none", r ? "None since PiDash started." : "Loading…"));
    for (const line of lines) {
      const p = el("p", "status-problem");
      p.dataset.noTranslate = "";
      if (line.when) p.appendChild(el("span", "status-when", line.when));
      p.appendChild(document.createTextNode(line.text));
      trouble.appendChild(p);
    }

    const left = el("div", "status-col");
    left.append(pi, tv);
    const right = el("div", "status-col");
    right.append(content, trouble);
    const grid = el("div", "status-grid");
    grid.append(left, right);
    this.root.replaceChildren(el("h2", "", "Status"), grid, el("footer", "help-foot", "Press Back or OK to close"));
  }
}
