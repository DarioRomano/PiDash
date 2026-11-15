import { t } from "./i18n";
import type { Menu } from "./menu";
import { postJSON, sayRefusal, toast, UNREACHABLE } from "./notify";
import type { PhotoInfo } from "./photoinfo";
import type { RailItem } from "./rail";
import type { Slideshow } from "./slideshow";
import {
  FAV_GLYPH,
  FAV_ID,
  FAV_ON_GLYPH,
  HIDE_GLYPH,
  HIDE_ID,
  INFO_GLYPH,
  INFO_ID,
  MORE_GLYPH,
  MORE_ID,
  NEW_GLYPH,
  NEW_ID,
  ONLY_FAV_GLYPH,
  ONLY_FAV_ID,
  SPEED_GLYPH,
  SPEED_ID,
} from "./tiles";
import type { Action, PhotoItem, PhotosStatus } from "./types";

/** What the photo's buttons need from the dashboard around them. */
export interface PhotoWorld {
  slideshow: Slideshow;
  menu: Menu;
  photoInfo: PhotoInfo;
  /** The photo on screen. */
  shown(): PhotoItem | null;
  /** What the Pi last said about the photos. */
  status(): PhotosStatus | null;
  /** The Pi answered with a new state of the photos. */
  onStatus(st: PhotosStatus): void;
  /** A photo's favourite mark changed: its caption and its buttons follow. */
  redraw(photo: PhotoItem): void;
  /** Closes the app row, for what leads somewhere else. */
  closeRow(): void;
  /** The locale dates are written in. */
  locale(): string | undefined;
  /** Not the moment to say something: night, an app, or nobody looking. */
  quiet(): boolean;
}

// How long a photo stays, as the button in the bar steps through it.
const PHOTO_SECONDS = [10, 20, 30, 60, 120, 300, 600];
// A photo that was hidden a moment ago can be taken back with OK.
const UNDO_MS = 8000;
const NEW_SAID = "pidash-new-photos-said";

function seconds(n: number): string {
  return n < 60 ? t("{n} seconds", { n }) : n === 60 ? t("1 minute") : t("{n} minutes", { n: Math.round(n / 60) });
}

/**
 * What can be done with the photo on screen: the buttons of the bar that Up
 * opens, and what each of them does.
 */
export class PhotoActions {
  private undoHide: { key: string; until: number } | null = null;
  private newSaid = "";

  constructor(private w: PhotoWorld) {}

  /** The buttons, as they stand for the photo on screen. */
  items(): RailItem[] {
    const items: RailItem[] = [];
    const st = this.w.status();
    const shown = this.w.shown();
    // Photos that arrived lately come first, while there are any.
    if ((st?.new ?? 0) > 0) {
      const n = st?.new ?? 0;
      const where = (st?.newIn ?? "").split(" / ").pop() ?? "";
      items.push({ id: NEW_ID, label: "Show the new photos", glyph: NEW_GLYPH, sub: where ? t(n === 1 ? "1 in {album}" : "{n} in {album}", { n, album: where }) : String(n), running: true });
    }
    if (shown) {
      items.push(
        shown.favourite
          ? { id: FAV_ID, label: "Remove from favourites", glyph: FAV_ON_GLYPH, running: true }
          : { id: FAV_ID, label: "Add to favourites", glyph: FAV_GLYPH },
      );
      items.push({ id: INFO_ID, label: "About this photo", glyph: INFO_GLYPH, sub: shown.place });
      items.push({ id: MORE_ID, label: "More like this", glyph: MORE_GLYPH, sub: t("its album, its day or its year") });
      // A photo that should not come up again can be hidden; of two side by
      // side, either one.
      const keys = shown.key.split("+");
      if (keys.length === 2) {
        items.push({ id: HIDE_ID + keys[0], label: "Hide left photo", glyph: HIDE_GLYPH });
        items.push({ id: HIDE_ID + keys[1], label: "Hide right photo", glyph: HIDE_GLYPH });
      } else {
        items.push({ id: HIDE_ID + keys[0], label: "Hide this photo", glyph: HIDE_GLYPH });
      }
    }
    if (st?.onlyFavourites) {
      items.push({ id: ONLY_FAV_ID, label: "Show all photos", glyph: ONLY_FAV_GLYPH, running: true });
    } else if ((st?.favourites ?? 0) > 0) {
      const n = st?.favourites ?? 0;
      items.push({ id: ONLY_FAV_ID, label: "Only favourites", glyph: ONLY_FAV_GLYPH, sub: t(n === 1 ? "1 photo" : "{n} photos", { n }) });
    }
    // How long a photo stays: each OK is the next length.
    if (st?.enabled && (st.count ?? 0) > 0) {
      items.push({ id: SPEED_ID, label: "Time per photo", glyph: SPEED_GLYPH, sub: seconds(st.intervalSeconds) });
    }
    return items;
  }

