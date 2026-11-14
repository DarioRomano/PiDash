// The ambient background, shown whenever no photo is on screen: broad ribbons
// of colour that wind slowly across the screen over a few soft glows.
//
// The ribbons answer to what the person does. Left and right send them
// flowing that way, up and down make them sway, OK sends a swell through
// them, opening the app row lifts them with the clock, and they lean a little
// towards the mouse pointer. Their colours follow the icon of the app selected
// in the app row and return to a calm default when the row closes.
//
// The ribbons are painted at half the screen's resolution (1080p on a 4K
// screen), which keeps their edges crisp at a quarter of the work. The glows
// behind them have no edges at all, so they are painted tiny and stretched.
// If the machine cannot keep up, the picture steps down to a coarser size by
// itself. A fine static grain on top hides the colour steps that dark
// gradients otherwise show on a TV.

export type RGB = [number, number, number];

const BASE: RGB = [11, 13, 18]; // --bg
export const DEFAULT_PALETTE: RGB[] = [
  [40, 66, 150], // indigo
  [16, 108, 120], // teal
  [104, 50, 138], // plum
];

/** Picture heights to choose from, finest first; the width follows at 16:9. */
const SIZES = [1080, 720, 540, 360, 270];
const GLOW_W = 160; // the glows' own small picture
const GLOW_H = 90;
/** Step down a size when pictures arrive slower than this on average, in milliseconds. */
// Late by this much on average, and the picture is too much work. Being a
// screen refresh late now and then is normal: pictures can only be shown when
// the screen is drawn.
const TOO_LATE_MS = 15;
const JUDGE_AFTER = 60; // pictures per verdict
// The drift is slow enough that 12 pictures a second look continuous; a
// reaction to a key press gets 30. Every picture costs the Pi work, and with
// it heat, so no more are drawn than can be seen.
const CALM_FRAME_MS = 83;
const STIRRED_FRAME_MS = 33;
/** How fast the shapes drift by themselves; 1 was the original, livelier pace. */
const DRIFT = 0.5;
const SETTLE_S = 0.6; // how quickly colours follow a new palette
const LINGER_MS = 2200; // keep painting while a photo fades in on top
const TILT = -0.2; // the ribbons run slightly uphill, in radians

/** A soft glow behind the ribbons: which palette colour it takes and how it wanders. */
const GLOWS = [
  { colour: 0, x: 0.26, y: 0.74, ax: 0.2, ay: 0.16, period: 71, phase: 0.0, size: 0.6, strength: 0.42 },
  { colour: 1, x: 0.76, y: 0.3, ax: 0.18, ay: 0.2, period: 89, phase: 1.9, size: 0.56, strength: 0.36 },
  { colour: 2, x: 0.58, y: 0.88, ax: 0.26, ay: 0.12, period: 113, phase: 3.7, size: 0.48, strength: 0.32 },
];

/**
 * A ribbon: a band that winds across the screen, swelling and pinching like a
 * strip of cloth seen edge-on. It blends from palette colour `from` to `to`.
 * `depth` is how strongly it answers to input; nearer ribbons move more.
 */
const RIBBONS = [
  { from: 1, to: 2, y: 0.6, amp: 0.2, waves: 0.8, speed: 0.07, phase: 2.1, thick: 0.3, depth: 0.6, alpha: 0.2 },
  { from: 0, to: 1, y: 0.4, amp: 0.17, waves: 1.1, speed: 0.1, phase: 0.3, thick: 0.22, depth: 1.0, alpha: 0.26 },
  { from: 2, to: 0, y: 0.24, amp: 0.13, waves: 1.5, speed: 0.13, phase: 4.4, thick: 0.15, depth: 1.5, alpha: 0.24 },
];
/** Each ribbon is painted three times, wider and fainter, for a feathered edge. */
const FEATHER = [
  [1, 1],
  [1.7, 0.42],
  [2.6, 0.18],
];

