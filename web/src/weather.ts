import type { Action, WeatherDay, WeatherState } from "./types";

// --- Icons --------------------------------------------------------------------
// One small hand-drawn set, built from shared parts so every icon matches.

const SUN = "#ffd257";
const CLOUD = "#e8edf6";
const CLOUD_DARK = "#aab4c8";
const WATER = "#7cc4ff";

function sun(cx: number, cy: number, r: number): string {
  let rays = "";
  for (let i = 0; i < 8; i++) {
    const a = (i * Math.PI) / 4;
    const x1 = cx + Math.cos(a) * (r + 4), y1 = cy + Math.sin(a) * (r + 4);
    const x2 = cx + Math.cos(a) * (r + 9), y2 = cy + Math.sin(a) * (r + 9);
    rays += `<line x1="${x1.toFixed(1)}" y1="${y1.toFixed(1)}" x2="${x2.toFixed(1)}" y2="${y2.toFixed(1)}"/>`;
  }
  return `<circle cx="${cx}" cy="${cy}" r="${r}" fill="${SUN}"/><g stroke="${SUN}" stroke-width="3" stroke-linecap="round">${rays}</g>`;
}

// A crescent: a disc with a second, offset disc cut out of its upper right.
const moon = (tx: number, ty: number, s: number) =>
  `<path transform="translate(${tx} ${ty}) scale(${s})" d="M38 10A21 21 0 1 0 54 38 17 17 0 0 1 38 10z" fill="#f3e3a6"/>`;

const cloud = (dx: number, dy: number, fill = CLOUD) =>
  `<path transform="translate(${dx} ${dy})" d="M18 46h29a9 9 0 0 0 1.2-17.9 13.5 13.5 0 0 0-26-2.6A10.4 10.4 0 0 0 18 46z" fill="${fill}"/>`;

const drops = (xs: number[], y: number, len = 7) =>
  `<g stroke="${WATER}" stroke-width="3.2" stroke-linecap="round">` +
  xs.map((x) => `<line x1="${x}" y1="${y}" x2="${x - len * 0.35}" y2="${y + len}"/>`).join("") +
  "</g>";

const flakes = (xs: number[], y: number) =>
  `<g fill="#ffffff">` + xs.map((x, i) => `<circle cx="${x}" cy="${y + (i % 2) * 5}" r="2.6"/>`).join("") + "</g>";

const svg = (body: string) => `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" aria-hidden="true">${body}</svg>`;

/** Returns SVG markup for a WMO weather code. */
export function weatherIcon(code: number, isDay: boolean): string {
  const sky = (small: boolean) => (isDay ? (small ? sun(23, 22, 8) : sun(32, 32, 12)) : small ? moon(0, 0, 0.6) : moon(0, 0, 1));
  if (code === 0) return svg(sky(false));
  if (code === 1 || code === 2) return svg(sky(true) + cloud(4, 4));
  if (code === 3) return svg(cloud(-6, -8, CLOUD_DARK) + cloud(4, 4));
  if (code === 45 || code === 48)
    return svg(
      `<g stroke="#c9d1e0" stroke-width="3.6" stroke-linecap="round">` +
        `<line x1="14" y1="24" x2="44" y2="24"/><line x1="20" y1="34" x2="52" y2="34"/><line x1="12" y1="44" x2="40" y2="44"/><line x1="24" y1="54" x2="50" y2="54"/></g>`,
    );
  if (code >= 51 && code <= 55) return svg(cloud(0, -8) + drops([27, 39], 46, 5));
  if (code === 56 || code === 57 || code === 66 || code === 67) return svg(cloud(0, -8) + drops([25], 45) + flakes([34, 43], 47));
  if (code >= 61 && code <= 65) return svg(cloud(0, -8, CLOUD_DARK) + drops([24, 33, 42], 45));
  if ((code >= 71 && code <= 77) || code === 85 || code === 86) return svg(cloud(0, -8) + flakes([24, 33, 42], 46));
  if (code >= 80 && code <= 82) return svg(sky(true) + cloud(4, -4) + drops([29, 38, 47], 48));
  if (code >= 95) return svg(cloud(0, -8, CLOUD_DARK) + `<path d="M34 40l-8 11h6l-3 9 10-13h-6l4-7z" fill="${SUN}"/>`);
  return svg(cloud(0, 0));
}

