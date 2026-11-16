// The dashboard on the TV. This file puts the pieces together: it makes the
// clock, the slideshow, the app row and the pages, tells each what the Pi
// reports, and decides what a button of the remote means at any moment. What
// a piece does by itself is in its own file:
//
//   slideshow, ambient, caption   the photo, the moving background, the line under a photo
//   rail, tiles                   the app row, and the names and pictures of PiDash's own tiles
//   photobar, photoactions        the bar that Up opens, and what its buttons do
//   grid, videos, places          the pages that fill the screen
//   menu, quickmenus              short lists: Bluetooth, restarting
//   music, soundtile, sleeptimer  now playing and radio; two tiles that act when pressed
//   night, agenda, weather        night mode, the calendar, the weather
//   hud, screen                   mouse and touch; fitting the screen and keeping it awake
//   notify                        the line of text at the bottom, and requests to the Pi
import { Agenda } from "./agenda";
import { Departures, type DeparturesState } from "./departures";
import { reportAlive } from "./alive";
import { Ambient, iconPalette } from "./ambient";
import { captionOf } from "./caption";
import { Clock } from "./clock";
import { Grid, type Card } from "./grid";
import { Help } from "./help";
import { Hud } from "./hud";
import { setButtonNames, setLanguage, t } from "./i18n";
import { Input } from "./input";
import { Menu } from "./menu";
import { NowPlaying, Radio } from "./music";
import { Night } from "./night";
import { postJSON, said, sayRefusal, toast, UNREACHABLE } from "./notify";
import { PhotoActions } from "./photoactions";
import { PhotoBar } from "./photobar";
import { PhotoInfo } from "./photoinfo";
import { placesList, tripsList } from "./journeys";
import { Places } from "./places";
import { openPowerMenu, QuickBluetooth } from "./quickmenus";
import { Quiz } from "./quiz";
import { Radar } from "./radar";
import { Rail, type RailItem } from "./rail";
import { fitToScreen, keepAwake, startPixelShift } from "./screen";
import { play, setSounds } from "./sfx";
import { SleepTimer } from "./sleeptimer";
import { Slideshow } from "./slideshow";
import { SoundTile } from "./soundtile";
import { connect } from "./sse";
import { StatusPanel } from "./status";
import type { Action, AppsStatus, CalendarState, ClientConfig, HomeEntity, PhotoItem, PhotosStatus, StateEntry, SystemState, WeatherState } from "./types";
import { UpdateNotice } from "./updatenotice";
import { Videos } from "./videos";
import { VolumeBar } from "./volume";
import { WeatherView } from "./weather";
import { WhatsNew } from "./whatsnew";
import { WifiPanel } from "./wifi";
import {
  BLUETOOTH_ID,
  EXIT_GLYPH,
  EXIT_ID,
  FAV_ID,
  GAMES_GLYPH,
  GAMES_ID,
  GEAR_GLYPH,
  ART_GLYPH,
  ART_ID,
  HOME_ID,
  INPUT_GLYPH,
  INPUT_ID,
  OWN_TILES,
  PLACES_GLYPH,
  PLACES_ID,
  QUIZ_GLYPH,
  QUIZ_ID,
  TRIPS_GLYPH,
  TRIPS_ID,
  POWER_GLYPH,
  POWER_ID,
  RADIO_GLYPH,
  RADIO_ID,
  SETTINGS_ID,
  SLEEP_ID,
  SOUND_ID,
  SPEED_ID,
  STATUS_GLYPH,
  STATUS_ID,
  STOP_GLYPH,
  STOP_ID,
  VIDEOS_GLYPH,
  VIDEOS_ID,
  WEATHER_ID,
  WIFI_GLYPH,
  WIFI_ID,
  homeGlyph,
} from "./tiles";

reportAlive();

const $ = <T extends HTMLElement>(sel: string) => document.querySelector<T>(sel)!;

const fit = $("#fit");
const stage = $("#stage");
/** The part of the stage drawn over the photo; see pixel_shift. */
const ui = $("#ui");
const exitHint = $("#exit-hint");
const railHint = $("#rail-hint");
const overlay = $("#overlay");
const conn = $("#conn");
const stale = $("#stale");
const debug = $("#debug");

// What the dashboard shows beside the clock is set once, in the settings:
// whether a photo has its caption, and how much of the weather there is.
// Down on the remote steps on from that to "everything" and to "only the
// photo", unless the settings lock it.
const VIEWS = ["set", "all", "photo"] as const;
let view = 0;
let captionSet = "off";
let weatherSet = "short";
let viewLocked = false;

function applyView() {
  const now = VIEWS[view];
  overlay.dataset.view = now;
  // A painting always has its label, as on a museum's wall.
  overlay.dataset.caption = now === "all" || artOn ? "on" : captionSet;
  overlay.dataset.weather = now === "all" ? "full" : weatherSet;
}
/** True once Up or Down was used: the configured start view no longer applies. */
let viewChosen = false;

const clock = new Clock($("#hm"), $("#ampm"), $("#date"));
clock.start();

const input = new Input();
input.attachKeyboard();

fitToScreen(fit);
keepAwake();
document.addEventListener("visibilitychange", () => updateSuspended());

// --- Exit to desktop ----------------------------------------------------------

input.onHold((holding) => exitHint.classList.toggle("visible", holding));

async function exitToDesktop() {
  exitHint.textContent = "Returning to the desktop…";
  exitHint.classList.add("visible");
  try {
    const res = await fetch("/api/kiosk", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: "stop" }),
    });
    if (!res.ok) throw new Error(await res.text());
  } catch {
    // Not running as the kiosk unit (e.g. a normal browser tab): try closing the tab.
    window.close();
    exitHint.textContent = "Press Alt+F4 to return to the desktop";
    window.setTimeout(() => {
      exitHint.classList.remove("visible");
      exitHint.textContent = "Keep holding to exit to the desktop…";
    }, 4000);
  }
}

// --- Background photos --------------------------------------------------------

const captionEl = $("#caption");
let dateLocale: string | undefined;
let lanAddress = "";

