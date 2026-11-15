// What the page with the list and the map can show: the places the photos
// were taken, and the trips they tell of.
import { t } from "./i18n";
import type { Spot, SpotList } from "./places";

interface Place {
  id: string;
  name: string;
  count: number;
  latitude: number;
  longitude: number;
  first: number;
  last: number;
  key: string;
  v: string;
}

interface Trip {
  id: string;
  name: string;
  start: string;
  end: string;
  days: number;
  count: number;
  latitude: number;
  longitude: number;
  key: string;
  v: string;
}

const photos = (n: number) => t(n === 1 ? "1 photo" : "{n} photos", { n });

async function get<T>(url: string): Promise<T> {
  const res = await fetch(url, { cache: "no-store" });
  if (!res.ok) throw new Error(String(res.status));
  return (await res.json()) as T;
}

/** Every place the photos were taken, the ones with the most photos first. */
export function placesList(onChoose: (spot: Spot) => void): SpotList {
  return {
    title: "Where the photos were taken",
    empty: "None of the photos in the slideshow says where it was taken.",
    what: "a place",
    onChoose,
    async load() {
      const data = await get<{ places: Place[]; map: boolean }>("/api/photos/places");
      return {
        map: data.map,
        spots: data.places.map((p) => {
          const years = p.first === 0 ? "" : p.first === p.last ? String(p.first) : `${p.first} – ${p.last}`;
          return { id: p.id, name: p.name, sub: years ? `${photos(p.count)}  ·  ${years}` : photos(p.count), latitude: p.latitude, longitude: p.longitude, key: p.key, v: p.v };
        }),
      };
    },
  };
}

/** From when to when a trip was: "2 – 6 September 2023". */
function span(trip: Trip, locale: string | undefined): string {
  const format = new Intl.DateTimeFormat(locale, { day: "numeric", month: "long", year: "numeric", timeZone: "UTC" });
  const start = new Date(trip.start), end = new Date(trip.end);
  if (trip.days <= 1) return format.format(start);
  try {
    return format.formatRange(start, end);
  } catch {
    return `${format.format(start)} – ${format.format(end)}`;
  }
}

/** The journeys the photos tell of, the latest first. */
export function tripsList(onChoose: (spot: Spot) => void, locale: () => string | undefined): SpotList {
  return {
    title: "Trips",
    empty: "The photos tell of no trips yet. A trip is several photos taken away from home within a few days.",
    what: "a trip",
    onChoose,
    async load() {
      const data = await get<{ trips: Trip[]; map: boolean }>("/api/photos/trips");
      return {
        map: data.map,
        spots: data.trips.map((tr) => ({ id: tr.id, name: tr.name, sub: `${span(tr, locale())}  ·  ${photos(tr.count)}`, latitude: tr.latitude, longitude: tr.longitude, key: tr.key, v: tr.v })),
      };
    },
  };
}