export class Ambient {
  private ctx: CanvasRenderingContext2D | null;
  private glows = document.createElement("canvas");
  private glowCtx: CanvasRenderingContext2D | null;
  private w = 0;
  private h = 0;
  private size = SIZES.length - 1; // index into SIZES
  private finest = 0; // never go finer than this again once the machine fell behind
  private slowSum = 0;
  private slowCount = 0;
  private slowVerdicts = 0;
  private colours: RGB[] = DEFAULT_PALETTE.map((c) => [...c] as RGB);
  private target: RGB[] = DEFAULT_PALETTE;
  private active = false;
  private raf = 0;
  private last = 0;
  private clock = Math.random() * 600; // seconds; a different sky after every start
  private lingerTimer = 0;
  // How often a picture is drawn while drifting and while reacting to a
  // button, and the finest picture allowed: the performance setting.
  private calmMs = CALM_FRAME_MS;
  private stirredMs = STIRRED_FRAME_MS;
  private floor = 0;
  private reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  private still = this.reducedMotion;

  // What input has set in motion. Everything here settles back to rest.
  private flow = 0; // how far the ribbons have been pushed along, in radians
  private flowSpeed = 0;
  private sway = 0; // vertical offset, as a fraction of the height
  private swaySpeed = 0;
  private lift = 0; // where the sway rests: raised while the app row is open
  private swell = 0; // 0..1 and a little more: wider, brighter waves
  private lean: [number, number] = [0, 0]; // towards the pointer, -1..1
  private leanTarget: [number, number] = [0, 0];

  constructor(
    private canvas: HTMLCanvasElement,
    private grain: HTMLElement,
  ) {
    this.ctx = canvas.getContext("2d", { alpha: false });
    this.glows.width = GLOW_W;
    this.glows.height = GLOW_H;
    this.glowCtx = this.glows.getContext("2d", { alpha: false });
    grain.style.backgroundImage = `url(${grainTile()})`;
    this.fitScreen();
    window.addEventListener("resize", () => this.fitScreen());
  }

  /** Chooses the picture size for this screen: about half its real resolution. */
  private fitScreen() {
    const screenHeight = Math.max(window.innerHeight, (window.innerWidth * 9) / 16) * (window.devicePixelRatio || 1);
    let size = SIZES.findIndex((s) => s <= screenHeight / 2 + 1);
    if (size < 0) size = SIZES.length - 1;
    this.resize(Math.max(size, this.finest, this.floor));
  }

  /**
   * Sets the pace: milliseconds between pictures while drifting and while
   * reacting to a button, and the finest picture size to use (an index into
   * SIZES; 0 allows the finest).
   */
  setPace(calmMs: number, stirredMs: number, floor = 0) {
    this.calmMs = Math.max(16, calmMs);
    this.stirredMs = Math.max(16, Math.min(stirredMs, this.calmMs));
    this.floor = Math.max(0, Math.min(SIZES.length - 1, floor));
    this.fitScreen();
  }

  /** What the pace is, for the status page and the tests. */
  get pace() {
    return { calmMs: this.calmMs, stirredMs: this.stirredMs, height: this.h };
  }

  private resize(size: number) {
    this.size = size;
    this.h = SIZES[size];
    this.w = Math.round((this.h * 16) / 9);
    if (this.canvas.width !== this.w || this.canvas.height !== this.h) {
      this.canvas.width = this.w;
      this.canvas.height = this.h;
    }
    this.slowSum = this.slowCount = this.slowVerdicts = 0;
    this.paint();
  }

  /**
   * Notes how late the last picture was, measured against when it was wanted,
   * and steps down a size if pictures keep arriving late.
   */
  private judge(interval: number, wanted: number) {
    if (interval <= 0 || interval > wanted + 250) return; // a pause, not a slow picture
    this.slowSum += interval - wanted;
    if (++this.slowCount < JUDGE_AFTER) return;
    const average = this.slowSum / this.slowCount;
    this.slowSum = this.slowCount = 0;
    // One slow stretch can be something else keeping the machine busy; two
    // in a row mean this size is too much for it.
    this.slowVerdicts = average > TOO_LATE_MS ? this.slowVerdicts + 1 : 0;
    if (this.slowVerdicts >= 2 && this.size < SIZES.length - 1) {
      this.finest = this.size + 1;
      this.resize(this.finest);
    }
  }

