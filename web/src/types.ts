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
  /** "on" or "off": whether a photo has its caption; empty leaves it to view. */
  caption?: string;
  /** "off", "short" or "full": how much of the weather the dashboard shows. */
  weather_detail?: string;
  /** Down on the remote does not change what is shown. */
  lock_view?: boolean;
  /** "smooth", "balanced" or "saver": how much work the Pi does for the picture. */
  performance?: string;
  /** Pictures a second for the moving background; 0 leaves it to performance. */
  background_fps?: number;
  /** "off", "light" or "strong": how much the picture darkens in the evening. */
  evening_dim?: string;
  /** "bold", "slim" or "analog". */
  clock_style?: string;
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
  /** The name of the Wi-Fi for visitors; empty when none is set up. */
  guest_wifi?: string;
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

/** The music that is playing somewhere on the Pi. */
export interface Track {
  player: string;
  title: string;
  artist: string;
  album: string;
  playing: boolean;
  /** Where the cover is; empty when there is none. */
  art?: string;
  canSkip: boolean;
}

export interface AppInfo {
  id: string;
  name: string;
  mode: "desktop" | "console";
  hasIcon: boolean;
  /** A game of the PC, from Moonlight: a tile that cannot be moved. */
  game?: boolean;
}

export interface AppsStatus {
  apps: AppInfo[];
  /** id of the app that has the screen, or "". */
  running: string;
  /** id of an app that keeps playing behind the dashboard, or "". */
  background?: string;
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
  /** How many favourites the slideshow has, and whether it shows nothing else. */
  favourites?: number;
  onlyFavourites?: boolean;
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
  /** One of the person's favourites. */
  favourite?: boolean;
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
  /** The next two hours: whether it rains, and from when to when. */
  rain?: { now: boolean; starts?: string; stops?: string; snow?: boolean };
  /** Air quality and pollen, where they are known. */
  air?: {
    index: number;
    scale: "eu" | "us";
    /** 0 (good) to 5. */
    level: number;
    pm25: number;
    pm10: number;
    pollen: { kind: string; level: number }[];
  };
}

/** The complete remote-control vocabulary. Everything else is ignored. */
export type Action =
  | "up" | "down" | "left" | "right"
  | "select" | "back" | "menu" | "home" | "playpause" | "next" | "previous" | "exit" | "weather" | "help" | "settings"
  | "0" | "1" | "2" | "3" | "4" | "5" | "6" | "7" | "8" | "9";

export const ACTIONS: ReadonlySet<string> = new Set<Action>([
  "up", "down", "left", "right", "select", "back", "menu", "home", "playpause", "next", "previous", "exit", "weather", "help", "settings",
  "0", "1", "2", "3", "4", "5", "6", "7", "8", "9",
]);

export function isAction(s: string): s is Action {
  return ACTIONS.has(s);
}
