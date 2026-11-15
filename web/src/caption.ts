import { t } from "./i18n";
import type { PhotoItem } from "./types";

/**
 * The line under a photo: "Holiday · Sintra, Portugal · 14 June 2024": the
 * innermost folder, the place if the photo carries one, and the day it was
 * taken. Of two photos side by side it says what they share once, and what
 * differs for each.
 */
export function captionOf(item: PhotoItem | null, locale: string | undefined): string {
  if (!item) return "";
  const facts = (p: { folder: string; taken: string; place?: string }) => {
    const taken = new Date(p.taken);
    const when = isNaN(taken.getTime()) || taken.getFullYear() < 1990
      ? ""
      : new Intl.DateTimeFormat(locale, { day: "numeric", month: "long", year: "numeric" }).format(taken);
    // Taken on this very day of the year, in an earlier one: worth saying.
    const now = new Date();
    const years = now.getFullYear() - taken.getFullYear();
    const today = when && years >= 1 && taken.getMonth() === now.getMonth() && taken.getDate() === now.getDate();
    const ago = today ? t(years === 1 ? "a year ago today" : "{n} years ago today", { n: years }) : "";
    return [p.folder.split(" / ").pop() ?? "", p.place ?? "", [when, ago].filter(Boolean).join(", ")];
  };
  let text: string;
  if (item.parts?.length === 2) {
    // The left one first.
    const [left, right] = item.parts.map(facts);
    const shared = left.filter((f, i) => f && f === right[i]);
    const own = (mine: string[], other: string[]) => mine.filter((f, i) => f && f !== other[i]).join("  ·  ");
    const [l, r] = [own(left, right), own(right, left)];
    text = [shared.join("  ·  "), l && `◀ ${l}`, r && `▶ ${r}`].filter(Boolean).join("     ");
  } else text = facts(item).filter(Boolean).join("  ·  ");
  return (item.favourite ? "★  " : "") + text;
}
