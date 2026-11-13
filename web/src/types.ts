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
  source: "key" | "remote";
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

/** The complete remote-control vocabulary. Everything else is ignored. */
export type Action =
  | "up" | "down" | "left" | "right"
  | "select" | "back" | "menu" | "home" | "playpause" | "exit"
  | "0" | "1" | "2" | "3" | "4" | "5" | "6" | "7" | "8" | "9";

export const ACTIONS: ReadonlySet<string> = new Set<Action>([
  "up", "down", "left", "right", "select", "back", "menu", "home", "playpause", "exit",
  "0", "1", "2", "3", "4", "5", "6", "7", "8", "9",
]);

export function isAction(s: string): s is Action {
  return ACTIONS.has(s);
}
