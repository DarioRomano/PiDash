import type { CalendarState } from "./types";

const LINES = 5;

/** The next few events of the calendar, grouped under "Today", "Tomorrow" or the weekday. */
export class Agenda {
  private events: CalendarState["events"] = [];
  private locale: string | undefined;
  private hour24 = true;

  constructor(private root: HTMLElement) {
    // Events pass, and "Tomorrow" becomes "Today", without anything arriving.
    window.setInterval(() => this.render(), 60_000);
  }

  configure(hour24: boolean, locale: string | undefined) {
    this.hour24 = hour24;
    this.locale = locale;
    this.render();
  }

  setEvents(events: CalendarState["events"]) {
    this.events = events;
    this.render();
  }

  private render() {
    const now = new Date();
    const dayStart = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
    const today = dayStart(now);
    const time = new Intl.DateTimeFormat(this.locale, { hour: "numeric", minute: "2-digit", hour12: !this.hour24 });
    const weekday = new Intl.DateTimeFormat(this.locale, { weekday: "long" });
    const date = new Intl.DateTimeFormat(this.locale, { weekday: "short", day: "numeric", month: "short" });

    const rows: HTMLElement[] = [];
    let lastDay = "";
    for (const ev of this.events) {
      const start = new Date(ev.start);
      const end = new Date(ev.end);
      if (end.getTime() <= now.getTime() && !(ev.allDay && end.getTime() > today)) continue; // over
      if (rows.filter((r) => r.className === "agenda-event").length >= LINES) break;
      // An event that began earlier and is still going on belongs to today.
      const day = Math.max(dayStart(start), today);
      const days = Math.round((day - today) / 86_400_000);
      const label = days === 0 ? "Today" : days === 1 ? "Tomorrow" : days < 7 ? weekday.format(day) : date.format(day);
      if (label !== lastDay) {
        lastDay = label;
        const h = document.createElement("div");
        h.className = "agenda-day";
        h.textContent = label;
        rows.push(h);
      }
      const row = document.createElement("div");
      row.className = "agenda-event";
      const when = document.createElement("span");
      when.className = "agenda-time";
      when.textContent = ev.allDay || start.getTime() < day ? "" : time.format(start);
      const what = document.createElement("span");
      what.className = "agenda-title";
      what.textContent = ev.title;
      row.append(what, when);
      rows.push(row);
    }
    this.root.replaceChildren(...rows);
    this.root.hidden = rows.length === 0;
  }
}
