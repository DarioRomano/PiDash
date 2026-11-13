// The Sound section of the settings page: where the Pi's sound goes and how
// loud it is. Everything here acts at once; nothing waits for "Save".
import { t } from "./i18n";

interface Output {
  id: string;
  name: string;
  kind: string;
  active: boolean;
  volume: number;
  muted: boolean;
}
interface Status {
  available: boolean;
  problem?: string;
  outputs: Output[];
}

const STEP = 5;
const $ = <T extends HTMLElement = HTMLElement>(id: string) => document.getElementById(id) as T;

export function startSound() {
  if (!$("snd-card")) return;
  const statusEl = $("snd-status");
  const body = $("snd-body");
  const pick = $<HTMLSelectElement>("snd-output");
  const level = $("snd-level");
  const muteBtn = $<HTMLButtonElement>("snd-mute");
  const result = $("snd-result");

  let status: Status = { available: false, outputs: [] };
  let options = "";
  let busy = false;

  const say = (raw: string, kind: "" | "ok" | "error" = "") => {
    const text = t(raw);
    result.textContent = text.charAt(0).toUpperCase() + text.slice(1);
    result.className = "result " + kind;
  };
  const active = () => status.outputs.find((o) => o.active) ?? status.outputs[0];

  function render() {
    statusEl.textContent = status.available ? (status.outputs.length === 0 ? "No sound output was found." : "") : (status.problem ?? "Sound cannot be set here.");
    statusEl.hidden = statusEl.textContent === "";
    const usable = status.available && status.outputs.length > 0;
    body.hidden = !usable;
    if (!usable) return;
    // The list is only rebuilt when it changed, so that it does not close
    // under someone who is choosing from it.
    const key = JSON.stringify(status.outputs.map((o) => [o.id, o.name]));
    if (key !== options) {
      options = key;
      pick.replaceChildren(
        ...status.outputs.map((o) => {
          const opt = new Option(o.name, o.id);
          opt.dataset.noTranslate = ""; // the names of devices stay as they are
          return opt;
        }),
      );
    }
    const now = active();
    pick.value = now.id;
    level.textContent = now.muted ? t("muted") : `${now.volume} %`;
    muteBtn.textContent = now.muted ? "Sound on" : "Mute";
  }

  async function load() {
    try {
      const res = await fetch("/api/sound", { cache: "no-store" });
      if (res.ok) status = (await res.json()) as Status;
    } catch {
      /* the next look may work */
    }
    render();
  }

  async function post(action: string, payload: Record<string, unknown>): Promise<boolean> {
    busy = true;
    try {
      const res = await fetch(`/api/sound/${action}`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(payload) });
      if (!res.ok) {
        say((await res.text()).trim(), "error");
        void load();
        return false;
      }
      status = (await res.json()) as Status;
      say("");
      return true;
    } catch {
      say("The Pi did not answer.", "error");
      return false;
    } finally {
      busy = false;
      render();
    }
  }

  pick.addEventListener("change", () => void post("use", { id: pick.value }));

  // The volume buttons may be pressed quickly, or held: the number follows
  // at once, and the Pi is told the latest value, one request at a time.
  let wanted: number | null = null;
  let sending = false;
  let unmute = false;
  async function step(by: number) {
    const now = active();
    if (!now) return;
    // Changing the volume says "I want to hear this": it ends the muting.
    if (now.muted) unmute = true;
    wanted = Math.max(0, Math.min(100, (wanted ?? now.volume) + by));
    now.volume = wanted;
    now.muted = false;
    render();
    if (sending) return;
    sending = true;
    const id = now.id;
    while (wanted !== null) {
      const volume = wanted;
      wanted = null;
      if (unmute) {
        unmute = false;
        await post("mute", { id, muted: false });
      }
      await post("volume", { id, volume });
    }
    sending = false;
  }
  $("snd-down").addEventListener("click", () => void step(-STEP));
  $("snd-up").addEventListener("click", () => void step(STEP));
  muteBtn.addEventListener("click", () => {
    const now = active();
    if (now) void post("mute", { id: now.id, muted: !now.muted });
  });
  $("snd-test").addEventListener("click", async () => {
    const now = active();
    if (!now) return;
    say("Playing…");
    if (await post("test", { id: now.id })) say("Did you hear it? If not, another output is the right one.", "ok");
  });

  // Outputs come and go, with Bluetooth headphones for example.
  window.setInterval(() => {
    if (!document.hidden && !busy && !sending) void load();
  }, 5000);
  void load();
}