function showCaption(item: PhotoItem | null) {
  // Under a painting: what it is called, who made it and when, and whose it is.
  captionEl.textContent = artOn && item ? [item.name, item.folder, item.place, artNow?.source].filter(Boolean).join("  ·  ") : captionOf(item, dateLocale);
}

// Art mode: paintings from a museum in place of the photos.
let artOffered = false;
let artOn = false;
let artNow: PhotosStatus | null = null;

/** What the slideshow is told about its pictures: the photos' state, or the paintings'. */
function feedSlideshow() {
  const st = artOn ? artNow : photosNow;
  // A painting stays as long as a photo does.
  if (st) slideshow.setStatus(artOn && photosNow ? { ...st, intervalSeconds: photosNow.intervalSeconds } : st);
}

function onArt(st: PhotosStatus) {
  artNow = st;
  if (!artOn) return;
  feedSlideshow();
  if (st.error && st.count === 0 && !st.indexing) toast(t("The paintings cannot be fetched right now: {why}", { why: st.error }), 10000);
}

/** Switches between the photos and the paintings. */
async function setArt(on: boolean) {
  try {
    const res = await postJSON("/api/art/mode", { on });
    if (!res.ok) return sayRefusal(res);
    applyConfig((await res.json()) as ClientConfig);
    toast(on ? "Paintings from the Art Institute of Chicago. ▲ brings your photos back." : "Your photos again.", 6000);
  } catch {
    toast(UNREACHABLE, 6000);
  }
}

// Without a photo on screen, the background is a slow play of colours.
const ambient = new Ambient($<HTMLCanvasElement>("#ambient"), $("#grain"));
let photoShowing = false;
function updateAmbient() {
  ambient.setActive(!photoShowing && appsStatus.running === "" && document.visibilityState === "visible" && !tvAsleep && !night.now);
}

let shownPhoto: PhotoItem | null = null;
const slideshow = new Slideshow($("#backdrop"), (item) => {
  photoShowing = item !== null;
  shownPhoto = item;
  updateAmbient();
  showCaption(item);
  photoBar.setItems(photoItems()); // what can be done follows the photo
});
let photosHintShown = false;

let photosNow: PhotosStatus | null = null;

function onPhotos(st: PhotosStatus) {
  const before = photosNow;
  photosNow = st;
  // The "only favourites" tile comes and goes with the favourites.
  if (before?.favourites !== st.favourites || before?.onlyFavourites !== st.onlyFavourites || before?.new !== st.new || before?.intervalSeconds !== st.intervalSeconds)
    photoBar.setItems(photoItems());
  // The Places tile is there once a photo says where it was taken.
  if ((before?.located ?? 0) > 0 !== (st.located ?? 0) > 0 || before?.trips !== st.trips || before?.quiz !== st.quiz) rail.setItems(railItems());
  photos.tellAboutNew();
  feedSlideshow();
  // First run: a source is configured but no folders have been chosen yet.
  if (st.enabled && !st.indexing && st.count === 0 && st.selection.folders.length === 0 && !st.error && !photosHintShown) {
    photosHintShown = true;
    toast("Choose the photos for the background: press OK, open Settings, then Background photos.", 15000);
  }
}

let tvAsleep = false;

function updateSuspended() {
  // Do not load photos while an app covers the dashboard, the page is hidden,
  // the TV has been switched off for the night, or night mode shows only a clock.
  // And while a photo is being read about, or something is being done with it, it stays.
  slideshow.setSuspended(appsStatus.running !== "" || document.visibilityState !== "visible" || tvAsleep || night.now || photoInfo.isOpen || photoBar.isOpen || gridPage.isOpen || places.isOpen || quiz.isOpen);
  // An app brings its own sound, and a TV that is off needs none.
  if (appsStatus.running !== "" || tvAsleep) radio.stop();
  updateAmbient();
}

// --- Night mode -----------------------------------------------------------------

const night: Night = new Night($("#dim"), (now) => {
  stage.classList.toggle("night", now);
  if (now) {
    rail.close();
    weather.close();
  }
  updateSuspended();
});

// --- Weather ------------------------------------------------------------------

const weatherEl = $("#weather");
const weather: WeatherView = new WeatherView(weatherEl, $("#weather-panel"), () => syncPanels());
// With a mouse or touch screen, clicking the summary opens the details too.
weatherEl.addEventListener("click", (e) => {
  e.stopPropagation();
  weather.open();
});

// --- Help ---------------------------------------------------------------------

const help = new Help($("#help"), () => syncPanels());

// --- Wi-Fi for visitors ---------------------------------------------------------

let guestWifi = "";
/** Only the dashboard is served (a container): there is no desktop behind it, and no machine to switch off. */
let standalone = false;
const wifiPanel: WifiPanel = new WifiPanel($("#wifi-panel"), () => syncPanels());

// --- About this photo, and favourites -------------------------------------------

const photoInfo: PhotoInfo = new PhotoInfo($("#photo-info"), () => {
  syncPanels();
  updateSuspended(); // the photo stays while it is being read about
});

// The bar that Up opens: what can be done with the photo on screen.
const photoBar: PhotoBar = new PhotoBar(
  $("#photo-bar"),
  (item) => {
    // Marking a favourite and setting the sleep timer leave the bar open, to
    // press again at once; everything else leads somewhere.
    if (item.id !== FAV_ID && item.id !== SLEEP_ID && item.id !== SPEED_ID) photoBar.close();
    void activateRailItem(item);
  },
  (open) => {
    stage.classList.toggle("photo-bar-open", open);
    updateSuspended();
  },
);

const photos: PhotoActions = new PhotoActions({
  slideshow,
  // Made further down; asked for when a button is pressed.
  get menu() {
    return menu;
  },
  photoInfo,
  shown: () => shownPhoto,
  status: () => photosNow,
  onStatus: (st) => onPhotos(st),
  redraw: (photo) => {
    if (shownPhoto === photo) showCaption(photo);
    photoBar.setItems(photoItems());
  },
  closeRow: () => rail.close(),
  locale: () => dateLocale,
  quiet: () => night.now || appsStatus.running !== "" || document.visibilityState !== "visible",
});