  /**
   * A still background is painted once and then left alone, apart from
   * changing colour with the selected app: it costs the Pi nothing.
   */
  setStill(still: boolean) {
    this.still = still || this.reducedMotion;
    if (this.still) {
      this.flowSpeed = this.swaySpeed = this.swell = 0;
      this.sway = this.lift;
      this.lean = [...this.leanTarget];
    }
    this.start();
  }

  /** Colours to move towards; null returns to the default. */
  setPalette(palette: RGB[] | null) {
    this.target = palette && palette.length >= 3 ? palette : DEFAULT_PALETTE;
    this.start();
  }

  /** Pushes the ribbons: dx along their length (-1 left, 1 right), dy up or down. */
  nudge(dx: number, dy: number) {
    if (this.still) return;
    this.flowSpeed = clamp(this.flowSpeed + dx * 2.4, -7, 7);
    this.swaySpeed = clamp(this.swaySpeed + dy * 0.5, -1.2, 1.2);
    this.pulse(0.25);
  }

  /** Sends a swell through the ribbons; 1 is a firm press. */
  pulse(strength = 1) {
    if (this.still) return;
    this.swell = Math.min(1.4, this.swell + strength);
    this.start();
  }

  /** Raises the ribbons while the app row is open, along with the clock. */
  setLifted(lifted: boolean) {
    this.lift = lifted && !this.still ? -0.09 : 0;
    this.start();
  }

  /** The pointer's place on screen, each axis from -1 to 1. */
  pointTo(x: number, y: number) {
    if (this.still) return;
    this.leanTarget = [clamp(x, -1, 1), clamp(y, -1, 1)];
    this.start();
  }

  /** Paints only while it can be seen: no photo on top, nothing covering the dashboard. */
  setActive(active: boolean) {
    if (active === this.active) return;
    this.active = active;
    clearTimeout(this.lingerTimer);
    if (active) {
      this.canvas.hidden = this.grain.hidden = false;
      this.start();
    } else {
      // A photo fades in over the ribbons; stop once it has fully covered them.
      this.lingerTimer = window.setTimeout(() => {
        this.canvas.hidden = this.grain.hidden = true;
        cancelAnimationFrame(this.raf);
        this.raf = 0;
      }, LINGER_MS);
    }
  }

  private start() {
    if (this.raf || this.canvas.hidden) return;
    this.last = 0;
    this.raf = requestAnimationFrame(this.frame);
  }

  /** True while something set off by input is still visibly moving. */
  private get stirred() {
    return (
      Math.abs(this.flowSpeed) > 0.03 ||
      Math.abs(this.swaySpeed) > 0.004 ||
      Math.abs(this.sway - this.lift) > 0.002 ||
      this.swell > 0.02 ||
      Math.abs(this.lean[0] - this.leanTarget[0]) + Math.abs(this.lean[1] - this.leanTarget[1]) > 0.01
    );
  }

