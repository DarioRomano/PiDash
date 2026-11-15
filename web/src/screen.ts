// The screen itself: fitting the design to it, keeping it awake, and moving
// what is drawn a little now and then.

/** Fits the 1920x1080 design to the actual screen. */
export function fitToScreen(fit: HTMLElement) {
  const apply = () => {
    const scale = Math.min(window.innerWidth / 1920, window.innerHeight / 1080);
    fit.style.setProperty("--scale", scale.toFixed(4));
  };
  apply();
  window.addEventListener("resize", apply);
}

/**
 * Keeps the screen awake while the dashboard is showing. Uses the desktop's
 * idle-inhibit mechanism, so screen blanking only applies again once the
 * dashboard is closed.
 */
export function keepAwake() {
  let lock: WakeLockSentinel | null = null;
  const ask = async () => {
    if (!("wakeLock" in navigator) || lock) return;
    try {
      lock = await navigator.wakeLock.request("screen");
      lock.addEventListener("release", () => (lock = null));
    } catch {
      /* not allowed right now; retried on the next visibility change */
    }
  };
  void ask();
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") void ask();
  });
}

// Pages that cover the screen reach this far past its edges (see #help in
// the styles, which allows a little more).
const MAX_PIXEL_SHIFT = 20;
let pixelShiftTimer = 0;

/** Burn-in protection: every few minutes texts and tiles move by a few pixels, at most max. */
export function startPixelShift(stage: HTMLElement, max: number) {
  clearInterval(pixelShiftTimer);
  stage.style.setProperty("--shift-x", "0px");
  stage.style.setProperty("--shift-y", "0px");
  if (max <= 0) return;
  max = Math.min(max, MAX_PIXEL_SHIFT);
  pixelShiftTimer = window.setInterval(() => {
    const x = Math.round((Math.random() * 2 - 1) * max);
    const y = Math.round((Math.random() * 2 - 1) * max);
    stage.style.setProperty("--shift-x", `${x}px`);
    stage.style.setProperty("--shift-y", `${y}px`);
  }, 3 * 60 * 1000);
}