/** Whatever covers the whole screen hides the clock, the weather and the rest. */
function syncPanels() {
  stage.classList.toggle("panel-open", weather.isOpen || help.isOpen || statusPanel.isOpen || wifiPanel.isOpen || photoInfo.isOpen || whatsNew.isOpen || gridPage.isOpen || places.isOpen || quiz.isOpen);
}

// --- Short lists: Bluetooth, restarting ----------------------------------------------

const menu: Menu = new Menu($("#menu"), (open) => {
  if (!open) bluetooth.closed();
});
const bluetooth: QuickBluetooth = new QuickBluetooth(
  menu,
  () => {
    rail.setItems(railItems());
    photoBar.setItems(photoItems());
  },
  (category) => openSettings(category),
);

// --- Status: how PiDash is doing, readable from the sofa -----------------------------

let lastSystem: SystemState | null = null;
const statusPanel: StatusPanel = new StatusPanel(
  $("#status-panel"),
  () => lastSystem,
  () => syncPanels(),
);

function openHelp() {
  const url = lanAddress ? `http://${lanAddress}:${location.port || "80"}/remote` : "";
  help.open(input.keyMap, url);
}

// --- Settings -------------------------------------------------------------------
// The settings page opens right on the TV, where it is driven with the remote.

/** Opens the settings on the TV, at a category if one is named. */
function openSettings(category = "") {
  location.href = "/settings?from=tv" + (category ? "#" + category : "");
}

// --- Panel rail and apps ------------------------------------------------------

// Smart-home tiles: lights, plugs, scenes and values from Home Assistant.
let homeEntities: HomeEntity[] = [];

// --- Music and radio ------------------------------------------------------------------

const playing = new NowPlaying(ui);
const radio: Radio = new Radio(ui, () => rail.setItems(railItems()));

// Where the sound goes, and the sleep timer: two tiles that act when pressed.
const sound: SoundTile = new SoundTile(() => rail.setItems(railItems()));
const sleep: SleepTimer = new SleepTimer(() => rail.setItems(railItems()));

let appsStatus: AppsStatus = { apps: [], running: "", consoleReady: true };
let appsLoaded = false;

const LAST_APP = "pidash-last-app";
const rail = new Rail(
  $("#rail"),
  $("#rail-strip"),
  activateRailItem,
  (open) => {
    stage.classList.toggle("rail-open", open);
    ambient.setLifted(open);
    railActivity();
    if (open) update.tell();
    // A controller may have connected since the row was last looked at.
    if (open) void bluetooth.look();
  },
);

try {
  rail.setPreferred(localStorage.getItem(LAST_APP) ?? "");
} catch {
  /* no storage: the row opens on its first tile */
}

// A new version that waits to be installed is pointed out on the Settings tile.
const update: UpdateNotice = new UpdateNotice(() => rail.setItems(railItems()));
void bluetooth.look();

// Each tile glows in the main colour of its icon.
rail.tintOf = async (item) => {
  if (!item.icon) return null;
  const palette = await iconPalette(item.icon);
  return palette?.[0] ? palette[0].map((v) => Math.round(v)).join(", ") : null;
};

// What the buttons do in the app row, under the tiles.
function showRailHint() {
  const n = appsStatus.apps.length;
  railHint.textContent = rail.isMoving
    ? "◀ ▶ move  ·  OK put down  ·  Back leave it where it was"
    : n > 1
      ? "◀ ▶ choose  ·  OK open  ·  ▲ move, remove or hide  ·  Back close"
      : n === 1
        ? "◀ ▶ choose  ·  OK open  ·  Back close"
        : "Add apps under Settings  ·  Back close";
}
rail.onMoving = showRailHint;

// --- Pages that fill the screen: the games, the videos, the places -----------------------

const gridPage: Grid = new Grid(
  $("#grid-page"),
  (open) => {
    if (!open) gridShows = "";
    syncPanels();
    updateSuspended();
  },
  // Under a video or a game the page waits, to be there when it ends.
  () => appsStatus.running !== "",
);
/** Whose page the grid is showing. */
let gridShows: "" | "games" | "videos" = "";

// The games, gathered on one page: their covers, the one played last first.
let gamesOnPage = false;

function games() {
  return appsStatus.apps.filter((a) => a.game);
}

function gameCards(): Card[] {
  return games().map((a) => {
    const running = appsStatus.running === a.id;
    return { id: a.id, title: a.name, image: a.hasIcon ? `/api/apps/${encodeURIComponent(a.id)}/icon` : undefined, on: running, sub: running ? "Running · OK to stop" : undefined };
  });
}

function openGames() {
  gridShows = "games";
  gridPage.open({
    title: t("Games"),
    cards: gameCards(),
    shape: "covers",
    selected: appsStatus.running || undefined,
    empty: "There are no games yet. Pair Moonlight with the PC, or mark games as favourites in RetroArch.",
    foot: "◀ ▶ ▲ ▼ choose  ·  OK play  ·  Back close",
    onChoose: (card) => void activateRailItem({ id: card.id, label: card.title }),
  });
}

// Other devices on the TV, each a tile.
let tvInputs: string[] = [];

// With a game controller in the hand, the lines about the buttons name its
// buttons; the bottom one chooses and the right one goes back.
const PAD_BUTTONS: Record<string, { select: string; back: string }> = {
  playstation: { select: "✕", back: "○" },
  nintendo: { select: "Ⓑ", back: "Ⓐ" },
  xbox: { select: "Ⓐ", back: "Ⓑ" },
  generic: { select: "Ⓐ", back: "Ⓑ" },
};

// The videos of a folder or of the NAS.
let videosSetUp = false;
const videos = new Videos(gridPage, () => (gridShows = "videos"));

// A list beside a map: where the photos were taken, or the trips they tell of.
const places: Places = new Places($("#places"), () => {
  syncPanels();
  updateSuspended();
});

