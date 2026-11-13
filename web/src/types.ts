// Shapes shared with the Go backend (internal/config, internal/providers, internal/server).

export interface DisplayConfig {
  safe_area: number;
  debug_keys: boolean;
  pixel_shift: number;
  /** Seconds without input after which the app row closes; 0 = never. */
  app_row_timeout: number;
  /** Night mode hours, "HH:MM"; both empty when it is off. */
  night_start: string;
  night_end: string;
  /** "en", "de", "fr" or "it"; empty means English. */
  language: string;
  /** Corner of the clock, its size, and what shows at start. */
  clock_position: string;
  clock_size: string;
  view: string;
  /** "moving" or "still": the background shown without photos. */
  background: string;
  /** Small sounds when the remote is used. */
  sounds?: boolean;
  clock_24h: boolean;
  locale: string;
}

/** An internet radio station in the app row. */
export interface RadioStation {
  name: string;
  url: string;
}

export interface ClientConfig {
  display: DisplayConfig;
  keys: Record<string, string>;
  version: string;
  notices: string[];
  started: number;
  radio?: RadioStation[];
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
  /** A direction sent again because its button is being held. */
  repeat?: boolean;
}

export interface SystemState {
  hostname: string;
  uptimeSeconds: number;
  load1: number;
  memTotalMB: number;
  memUsedMB: number;
  cpuTempC: number | null;
  addresses: string[];
  /** The Pi is hot: do less until it has cooled down. */
  hot?: boolean;
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

/** A Home Assistant device or value shown as a tile. */
export interface HomeEntity {
  id: string;
  name: string;
  domain: string;
  text: string;
  on: boolean;
  actionable: boolean;
  available: boolean;
}

/** One coming calendar event. */
export interface CalendarEvent {
  title: string;
  start: string;
  end: string;
  allDay: boolean;
}

export interface CalendarState {
  events: CalendarEvent[];
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
  /** How photos move: standing still, moving with the whole photo in view, or filling the screen. */
  motion?: "off" | "whole" | "fill";
  /** The largest zoom at which a photo prepared for "whole" still fits the screen. */
  motionZoom?: number;
  /** Changes when more photos can be shown two at a time. */
  pairs?: number;
  /** Photos the person chose never to see again. */
  hidden?: number;
}

export interface PhotoItem {
  key: string;
  name: string;
  folder: string;
  taken: string;
  /** Where it was taken, when known. */
  place?: string;
  /** A video clip rather than a photo. */
  video?: boolean;
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
