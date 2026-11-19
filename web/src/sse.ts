import type { AppsStatus, ClientConfig, InputEvent, PhotosStatus, StateEntry, Track } from "./types";

export interface SSEHandlers {
  onConfig(cfg: ClientConfig): void;
  onState(entry: StateEntry): void;
  onInput(ev: InputEvent): void;
  onApps(status: AppsStatus): void;
  onPhotos(status: PhotosStatus): void;
  /** The state of the paintings that can stand in for the photos. */
  onArt(status: PhotosStatus): void;
  onTV(status: { asleep: boolean; elsewhere?: boolean; available?: boolean; sleepAt?: string; sleepMinutes?: number; idle?: boolean }): void;
  /** A PC is being woken for a game. */
  onWaking(pc: { name: string }): void;
  /** The Pi asks for one of the dashboard's pages to be opened. */
  onOpen(what: { page: string }): void;
  /** The music that is playing changed; null when nothing is. */
  onPlaying(track: Track | null): void;
  /** The sound outputs changed: one appeared, went away, or was chosen. */
  onSound(status: { available: boolean; outputs: { id: string; name: string; active: boolean; unplugged?: boolean }[] }): void;
  /** A controller or headset connected over Bluetooth is running low. */
  onBattery(notice: { name: string; percent: number }): void;
  /** The remote changed the volume of headphones or a speaker. */
  onVolume(now: { name: string; volume: number }): void;
  onStatus(connected: boolean): void;
}

/**
 * Connects to the backend's Server-Sent Events stream. EventSource reconnects
 * on its own; we only surface the connection state for the status dot.
 */
export function connect(url: string, h: SSEHandlers): EventSource {
  const es = new EventSource(url);

  es.addEventListener("open", () => h.onStatus(true));
  es.addEventListener("error", () => h.onStatus(false));

  es.addEventListener("config", (e) => h.onConfig(JSON.parse((e as MessageEvent).data)));
  es.addEventListener("state", (e) => h.onState(JSON.parse((e as MessageEvent).data)));
  es.addEventListener("input", (e) => h.onInput(JSON.parse((e as MessageEvent).data)));
  es.addEventListener("apps", (e) => h.onApps(JSON.parse((e as MessageEvent).data)));
  es.addEventListener("photos", (e) => h.onPhotos(JSON.parse((e as MessageEvent).data)));
  es.addEventListener("art", (e) => h.onArt(JSON.parse((e as MessageEvent).data)));
  es.addEventListener("waking", (e) => h.onWaking(JSON.parse((e as MessageEvent).data)));
  // ("open" is the connection's own word for having been made.)
  es.addEventListener("page", (e) => h.onOpen(JSON.parse((e as MessageEvent).data)));
  es.addEventListener("playing", (e) => h.onPlaying(JSON.parse((e as MessageEvent).data)));
  es.addEventListener("sound", (e) => h.onSound(JSON.parse((e as MessageEvent).data)));
  es.addEventListener("battery", (e) => h.onBattery(JSON.parse((e as MessageEvent).data)));
  es.addEventListener("volume", (e) => h.onVolume(JSON.parse((e as MessageEvent).data)));
  es.addEventListener("tv", (e) => h.onTV(JSON.parse((e as MessageEvent).data)));

  return es;
}