/** Shows the photos of a place or a trip next, and closes the page they were chosen on. */
async function showPhotosOf(query: string, name: string, limit: number) {
  try {
    const data: { items: PhotoItem[] } = await (await fetch(`/api/photos/playlist?${query}&limit=${limit}`)).json();
    if (data.items.length === 0) return toast(t("There are no other photos from {when}.", { when: name }), 5000);
    places.close();
    // They are photos: the paintings make way for them.
    if (artOn) await setArt(false);
    slideshow.queue(data.items);
    slideshow.next();
    toast(t(data.items.length === 1 ? "The next picture is from {when}." : "The next {n} pictures are from {when}.", { when: name, n: data.items.length }), 6000);
  } catch {
    toast(UNREACHABLE, 6000);
  }
}
const allPlaces = placesList((place) => void showPhotosOf(`place=${encodeURIComponent(place.id)}`, place.name, 200));
const allTrips = tripsList(
  (trip) => void showPhotosOf(`trip=${encodeURIComponent(trip.id)}`, trip.name, 500),
  () => dateLocale,
);

// The rain radar, opened from the forecast with Right.
const radar: Radar = new Radar($("#radar"), () => syncPanels());

// A quiz about the photos: which year, and where.
const quiz: Quiz = new Quiz($("#quiz"), () => {
  syncPanels();
  updateSuspended();
});

// --- What can be done with a tile: Up in the app row -----------------------------------

// The tiles of PiDash's own that can be hidden, by the name the settings know them by.
let hiddenTiles = new Set<string>();

rail.onOptions = (item) => {
  type Choice = { label: string; hint?: string; run: () => void };
  const choices: Choice[] = [];
  const app = appsStatus.apps.find((a) => a.id === item.id && !a.game);
  if (app) {
    if (rail.canMove) choices.push({ label: t("Move"), hint: t("Carry it with ◀ ▶ and put it down with OK."), run: () => rail.pickUp() });
    choices.push({
      label: t("Remove from the app row"),
      hint: t("Nothing is uninstalled. It can be added again under Settings."),
      run: () => {
        void postJSON("/api/apps/remove", { id: app.id })
          .then(async (res) => (res.ok ? (onApps((await res.json()) as AppsStatus), toast(t("{name} is no longer in the app row.", { name: app.name }), 5000)) : sayRefusal(res)))
          .catch(() => toast(UNREACHABLE, 6000));
      },
    });
    choices.push({ label: t("Rename or change it"), hint: t("Opens the settings."), run: () => openSettings("apps") });
  } else if (OWN_TILES[item.id]) {
    choices.push({
      label: t("Hide this tile"),
      hint: t("It comes back under Settings, Apps and radio."),
      run: () => {
        void postJSON("/api/tiles/hidden", { tile: OWN_TILES[item.id], hidden: true })
          .then(async (res) => (res.ok ? (applyConfig((await res.json()) as ClientConfig), toast(t("Hidden. It comes back under Settings, Apps and radio."), 6000)) : sayRefusal(res)))
          .catch(() => toast(UNREACHABLE, 6000));
      },
    });
  }
  if (choices.length === 0) return play("edge");
  play("open");
  menu.open(item.label, choices, 0, (i) => choices[i]?.run());
};

const whatsNew: WhatsNew = new WhatsNew($("#whatsnew"), () => syncPanels());
const volume = new VolumeBar($("#volume"));

// An app was put down in a new place: remember the order on the Pi.
rail.onReorder = (ids) => {
  // Shown at once; the Pi confirms with the same order a moment later.
  const byId = new Map(appsStatus.apps.map((a) => [a.id, a]));
  const apps = ids.map((id) => byId.get(id)).filter((a): a is NonNullable<typeof a> => !!a);
  if (apps.length === appsStatus.apps.length) appsStatus = { ...appsStatus, apps };
  rail.setItems(railItems());
  void (async () => {
    let problem = "";
    try {
      const res = await postJSON("/api/apps/order", { ids });
      if (res.status === 401 || res.status === 429) problem = "The settings are locked, so apps can only be moved there: Settings, Apps.";
      else if (!res.ok) problem = (await res.text()).trim() || "The new order could not be saved.";
    } catch {
      problem = "The new order could not be saved.";
    }
    if (!problem) return;
    toast(problem, 8000);
    // Back to the order the Pi has.
    try {
      onApps((await (await fetch("/api/apps")).json()) as AppsStatus);
    } catch {
      /* the next update from the Pi brings it */
    }
  })();
};

// The background takes on the colours of the selected app's icon, and returns
// to its own when the row closes or a tile without an icon is selected.
let paletteRequest = 0;
rail.onFocus = (item) => {
  const request = ++paletteRequest;
  if (!item?.icon) {
    ambient.setPalette(null);
    return;
  }
  void iconPalette(item.icon).then((palette) => {
    if (request === paletteRequest) ambient.setPalette(palette);
  });
};

// Left alone, the app row closes again, so the TV goes back to being a clock
// and a picture frame instead of showing a menu all evening.
let railTimeoutMs = 30_000;
let railTimer = 0;
function railActivity() {
  clearTimeout(railTimer);
  if (!rail.isOpen || railTimeoutMs <= 0) return;
  railTimer = window.setTimeout(() => {
    // Not under an app that is starting or running: the row is not visible
    // then, and should be where it was when the app closes.
    if (appsStatus.running !== "" || document.visibilityState !== "visible") railActivity();
    else rail.close();
  }, railTimeoutMs);
}

/** The buttons of the bar that Up opens: what can be done with the photo, then Bluetooth and the sleep timer. */
function photoItems(): RailItem[] {
  // In art mode there is nothing to mark or hide: the way back, and how long a painting stays.
  const items = artOn ? photos.items().filter((i) => i.id === SPEED_ID) : photos.items();
  if (artOn) items.unshift({ id: ART_ID, label: "Back to your photos", glyph: ART_GLYPH, running: true });
  else if (artOffered) items.push({ id: ART_ID, label: "Art instead of photos", glyph: ART_GLYPH, sub: t("paintings from a museum") });
  // Not about the photo, but wanted just as quickly: headphones on, a controller off.
  if (bluetooth.available) items.push(bluetooth.item());
  if (sleep.available) items.push(sleep.item());
  return items;
}

