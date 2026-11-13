import type { PhotoItem, PhotosStatus } from "./types";

const FADE_MS = 1700; // a little longer than the CSS opacity transition
const PLAYLIST_SIZE = 200;
const VIDEO_MAX_MS = 45_000; // a video clip is shown for at most this long
const VIDEO_WAIT_MS = 20_000; // and given this long to start
/**
 * How often a moving photo is redrawn, per second. The movement is so slow
 * that this many steps cannot be told from a continuous glide (each is well
 * under a pixel), while the screen is redrawn a quarter as often as it would be.
 */
const MOTION_STEPS_PER_SECOND = 15;
const FILL_ZOOM = 1.14; // how far a photo that fills the screen is zoomed at most

/** One end of a photo's movement: its zoom, and its shift as a fraction of its size. */
interface Pose {
  zoom: number;
  x: number;
  y: number;
}

/**
 * The background slideshow. Photos arrive from the backend already rendered at
 * 1920x1080, so the browser only decodes a small JPEG and crossfades two
 * stacked <img> elements by opacity.
 */
export class Slideshow {
  private layers: [HTMLImageElement, HTMLImageElement];
  private videos: [HTMLVideoElement, HTMLVideoElement];
  /** What is on screen now: one of the layers, or one of the videos. */
  private current: HTMLElement | null = null;
  private playlist: PhotoItem[] = [];
  private index = -1;
  private version = -1;
  private intervalMs = 30_000;
  private timer = 0;
  private paused = false;
  private suspended = false;
  private showing = false;
  private failures = 0;
  private loadToken = 0;
  private pairs = -1;
  private motion: "off" | "whole" | "fill" = "off";
  private wholeZoom = 1.12;
  private lastMove = -1;
  private still = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  private calm = false;

  constructor(
    private backdrop: HTMLElement,
    private onPhoto: (item: PhotoItem | null) => void,
  ) {
    const make = () => {
      const img = document.createElement("img");
      img.alt = "";
      img.decoding = "async";
      backdrop.appendChild(img);
      return img;
    };
    this.layers = [make(), make()];
    const makeVideo = () => {
      const v = document.createElement("video");
      v.muted = true; // the slideshow is silent
      v.playsInline = true;
      v.preload = "auto";
      backdrop.appendChild(v);
      return v;
    };
    this.videos = [makeVideo(), makeVideo()];
  }

  get hasPhotos() {
    return this.playlist.length > 0;
  }

  get isPaused() {
    return this.paused;
  }

  /** Called whenever the backend reports the state of the photo library. */
  setStatus(st: PhotosStatus) {
    this.intervalMs = Math.max(5, st.intervalSeconds || 30) * 1000;
    this.motion = st.motion ?? "off";
    if (st.motionZoom && st.motionZoom > 1) this.wholeZoom = st.motionZoom;
    if (!st.enabled) return;
    const pairsChanged = this.pairs >= 0 && st.pairs !== undefined && st.pairs !== this.pairs;
    this.pairs = st.pairs ?? this.pairs;
    if (st.version !== this.version) {
      this.version = st.version;
      void this.reload();
    } else if (pairsChanged) {
      void this.refreshUpcoming();
    }
  }

  /** Stops fetching photos while something else covers the dashboard. */
  setSuspended(suspended: boolean) {
    if (suspended === this.suspended) return;
    this.suspended = suspended;
    // Nobody is looking: a photo's movement and a clip stop where they are,
    // so nothing has to be redrawn, and carry on afterwards.
    const shown = this.current;
    if (shown instanceof HTMLVideoElement) {
      if (suspended) shown.pause();
      else void shown.play().catch(() => {});
    }
    for (const a of shown?.getAnimations() ?? []) {
      if (suspended) a.pause();
      else if (a.playState === "paused" && !this.calm) a.play();
    }
    if (suspended) clearTimeout(this.timer);
    else this.schedule();
  }

  /**
   * While the Pi is hot, photos stand still: the one on screen stops where
   * it is and the next ones do not start moving. Photos keep changing.
   */
  setCalm(calm: boolean) {
    if (calm === this.calm) return;
    this.calm = calm;
    if (this.suspended) return; // already standing still; the next photo moves again
    for (const a of this.current?.getAnimations({ subtree: true }) ?? []) {
      if (calm) a.pause();
      else if (a.playState === "paused") a.play();
    }
  }