  private frame = (now: number) => {
    this.raf = 0;
    // Drifting needs few pictures; a reaction to a key press gets more.
    // (A millisecond of slack, or a frame that arrives just early is skipped.)
    const wanted = this.stirred ? this.stirredMs : this.calmMs;
    if (this.last && now - this.last < wanted - 1) {
      this.raf = requestAnimationFrame(this.frame);
      return;
    }
    // A picture covers the time since the last one, up to a limit: after a
    // long pause the background does not jump ahead.
    const dt = this.last ? Math.min(0.4, (now - this.last) / 1000) : 0;
    if (this.last) this.judge(now - this.last, wanted);
    this.last = now;

    // Colours ease towards the target palette.
    const k = 1 - Math.exp(-dt / SETTLE_S);
    let moving = false;
    for (let i = 0; i < this.colours.length; i++) {
      for (let c = 0; c < 3; c++) {
        const d = this.target[i][c] - this.colours[i][c];
        if (Math.abs(d) > 0.5) moving = true;
        this.colours[i][c] += d * k;
      }
    }

    if (!this.still) this.clock += dt * DRIFT;
    // What a button set in motion is worked out in small steps, so that it
    // comes out the same however seldom a picture is drawn.
    for (let rest = dt; rest > 0.0005; ) {
      const h = Math.min(0.05, rest);
      rest -= h;
      // A push along the ribbons glides to a stop.
      this.flow += this.flowSpeed * h;
      this.flowSpeed *= Math.exp(-h / 0.7);
      // The sway is a damped spring around its resting height.
      this.swaySpeed += (-(this.sway - this.lift) * 26 - this.swaySpeed * 6.5) * h;
      this.sway += this.swaySpeed * h;
      this.swell *= Math.exp(-h / 1.1);
      const follow = 1 - Math.exp(-h / 0.5);
      this.lean[0] += (this.leanTarget[0] - this.lean[0]) * follow;
      this.lean[1] += (this.leanTarget[1] - this.lean[1]) * follow;
    }

    this.paint();

    // With motion switched off in the system, repaint only while something changes.
    if (!this.still || moving || this.stirred) this.raf = requestAnimationFrame(this.frame);
  };

  private paint() {
    const ctx = this.ctx;
    const glows = this.glowCtx;
    if (!ctx || !glows) return;
    const { w, h } = this;

    // The glows, small.
    glows.globalCompositeOperation = "source-over";
    glows.fillStyle = rgba(BASE, 1);
    glows.fillRect(0, 0, GLOW_W, GLOW_H);
    glows.globalCompositeOperation = "lighter"; // colours add up where shapes overlap
    for (const g of GLOWS) {
      const a = (this.clock / g.period) * Math.PI * 2 + g.phase;
      // Two different rhythms per axis, so a path never visibly repeats.
      const x = (g.x + g.ax * Math.sin(a) + 0.05 * Math.sin(a * 2.3 + 1) - this.lean[0] * 0.01) * GLOW_W;
      const y = (g.y + g.ay * Math.cos(a * 0.8) + 0.04 * Math.cos(a * 1.7 + 2) + this.sway * 0.4) * GLOW_H;
      const r = g.size * GLOW_W * (1 + 0.12 * Math.sin(a * 0.6 + g.phase));
      const c = this.colours[g.colour];
      const glow = glows.createRadialGradient(x, y, 0, x, y, r);
      glow.addColorStop(0, rgba(c, g.strength));
      glow.addColorStop(0.45, rgba(c, g.strength * 0.42));
      glow.addColorStop(1, rgba(c, 0));
      glows.fillStyle = glow;
      glows.fillRect(0, 0, GLOW_W, GLOW_H);
    }

    // Stretched over the whole picture, they are its ground.
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.globalCompositeOperation = "source-over";
    ctx.imageSmoothingEnabled = true;
    ctx.imageSmoothingQuality = "high";
    ctx.drawImage(this.glows, 0, 0, w, h);

    // The ribbons are drawn in a tilted frame, wider than the screen, so their
    // ends are never seen.
    ctx.globalCompositeOperation = "lighter";
    ctx.translate(w / 2, h / 2);
    ctx.rotate(TILT);
    ctx.translate(-w / 2, -h / 2);
    const bright = 1 + 0.22 * Math.min(1, this.swell);
    for (const r of RIBBONS) {
      const from = lighten(this.colours[r.from], 0.16);
      const to = lighten(this.colours[r.to], 0.16);
      for (const [width, share] of FEATHER) {
        const alpha = r.alpha * share * bright;
        const fill = ctx.createLinearGradient(-0.25 * w, 0, 1.25 * w, 0);
        fill.addColorStop(0, rgba(from, alpha * 0.5));
        fill.addColorStop(0.3, rgba(from, alpha));
        fill.addColorStop(0.72, rgba(to, alpha));
        fill.addColorStop(1, rgba(to, alpha * 0.5));
        ctx.fillStyle = fill;
        this.traceRibbon(ctx, r, width);
        ctx.fill();
      }
    }
  }

