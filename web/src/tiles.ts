// The tiles of the app row and the buttons of the photo's bar that are
// PiDash's own: the names they go by, and their pictures.
import type { HomeEntity } from "./types";

export const svg = (path: string) =>
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">${path}</svg>`;

const STAR = "M12 3.600l2.600 5.300 5.800.800-4.200 4.100 1 5.800-5.200-2.700-5.200 2.700 1-5.800-4.200-4.100 5.800-.800z";
export const WEATHER_ID = "__weather";
export const WIFI_ID = "__wifi";
export const WIFI_GLYPH =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">' +
  '<path d="M3 9.500a13 13 0 0 1 18 0M6 12.800a8.600 8.600 0 0 1 12 0M9 16a4.300 4.300 0 0 1 6 0"/><circle cx="12" cy="19" r="0.900" fill="currentColor"/></svg>';
export const INFO_ID = "__photo-info";
export const INFO_GLYPH =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">' +
  '<path d="M12 21s6.500-6 6.500-11A6.500 6.500 0 0 0 5.500 10c0 5 6.500 11 6.500 11z"/><circle cx="12" cy="10" r="2.300"/></svg>';
export const FAV_ID = "__favourite";
export const FAV_GLYPH = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"><path d="${STAR}"/></svg>`;
export const FAV_ON_GLYPH = `<svg viewBox="0 0 24 24" fill="#ffd257" stroke="#ffd257" stroke-width="1.6" stroke-linejoin="round"><path d="${STAR}"/></svg>`;
export const ONLY_FAV_ID = "__only-favourites";
export const ONLY_FAV_GLYPH =
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">` +
  `<rect x="3" y="5" width="18" height="14" rx="2.500"/><path transform="translate(6.600 6.800) scale(0.450)" d="${STAR}"/></svg>`;
export const POWER_ID = "__power";
export const POWER_GLYPH =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">' +
  '<path d="M12 3.500v8"/><path d="M7 6.800a7.500 7.500 0 1 0 10 0"/></svg>';
export const BLUETOOTH_ID = "__bluetooth";
export const BLUETOOTH_GLYPH =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">' +
  '<path d="M7 7.500l10 9-5 4.500V3l5 4.500-10 9"/></svg>';
export const STOP_ID = "__stop-music";
export const STOP_GLYPH =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">' +
  '<circle cx="12" cy="12" r="9"/><rect x="9" y="9" width="6" height="6" rx="1" fill="currentColor"/></svg>';
export const STATUS_ID = "__status";
export const STATUS_GLYPH =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">' +
  '<path d="M2.8 12.5h4l2.2-6 4 12 2.6-8 1.4 2h4.2"/></svg>';
export const SETTINGS_ID = "__settings";
export const GEAR_GLYPH =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">' +
  '<circle cx="12" cy="12" r="3.2"/><path d="M12 2.8v2.6M12 18.6v2.6M2.8 12h2.6M18.6 12h2.6M5.5 5.5l1.8 1.8M16.7 16.7l1.8 1.8M18.5 5.5l-1.8 1.8M7.3 16.7l-1.8 1.8"/></svg>';
export const HOME_ID = "__home:";
export const MORE_ID = "__more";
export const MORE_GLYPH = svg('<rect x="3" y="6" width="13" height="13" rx="1.500"/><path d="M7 6V4.500A1.500 1.500 0 0 1 8.500 3H19.500A1.500 1.500 0 0 1 21 4.500V15.500A1.500 1.500 0 0 1 19.500 17H16"/>');
export const RADIO_ID = "__radio:";
export const RADIO_GLYPH = svg('<rect x="3" y="9" width="18" height="11" rx="2"/><path d="M7 9l9-5"/><circle cx="8.500" cy="14.500" r="2.500"/><path d="M14 13h4M14 16h4"/>');
export const SLEEP_ID = "__sleep";
export const SOUND_ID = "__sound";
export const SOUND_GLYPH = svg('<path d="M4 9.500h3.500L12 5.500v13l-4.500-4H4z"/><path d="M15.500 9a4.200 4.200 0 0 1 0 6M18 6.500a7.800 7.800 0 0 1 0 11"/>');
export const SLEEP_GLYPH = svg('<path d="M20 14.500A8 8 0 0 1 9.500 4a8 8 0 1 0 10.500 10.500z"/>');
export const NEW_ID = "__new-photos";
export const NEW_GLYPH =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">' +
  '<rect x="3" y="6" width="14" height="13" rx="2"/><path d="M6 16l3.500-4 3 3 1.500-1.500L17 16"/><path d="M19.500 2.500v5M17 5h5"/></svg>';
export const HIDE_ID = "__hide:";
export const HIDE_GLYPH =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">' +
  '<path d="M3 3l18 18M10.6 6.2A9.8 9.8 0 0 1 12 6c5 0 8.5 4.5 9.5 6-.4.7-1.300 1.900-2.600 3.100M6.300 7.700C4.300 9 3 10.900 2.500 12c1 1.500 4.500 6 9.500 6 1.300 0 2.500-.300 3.600-.800M9.900 9.900a3 3 0 0 0 4.200 4.200"/></svg>';
