// Phone remote: posts actions to the backend, which forwards them to the TV.

const statusDot = document.getElementById("status")!;

async function send(action: string, text?: string) {
  try {
    const res = await fetch("/api/input", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(text === undefined ? { action } : { action, text }),
    });
    statusDot.classList.toggle("ok", res.ok);
  } catch {
    statusDot.classList.remove("ok");
  }
}

document.querySelectorAll<HTMLButtonElement>("button[data-action]").forEach((btn) => {
  btn.addEventListener("click", () => {
    if (navigator.vibrate) navigator.vibrate(10);
    send(btn.dataset.action!);
  });
});

const form = document.getElementById("textform") as HTMLFormElement;
const text = document.getElementById("text") as HTMLInputElement;
form.addEventListener("submit", (e) => {
  e.preventDefault();
  const value = text.value.trim();
  if (!value) return;
  send("text", value);
  text.value = "";
});

// Keyboard on a laptop works too.
const keymap: Record<string, string> = {
  ArrowUp: "up", ArrowDown: "down", ArrowLeft: "left", ArrowRight: "right",
  Enter: "select", Escape: "back", Backspace: "back",
};
window.addEventListener("keydown", (e) => {
  if (document.activeElement === text) return;
  const a = keymap[e.code] ?? (/^Digit\d$/.test(e.code) ? e.code.slice(5) : undefined);
  if (a) { e.preventDefault(); send(a); }
});

fetch("/healthz").then((r) => statusDot.classList.toggle("ok", r.ok)).catch(() => {});
