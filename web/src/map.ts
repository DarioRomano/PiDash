import { t } from "./i18n";

/** The size a piece of map is drawn at when it is not enlarged. */
export const TILE = 256;

/** Where a spot lies on the map of the whole world at a zoom level, in tiles. */
export function tileOf(lat: number, lon: number, zoom: number): { x: number; y: number } {
  const n = 2 ** zoom;
  const rad = (Math.max(-85, Math.min(85, lat)) * Math.PI) / 180;
  return { x: ((lon + 180) / 360) * n, y: ((1 - Math.asinh(Math.tan(rad)) / Math.PI) / 2) * n };
}

/** A map laid into a box: its pieces, a note for while they load, and where a spot is on it. */
export interface LaidMap {
  note: HTMLElement;
  layer: HTMLElement;
  credit: HTMLElement;
  /** Where a spot is in the box, in pixels from its top left corner. */
  spot(lat: number, lon: number): { x: number; y: number };
}

/**
 * Lays the pieces of map that cover a box around one spot. Each piece is
 * drawn `size` pixels wide: 256 is its own size, which is sharp; twice that
 * is softer, with lettering twice as large.
 */
export function layMap(box: HTMLElement, lat: number, lon: number, zoom: number, size = TILE): LaidMap {
  const laid = layTiles(box, lat, lon, zoom, size, (z, x, y) => `/api/map/${z}/${x}/${y}`);
  const note = document.createElement("div");
  note.className = "pi-map-note";
  note.textContent = "Loading the map…";
  laid.onLoad = (loaded) => {
    if (loaded === 0) note.textContent = t("The map could not be loaded.");
  };
  // Whoever draws the map is to be named on it.
  const credit = document.createElement("div");
  credit.className = "pi-credit";
  credit.dataset.noTranslate = "";
  credit.textContent = "© OpenStreetMap contributors";
  return { note, layer: laid.layer, credit, spot: laid.spot };
}

/** A sheet of pieces laid into a box: the map, or something that lies over it. */
export interface LaidTiles {
  layer: HTMLElement;
  spot(lat: number, lon: number): { x: number; y: number };
  /** Called when a piece could not be loaded, with how many have been so far. */
  onLoad?: (loaded: number) => void;
}

/**
 * Lays the pieces that cover a box around one spot, each from the address
 * that src gives for it.
 */
export function layTiles(box: HTMLElement, lat: number, lon: number, zoom: number, size: number, src: (z: number, x: number, y: number) => string): LaidTiles {
  const n = 2 ** zoom;
  const width = box.offsetWidth, height = box.offsetHeight;
  const centre = tileOf(lat, lon, zoom);
  const layer = document.createElement("div");
  layer.className = "pi-tiles";
  let loaded = 0;
  const laid: LaidTiles = {
    layer,
    spot(la, lo) {
      const at = tileOf(la, lo, zoom);
      return { x: width / 2 + (at.x - centre.x) * size, y: height / 2 + (at.y - centre.y) * size };
    },
  };
  const x0 = Math.floor(centre.x - width / 2 / size), x1 = Math.floor(centre.x + width / 2 / size);
  const y0 = Math.floor(centre.y - height / 2 / size), y1 = Math.floor(centre.y + height / 2 / size);
  for (let ty = y0; ty <= y1; ty++) {
    if (ty < 0 || ty >= n) continue; // beyond the poles there is no map
    for (let tx = x0; tx <= x1; tx++) {
      const img = document.createElement("img");
      img.alt = "";
      img.style.width = img.style.height = `${size}px`;
      img.style.left = `${Math.round(width / 2 + (tx - centre.x) * size)}px`;
      img.style.top = `${Math.round(height / 2 + (ty - centre.y) * size)}px`;
      img.addEventListener("load", () => {
        loaded++;
        img.classList.add("loaded");
      });
      img.addEventListener("error", () => {
        img.remove();
        laid.onLoad?.(loaded);
      });
      // The world goes round: east of the last piece comes the first again.
      img.src = src(zoom, ((tx % n) + n) % n, ty);
      layer.appendChild(img);
    }
  }
  return laid;
}

export const PIN =
  '<svg viewBox="0 0 48 64" aria-hidden="true"><path d="M24 2C11.800 2 2 11.800 2 24c0 16 22 38 22 38s22-22 22-38C46 11.800 36.200 2 24 2z" fill="#e5484d" stroke="#fff" stroke-width="3"/>' +
  '<circle cx="24" cy="24" r="8" fill="#fff"/></svg>';
