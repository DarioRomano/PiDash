/**
 * Clock that updates exactly once a second and only touches the DOM when the
 * rendered text actually changes. Nothing else in the app runs on a timer.
 */
export class Clock {
  private hm: HTMLElement;
  private ampm: HTMLElement;
  private date: HTMLElement;
  private lastTime = "";
  private lastDate = "";
  private timer = 0;
  private timeFmt!: Intl.DateTimeFormat;
  private dateFmt!: Intl.DateTimeFormat;

  constructor(hm: HTMLElement, ampm: HTMLElement, date: HTMLElement) {
    this.hm = hm;
    this.ampm = ampm;
    this.date = date;
    this.configure(true, "");
  }

  configure(hour24: boolean, locale: string) {
    const loc = locale || undefined;
    this.timeFmt = new Intl.DateTimeFormat(loc, {
      hour: "2-digit",
      minute: "2-digit",
      hour12: !hour24,
    });
    this.dateFmt = new Intl.DateTimeFormat(loc, {
      weekday: "long",
      day: "numeric",
      month: "long",
    });
    this.lastTime = "";
    this.lastDate = "";
    this.tick();
  }

  start() {
    this.stop();
    const loop = () => {
      this.tick();
      // Align the next tick to the next whole second to avoid drift.
      this.timer = window.setTimeout(loop, 1000 - (Date.now() % 1000));
    };
    loop();
  }

  stop() {
    clearTimeout(this.timer);
  }

  private tick() {
    const now = new Date();
    let hm = "";
    let ampm = "";
    for (const part of this.timeFmt.formatToParts(now)) {
      if (part.type === "dayPeriod") ampm = part.value;
      else if (part.type !== "literal" || part.value.trim() !== "") hm += part.value;
    }
    hm = hm.trim();
    const dateText = this.dateFmt.format(now);

    if (hm !== this.lastTime) {
      this.hm.textContent = hm;
      this.ampm.textContent = ampm;
      this.lastTime = hm;
    }
    if (dateText !== this.lastDate) {
      this.date.textContent = dateText;
      this.lastDate = dateText;
    }
  }
}