  togglePause(): boolean {
    this.paused = !this.paused;
    if (this.paused) clearTimeout(this.timer);
    else this.schedule();
    return this.paused;
  }

  next() {
    void this.step(1);
  }

  previous() {
    void this.step(-1);
  }

  private async reload() {
    const token = ++this.loadToken;
    try {
      const res = await fetch(`/api/photos/playlist?limit=${PLAYLIST_SIZE}`);
      const data: { version: number; items: PhotoItem[] } = await res.json();
      if (token !== this.loadToken) return;
      this.playlist = data.items;
      this.index = -1;
      this.failures = 0;
    } catch {
      return; // backend restarting; the next status event retries
    }
    if (this.playlist.length === 0) {
      this.clear();
      return;
    }
    await this.step(1);
  }

  /**
   * The backend has learned which more photos are upright and can share the
   * screen. Takes the photos still to come from a fresh list, without
   * disturbing the one that is showing.
   */
  private async refreshUpcoming() {
    const token = this.loadToken;
    try {
      const res = await fetch(`/api/photos/playlist?limit=${PLAYLIST_SIZE}`);
      const data: { version: number; items: PhotoItem[] } = await res.json();
      if (token !== this.loadToken || data.items.length === 0) return;
      this.playlist = [...this.playlist.slice(0, this.index + 1), ...data.items];
    } catch {
      /* the list in hand keeps working */
    }
  }

  /** Shows these photos next, in this order; the usual shuffle follows them. */
  queue(items: PhotoItem[]) {
    if (items.length === 0) return;
    this.loadToken++; // a refresh of the list that is under way must not undo this
    this.playlist = [...this.playlist.slice(0, Math.max(0, this.index + 1)), ...items];
  }

  private clear() {
    clearTimeout(this.timer);
    for (const l of [...this.layers, ...this.videos]) l.classList.remove("visible");
    for (const v of this.videos) this.unload(v);
    this.current = null;
    this.backdrop.classList.remove("has-photo");
    this.onPhoto(null);
  }

  private url(item: PhotoItem) {
    return `/photos/${encodeURIComponent(item.key)}?v=${item.v}`;
  }