function railItems(): RailItem[] {
  const items: RailItem[] = [];
  const w = weather.tile;
  if (w) items.push({ id: WEATHER_ID, label: "Weather", glyph: w.glyph, sub: w.sub });
  for (const a of appsStatus.apps) {
    // The games are behind one tile, on a page of their own.
    if (a.game && gamesOnPage) continue;
    const running = appsStatus.running === a.id;
    // A music app that was left keeps playing behind the dashboard.
    const behind = appsStatus.background === a.id;
    items.push({
      id: a.id,
      label: a.name,
      icon: a.hasIcon ? `/api/apps/${encodeURIComponent(a.id)}/icon` : undefined,
      // Games come as Moonlight lists them; only the apps are arranged by hand.
      movable: !a.game,
      running: running || behind,
      sub: running ? "Running · OK to stop" : behind ? "Playing · OK to open" : a.game ? "Game" : "",
    });
  }
  if (gamesOnPage && games().length > 0) {
    const n = games().length;
    const playing = games().find((g) => g.id === appsStatus.running);
    items.push({ id: GAMES_ID, label: "Games", glyph: GAMES_GLYPH, running: !!playing, sub: playing ? playing.name : t(n === 1 ? "1 game" : "{n} games", { n }) });
  }
  if (videosSetUp) items.push({ id: VIDEOS_ID, label: "Videos", glyph: VIDEOS_GLYPH, running: appsStatus.running === "pidash-video" });
  // Other devices on the TV: OK has the TV show them.
  if (sleep.available) tvInputs.forEach((name, i) => items.push({ id: INPUT_ID + i, label: name, glyph: INPUT_GLYPH, sub: t("on the TV") }));
  radio.stations.forEach((st, i) => {
    const playing = radio.playing === i;
    items.push({ id: RADIO_ID + i, label: st.name, glyph: RADIO_GLYPH, running: playing, sub: playing ? "Playing · OK to stop" : "Radio" });
  });
  if (sleep.available) items.push(sleep.item());
  for (const e of homeEntities) {
    items.push({ id: HOME_ID + e.id, label: e.name, sub: e.text, glyph: homeGlyph(e), running: e.on });
  }
  if (bluetooth.available) items.push(bluetooth.item());
  const soundTile = sound.item();
  if (soundTile) items.push(soundTile);
  const behind = appsStatus.apps.find((a) => a.id === appsStatus.background);
  if (behind) items.push({ id: STOP_ID, label: t("Stop {name}", { name: behind.name }), glyph: STOP_GLYPH });
  if ((photosNow?.located ?? 0) > 0) items.push({ id: PLACES_ID, label: "Places", glyph: PLACES_GLYPH, sub: t("where the photos were taken") });
  const trips = photosNow?.trips ?? 0;
  if (trips > 0) items.push({ id: TRIPS_ID, label: "Trips", glyph: TRIPS_GLYPH, sub: t(trips === 1 ? "1 trip" : "{n} trips", { n: trips }) });
  if (photosNow?.quiz) items.push({ id: QUIZ_ID, label: "Photo quiz", glyph: QUIZ_GLYPH, sub: t("which year, and where?") });
  if (guestWifi) items.push({ id: WIFI_ID, label: "Guest Wi-Fi", glyph: WIFI_GLYPH, sub: guestWifi });
  items.push({ id: STATUS_ID, label: "Status", glyph: STATUS_GLYPH });
  items.push({ id: SETTINGS_ID, label: "Settings", glyph: GEAR_GLYPH, sub: update.version ? "Update available" : undefined, notice: !!update.version });
  if (!standalone) {
    items.push({ id: EXIT_ID, label: "Desktop", glyph: EXIT_GLYPH });
    items.push({ id: POWER_ID, label: "Power", glyph: POWER_GLYPH });
  }
  // Tiles of PiDash's own that were hidden (Up on a tile, or the settings).
  return items.filter((i) => !hiddenTiles.has(OWN_TILES[i.id] ?? ""));
}

function onApps(st: AppsStatus) {
  const previousError = appsStatus.error;
  appsStatus = st;
  rail.setItems(railItems());
  if (gridShows === "games") gridPage.setCards(gameCards());
  updateSuspended();
  showRailHint();
  // Do not replay an old problem when the page (re)connects.
  if (appsLoaded && st.error && st.error !== previousError) toast(st.error, 9000);
}
onApps(appsStatus);

