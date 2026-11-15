// What the dashboard says in a line at the bottom of the screen, and how it
// asks the Pi to change something.
import { t } from "./i18n";

const toastEl = document.querySelector<HTMLElement>("#toast")!;
let toastTimer = 0;

/** Says something for a few seconds. */
export function toast(text: string, ms = 4000) {
  toastEl.textContent = text;
  toastEl.classList.add("visible");
  clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => toastEl.classList.remove("visible"), ms);
}

/** What is said when the Pi does not answer at all. */
export const UNREACHABLE = "The PiDash backend is not reachable.";

export async function postJSON(url: string, body: unknown): Promise<Response> {
  return fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

/** What the Pi answers starts in lower case, as its log does: as a sentence for the screen. */
export async function said(res: Response): Promise<string> {
  const text = t((await res.text()).trim());
  return text.charAt(0).toUpperCase() + text.slice(1);
}

/** Says what went wrong with something the TV asked the Pi to change. */
export async function sayRefusal(res: Response, ms = 10000) {
  if (res.status === 401 || res.status === 429) return toast("The settings are locked: this needs the PIN, under Settings.", 8000);
  toast(await said(res), ms);
}