  /** Traces one ribbon's outline: along its upper edge, back along the lower. */
  private traceRibbon(ctx: CanvasRenderingContext2D, r: (typeof RIBBONS)[number], width: number) {
    const { w, h } = this;
    const t = this.clock;
    const push = this.flow * r.depth;
    const reach = 1 + 0.4 * this.swell; // a swell makes the waves taller
    const top: Point[] = [];
    const bottom: Point[] = [];
    const steps = 56;
    for (let i = 0; i <= steps; i++) {
      const u = -0.25 + (i / steps) * 1.5; // along the ribbon; 0..1 is the screen
      const turn = u * Math.PI * 2 * r.waves;
      const centre =
        r.y +
        this.sway * r.depth +
        this.lean[1] * 0.012 * r.depth +
        r.amp * reach * (0.7 * Math.sin(turn + r.phase - push + t * r.speed) + 0.45 * Math.sin(turn * 0.47 + r.phase * 1.7 + push * 0.6 - t * r.speed * 0.6));
      // Pinches to a waist and opens again, like cloth turning over.
      const turnOver = Math.sin(turn * 0.6 + r.phase * 0.5 - push * 0.5 + t * r.speed * 0.7);
      const half = (r.thick * (0.2 + 0.8 * turnOver * turnOver) * (1 + 0.25 * this.swell) * width) / 2;
      const x = (u - this.lean[0] * 0.015 * r.depth) * w;
      top.push([x, (centre - half) * h]);
      bottom.push([x, (centre + half) * h]);
    }
    bottom.reverse();
    ctx.beginPath();
    ctx.moveTo(top[0][0], top[0][1]);
    curveThrough(ctx, top);
    ctx.lineTo(bottom[0][0], bottom[0][1]);
    curveThrough(ctx, bottom);
    ctx.closePath();
  }
}

type Point = [number, number];

/** Continues the path through the points as one smooth curve, without corners. */
function curveThrough(ctx: CanvasRenderingContext2D, points: Point[]) {
  for (let i = 1; i < points.length - 1; i++) {
    const [x, y] = points[i];
    const [nx, ny] = points[i + 1];
    ctx.quadraticCurveTo(x, y, (x + nx) / 2, (y + ny) / 2);
  }
  const last = points[points.length - 1];
  ctx.lineTo(last[0], last[1]);
}

const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v));
const rgba = (c: RGB, a: number) => `rgba(${Math.round(c[0])},${Math.round(c[1])},${Math.round(c[2])},${a.toFixed(3)})`;
const lighten = (c: RGB, by: number): RGB => [c[0] + (255 - c[0]) * by, c[1] + (255 - c[1]) * by, c[2] + (255 - c[2]) * by];

/** A small tile of faint light and dark specks, as a data URL. */
function grainTile(): string {
  const size = 128;
  const c = document.createElement("canvas");
  c.width = c.height = size;
  const ctx = c.getContext("2d");
  if (!ctx) return "";
  const img = ctx.createImageData(size, size);
  for (let i = 0; i < img.data.length; i += 4) {
    const v = Math.random() < 0.5 ? 0 : 255;
    img.data[i] = img.data[i + 1] = img.data[i + 2] = v;
    img.data[i + 3] = Math.random() * 22;
  }
  ctx.putImageData(img, 0, 0);
  return c.toDataURL("image/png");
}

// --- Colours of an icon ---------------------------------------------------------

const palettes = new Map<string, Promise<RGB[] | null>>();

/**
 * The main colours of an icon, tuned for use as background glows. Resolves to
 * null for an icon without colour (black, white, grey) or one that cannot be read.
 */
export function iconPalette(url: string): Promise<RGB[] | null> {
  let p = palettes.get(url);
  if (!p) {
    p = extract(url).catch(() => null);
    palettes.set(url, p);
  }
  return p;
}