async function activateRailItem(item: RailItem) {
  if (item.id === EXIT_ID) {
    exitToDesktop();
    return;
  }
  if (item.id === WEATHER_ID) {
    rail.close();
    weather.open();
    return;
  }
  if (item.id === SETTINGS_ID) {
    openSettings();
    return;
  }
  if (item.id === SOUND_ID) {
    void sound.next();
    return;
  }
  if (item.id === GAMES_ID) {
    rail.close();
    openGames();
    return;
  }
  if (item.id === VIDEOS_ID) {
    rail.close();
    void videos.open();
    return;
  }
  if (item.id === PLACES_ID || item.id === TRIPS_ID) {
    rail.close();
    places.open(item.id === TRIPS_ID ? allTrips : allPlaces);
    return;
  }
  if (item.id.startsWith(INPUT_ID)) {
    const index = Number(item.id.slice(INPUT_ID.length));
    rail.close();
    toast(t("Switching the TV to {name}…", { name: item.label }), 6000);
    try {
      const res = await postJSON("/api/tv/input", { index });
      if (!res.ok) await sayRefusal(res);
    } catch {
      toast(UNREACHABLE, 6000);
    }
    return;
  }
  if (item.id === ART_ID) {
    await setArt(!artOn);
    return;
  }
  if (item.id === QUIZ_ID) {
    rail.close();
    quiz.open();
    return;
  }
  if (item.id === POWER_ID) {
    rail.close();
    openPowerMenu(menu);
    return;
  }
  if (item.id === BLUETOOTH_ID) {
    rail.close();
    bluetooth.open();
    return;
  }
  if (item.id === STOP_ID) {
    // Ends the music app that plays behind the dashboard.
    try {
      onApps((await (await postJSON("/api/apps/stop", {})).json()) as AppsStatus);
    } catch {
      toast(UNREACHABLE, 5000);
    }
    return;
  }
  if (item.id === WIFI_ID) {
    rail.close();
    wifiPanel.open();
    return;
  }
  if (item.id === STATUS_ID) {
    rail.close();
    statusPanel.open();
    return;
  }
  if (item.id.startsWith(RADIO_ID)) {
    radio.toggle(Number(item.id.slice(RADIO_ID.length)));
    return;
  }
  if (item.id === SLEEP_ID) {
    await sleep.next();
    return;
  }
  if (item.id.startsWith(HOME_ID)) {
    const e = homeEntities.find((x) => HOME_ID + x.id === item.id);
    if (!e?.actionable) {
      toast(e?.available === false ? t("{name} is not available right now.", { name: item.label }) : t("{name} only shows a value.", { name: item.label }), 3000);
      return;
    }
    try {
      const res = await postJSON("/api/home/activate", { id: e.id });
      if (!res.ok) toast((await res.text()).trim(), 6000);
    } catch {
      toast(UNREACHABLE, 6000);
    }
    return;
  }
  if (await photos.act(item)) return;
  const stopping = appsStatus.running === item.id;
  toast(t(stopping ? "Closing {name}…" : "Starting {name}…", { name: item.label }));
  try {
    const res = stopping
      ? await postJSON("/api/apps/stop", {})
      : await postJSON("/api/apps/start", { id: item.id });
    // Launch problems arrive as an "apps" event with an error message, which
    // is shown as a toast; only report what that channel cannot.
    if (!res.ok && res.status >= 500 && !appsStatus.error) {
      // What the Pi answers starts in lower case, as its log does.
      toast(await said(res), 12000);
    }
    if (res.ok && !stopping) {
      rail.close();
      // The row opens on this app next time; for a game, on the Games tile.
      const tile = gamesOnPage && games().some((g) => g.id === item.id) ? GAMES_ID : item.id;
      if (gridShows === "games") gridPage.close();
      rail.setPreferred(tile);
      try {
        localStorage.setItem(LAST_APP, tile);
      } catch {
        /* then only until the page is loaded again */
      }
    }
  } catch {
    toast(UNREACHABLE, 6000);
  }
}

// --- Config -----------------------------------------------------------------

// While the Pi is hot the page does without everything that moves all the
// time: the flowing background and the movement of photos.
let hot = false;
let stillBackground = false;
/** Power saver: photos stand still, as while the Pi is hot. */
let saving = false;
// What each performance setting means for the moving background: milliseconds
// between pictures while it drifts by itself, and the finest picture it may
// use (0: as fine as the screen calls for).
const PACES: Record<string, [number, number]> = {
  smooth: [33, 0],
  balanced: [83, 0],
  saver: [200, 3],
};
// What a button sets in motion (the background lifting as the app row opens,
// swaying as the highlight moves) is drawn on every picture the screen shows,
// whatever the setting: it lasts a second, and is part of how the menu feels.
const REACTING_MS = 16;

let runningVersion = "";
let shownNotice = "";

function applyConfig(cfg: ClientConfig) {
  // Nothing is set up yet: the few questions of a first start come before
  // the dashboard. Once per session of the browser, so that a way out of
  // them never leads straight back in.
  if (cfg.setup) {
    let asked = false;
    try {
      asked = sessionStorage.getItem("pidash-setup") === "1";
      sessionStorage.setItem("pidash-setup", "1");
    } catch {
      /* asked each time, then */
    }
    if (!asked) {
      location.href = "/setup?from=tv";
      return;
    }
  }
  // After an update the backend restarts with a new version: load the new
  // dashboard too, so the TV never runs an old page against a new backend.
  // The same goes for saved settings, which the backend applies by restarting.
  const run = `${cfg.version}/${cfg.started}`;
  if (runningVersion && run !== runningVersion) {
    location.reload();
    return;
  }
  runningVersion = run;
  // Start-up problems, such as a settings mistake or a port that was taken.
  const notice = (cfg.notices ?? []).join("  ·  ");
  if (notice && notice !== shownNotice) toast(notice, 15000);
  shownNotice = notice;
  const d = cfg.display;
  document.documentElement.style.setProperty("--safe", `${(d.safe_area * 100).toFixed(2)}%`);
  // Dates follow the language unless a locale is set for them.
  setLanguage(d.language || "en");
  const locale = d.locale || (d.language && d.language !== "en" ? d.language : "");
  clock.configure(d.clock_24h, locale);
  setSounds(d.sounds ?? true);
  if ((cfg.guest_wifi ?? "") !== guestWifi || !!cfg.standalone !== standalone) {
    guestWifi = cfg.guest_wifi ?? "";
    standalone = !!cfg.standalone;
    rail.setItems(railItems());
  }
  stillBackground = d.background === "still";
  ambient.setStill(stillBackground || hot);
  let [calmMs, floor] = PACES[d.performance ?? ""] ?? PACES.balanced;
  if ((d.background_fps ?? 0) > 0) calmMs = 1000 / (d.background_fps ?? 1);
  ambient.setPace(calmMs, REACTING_MS, floor);
  saving = d.performance === "saver";
  slideshow.setCalm(hot || saving);
  // Moving photos: redrawn twice as often when smoothness is what is wanted.
  slideshow.setMotionRate(d.performance === "smooth" ? 30 : 15);
  stage.dataset.performance = d.performance || "balanced";
  stage.dataset.tiles = d.tiles || "glow";
  whatsNew.tell(cfg.version, cfg.whats_new ?? []);
  if (gamesOnPage !== !!cfg.games_page || videosSetUp !== !!cfg.videos || JSON.stringify(tvInputs) !== JSON.stringify(cfg.tv_inputs ?? [])) {
    gamesOnPage = !!cfg.games_page;
    videosSetUp = !!cfg.videos;
    tvInputs = cfg.tv_inputs ?? [];
    rail.setItems(railItems());
  }
  const hiddenNow = new Set(d.hidden_tiles ?? []);
  if ([...hiddenNow].sort().join() !== [...hiddenTiles].sort().join()) {
    hiddenTiles = hiddenNow;
    rail.setItems(railItems());
  }
  stage.dataset.clock = d.clock_position || "bottom-left";
  stage.dataset.clockSize = d.clock_size || "large";
  stage.dataset.clockStyle = d.clock_style || "bold";
  // Settings from before these two existed said it all with one word.
  captionSet = d.caption || (d.view === "details" ? "on" : "off");
  weatherSet = d.weather_detail || (d.view === "details" ? "full" : "short");
  viewLocked = !!d.lock_view;
  if (viewLocked) view = 0;
  else if (!viewChosen && d.view === "photo") view = 2;
  applyView();
  weather.configure(d.clock_24h, locale);
  photoInfo.configure(d.clock_24h, locale || undefined);
  radar.configure(d.clock_24h, locale || undefined);
  if (artOffered !== !!cfg.art || artOn !== !!cfg.art_on) {
    artOffered = !!cfg.art;
    artOn = !!cfg.art_on;
    slideshow.setSource(artOn ? "/api/art/playlist" : "/api/photos/playlist", artOn ? "/art/" : "/photos/");
    feedSlideshow();
    photoBar.setItems(photoItems());
  }
  weather.onRadar = cfg.radar ? (place) => radar.open(place) : null;
  dateLocale = locale || undefined;
  agenda.configure(d.clock_24h, locale || undefined);
  departures.configure(d.clock_24h, locale || undefined);
  input.setKeyMap(cfg.keys);
  input.setDebugElement(d.debug_keys ? debug : null);
  if (!d.debug_keys) debug.hidden = true;
  radio.setStations(cfg.radio ?? []);
  startPixelShift(stage, d.pixel_shift);
  night.configure(d.night_start ?? "", d.night_end ?? "", { light: 0.22, strong: 0.42 }[d.evening_dim ?? ""] ?? 0);
  railTimeoutMs = (d.app_row_timeout ?? 30) * 1000;
  railActivity();
  sysBadge.hidden = !d.debug_keys;
  stage.dataset.debug = String(!!d.debug_keys);
}

