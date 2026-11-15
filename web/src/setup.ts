// The questions of the first start: which language, where the TV is (for the
// weather), and where the photos are. Shown on the TV once, when nothing has
// been set up yet; everything it asks can also be set on the settings page.
import { LANGUAGES, setLanguage, t } from "./i18n";
import { ask, highlight, onTV, startTVNav } from "./tvnav";

const $ = <T extends HTMLElement = HTMLElement>(id: string) => document.getElementById(id) as T;
const STEPS = ["language", "weather", "photos", "done"] as const;
type Step = (typeof STEPS)[number];

let step: Step = "language";
/** What was answered so far. */
let language = "";
let town = "";
let source = "";
let busy = false;

function setResult(el: HTMLElement, text: string, kind: "" | "ok" | "error" = "") {
  el.textContent = text;
  el.className = "result " + kind;
}

function show(next: Step) {
  step = next;
  for (const s of document.querySelectorAll<HTMLElement>(".step")) s.hidden = s.dataset.step !== next;
  document.querySelectorAll("#setup-steps li").forEach((dot, i) => dot.classList.toggle("on", i <= STEPS.indexOf(next)));
  if (next === "done") {
    $("finish-note").textContent = source ? t("Start goes on to the page that asks how to reach your photos.") : "";
  }
  // The highlight goes to what is asked for first.
  const first = [...document.querySelectorAll<HTMLElement>(`.step[data-step="${next}"] button, .step[data-step="${next}"] input`)].find((el) => !(el as HTMLButtonElement).disabled);
  if (first) {
    if (onTV) highlight(first);
    else first.focus({ preventScroll: true });
  }
}

// --- Which language ----------------------------------------------------------------

$("languages").replaceChildren(
  ...Object.entries(LANGUAGES).map(([code, name]) => {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "secondary wide";
    b.dataset.lang = code;
    b.textContent = name;
    b.addEventListener("click", () => {
      language = code;
      setLanguage(code);
      show("weather");
    });
    return b;
  }),
);

// --- Where the TV is ------------------------------------------------------------------

$("town").addEventListener("input", () => {
  // Another town than the one that was found: to be looked up again.
  $<HTMLButtonElement>("weather-next").disabled = true;
  town = "";
});

$("town-find").addEventListener("click", async () => {
  const out = $("town-result");
  const wanted = $<HTMLInputElement>("town").value.trim();
  if (!wanted) return setResult(out, "Enter a town first.", "error");
  setResult(out, "Looking it up…");
  try {
    const res = await fetch("/api/settings/test-weather", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ location: wanted }) });
    if (!res.ok) return setResult(out, (await res.text()).trim(), "error");
    town = wanted;
    setResult(out, t("Found: {place}", { place: (await res.json()).place }), "ok");
    const next = $<HTMLButtonElement>("weather-next");
    next.disabled = false;
    if (onTV) highlight(next);
    else next.focus();
  } catch {
    setResult(out, "The Pi did not answer.", "error");
  }
});

$("weather-skip").addEventListener("click", () => {
  town = "";
  show("photos");
});
$("weather-next").addEventListener("click", () => show("photos"));

// --- Where the photos are ---------------------------------------------------------------

for (const b of document.querySelectorAll<HTMLButtonElement>("[data-source]")) {
  b.addEventListener("click", () => {
    source = b.dataset.source ?? "";
    show("done");
  });
}

// --- Done ---------------------------------------------------------------------------------

async function startedAt(): Promise<number> {
  return (await (await fetch("/api/config", { cache: "no-store" })).json()).started;
}

/** Saves what was answered, and goes on: to the page for the photos, or to the dashboard. */
async function finish(leaving = false) {
  if (busy) return;
  busy = true;
  const out = $("finish-result");
  const onward = !leaving && source ? `/settings?${onTV ? "from=tv&" : ""}source=${encodeURIComponent(source)}#photos` : "/";
  try {
    // Not asked again, whatever was answered.
    await fetch("/api/setup/done", { method: "POST" });
    if (!leaving && (language || town)) {
      setResult(out, "Saving…");
      // What is there already stays; the language and the town are laid into it.
      const info = await (await fetch("/api/settings", { cache: "no-store" })).json();
      const before = await startedAt();
      const res = await fetch("/api/settings", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          ...(language ? { screen: { ...info.screen, language } } : {}),
          ...(town ? { weather: { ...info.weather, location: town } } : {}),
        }),
      });
      if (!res.ok) {
        busy = false;
        return setResult(out, (await res.text()).trim(), "error");
      }
      // PiDash starts anew with what was saved; then on.
      for (let i = 0; i < 60; i++) {
        await new Promise((r) => setTimeout(r, 400));
        try {
          if ((await startedAt()) !== before) break;
        } catch {
          /* restarting */
        }
      }
    }
  } catch {
    /* the dashboard shows what there is */
  }
  location.href = onward;
}

$("finish").addEventListener("click", () => void finish());

// Back goes a question back; from the first, it offers to leave it for later.
if (onTV) {
  startTVNav({
    onBack: async () => {
      const at = STEPS.indexOf(step);
      if (at > 0) return show(STEPS[at - 1]);
      const leave = await ask("Leave the questions for later? Everything can be set under Settings.", "Leave them", "Stay");
      if (leave) void finish(true);
    },
  });
}

// The language the browser was last used in, if any, is where the highlight starts.
show("language");
