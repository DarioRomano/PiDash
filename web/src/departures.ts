import { t } from "./i18n";

/** One tram, bus or train leaving the stop. */
export interface Departure {
  line: string;
  /** tram, bus, train, boat or cable. */
  kind: string;
  to: string;
  /** When the timetable has it leave. */
  at: string;
  /** Minutes later than that. */
  delay?: number;
  platform?: string;
}

export interface DeparturesState {
  stop: string;
  departures: Departure[];
  /** Minutes it takes to get to the stop. */
  walk: number;
}

const LINES = 4;

/**
 * The next departures from the stop near the house: the line, where it
 * goes, and in how many minutes. The minutes count down by themselves; what
 * has left, or leaves sooner than one can be there, drops off the list.
 */
export class Departures {
  private state: DeparturesState | null = null;
  private stale = false;
  private locale: string | undefined;
  private hour24 = true;

  constructor(private root: HTMLElement) {
    window.setInterval(() => this.render(), 15_000);
  }

  configure(hour24: boolean, locale: string | undefined) {
    this.hour24 = hour24;
    this.locale = locale;
    this.render();
  }

  update(state: DeparturesState | null, stale: boolean) {
    this.state = state;
    this.stale = stale;
    this.render();
  }

  private render() {
    const st = this.state;
    const now = Date.now();
    const rows: HTMLElement[] = [];
    const clock = new Intl.DateTimeFormat(this.locale, { hour: "numeric", minute: "2-digit", hour12: !this.hour24 });
    for (const d of st?.departures ?? []) {
      const leaves = Date.parse(d.at) + (d.delay ?? 0) * 60_000;
      const minutes = Math.floor((leaves - now) / 60_000);
      // Gone, or gone before anyone from here is at the stop.
      if (leaves < now - 20_000 || minutes < (st?.walk ?? 0)) continue;
      if (rows.length >= LINES) break;
      const row = document.createElement("div");
      row.className = "dep-row";
      row.dataset.noTranslate = ""; // names of places and lines
      const line = document.createElement("span");
      line.className = `dep-line ${d.kind}`;
      line.textContent = d.line;
      const to = document.createElement("span");
      to.className = "dep-to";
      // "Zürich, Rehalp" from a stop in Zürich is "Rehalp".
      const town = st?.stop.includes(",") ? st.stop.slice(0, st.stop.indexOf(",") + 1) : "";
      to.textContent = town && d.to.startsWith(town) ? d.to.slice(town.length).trim() : d.to;
      const when = document.createElement("span");
      when.className = "dep-when";
      // In minutes while that says more than the clock does; beyond the hour, the time.
      when.textContent = minutes <= 0 ? t("now") : minutes < 60 ? `${minutes}′` : clock.format(new Date(leaves));
      if (d.delay) {
        const late = document.createElement("span");
        late.className = "dep-late";
        late.textContent = `+${d.delay}`;
        when.prepend(late);
      }
      row.append(line, to, when);
      rows.push(row);
    }
    // A timetable that has not been heard from for a while is worse than none.
    const old = this.stale && st !== null && rows.length === 0;
    if (!st || rows.length === 0 || old) {
      this.root.replaceChildren();
      this.root.hidden = true;
      return;
    }
    const head = document.createElement("div");
    head.className = "dep-stop";
    head.dataset.noTranslate = "";
    head.textContent = st.stop;
    this.root.replaceChildren(head, ...rows);
    this.root.hidden = false;
  }
}
