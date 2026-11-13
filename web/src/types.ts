// Shapes shared with the Go backend (internal/config, internal/providers, internal/server).

export interface DisplayConfig {
  safe_area: number;
  debug_keys: boolean;
  pixel_shift: number;
  clock_24h: boolean;
  locale: string;
}

export interface ClientConfig {
  display: DisplayConfig;
  keys: Record<string, string>;
  version: string;
  notices: string[];
  started: number;
}

export interface StateEntry<T = unknown> {
  provider: string;
  data: T;
  updatedAt: string;
  stale: boolean;
  error?: string;
}

export interface InputEvent {
  action: string;
  text?: string;
  /** key: the browser saw it; evdev: the backend read it from the TV remote. */
  source: "key" | "remote" | "pointer" | "evdev";
}

export interface SystemState {
  hostname: string;
  uptimeSeconds: number;
  load1: number;
  memTotalMB: number;
  memUsedMB: number;
  cpuTempC: number | null;
  addresses: string[];
}

export interface AppInfo {
  id: string;
  name: string;
  mode: "desktop" | "console";
  hasIcon: boolean;
}

export interface AppsStatus {
  apps: AppInfo[];
  /** id of the running app, or "". */
  running: string;
  consoleReady: boolean;
  error?: string;
}

export interface PhotoFolder {
  id: string;
  name: string;
  path: string;
  /** Cannot contain other folders: an album. */
  leaf?: boolean;
  /** A short note next to the name, e.g. "12 items". */
  detail?: string;
}

export interface PhotoSelection {
  folders: PhotoFolder[];
  recursive: boolean;
}

export interface PhotosStatus {
  enabled: boolean;
  source: string;
  count: number;
  indexing: boolean;
  error?: string;
  intervalSeconds: number;
  selection: PhotoSelection;
  version: number;
}

export interface PhotoItem {
  key: string;
  name: string;
  folder: string;
  taken: string;
  v: string;
}

export interface WeatherDay {
  date: string;
  code: number;
  max: number;
  min: number;
  precipitationProbability: number;
  precipitationSum: number;
  sunrise: string;
  sunset: string;
  uvIndexMax: number;
  windSpeedMax: number;
}

export interface WeatherState {
  location: string;
  timezone: string;
  units: { temperature: string; wind: string; precipitation: string };
  current: {
    time: string;
    temperature: number;
    feelsLike: number;
    humidity: number;
    windSpeed: number;
    windGusts: number;
    windDirection: number;
    precipitation: number;
    code: number;
    isDay: boolean;
  };
  hourly: { time: string; temperature: number; precipitationProbability: number; precipitation: number; code: number; isDay: boolean }[];
  daily: WeatherDay[];
}

/** The complete remote-control vocabulary. Everything else is ignored. */
export type Action =
  | "up" | "down" | "left" | "right"
  | "select" | "back" | "menu" | "home" | "playpause" | "exit" | "weather" | "help" | "settings"
  | "0" | "1" | "2" | "3" | "4" | "5" | "6" | "7" | "8" | "9";

export const ACTIONS: ReadonlySet<string> = new Set<Action>([
  "up", "down", "left", "right", "select", "back", "menu", "home", "playpause", "exit", "weather", "help", "settings",
  "0", "1", "2", "3", "4", "5", "6", "7", "8", "9",
]);

export function isAction(s: string): s is Action {
  return ACTIONS.has(s);
}
