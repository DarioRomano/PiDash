// Times of day that follow the sun: night mode from sunset, the picture
// dimming as the evening comes. Sunrise and sunset are those of the town the
// weather is set up for, which the weather forecast brings along.
import type { WeatherState } from "./types";

export interface Sun {
  /** Minutes after midnight. */
  rise: number;
  set: number;
}

/** Today's sunrise and sunset, or null while the weather has none to offer. */
export function sunToday(w: WeatherState | null, now = new Date()): Sun | null {
  if (!w || !w.daily?.length) return null;
  const today = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
  const day = w.daily.find((d) => d.date?.startsWith(today)) ?? w.daily[0];
  const minutes = (stamp: string) => {
    const t = new Date(stamp);
    return Number.isNaN(t.getTime()) ? NaN : t.getHours() * 60 + t.getMinutes();
  };
  const rise = minutes(day.sunrise);
  const set = minutes(day.sunset);
  return Number.isNaN(rise) || Number.isNaN(set) ? null : { rise, set };
}

const SUN_TIME = /^(sunrise|sunset)([+-]\d{1,3})?$/;

/** True for "sunset", "sunrise+60" and the like. */
export function followsSun(spec: string): boolean {
  return SUN_TIME.test(spec);
}

/**
 * Turns a time as it is set ("22:30", "sunset", "sunset+90", "sunrise-30")
 * into minutes after midnight. Null for a time that follows the sun while
 * the sun's times are not known.
 */
export function timeOf(spec: string, sun: Sun | null): number | null {
  const m = SUN_TIME.exec(spec);
  if (!m) {
    const [h, min] = spec.split(":").map(Number);
    return Number.isFinite(h) && Number.isFinite(min) ? h * 60 + min : null;
  }
  if (!sun) return null;
  const at = (m[1] === "sunrise" ? sun.rise : sun.set) + Number(m[2] ?? 0);
  return ((at % 1440) + 1440) % 1440;
}

/** Whether a time of day lies in a span that may run past midnight. */
export function within(now: number, from: number, to: number): boolean {
  return from <= to ? now >= from && now < to : now >= from || now < to;
}

/** Without the sun's times the evening is taken to be from eight to seven. */
const FALLBACK: Sun = { rise: 7 * 60, set: 20 * 60 };
/** The picture dims over this long after sunset, and brightens over this long before sunrise. */
const DUSK = 60;
const DAWN = 30;

/**
 * How far the evening has come, from 0 (day) to 1 (night): rising over the
 * hour after sunset, staying through the night, falling in the half hour
 * before sunrise.
 */
export function evening(now: number, sun: Sun | null): number {
  const { rise, set } = sun ?? FALLBACK;
  const since = (now - set + 1440) % 1440; // minutes since sunset
  const night = (rise - set + 1440) % 1440; // how long the night lasts
  if (since >= night) return 0;
  return Math.max(0, Math.min(1, since / DUSK, (night - since) / DAWN));
}
