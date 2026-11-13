import type { PhotoItem, PhotosStatus } from "./types";

const FADE_MS = 1700; // a little longer than the CSS opacity transition
const PLAYLIST_SIZE = 200;

/**
 * The background slideshow. Photos arrive from the backend already rendered at
 * 1920x1080, so the browser only decodes a small JPEG and crossfades two
 * stacked <img> elements by opacity.
 */
export class Slideshow {
  private layers: [HTMLImageElement, HTMLImageElement];
  private front = 0;
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
    if (!st.enabled) return;
    if (st.version !== this.version) {
      this.version = st.version;
      void this.reload();
    }
  }

  /** Stops fetching photos while something else covers the dashboard. */
  setSuspended(suspended: boolean) {
    if (suspended === this.suspended) return;
    this.suspended = suspended;
    if (suspended) clearTimeout(this.timer);
    else this.schedule();
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

  private clear() {
    clearTimeout(this.timer);
    for (const l of this.layers) l.classList.remove("visible");
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
    const incoming = this.layers[1 - this.front];
    try {
      incoming.src = this.url(item);
      await incoming.decode();
    } catch {
      // Unreadable photo or the NAS is away: skip it, but do not spin.
      this.showing = false;
      this.index = target;
      this.failures++;
      const delay = this.failures >= 5 ? 60_000 : 500;
      this.timer = window.setTimeout(() => void this.step(delta >= 0 ? 1 : -1), delay);
      return;
    }
    this.failures = 0;
    this.index = target;

    // The new photo fades in on top of the old one, which stays fully opaque
    // underneath, so the screen never dips darker mid-fade.
    const outgoing = this.layers[this.front];
    incoming.style.zIndex = "2";
    outgoing.style.zIndex = "1";
    incoming.classList.add("visible");
    this.backdrop.classList.add("has-photo");
    this.front = 1 - this.front;
    this.onPhoto(item);

    window.setTimeout(() => {
      outgoing.classList.remove("visible");
      this.showing = false;
    }, FADE_MS);

    // Ask for the next photo now, so the backend has it rendered in time.
    const upcoming = this.playlist[target + 1];
    if (upcoming) new Image().src = this.url(upcoming);

    this.schedule();
  }

  private schedule() {
    clearTimeout(this.timer);
    if (this.paused || this.suspended || this.playlist.length === 0) return;
    this.timer = window.setTimeout(() => void this.step(1), this.intervalMs);
  }
}