export const EXIT_ID = "__desktop";
export const EXIT_GLYPH =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">' +
  '<rect x="3" y="4" width="18" height="12" rx="1.5"/><path d="M8 20h8M12 16v4"/></svg>';
export const GAMES_ID = "__games";
export const GAMES_GLYPH = svg('<path d="M7.500 7h9a5 5 0 0 1 4.900 4l.900 4.500a2.600 2.600 0 0 1-4.500 2.200L16 16H8l-1.800 1.700a2.600 2.600 0 0 1-4.500-2.200L2.600 11a5 5 0 0 1 4.900-4z"/><path d="M7 10.500v3M5.500 12h3M15.500 11h.010M17.500 13h.010"/>');
export const VIDEOS_ID = "__videos";
export const VIDEOS_GLYPH = svg('<rect x="3" y="5" width="18" height="14" rx="2.500"/><path d="M10 9.200v5.600l4.800-2.800z"/>');
export const FOLDER_GLYPH = svg('<path d="M3 7.500A1.500 1.500 0 0 1 4.500 6h4.300l2 2.200h8.700A1.500 1.500 0 0 1 21 9.700v7.800a1.500 1.500 0 0 1-1.500 1.500h-15A1.500 1.500 0 0 1 3 17.500z"/>');
export const ART_ID = "__art";
export const ART_GLYPH = svg('<rect x="3" y="4" width="18" height="16" rx="1.500"/><rect x="6.500" y="7.500" width="11" height="9"/><path d="M6.500 14.500l3-3 2.500 2.500 2-2 3 3"/>');
export const INPUT_ID = "__input:";
export const INPUT_GLYPH = svg('<rect x="3" y="5" width="18" height="12" rx="2"/><path d="M8 21h8M12 17v4M9.500 9v4M9.500 11h3M12.500 9v4M15 9.500l2 1.500-2 1.500z"/>');
export const TRIPS_ID = "__trips";
export const TRIPS_GLYPH = svg('<path d="M9 7V5.500A1.500 1.500 0 0 1 10.500 4h3A1.500 1.500 0 0 1 15 5.500V7"/><rect x="3.500" y="7" width="17" height="12.500" rx="2"/><path d="M8 7v12.500M16 7v12.500"/>');
export const QUIZ_ID = "__quiz";
export const QUIZ_GLYPH = svg('<circle cx="12" cy="12" r="9"/><path d="M9.300 9.400a2.800 2.800 0 1 1 4.100 2.500c-.900.500-1.400 1.100-1.400 2.100M12 17v.200"/>');
export const PLACES_ID = "__places";
export const PLACES_GLYPH = svg('<path d="M9 4.500 3.500 6.500v13L9 17.500l6 2 5.500-2v-13L15 6.500z"/><path d="M9 4.500v13M15 6.500v13"/>');
export const OWN_TILES: Record<string, string> = {
  [WEATHER_ID]: "weather", [SOUND_ID]: "sound", [BLUETOOTH_ID]: "bluetooth", [SLEEP_ID]: "sleep",
  [WIFI_ID]: "wifi", [STATUS_ID]: "status", [EXIT_ID]: "desktop", [POWER_ID]: "power",
  [PLACES_ID]: "places", [VIDEOS_ID]: "videos", [TRIPS_ID]: "trips", [QUIZ_ID]: "quiz",
};
export const SPEED_ID = "__photo-speed";
export const SPEED_GLYPH =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">' +
  '<circle cx="12" cy="13" r="7.500"/><path d="M12 9v4l2.500 2M9.500 2.500h5"/></svg>';

/** The picture for a smart-home tile, by what kind of thing it is. */
export function homeGlyph(e: HomeEntity): string {
  switch (e.domain) {
    case "light":
      return svg('<path d="M9 18h6M10 21h4M12 3a6 6 0 0 0-3.6 10.8c.6.5 1 1.300 1 2.200h5.200c0-.900.400-1.700 1-2.200A6 6 0 0 0 12 3z"/>');
    case "scene":
    case "script":
      return svg('<path d="M8 5l11 7-11 7z"/>');
    case "cover":
      return svg('<rect x="4" y="3" width="16" height="18" rx="1.5"/><path d="M4 8h16M4 13h16M12 13v5"/>');
    case "sensor":
    case "climate":
    case "weather":
      return svg('<path d="M12 3a2 2 0 0 0-2 2v8.500a4 4 0 1 0 4 0V5a2 2 0 0 0-2-2z"/><path d="M12 9v7"/>');
    case "switch":
    case "input_boolean":
    case "fan":
      return svg('<path d="M12 3v8"/><path d="M6.300 6.800a8 8 0 1 0 11.400 0"/>');
  }
  return svg('<circle cx="12" cy="12" r="7"/>');
}
