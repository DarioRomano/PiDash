import { evening, sunToday, timeOf, within } from "./sun";
import type { WeatherState } from "./types";

/**
 * Night mode, and the evening's dimming. Between two times of day the
 * screen shows only a dim clock on black; any button brings the dashboard
 * back for a minute. Either time may follow the sun ("sunset",
 * "sunrise+60"): then it is worked out anew from the weather's sunrise and
 * sunset each time it is looked at.
 */
export class Night {
  /** Whether the screen shows only the clock right now. */
  now = false;
  private times: [string, string] | null = null;
  private awakeUntil = 0;
  /** The latest weather, for the sun's times. */
  private sky: WeatherState | null = null;
  /** How much the picture darkens when the evening has fully come; 0 for not at all. */
  private dim = 0;

  constructor(
    private dimSheet: HTMLElement,
    /** Night has begun or ended. */
    private onChange: (night: boolean) => void,
  ) {
    window.setInterval(() => this.update(), 15_000);
  }

  configure(start: string, end: string, dim: number) {
    this.times = start && end ? [start, end] : null;
    this.dim = dim;
    this.update();
  }

  /** New weather: the sun's times may have changed. */
  setSky(sky: WeatherState | null) {
    this.sky = sky ?? this.sky;
    this.update();
  }

  update() {
    const d = new Date();
    const now = d.getHours() * 60 + d.getMinutes();
    const sun = sunToday(this.sky, d);
    this.dimSheet.style.opacity = (this.dim * evening(now, sun)).toFixed(3);
    let night = false;
    if (this.times) {
      const from = timeOf(this.times[0], sun);
      const to = timeOf(this.times[1], sun);
      // Without the sun's times, a night that follows the sun does not begin.
      night = from !== null && to !== null && within(now, from, to);
    }
    night = night && Date.now() > this.awakeUntil;
    if (night === this.now) return;
    this.now = night;
    this.onChange(night);
  }

  /** A button was pressed. Returns true when that press only woke the screen. */
  wake(): boolean {
    if (!this.times) return false;
    const wasNight = this.now;
    this.awakeUntil = Date.now() + 60_000;
    this.update();
    return wasNight;
  }
}