const CONDITIONS: Record<number, string> = {
  0: "Clear", 1: "Mostly clear", 2: "Partly cloudy", 3: "Overcast",
  45: "Fog", 48: "Freezing fog",
  51: "Light drizzle", 53: "Drizzle", 55: "Heavy drizzle", 56: "Freezing drizzle", 57: "Freezing drizzle",
  61: "Light rain", 63: "Rain", 65: "Heavy rain", 66: "Freezing rain", 67: "Freezing rain",
  71: "Light snow", 73: "Snow", 75: "Heavy snow", 77: "Snow grains",
  80: "Light showers", 81: "Showers", 82: "Heavy showers", 85: "Snow showers", 86: "Heavy snow showers",
  95: "Thunderstorm", 96: "Thunderstorm with hail", 99: "Thunderstorm with hail",
};

export function conditionText(code: number): string {
  return CONDITIONS[code] ?? "Unknown";
}

// --- Formatting ---------------------------------------------------------------

const deg = (v: number) => `${Math.round(v) === 0 ? 0 : Math.round(v)}°`;
const COMPASS = ["N", "NE", "E", "SE", "S", "SW", "W", "NW"];
const compass = (d: number) => COMPASS[Math.round(d / 45) % 8];

function uvWord(v: number): string {
  return v < 3 ? "Low" : v < 6 ? "Moderate" : v < 8 ? "High" : v < 11 ? "Very high" : "Extreme";
}

function esc(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);
}

/**
 * Weather on the dashboard: a compact summary in the corner of the ambient
 * screen, and a full-screen detail panel opened from the app row.
 */
export class WeatherView {
  private state: WeatherState | null = null;
  private stale = false;
  private opened = false;
  private hour24 = true;
  private locale: string | undefined;

  constructor(
    private summary: HTMLElement,
    private panel: HTMLElement,
    private onToggle: (open: boolean) => void = () => {},
  ) {}

  get hasData() {
    return this.state !== null;
  }

  get isOpen() {
    return this.opened;
  }

  /** Icon and one-line description for the app row tile. */
  get tile(): { glyph: string; sub: string } | null {
    const s = this.state;
    if (!s) return null;
    return { glyph: weatherIcon(s.current.code, s.current.isDay), sub: `${deg(s.current.temperature)} · ${conditionText(s.current.code)}` };
  }

  configure(hour24: boolean, locale: string) {
    this.hour24 = hour24;
    this.locale = locale || undefined;
    this.render();
  }

  update(state: WeatherState | null, stale: boolean) {
    // A failed refresh keeps the last forecast (state stays), marked as stale.
    if (state) this.state = state;
    this.stale = stale;
    this.render();
  }

  open() {
    if (!this.state || this.opened) return;
    this.opened = true;
    this.renderPanel();
    this.panel.classList.add("open");
    this.panel.setAttribute("aria-hidden", "false");
    this.onToggle(true);
  }

  close() {
    if (!this.opened) return;
    this.opened = false;
    this.panel.classList.remove("open");
    this.panel.setAttribute("aria-hidden", "true");
    this.onToggle(false);
  }

  /** While the panel is open it takes every key; Back or OK closes it. */
  handle(action: Action): boolean {
    if (!this.opened) return false;
    if (action === "back" || action === "select" || action === "menu" || action === "home") this.close();
    return true;
  }

  // Times are shown in the place's own time zone, whatever the Pi's clock is set to.
  private fmt(opts: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
    const tz = this.state?.timezone || undefined;
    try {
      return new Intl.DateTimeFormat(this.locale, { ...opts, timeZone: tz });
    } catch {
      return new Intl.DateTimeFormat(this.locale, opts);
    }
  }

  private clock(iso: string): string {
    if (!iso) return "–";
    return this.fmt({ hour: "2-digit", minute: "2-digit", hour12: !this.hour24 }).format(new Date(iso));
  }

  private hourLabel(iso: string): string {
    const d = new Date(iso);
    return this.hour24
      ? this.fmt({ hour: "2-digit", minute: "2-digit", hour12: false }).format(d)
      : this.fmt({ hour: "numeric", hour12: true }).format(d);
  }

  private dayLabel(day: WeatherDay, index: number): string {
    if (index === 0) return "Today";
    // Noon UTC of that calendar date names the right weekday in every time zone.
    return new Intl.DateTimeFormat(this.locale, { weekday: "long", timeZone: "UTC" }).format(new Date(day.date + "T12:00:00Z"));
  }

  private render() {
    this.renderSummary();
    if (this.opened) this.renderPanel();
  }

  private renderSummary() {
    const s = this.state;
    this.summary.hidden = !s;
    if (!s) return;
    const c = s.current;
    const today = s.daily[0];
    const rain = today ? Math.max(today.precipitationProbability, s.hourly[0]?.precipitationProbability ?? 0) : 0;
    this.summary.innerHTML =
      `<div class="w-now"><span class="w-icon">${weatherIcon(c.code, c.isDay)}</span><span class="w-temp">${deg(c.temperature)}</span></div>` +
      `<div class="w-line">${esc(conditionText(c.code))}` +
      (today ? `<span class="w-hilo">${deg(today.max)} / ${deg(today.min)}</span>` : "") +
      `</div>` +
      `<div class="w-extra">${esc(s.location)}  ·  Feels like ${deg(c.feelsLike)}  ·  Rain ${rain}%</div>`;
  }

