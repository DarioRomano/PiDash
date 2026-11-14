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
  private lastHands = -1;
  private hour = document.getElementById("dial-hour");
  private minute = document.getElementById("dial-minute");
  private timeFmt!: Intl.DateTimeFormat;
  private dateFmt!: Intl.DateTimeFormat;

  constructor(hm: HTMLElement, ampm: HTMLElement, date: HTMLElement) {
    this.hm = hm;
    this.ampm = ampm;
    this.date = date;
    // Twelve marks around the dial, the quarters a little longer.
    const marks = document.getElementById("dial-marks");
    if (marks && marks.childElementCount === 0) {
      for (let h = 0; h < 12; h++) {
        const mark = document.createElementNS("http://www.w3.org/2000/svg", "line");
        mark.setAttribute("x1", "0");
        mark.setAttribute("x2", "0");
        mark.setAttribute("y1", h % 3 === 0 ? "-78" : "-86");
        mark.setAttribute("y2", "-96");
        mark.setAttribute("transform", `rotate(${h * 30})`);
        marks.appendChild(mark);
      }
    }
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
    // The hands move once a minute, like the numbers: nothing is redrawn in between.
    const minutes = now.getHours() * 60 + now.getMinutes();
    if (minutes !== this.lastHands) {
      this.lastHands = minutes;
      this.hour?.setAttribute("transform", `rotate(${((minutes % 720) / 2).toFixed(1)})`);
      this.minute?.setAttribute("transform", `rotate(${(minutes % 60) * 6})`);
    }
    if (dateText !== this.lastDate) {
      this.date.textContent = dateText;
      this.lastDate = dateText;
    }
  }
}
