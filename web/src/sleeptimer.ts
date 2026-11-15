import { t } from "./i18n";
import { postJSON, toast, UNREACHABLE } from "./notify";
import type { RailItem } from "./rail";
import { SLEEP_GLYPH, SLEEP_ID } from "./tiles";

const STEPS = [0, 30, 60, 90, 120];

/**
 * The sleep timer: switches the TV off after a while. Each OK on its button
 * sets the next length, then off again. It is also what counts down the last
 * minute when the TV is switched off because nobody pressed anything.
 */
export class SleepTimer {
  /** Whether the TV can be switched off at all. */
  available = false;
  private at = 0; // when the TV will be switched off, in milliseconds; 0 when not set
  private minutes = 0;
  private warned = 0;

  constructor(private onTick: () => void) {
    // The button counts down, and a minute before the end the screen says so.
    window.setInterval(() => {
      if (!this.at) return;
      this.onTick();
      if (this.at - Date.now() < 75_000 && this.warned !== this.at) {
        this.warned = this.at;
        toast("The TV switches off in a minute. The sleep timer in the app row changes that.", 12000);
      }
    }, 30_000);
  }

  /** What the Pi says about the TV. */
  update(st: { available?: boolean; sleepAt?: string; sleepMinutes?: number; idle?: boolean }) {
    this.available = !!st.available;
    this.at = st.sleepAt ? new Date(st.sleepAt).getTime() : 0;
    this.minutes = st.sleepMinutes ?? 0;
    // Nobody pressed anything for a while: said at once, as there is only a minute.
    if (st.idle && this.at && this.warned !== this.at) {
      this.warned = this.at;
      toast("Nobody has pressed a button for a while, so the TV switches off in a minute. Any button keeps it on.", 20000);
    }
  }

  /** The button, for the app row and the photo's bar. */
  item(): RailItem {
    const left = Math.max(1, Math.round((this.at - Date.now()) / 60_000));
    return { id: SLEEP_ID, label: "Sleep timer", glyph: SLEEP_GLYPH, running: this.at > 0, sub: this.at > 0 ? t("TV off in {n} min", { n: left }) : "Off" };
  }

  /** Sets the next length. */
  async next() {
    const next = STEPS[(STEPS.indexOf(this.minutes) + 1) % STEPS.length];
    try {
      await postJSON("/api/tv/sleep", { minutes: next });
    } catch {
      toast(UNREACHABLE, 6000);
    }
  }
}
