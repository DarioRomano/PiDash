// The ambient background: slow, soft glows of colour, shown whenever no photo
// is on screen. Its colours follow the icon of the app that is selected in the
// app row and drift back to a calm default when the row closes.
//
// It is painted into a tiny canvas (160x90) that the browser stretches over
// the screen. Stretching blurs it for free, so a 4K screen costs no more than
// a small one, and a fine static grain on top hides the colour steps that dark
// gradients otherwise show on a TV.

export type RGB = [number, number, number];

const BASE: RGB = [11, 13, 18]; // --bg
export const DEFAULT_PALETTE: RGB[] = [
  [40, 66, 150], // indigo
  [16, 108, 120], // teal
  [104, 50, 138], // plum
];

const W = 160;
const H = 90;
const FRAME_MS = 33; // about 30 pictures a second is plenty for motion this slow
const SETTLE_S = 0.6; // how quickly colours follow a new palette
const LINGER_MS = 2200; // keep painting while a photo fades in on top

/** One glow: which palette colour it takes and how it wanders. */
const GLOWS = [
  { colour: 0, x: 0.28, y: 0.72, ax: 0.2, ay: 0.16, period: 71, phase: 0.0, size: 0.62, strength: 0.6 },
  { colour: 1, x: 0.74, y: 0.34, ax: 0.18, ay: 0.2, period: 89, phase: 1.9, size: 0.58, strength: 0.52 },
  { colour: 2, x: 0.55, y: 0.85, ax: 0.26, ay: 0.12, period: 113, phase: 3.7, size: 0.5, strength: 0.46 },
  { colour: 0, x: 0.9, y: 0.82, ax: 0.12, ay: 0.14, period: 97, phase: 5.1, size: 0.4, strength: 0.34 },
];

export class Ambient {
  private ctx: CanvasRenderingContext2D | null;
  private colours: RGB[] = DEFAULT_PALETTE.map((c) => [...c] as RGB);
  private target: RGB[] = DEFAULT_PALETTE;
  private active = false;
  private raf = 0;
  private last = 0;
  private clock = Math.random() * 600; // seconds; a different sky after every start
  private lingerTimer = 0;
  private still = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  constructor(
    private canvas: HTMLCanvasElement,
    private grain: HTMLElement,
  ) {
    canvas.width = W;
    canvas.height = H;
    this.ctx = canvas.getContext("2d", { alpha: false });
    grain.style.backgroundImage = `url(${grainTile()})`;
    this.paint();
  }

  /** Colours to move towards; null returns to the default. */
  setPalette(palette: RGB[] | null) {
    this.target = palette && palette.length >= 3 ? palette : DEFAULT_PALETTE;
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
      // A photo fades in over the glows; stop once it has fully covered them.
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

  private frame = (now: number) => {
    this.raf = 0;
    const dt = this.last ? Math.min(0.25, (now - this.last) / 1000) : 0;
    if (this.last && now - this.last < FRAME_MS) {
      this.raf = requestAnimationFrame(this.frame);
      return;
    }
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
    if (!this.still) this.clock += dt;
    this.paint();

    // With motion switched off in the system, repaint only while colours change.
    if (!this.still || moving) this.raf = requestAnimationFrame(this.frame);
  };

  private paint() {
    const ctx = this.ctx;
    if (!ctx) return;
    ctx.globalCompositeOperation = "source-over";
    ctx.fillStyle = `rgb(${BASE[0]},${BASE[1]},${BASE[2]})`;
    ctx.fillRect(0, 0, W, H);
    ctx.globalCompositeOperation = "lighter"; // glows add up where they overlap
    for (const g of GLOWS) {
      const a = (this.clock / g.period) * Math.PI * 2 + g.phase;
      // Two different rhythms per axis, so a path never visibly repeats.
      const x = (g.x + g.ax * Math.sin(a) + 0.05 * Math.sin(a * 2.3 + 1)) * W;
      const y = (g.y + g.ay * Math.cos(a * 0.8) + 0.04 * Math.cos(a * 1.7 + 2)) * H;
      const r = g.size * W * (1 + 0.12 * Math.sin(a * 0.6 + g.phase));
      const [cr, cg, cb] = this.colours[g.colour].map(Math.round);
      const glow = ctx.createRadialGradient(x, y, 0, x, y, r);
      glow.addColorStop(0, `rgba(${cr},${cg},${cb},${g.strength})`);
      glow.addColorStop(0.45, `rgba(${cr},${cg},${cb},${g.strength * 0.42})`);
      glow.addColorStop(1, `rgba(${cr},${cg},${cb},0)`);
      ctx.fillStyle = glow;
      ctx.fillRect(0, 0, W, H);
    }
  }
}

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