async function extract(url: string): Promise<RGB[] | null> {
  const img = new Image();
  img.src = url;
  await img.decode();
  const size = 40;
  const c = document.createElement("canvas");
  c.width = c.height = size;
  const ctx = c.getContext("2d", { willReadFrequently: true });
  if (!ctx) return null;
  ctx.drawImage(img, 0, 0, size, size);
  return paletteFromPixels(ctx.getImageData(0, 0, size, size).data);
}

const BINS = 24; // 15 degrees of hue each

/** Exported for the tests: picks up to three distinct hues from RGBA pixels. */
export function paletteFromPixels(data: Uint8ClampedArray): RGB[] | null {
  const weight = new Float64Array(BINS);
  const sum = Array.from({ length: BINS }, () => [0, 0, 0]);
  let pixels = 0;
  for (let i = 0; i < data.length; i += 4) {
    if (data[i + 3] < 128) continue; // transparent
    pixels++;
    const r = data[i] / 255, g = data[i + 1] / 255, b = data[i + 2] / 255;
    const max = Math.max(r, g, b), min = Math.min(r, g, b);
    const chroma = max - min;
    if (chroma < 0.18 || max < 0.2) continue; // grey, white or black says nothing about colour
    const bin = Math.floor((hueOf(r, g, b, max, chroma) / 360) * BINS) % BINS;
    weight[bin] += chroma;
    sum[bin][0] += r * chroma;
    sum[bin][1] += g * chroma;
    sum[bin][2] += b * chroma;
  }
  const total = weight.reduce((a, b) => a + b, 0);
  if (pixels === 0 || total < pixels * 0.01) return null;

  // The strongest hues, at least 45 degrees apart from each other.
  const order = [...weight.keys()].sort((a, b) => weight[b] - weight[a]);
  const picked: number[] = [];
  for (const bin of order) {
    if (picked.length === 3 || weight[bin] < weight[order[0]] * 0.12) break;
    const apart = picked.every((p) => Math.min(Math.abs(p - bin), BINS - Math.abs(p - bin)) >= 3);
    if (apart) picked.push(bin);
  }
  const hues = picked.map((bin) => {
    const [r, g, b] = sum[bin].map((v) => v / weight[bin]);
    const max = Math.max(r, g, b), min = Math.min(r, g, b);
    return { h: hueOf(r, g, b, max, max - min), s: max === 0 ? 0 : (max - min) / max };
  });
  // An icon in a single colour gets neighbours of that colour, so the
  // background still has depth instead of one flat tint.
  const first = hues[0];
  if (hues.length === 1) hues.push({ h: first.h + 28, s: first.s }, { h: first.h - 24, s: first.s });
  else if (hues.length === 2) hues.push({ h: first.h + 20, s: first.s });

  // Deep and rich rather than bright: the clock in front must stay readable.
  const light = [0.4, 0.34, 0.3];
  return hues.map((c, i) => hslToRgb(c.h, Math.min(0.85, Math.max(0.5, c.s)), light[i]));
}

function hueOf(r: number, g: number, b: number, max: number, chroma: number): number {
  if (chroma === 0) return 0;
  let h: number;
  if (max === r) h = ((g - b) / chroma) % 6;
  else if (max === g) h = (b - r) / chroma + 2;
  else h = (r - g) / chroma + 4;
  return (h * 60 + 360) % 360;
}

function hslToRgb(h: number, s: number, l: number): RGB {
  h = ((h % 360) + 360) % 360;
  const c = (1 - Math.abs(2 * l - 1)) * s;
  const x = c * (1 - Math.abs(((h / 60) % 2) - 1));
  const m = l - c / 2;
  const [r, g, b] =
    h < 60 ? [c, x, 0] : h < 120 ? [x, c, 0] : h < 180 ? [0, c, x] : h < 240 ? [0, x, c] : h < 300 ? [x, 0, c] : [c, 0, x];
  return [Math.round((r + m) * 255), Math.round((g + m) * 255), Math.round((b + m) * 255)];
}
