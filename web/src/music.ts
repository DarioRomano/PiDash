// What is heard while the dashboard shows: music that plays somewhere on the
// Pi, and the radio stations of the app row.
import { t } from "./i18n";
import { toast } from "./notify";
import type { RadioStation, Track } from "./types";

/**
 * Music that plays somewhere on the Pi: Spotify behind the dashboard, a
 * Spotify Connect or AirPlay receiver, any player that announces itself. A
 * small card says what it is; the remote's play and skip buttons control it.
 */
export class NowPlaying {
  /** What is playing; null when nothing is. */
  track: Track | null = null;
  private card = document.createElement("div");
  private art = document.createElement("img");
  private note = document.createElement("div"); // shown when there is no cover
  private title = document.createElement("div");
  private by = document.createElement("div");

  constructor(parent: HTMLElement) {
    this.card.id = "playing";
    this.card.hidden = true;
    this.art.alt = "";
    this.note.className = "playing-note";
    this.note.textContent = "♪";
    this.title.className = "playing-title";
    this.by.className = "playing-by";
    const words = document.createElement("div");
    words.className = "playing-words";
    words.append(this.title, this.by);
    this.card.append(this.art, this.note, words);
    // Names of songs and artists are not for translating.
    this.card.dataset.noTranslate = "";
    parent.appendChild(this.card);
    this.art.addEventListener("error", () => {
      this.art.hidden = true;
      this.note.hidden = false;
    });
  }

  update(next: Track | null) {
    if (next && !next.title) next = null; // a player with nothing to say
    this.track = next;
    this.card.hidden = !next;
    if (!next) return;
    this.card.classList.toggle("paused", !next.playing);
    this.title.textContent = next.title;
    this.by.textContent = [next.artist, next.playing ? "" : t("paused")].filter(Boolean).join("  ·  ") || next.player;
    const art = next.art ?? "";
    if (this.art.getAttribute("src") !== art) {
      if (art) this.art.src = art;
      else this.art.removeAttribute("src");
    }
    this.art.hidden = !art;
    this.note.hidden = !!art;
  }

  /** Presses play/pause, next or previous on whatever is playing. */
  press(button: "playpause" | "next" | "previous") {
    void fetch(`/api/playing/${button}`, { method: "POST" }).catch(() => {});
  }
}

/**
 * Radio stations play in the background while the photos carry on. The play
 * button of the remote pauses and resumes the station; opening an app stops it.
 */
export class Radio {
  /** The stations of the app row. */
  stations: RadioStation[] = [];
  /** Which of them plays; -1 when none does. */
  playing = -1;
  private audio = new Audio();
  private label = document.createElement("div");

  constructor(
    parent: HTMLElement,
    /** What plays, or whether it is paused, has changed. */
    private onChange: () => void,
  ) {
    this.audio.preload = "none";
    this.label.id = "radio";
    this.label.hidden = true;
    parent.appendChild(this.label);
    this.audio.addEventListener("error", () => {
      const st = this.stations[this.playing];
      if (!st) return;
      toast(t("{name} cannot be played right now.", { name: st.name }), 6000);
      this.stop();
    });
    this.audio.addEventListener("pause", () => this.show());
    this.audio.addEventListener("playing", () => this.show());
  }

  /** New stations from the settings; what plays is stopped when they differ. */
  setStations(stations: RadioStation[]) {
    if (JSON.stringify(stations) === JSON.stringify(this.stations)) return;
    this.stop();
    this.stations = stations;
    this.onChange();
  }

  private show() {
    const st = this.stations[this.playing];
    this.label.hidden = !st;
    this.label.textContent = st ? `♪  ${st.name}${this.audio.paused ? "  ·  " + t("paused") : ""}` : "";
    this.onChange();
  }

  stop() {
    if (this.playing < 0) return;
    this.playing = -1;
    this.audio.pause();
    this.audio.removeAttribute("src");
    this.audio.load(); // drops the connection to the station
    this.show();
  }

  /** Starts a station, or stops it when it is the one that plays. */
  toggle(index: number) {
    if (this.playing === index) return this.stop();
    const st = this.stations[index];
    if (!st) return;
    this.playing = index;
    this.audio.src = st.url;
    // The browser announces what it plays to the rest of the Pi. Marked as
    // PiDash's own, so that it is not shown a second time as "now playing".
    if ("mediaSession" in navigator) navigator.mediaSession.metadata = new MediaMetadata({ title: st.name, album: "PiDash" });
    this.audio.play().catch((err: unknown) => {
      // Pausing before the station has started is not a failure.
      if (this.playing !== index || (err instanceof DOMException && err.name === "AbortError")) return;
      toast(t("{name} cannot be played right now.", { name: st.name }), 6000);
      this.stop();
    });
    this.show();
  }

  /** The remote's play button. A live station cannot be resumed where it stopped: pausing mutes it. */
  playPause() {
    if (this.audio.paused) void this.audio.play().catch(() => this.stop());
    else this.audio.pause();
  }
}
