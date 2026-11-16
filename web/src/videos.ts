import type { Card, Grid } from "./grid";
import { t } from "./i18n";
import { postJSON, sayRefusal, toast, UNREACHABLE } from "./notify";
import { FOLDER_GLYPH, VIDEOS_GLYPH } from "./tiles";

interface VideoEntry {
  name: string;
  path: string;
  folder: boolean;
  size?: number;
  /** Begun before: playing carries on where it stopped, at so many seconds. */
  started?: boolean;
  position?: number;
  /** It can have a cover; this changes when the video's file does. */
  cover?: string;
}
interface VideoList {
  title: string;
  path: string;
  /** The folder above; absent at the top. */
  parent?: string;
  entries: VideoEntry[];
  /** The program that plays them; empty when none is installed. */
  player: string;
  /** Begun and not finished, the one watched last first. Only at the top. */
  continue?: VideoEntry[];
}

const AT = "pidash-videos-at";
/** Before the id of a card under "Carry on watching". */
const CONTINUE = "again\n";

/** Seconds into a film, as a player shows them: 1:02:33, or 4:05. */
function clockTime(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  const two = (n: number) => String(n).padStart(2, "0");
  return s >= 3600 ? `${Math.floor(s / 3600)}:${two(Math.floor((s % 3600) / 60))}:${two(s % 60)}` : `${Math.floor(s / 60)}:${two(s % 60)}`;
}

function fileSize(bytes: number): string {
  if (bytes >= 1 << 30) return `${(bytes / (1 << 30)).toFixed(1)} GB`;
  return `${Math.max(1, Math.round(bytes / (1 << 20)))} MB`;
}

/** The videos of a folder or of the NAS: folders to walk through, films to play. */
export class Videos {
  private list: VideoList | null = null;

  constructor(
    private grid: Grid,
    /** The page is about to show the videos. */
    private onShow: () => void,
  ) {}

  /** Opens a folder; without one, the folder that was open last. */
  async open(path?: string, selected?: string) {
    if (path === undefined) {
      try {
        path = localStorage.getItem(AT) ?? "";
      } catch {
        path = "";
      }
    }
    let list: VideoList;
    try {
      let res = await fetch(`/api/videos?path=${encodeURIComponent(path)}`, { cache: "no-store" });
      // A folder that is gone: start again at the top.
      if (res.status === 404 && path !== "") res = await fetch("/api/videos?path=", { cache: "no-store" });
      if (!res.ok) return sayRefusal(res);
      list = (await res.json()) as VideoList;
    } catch {
      return toast(UNREACHABLE, 6000);
    }
    this.list = list;
    try {
      localStorage.setItem(AT, list.path);
    } catch {
      /* then it opens at the top next time */
    }
    this.onShow();
    const card = (e: VideoEntry, again: boolean): Card => ({
      // What is begun is on the page twice at the top: under "Carry on", and where it lies.
      id: (again ? CONTINUE : "") + e.path,
      // A film's name without the ending of its file.
      title: e.folder ? e.name : e.name.replace(/\.[a-z0-9]{2,5}$/i, ""),
      glyph: e.folder ? FOLDER_GLYPH : VIDEOS_GLYPH,
      image: e.cover ? `/api/videos/cover?path=${encodeURIComponent(e.path)}&v=${e.cover}` : undefined,
      on: !!e.started,
      sub: e.folder ? undefined : [e.started ? (e.position ? t("Carries on at {time}", { time: clockTime(e.position) }) : t("Carries on where it stopped")) : "", e.size ? fileSize(e.size) : ""].filter(Boolean).join("  ·  ") || undefined,
    });
    const begun = (list.continue ?? []).map((e) => card(e, true));
    const rest = list.entries.map((e) => card(e, false));
    if (begun.length > 0) {
      begun[0].heading = t("Carry on watching");
      if (rest.length > 0) rest[0].heading = t("Everything");
    }
    const cards = [...begun, ...rest];
    this.grid.open({
      title: list.title || t("Videos"),
      cards,
      shape: "wide",
      selected,
      empty: list.player ? "There are no videos in this folder." : "No program that plays videos is installed. On the Pi: sudo apt install mpv",
      foot: list.parent !== undefined ? "◀ ▶ ▲ ▼ choose  ·  OK open or play  ·  Back the folder above" : "◀ ▶ ▲ ▼ choose  ·  OK open or play  ·  Back close",
      onChoose: (card) => {
        const again = card.id.startsWith(CONTINUE);
        const entry = again ? this.list?.continue?.find((e) => CONTINUE + e.path === card.id) : this.list?.entries.find((e) => e.path === card.id);
        if (!entry) return;
        if (entry.folder) return void this.open(entry.path);
        toast(t("Starting {name}…", { name: entry.name }));
        void postJSON("/api/videos/play", { path: entry.path })
          .then((res) => (res.ok ? undefined : sayRefusal(res)))
          .catch(() => toast(UNREACHABLE, 6000));
      },
      onBack: () => {
        const here = this.list;
        if (!here || here.parent === undefined) return false;
        void this.open(here.parent, here.path);
        return true;
      },
    });
  }
}
