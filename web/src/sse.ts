import type { AppsStatus, ClientConfig, InputEvent, PhotosStatus, StateEntry } from "./types";

export interface SSEHandlers {
  onConfig(cfg: ClientConfig): void;
  onState(entry: StateEntry): void;
  onInput(ev: InputEvent): void;
  onApps(status: AppsStatus): void;
  onPhotos(status: PhotosStatus): void;
  onTV(status: { asleep: boolean; available?: boolean; sleepAt?: string; sleepMinutes?: number }): void;
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
  es.addEventListener("tv", (e) => h.onTV(JSON.parse((e as MessageEvent).data)));

  return es;
}