  /** Does what one of the buttons is for. Returns false for a button that is not one of these. */
  async act(item: RailItem): Promise<boolean> {
    if (item.id === INFO_ID) this.about();
    else if (item.id === FAV_ID) await this.favourite();
    else if (item.id === ONLY_FAV_ID) await this.onlyFavourites();
    else if (item.id === MORE_ID) this.more();
    else if (item.id === SPEED_ID) await this.nextSpeed();
    else if (item.id.startsWith(HIDE_ID)) await this.hide(item.id.slice(HIDE_ID.length));
    else if (item.id === NEW_ID) await this.showNew();
    else return false;
    return true;
  }

  private about() {
    const photo = this.w.shown();
    if (!photo) return;
    this.w.closeRow();
    this.w.photoInfo.open(photo.key, photo.v);
  }

  private async favourite() {
    const photo = this.w.shown();
    if (!photo) return;
    const on = !photo.favourite;
    try {
      const res = await postJSON("/api/photos/favourite", { keys: [photo.key], on });
      if (!res.ok) throw new Error(String(res.status));
      photo.favourite = on;
      this.w.redraw(photo);
      toast(on ? "Added to favourites. Favourites come round more often." : "Removed from favourites.", 5000);
    } catch {
      toast(UNREACHABLE, 5000);
    }
  }

  private async onlyFavourites() {
    const on = !this.w.status()?.onlyFavourites;
    try {
      const res = await postJSON("/api/photos/only-favourites", { on });
      if (!res.ok) throw new Error(String(res.status));
      this.w.onStatus((await res.json()) as PhotosStatus);
      this.w.closeRow();
      toast(on ? "Showing only your favourites." : "Showing all photos again.", 5000);
    } catch {
      toast(UNREACHABLE, 5000);
    }
  }

  /** More of what: its album, the day it was taken, or that year. */
  private more() {
    const photo = this.w.shown();
    if (!photo) return;
    this.w.closeRow();
    const album = photo.folder.split(" / ").pop() ?? "";
    const taken = new Date(photo.taken);
    const dated = !isNaN(taken.getTime()) && taken.getFullYear() >= 1990;
    const more = async (query: string, none: string, some: string) => {
      try {
        const data: { items: PhotoItem[] } = await (await fetch(`/api/photos/playlist?${query}&limit=40`)).json();
        // Without the photo that is on screen already.
        const others = data.items.filter((i) => i.key !== photo.key);
        this.w.slideshow.queue(others);
        toast(others.length === 0 ? none : some.replace("{n}", String(others.length)), 5000);
        if (others.length > 0) this.w.slideshow.next();
      } catch {
        toast(UNREACHABLE, 6000);
      }
    };
    const day = dated ? new Intl.DateTimeFormat(this.w.locale(), { day: "numeric", month: "long", year: "numeric", timeZone: "UTC" }).format(taken) : "";
    const choices = [
      {
        label: t("From this album"),
        hint: album,
        run: () =>
          more(
            `folder=${encodeURIComponent(photo.folder)}&after=${encodeURIComponent(photo.key)}`,
            t("There are no other photos in “{album}”.", { album }),
            t("Staying with “{album}” for the next {n} pictures.", { album, n: "{n}" }),
          ),
      },
    ];
    if (dated) {
      const iso = taken.toISOString().slice(0, 10);
      choices.push({
        label: t("From this day"),
        hint: day,
        run: () => more(`day=${iso}`, t("There are no other photos from {when}.", { when: day }), t("The next {n} pictures are from {when}.", { when: day, n: "{n}" })),
      });
      const year = String(taken.getUTCFullYear());
      choices.push({
        label: t("From this year"),
        hint: year,
        run: () => more(`year=${year}`, t("There are no other photos from {when}.", { when: year }), t("The next {n} pictures are from {when}.", { when: year, n: "{n}" })),
      });
    }
    this.w.menu.open(t("More like this"), choices, 0, (i) => void choices[i]?.run());
  }

