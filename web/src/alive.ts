// Tells the backend that this page is still running.
//
// A page in a browser that runs for weeks can freeze or crash while the
// browser itself stays up. The backend cannot see that from outside, so the
// page reports in every twenty seconds; when the reports stop, the backend
// starts the browser again. Only reports from the Pi's own browser count.
const EVERY_MS = 20_000;

let started = false;

export function reportAlive() {
  if (started) return;
  started = true;
  const beat = () => void fetch("/api/alive", { method: "POST", keepalive: true }).catch(() => {});
  beat();
  window.setInterval(beat, EVERY_MS);
}