  private async step(delta: number) {
    if (this.showing || this.playlist.length === 0) return;
    clearTimeout(this.timer);

    let target = this.index + delta;
    if (target < 0) target = 0;
    if (target >= this.playlist.length) {
      // End of this shuffle: get a fresh one.
      await this.reload();
      return;
    }

    this.showing = true;
    const item = this.playlist[target];
    // The photo or clip goes into the layer that is not on screen.
    const incoming: HTMLElement = item.video
      ? this.videos.find((v) => v !== this.current)!
      : this.layers.find((l) => l !== this.current)!;
    try {
      if (incoming instanceof HTMLVideoElement) await this.loadVideo(incoming, item);
      else {
        (incoming as HTMLImageElement).src = this.url(item);
        await (incoming as HTMLImageElement).decode();
      }
    } catch {
      // Unreadable photo, a clip the browser cannot play, or the NAS is
      // away: skip it, but do not spin.
      if (incoming instanceof HTMLVideoElement) {
        this.unload(incoming);
        if (!this.suspended) void fetch("/api/photos/unplayable", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ key: item.key }) }).catch(() => {});
      }
      this.showing = false;
      this.index = target;
      this.failures++;
      const delay = this.failures >= 5 ? 60_000 : 500;
      this.timer = window.setTimeout(() => void this.step(delta >= 0 ? 1 : -1), delay);
      return;
    }
    this.failures = 0;
    this.index = target;

    // The new picture fades in on top of the old one, which stays fully opaque
    // underneath, so the screen never dips darker mid-fade.
    const outgoing = this.current;
    incoming.style.zIndex = "2";
    if (outgoing) outgoing.style.zIndex = "1";
    let showFor = this.intervalMs;
    if (incoming instanceof HTMLVideoElement) {
      // A clip plays once, or over and over if it is very short.
      const length = (isFinite(incoming.duration) ? incoming.duration : 30) * 1000;
      incoming.loop = length < 6000;
      showFor = Math.min(VIDEO_MAX_MS, Math.max(length, 6000));
      void incoming.play().catch(() => {});
    } else {
      this.move(incoming as HTMLImageElement);
    }
    incoming.classList.add("visible");
    this.backdrop.classList.add("has-photo");
    this.current = incoming;
    this.onPhoto(item);

    window.setTimeout(() => {
      if (outgoing && outgoing !== this.current) {
        outgoing.classList.remove("visible");
        if (outgoing instanceof HTMLVideoElement) this.unload(outgoing);
      }
      this.showing = false;
    }, FADE_MS);

    // Ask for the next photo now, so the backend has it rendered in time.
    const upcoming = this.playlist[target + 1];
    if (upcoming && !upcoming.video) new Image().src = this.url(upcoming);

    this.schedule(showFor);
  }

  /** Waits until a clip can be played; fails if the browser cannot play it. */
  private loadVideo(v: HTMLVideoElement, item: PhotoItem): Promise<void> {
    return new Promise((resolve, reject) => {
      const done = (ok: boolean) => {
        clearTimeout(timer);
        v.oncanplay = v.onerror = null;
        if (ok) resolve();
        else reject(new Error("video"));
      };
      const timer = window.setTimeout(() => done(false), VIDEO_WAIT_MS);
      v.oncanplay = () => done(true);
      v.onerror = () => done(false);
      v.src = `/videos/${encodeURIComponent(item.key)}?v=${item.v}`;
      v.load();
    });
  }

  /** Stops a clip and lets go of its file. */
  private unload(v: HTMLVideoElement) {
    v.pause();
    v.removeAttribute("src");
    v.load();
  }

  /**
   * Sets a photo moving slowly for as long as it will be on screen: zooming
   * in, zooming out, or gliding sideways, a different one each time.
   *
   * A photo may be shifted by at most (zoom - 1) / 2 of its size before an
   * edge of the frame would show. A photo prepared for "whole" sits in the
   * middle of its frame with room around it, and stays entirely on screen as
   * long as the shift is also at most (1 - zoom / wholeZoom) / 2. Both limits
   * are straight lines, so a movement between two poses that respect them
   * respects them all the way.
   */
  private move(img: HTMLImageElement) {
    for (const a of img.getAnimations()) a.cancel();
    if (this.motion === "off" || this.still || this.calm) return;
    const whole = this.motion === "whole";
    const maxZoom = whole ? this.wholeZoom : FILL_ZOOM;
    const room = (zoom: number) => Math.max(0, Math.min((zoom - 1) / 2, whole ? (1 - zoom / this.wholeZoom) / 2 : 1));

    // Never the same kind of movement twice in a row.
    let kind = Math.floor(Math.random() * 3);
    if (kind === this.lastMove) kind = (kind + 1) % 3;
    this.lastMove = kind;

    const angle = Math.random() * Math.PI * 2;
    const towards = (zoom: number, share: number): Pose => ({
      zoom,
      x: Math.cos(angle) * room(zoom) * share,
      y: Math.sin(angle) * room(zoom) * share,
    });
    let from: Pose, to: Pose;
    if (kind === 2) {
      // A glide from one side to the other, at the zoom that leaves the most room for it.
      const zoom = whole ? (2 * maxZoom) / (maxZoom + 1) : 1 + (maxZoom - 1) * 0.75;
      from = towards(zoom, -1);
      to = towards(zoom, 1);
    } else {
      // A zoom, drifting a little off centre where there is room to.
      const wide: Pose = { zoom: 1, x: 0, y: 0 };
      const close = towards(maxZoom, 0.7);
      [from, to] = kind === 0 ? [wide, close] : [close, wide];
    }
    const css = (p: Pose) => `translate(${(p.x * 100).toFixed(3)}%, ${(p.y * 100).toFixed(3)}%) scale(${p.zoom.toFixed(4)})`;
    // Until the next photo has fully covered this one.
    const duration = this.intervalMs + FADE_MS;
    img.animate([{ transform: css(from) }, { transform: css(to) }], {
      duration,
      // In small steps rather than on every frame: see MOTION_STEPS_PER_SECOND.
      easing: `steps(${Math.max(1, Math.round((duration / 1000) * MOTION_STEPS_PER_SECOND))}, end)`,
      fill: "both",
    });
  }

  private schedule(after = this.intervalMs) {
    clearTimeout(this.timer);
    if (this.paused || this.suspended || this.playlist.length === 0) return;
    this.timer = window.setTimeout(() => void this.step(1), after);
  }
}
