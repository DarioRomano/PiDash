// Small sounds that answer the remote: a tick when the highlight moves, two
// rising notes when something is chosen, a low note when there is nowhere
// further to go. They are made on the spot from sine waves, so there are no
// sound files to load, and each lasts a fraction of a second.

export type Sound = "move" | "edge" | "select" | "back" | "open" | "pick" | "drop" | "error";

/** One note: from one pitch to another, in Hz, starting `at` seconds in. */
interface Note {
  from: number;
  to?: number;
  at?: number;
  /** How long it sounds, in seconds. */
  for: number;
  /** How loud, 0 to 1, before the overall volume. */
  gain: number;
  wave?: OscillatorType;
}

const SOUNDS: Record<Sound, Note[]> = {
  move: [{ from: 1250, to: 980, for: 0.045, gain: 0.5 }],
  edge: [{ from: 190, to: 150, for: 0.09, gain: 0.8, wave: "triangle" }],
  select: [
    { from: 660, for: 0.07, gain: 0.6 },
    { from: 990, at: 0.06, for: 0.14, gain: 0.6 },
  ],
  back: [
    { from: 620, for: 0.06, gain: 0.5 },
    { from: 440, at: 0.055, for: 0.12, gain: 0.5 },
  ],
  open: [{ from: 520, to: 780, for: 0.13, gain: 0.5 }],
  pick: [{ from: 560, to: 840, for: 0.1, gain: 0.55, wave: "triangle" }],
  drop: [{ from: 840, to: 500, for: 0.12, gain: 0.55, wave: "triangle" }],
  error: [
    { from: 220, for: 0.1, gain: 0.7, wave: "triangle" },
    { from: 196, at: 0.12, for: 0.16, gain: 0.7, wave: "triangle" },
  ],
};

/** Everything is this quiet: the sounds accompany, they do not announce. */
const VOLUME = 0.16;
const STORE = "pidash-sounds";

let enabled = true;
try {
  enabled = localStorage.getItem(STORE) !== "off";
} catch {
  /* sounds stay on */
}
let ctx: AudioContext | null = null;
let out: GainNode | null = null;
let rest = 0;

/** Switches the sounds on or off, for this page and the ones opened after it. */
export function setSounds(on: boolean) {
  enabled = on;
  try {
    localStorage.setItem(STORE, on ? "on" : "off");
  } catch {
    /* not remembered */
  }
}

/** For the tests: the sounds played so far, when window.pidashSounds is an array. */
function note(name: Sound) {
  const log = (window as unknown as { pidashSounds?: string[] }).pidashSounds;
  if (log) log.push(name);
}

export function play(name: Sound) {
  if (!enabled) return;
  note(name);
  try {
    if (!ctx) {
      const Ctx = window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
      if (!Ctx) return;
      ctx = new Ctx({ latencyHint: "interactive" });
      out = ctx.createGain();
      out.gain.value = VOLUME;
      out.connect(ctx.destination);
    }
    const audio = ctx;
    const master = out!;
    const sound = () => {
      const start = audio.currentTime + 0.005;
      for (const n of SOUNDS[name]) {
        const t0 = start + (n.at ?? 0);
        const osc = audio.createOscillator();
        const gain = audio.createGain();
        osc.type = n.wave ?? "sine";
        osc.frequency.setValueAtTime(n.from, t0);
        if (n.to) osc.frequency.exponentialRampToValueAtTime(n.to, t0 + n.for);
        // In over a few milliseconds and out along a curve: no clicks.
        gain.gain.setValueAtTime(0.0001, t0);
        gain.gain.exponentialRampToValueAtTime(n.gain, t0 + 0.006);
        gain.gain.exponentialRampToValueAtTime(0.0001, t0 + n.for);
        osc.connect(gain).connect(master);
        osc.start(t0);
        osc.stop(t0 + n.for + 0.02);
      }
    };
    // Between sounds the sound device is let go, so that it can rest and
    // nothing hums; it is back within a few milliseconds.
    clearTimeout(rest);
    rest = window.setTimeout(() => void audio.suspend().catch(() => {}), 4000);
    if (audio.state === "running") sound();
    else void audio.resume().then(sound).catch(() => {});
  } catch {
    /* a page without sound still works */
  }
}