  private renderPanel() {
    const s = this.state;
    if (!s) return;
    const c = s.current;
    const today = s.daily[0];
    const u = s.units;

    const tile = (label: string, value: string, note = "") =>
      `<div class="wp-tile"><div class="wp-label">${label}</div><div class="wp-value">${value}</div><div class="wp-note">${note}</div></div>`;

    const tiles =
      tile("Wind", `${Math.round(c.windSpeed)} <small>${esc(u.wind)}</small>`, `${compass(c.windDirection)} · gusts ${Math.round(c.windGusts)}`) +
      tile("Humidity", `${c.humidity}<small>%</small>`) +
      tile(
        "Rain today",
        `${today ? today.precipitationProbability : 0}<small>%</small>`,
        today ? `${today.precipitationSum.toFixed(today.precipitationSum < 10 ? 1 : 0)} ${esc(u.precipitation)} expected` : "",
      ) +
      tile("UV index", today ? `${Math.round(today.uvIndexMax)}` : "–", today ? uvWord(today.uvIndexMax) : "") +
      tile("Sunrise", today ? this.clock(today.sunrise) : "–") +
      tile("Sunset", today ? this.clock(today.sunset) : "–");

    // Every second hour: twelve columns cover the next 24 hours.
    const hours = s.hourly
      .filter((_, i) => i % 2 === 0)
      .slice(0, 12)
      .map(
        (h, i) =>
          `<div class="wp-hour"><div class="wp-htime">${i === 0 ? "Now" : this.hourLabel(h.time)}</div>` +
          `<div class="wp-hicon">${weatherIcon(h.code, h.isDay)}</div>` +
          `<div class="wp-htemp">${deg(h.temperature)}</div>` +
          `<div class="wp-hpop">${h.precipitationProbability >= 10 ? h.precipitationProbability + "%" : ""}</div></div>`,
      )
      .join("");

    // One shared scale for the week, so the bars can be compared down the list.
    const lo = Math.min(...s.daily.map((d) => d.min));
    const hi = Math.max(...s.daily.map((d) => d.max));
    const span = Math.max(1, hi - lo);
    const pct = (v: number) => (((v - lo) / span) * 100).toFixed(1);
    const days = s.daily
      .map((d, i) => {
        const marker =
          i === 0 ? `<span class="wp-marker" style="left:${pct(Math.min(hi, Math.max(lo, c.temperature)))}%"></span>` : "";
        return (
          `<div class="wp-day"><div class="wp-dname">${esc(this.dayLabel(d, i))}</div>` +
          `<div class="wp-dicon">${weatherIcon(d.code, true)}</div>` +
          `<div class="wp-dpop">${d.precipitationProbability >= 10 ? d.precipitationProbability + "%" : ""}</div>` +
          `<div class="wp-dmin">${deg(d.min)}</div>` +
          `<div class="wp-track"><span class="wp-range" style="left:${pct(d.min)}%;right:${(100 - Number(pct(d.max))).toFixed(1)}%"></span>${marker}</div>` +
          `<div class="wp-dmax">${deg(d.max)}</div></div>`
        );
      })
      .join("");

    this.panel.innerHTML =
      `<div class="wp-grid">` +
      `<section class="wp-left">` +
      `<div class="wp-loc">${esc(s.location)}</div>` +
      `<div class="wp-hero"><span class="wp-icon">${weatherIcon(c.code, c.isDay)}</span><span class="wp-temp">${deg(c.temperature)}</span></div>` +
      `<div class="wp-cond">${esc(conditionText(c.code))}</div>` +
      `<div class="wp-sub">Feels like ${deg(c.feelsLike)}` + (today ? `  ·  High ${deg(today.max)}  ·  Low ${deg(today.min)}` : "") + `</div>` +
      `<div class="wp-tiles">${tiles}</div>` +
      `</section>` +
      `<section class="wp-right">` +
      `<h3>Next 24 hours</h3><div class="wp-hours">${hours}</div>` +
      `<h3>This week <span class="wp-legend">low to high, in ${esc(u.temperature)}</span></h3><div class="wp-days">${days}</div>` +
      `</section>` +
      `</div>` +
      `<footer class="wp-foot"><span>${this.stale ? "Could not refresh · last update " : "Updated "}${this.clock(c.time)}  ·  Weather data by Open-Meteo.com</span><span>Back to close</span></footer>`;
  }
}
