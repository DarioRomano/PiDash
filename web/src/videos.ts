import type { Card, Grid } from "./grid";
import { t } from "./i18n";
import { postJSON, sayRefusal, toast, UNREACHABLE } from "./notify";
import { FOLDER_GLYPH, VIDEOS_GLYPH } from "./tiles";

interface VideoEntry {
  name: string;
  path: string;
  folder: boolean;
  size?: number;
  /** Begun before: playing carries on where it stopped. */
  started?: boolean;
}
interface VideoList {
  title: string;
  path: string;
  /** The folder above; absent at the top. */
  parent?: string;
  entries: VideoEntry[];
  /** The program that plays them; empty when none is installed. */
  player: string;
}

const AT = "pidash-videos-at";

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
    const cards: Card[] = list.entries.map((e) => ({
      id: e.path,
      title: e.name,
      glyph: e.folder ? FOLDER_GLYPH : VIDEOS_GLYPH,
      on: !!e.started,
      sub: e.folder ? undefined : [e.size ? fileSize(e.size) : "", e.started ? t("Carries on where it stopped") : ""].filter(Boolean).join("  ·  ") || undefined,
    }));
    this.grid.open({
      title: list.title || t("Videos"),
      cards,
      shape: "rows",
      selected,
      empty: list.player ? "There are no videos in this folder." : "No program that plays videos is installed. On the Pi: sudo apt install mpv",
      foot: list.parent !== undefined ? "◀ ▶ ▲ ▼ choose  ·  OK open or play  ·  Back the folder above" : "◀ ▶ ▲ ▼ choose  ·  OK open or play  ·  Back close",
      onChoose: (card) => {
        const entry = this.list?.entries.find((e) => e.path === card.id);
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