  /** On to the next length, and round again. */
  private async nextSpeed() {
    const now = this.w.status()?.intervalSeconds ?? 30;
    const next = PHOTO_SECONDS.find((s) => s > now) ?? PHOTO_SECONDS[0];
    try {
      const res = await postJSON("/api/photos/interval", { seconds: next });
      if (res.ok) this.w.onStatus((await res.json()) as PhotosStatus);
      else await sayRefusal(res);
    } catch {
      toast(UNREACHABLE, 6000);
    }
  }

  private async hide(key: string) {
    this.w.closeRow();
    try {
      const res = await postJSON("/api/photos/hide", { keys: [key] });
      if (res.ok) {
        // A slip of the thumb can be taken back for a few seconds.
        this.undoHide = { key, until: Date.now() + UNDO_MS };
        toast("Hidden. Press OK now to take that back.", UNDO_MS);
      } else toast((await res.text()).trim(), 6000);
    } catch {
      toast(UNREACHABLE, 6000);
    }
  }

  /**
   * Right after a photo was hidden, OK takes that back; anything else lets it
   * stand. Returns true when the button was used up for that.
   */
  undo(action: Action): boolean {
    const undo = this.undoHide;
    if (!undo) return false;
    this.undoHide = null;
    if (action !== "select" || Date.now() >= undo.until) return false;
    void postJSON("/api/photos/unhide", { keys: [undo.key] })
      .then((res) => toast(res.ok ? "It is back, and will come round again." : UNREACHABLE, 5000))
      .catch(() => toast(UNREACHABLE, 5000));
    return true;
  }

  private async showNew() {
    try {
      const data: { items: PhotoItem[] } = await (await fetch("/api/photos/playlist?new=1&limit=60")).json();
      if (data.items.length > 0) {
        this.w.slideshow.queue(data.items);
        this.w.slideshow.next();
        toast(t(data.items.length === 1 ? "Here is the new photo." : "Here are the {n} new photos, the newest first.", { n: data.items.length }), 5000);
      }
      // Looked at: they are new no longer.
      this.w.onStatus((await (await postJSON("/api/photos/seen-new", {})).json()) as PhotosStatus);
    } catch {
      toast(UNREACHABLE, 6000);
    }
  }

  /** Says once that photos have arrived; after that the bar that Up opens keeps offering them. */
  tellAboutNew() {
    const st = this.w.status();
    const n = st?.new ?? 0;
    if (n === 0 || this.w.quiet()) return;
    const what = `${n}/${st?.newIn ?? ""}`;
    try {
      if (localStorage.getItem(NEW_SAID) === what) return;
      localStorage.setItem(NEW_SAID, what);
    } catch {
      if (this.newSaid === what) return;
    }
    this.newSaid = what;
    const album = (st?.newIn ?? "").split(" / ").pop() ?? "";
    toast(t(n === 1 ? "A new photo in “{album}”. ▲ shows it." : "{n} new photos, most of them in “{album}”. ▲ shows them.", { n, album }), 12000);
  }
}