// --- Calendar ---------------------------------------------------------------------

const agenda = new Agenda($("#agenda"));
const departures = new Departures($("#departures"));
window.setInterval(() => weather.tick(), 30_000);

// --- State ------------------------------------------------------------------

const staleProviders = new Set<string>();

const sysBadge = document.createElement("div");
sysBadge.id = "sys";
sysBadge.hidden = true;
ui.appendChild(sysBadge);

function onState(entry: StateEntry) {
  if (entry.stale) staleProviders.add(entry.provider);
  else staleProviders.delete(entry.provider);
  stale.hidden = staleProviders.size === 0;

  switch (entry.provider) {
    case "system":
      renderSystem(entry.data as SystemState | null);
      break;
    case "home":
      homeEntities = (entry.data as { entities: HomeEntity[] } | null)?.entities ?? homeEntities;
      rail.setItems(railItems());
      break;
    case "calendar":
      agenda.setEvents((entry.data as CalendarState | null)?.events ?? []);
      break;
    case "departures":
      departures.update(entry.data as DeparturesState | null, entry.stale);
      break;
    case "weather":
      weather.update(entry.data as WeatherState | null, entry.stale);
      night.setSky(entry.data as WeatherState | null);
      rail.setItems(railItems()); // the Weather tile shows the current conditions
      break;
  }
}

function renderSystem(s: SystemState | null) {
  if (!s) return;
  lastSystem = s;
  statusPanel.refresh();
  if (!!s.hot !== hot) {
    hot = !!s.hot;
    ambient.setStill(stillBackground || hot);
    slideshow.setCalm(hot || saving);
    stage.dataset.hot = String(hot);
  }
  lanAddress = s.addresses[0] ?? "";
  const up = formatUptime(s.uptimeSeconds);
  const temp = s.cpuTempC == null ? "" : ` · ${s.cpuTempC.toFixed(0)}°C`;
  sysBadge.textContent =
    `${s.hostname} · ${s.addresses.join(", ") || "no network"}\n` +
    `load ${s.load1.toFixed(2)} · ${s.memUsedMB}/${s.memTotalMB} MB${temp} · up ${up}`;
  sysBadge.style.whiteSpace = "pre";
}

function formatUptime(sec: number): string {
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  return d > 0 ? `${d}d ${h}h` : h > 0 ? `${h}h ${m}m` : `${m}m`;
}

// --- Actions ----------------------------------------------------------------

// The background answers to the remote: it flows the way an arrow points and
// swells on a press, so the screen feels alive under the hand.
function stirAmbient(action: Action) {
  switch (action) {
    case "left":
      ambient.nudge(-1, 0);
      break;
    case "right":
      ambient.nudge(1, 0);
      break;
    case "up":
      ambient.nudge(0, -1);
      break;
    case "down":
      ambient.nudge(0, 1);
      break;
    case "select":
      ambient.pulse(1);
      break;
    default:
      ambient.pulse(0.45);
  }
}

