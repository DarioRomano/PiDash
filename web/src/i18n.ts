// Languages for everything the pages show.
//
// The pages are written in English. A translation is a list from the English
// text to the text in another language (lang/de.ts and so on). Text is
// translated in two ways:
//
//   - by itself: whatever text appears on the page, at any time, is looked up
//     and replaced when the list has it. This covers the pages' fixed text and
//     most of what scripts put on screen, without changing those scripts.
//   - t("… {n} …", { n }): for text with something variable in it, which can
//     only be looked up before the variable part is filled in.
//
// Messages that come from the Pi itself (a NAS that cannot be reached, a wrong
// password) are not translated.
import de from "./lang/de";
import fr from "./lang/fr";
import it from "./lang/it";

export const LANGUAGES: Record<string, string> = { en: "English", de: "Deutsch", fr: "Français", it: "Italiano" };
const LISTS: Record<string, Record<string, string>> = { de, fr, it };
const STORE = "pidash-language";

let list: Record<string, string> | null = null;
let current = "en";

/** The language in use, e.g. "de". */
export const language = () => current;

const tidy = (s: string) => s.replace(/\s+/g, " ").trim();

/** Translates a text, then fills in {name} placeholders. */
export function t(text: string, vars?: Record<string, string | number>): string {
  let out = list?.[tidy(text)] ?? text;
  if (vars) for (const [k, v] of Object.entries(vars)) out = out.split(`{${k}}`).join(String(v));
  return out;
}

// --- Translating what is on the page ----------------------------------------------

const ATTRIBUTES = ["placeholder", "title", "aria-label"];
/** What a piece of text said in English, and what was last put in its place. */
const texts = new WeakMap<Text, { source: string; shown: string }>();
const attrs = new WeakMap<Element, Record<string, { source: string; shown: string }>>();

function lookup(source: string): string {
  const key = tidy(source);
  const found = key && list ? list[key] : undefined;
  if (found === undefined) {
    // For finding texts a translation lacks: set window.pidashMissing = [] and look at it later.
    const missing = (window as unknown as { pidashMissing?: string[] }).pidashMissing;
    if (missing && list && /[A-Za-z]{3}/.test(key) && !missing.includes(key)) missing.push(key);
    return source;
  }
  // Keep the spaces around it: they separate it from its neighbours.
  return source.slice(0, source.length - source.trimStart().length) + found + source.slice(source.trimEnd().length);
}

function textNode(node: Text) {
  const parent = node.parentElement;
  if (!parent || parent.closest("script, style, [data-no-translate]")) return;
  const now = node.nodeValue ?? "";
  const known = texts.get(node);
  // Unless it still shows what was put there, a script has written new text.
  const source = known && known.shown === now ? known.source : now;
  const shown = lookup(source);
  texts.set(node, { source, shown });
  if (shown !== now) node.nodeValue = shown;
}

function element(el: Element) {
  for (const name of ATTRIBUTES) {
    const now = el.getAttribute(name);
    if (now === null) continue;
    const all = attrs.get(el) ?? {};
    const known = all[name];
    const source = known && known.shown === now ? known.source : now;
    const shown = lookup(source);
    all[name] = { source, shown };
    attrs.set(el, all);
    if (shown !== now) el.setAttribute(name, shown);
  }
}

function sweep(root: Node) {
  if (root instanceof Text) return textNode(root);
  if (!(root instanceof Element)) return;
  element(root);
  for (const el of root.querySelectorAll("[placeholder], [title], [aria-label]")) element(el);
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  for (let n = walker.nextNode(); n; n = walker.nextNode()) textNode(n as Text);
}

let watching = false;
function watch() {
  if (watching) return;
  watching = true;
  new MutationObserver((changes) => {
    for (const c of changes) {
      if (c.type === "characterData") sweep(c.target);
      else if (c.type === "attributes") element(c.target as Element);
      else for (const n of c.addedNodes) sweep(n);
    }
  }).observe(document.documentElement, { subtree: true, childList: true, characterData: true, attributes: true, attributeFilter: ATTRIBUTES });
}

/** Switches the language of the page, now and for everything that appears later. */
export function setLanguage(code: string) {
  code = LISTS[code] ? code : "en";
  try {
    localStorage.setItem(STORE, code);
  } catch {
    /* not remembered */
  }
  if (code === current && watching) return;
  current = code;
  list = LISTS[code] ?? null;
  document.documentElement.lang = code;
  watch();
  sweep(document.documentElement);
}

// Start in the language used last, so the page does not flash English while
// it waits to hear from the Pi which language is set.
try {
  setLanguage(localStorage.getItem(STORE) ?? "en");
} catch {
  setLanguage("en");
}