input.on((action, ev) => {
  // A held direction runs along the app row. Everywhere else on the dashboard
  // one press is one step: nobody wants to race through photos.
  const arrow = action === "left" || action === "right" || action === "up" || action === "down";
  if (ev.repeat && !((rail.isOpen || photoBar.isOpen) && (action === "left" || action === "right")) && !((gridPage.isOpen || places.isOpen || quiz.isOpen || radar.isOpen) && arrow && !menu.isOpen)) return;
  // The lines about the buttons name those of whatever was pressed last.
  if (!ev.repeat) setButtonNames(ev.pad ? (PAD_BUTTONS[ev.pad] ?? PAD_BUTTONS.generic) : null);
  // In night mode the first press only brings the dashboard back.
  if (night.wake() && action !== "exit") return;
  railActivity();
  stirAmbient(action);
  if (action === "exit") {
    exitToDesktop();
    return;
  }
  if (whatsNew.isOpen) {
    play("back");
    return whatsNew.close();
  }
  if (menu.handle(action)) return;
  if (gridPage.handle(action)) return;
  if (places.handle(action)) return;
  if (quiz.handle(action)) return;
  if (radar.handle(action)) return;
  if (help.handle(action)) return play("back");
  if (wifiPanel.handle(action)) return play("back");
  if (photoInfo.handle(action)) return;
  if (statusPanel.handle(action)) {
    if (!statusPanel.isOpen) play("back");
    return;
  }
  if (action === "settings") {
    openSettings();
    return;
  }
  if (action === "help") {
    play("open");
    openHelp();
    return;
  }
  if (action === "weather") {
    // A direct shortcut: toggles the details from anywhere.
    if (weather.isOpen) {
      play("back");
      weather.close();
    } else if (weather.hasData) {
      play("open");
      rail.close();
      weather.open();
    } else toast("Weather is not set up. Add a location under [weather] in the settings.", 6000);
    return;
  }
  if (weather.handle(action)) {
    play(weather.isOpen ? "move" : "back");
    return;
  }
  if (rail.handle(action)) return;
  if (photoBar.handle(action)) return;
  // Right after a photo was hidden, OK takes that back; anything else lets it stand.
  if (photos.undo(action)) return play("select");
  switch (action) {
    case "select":
    case "menu":
      play("open");
      rail.open();
      break;
    case "up":
      // What can be done with the photo on screen, and Bluetooth.
      if (photoBar.open()) play("open");
      else play("edge");
      // A controller may have connected since this was last looked at.
      void bluetooth.look();
      break;
    case "down": {
      if (viewLocked) {
        play("edge");
        break;
      }
      viewChosen = true;
      // On from what is set, to everything, to only the photo, and round
      // again; "everything" is left out where that is what is set.
      const everything = captionSet === "on" && weatherSet === "full";
      do view = (view + 1) % VIEWS.length;
      while (VIEWS[view] === "all" && everything);
      play("move");
      applyView();
      break;
    }
    case "home":
      view = 0;
      applyView();
      break;
    case "left":
      play("move");
      slideshow.previous();
      break;
    case "right":
      play("move");
      slideshow.next();
      break;
    case "next":
    case "previous":
      // The next piece of music if there is music; otherwise the next photo.
      if (playing.track?.canSkip && radio.playing < 0) {
        playing.press(action);
        break;
      }
      play("move");
      if (action === "next") slideshow.next();
      else slideshow.previous();
      break;
    case "playpause":
      if (radio.playing >= 0) {
        radio.playPause();
        break;
      }
      if (playing.track) {
        playing.press("playpause");
        break;
      }
      if (slideshow.hasPhotos) {
        const held = slideshow.togglePause();
        // A small mark stays for as long as the photo is held.
        stage.classList.toggle("held", held);
        toast(held ? "Slideshow paused" : "Slideshow resumed", 2000);
      }
      hud.refresh();
      break;
  }
});

// --- Mouse, touchpad and touch screen -------------------------------------------

const hud = new Hud(
  $("#hud"),
  (action) => input.emit(action),
  () => ({ photos: slideshow.hasPhotos, paused: slideshow.isPaused, weather: weather.hasData }),
  railActivity,
  (x, y) => ambient.pointTo(x, y),
);

// A click on the screen itself works like OK: it opens the app row, or closes
// whatever is open.
stage.addEventListener("click", (e) => {
  if (help.isOpen) return help.close();
  if (whatsNew.isOpen) return whatsNew.close();
  if (menu.isOpen) return menu.close();
  if (statusPanel.isOpen) return statusPanel.close();
  if (wifiPanel.isOpen) return wifiPanel.close();
  if (photoInfo.isOpen) return photoInfo.close();
  if (weather.isOpen) return weather.close();
  if ((e.target as Element).closest("#hud")) return;
  if (rail.isOpen) rail.close();
  else rail.open();
});

// Right-click means Back, as in most TV interfaces driven by an air mouse.
window.addEventListener("contextmenu", (e) => {
  e.preventDefault();
  input.emit("back");
});

// The wheel steps through photos, or along the app row while it is open.
let lastWheel = 0;
window.addEventListener(
  "wheel",
  (e) => {
    e.preventDefault();
    if (help.isOpen || weather.isOpen || statusPanel.isOpen || wifiPanel.isOpen || photoInfo.isOpen) return;
    const now = performance.now();
    if (now - lastWheel < (rail.isOpen ? 110 : 450)) return;
    const delta = Math.abs(e.deltaY) >= Math.abs(e.deltaX) ? e.deltaY : e.deltaX;
    if (delta === 0) return;
    lastWheel = now;
    input.emit(delta > 0 ? "right" : "left");
  },
  { passive: false },
);

// --- Connect ----------------------------------------------------------------

connect("/events", {
  onConfig: applyConfig,
  onState,
  onInput: (ev) => input.fromRemote(ev),
  onApps: (st) => {
    onApps(st);
    appsLoaded = true;
  },
  onPhotos,
  onArt,
  // A game was chosen while its PC is asleep: say why nothing happens yet.
  onVolume: (v) => volume.show(v),
  // A controller switched the TV on: like a games console, what comes up is the games.
  onOpen: (what) => {
    if (what.page !== "games" || !gamesOnPage || games().length === 0 || appsStatus.running !== "" || gridShows === "games") return;
    night.wake();
    for (const page of [menu, radar, weather, statusPanel, wifiPanel, photoInfo, places, quiz, whatsNew, help, rail, photoBar]) page.close();
    openGames();
  },
  onWaking: (pc) => toast(t("Waking {name}. This can take a minute.", { name: pc.name }), 60000),
  onPlaying: (track) => playing.update(track),
  onSound: (st) => sound.update(st),
  onBattery: (n) => toast(t("{name} is running low: {n} % battery left.", { name: n.name, n: n.percent }), 12000),
  onTV: (st) => {
    tvAsleep = st.asleep;
    sleep.update(st);
    rail.setItems(railItems());
    photoBar.setItems(photoItems());
    updateSuspended();
  },
  onStatus: (ok) => conn.classList.toggle("ok", ok),
});
