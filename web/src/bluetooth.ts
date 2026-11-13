// The Bluetooth section of the settings page: the Pi's devices, and pairing
// new ones. Everything here acts at once; nothing waits for "Save".
import { t } from "./i18n";
import { highlight } from "./tvnav";

interface Device {
  address: string;
  name: string;
  kind: string;
  paired: boolean;
  connected: boolean;
  signal?: number;
  /** Charge in percent, for devices that report it. */
  battery?: number;
}
interface Status {
  available: boolean;
  problem?: string;
  on: boolean;
  searching: boolean;
  devices: Device[];
}

type Send = (method: string, url: string, body: unknown) => Promise<Response>;

const KINDS: Record<string, string> = {
  controller: "Game controller",
  audio: "Headphones or speaker",
  keyboard: "Keyboard",
  mouse: "Mouse",
  phone: "Phone",
  computer: "Computer",
  tv: "TV",
  other: "Device",
};

const $ = <T extends HTMLElement = HTMLElement>(id: string) => document.getElementById(id) as T;

export function startBluetooth(send: Send) {
  const card = $("bt-card");
  if (!card) return;
  const statusEl = $("bt-status");
  const known = $("bt-list");
  const found = $("bt-found");
  const searchBtn = $<HTMLButtonElement>("bt-search");
  const powerBtn = $<HTMLButtonElement>("bt-power");
  const result = $("bt-result");

  let status: Status = { available: false, on: false, searching: false, devices: [] };
  let shown = "";
  /** Devices found nearby, in the order they turned up. */
  const order: string[] = [];
  /** The device something is being done with, and what: shown in its row. */
  let busy: { address: string; text: string } | null = null;

  // What the Pi answers starts in lower case, as its log does; on the page it
  // is a sentence.
  const say = (raw: string, kind: "" | "ok" | "error" = "") => {
    const text = t(raw);
    result.textContent = text.charAt(0).toUpperCase() + text.slice(1);
    result.className = "result " + kind;
  };

  function button(role: string, label: string, onClick: () => void): HTMLButtonElement {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "text";
    b.dataset.role = role;
    b.textContent = label;
    b.addEventListener("click", onClick);
    return b;
  }

  function row(d: Device): HTMLLIElement {
    const li = document.createElement("li");
    li.dataset.address = d.address;
    const what = document.createElement("div");
    what.className = "what";
    // Names of devices are not for translating.
    const name = document.createElement("span");
    name.dataset.noTranslate = "";
    name.textContent = d.name;
    // Two texts of their own, so that each is put into the page's language,
    // also when that only becomes known after the row was drawn.
    const small = document.createElement("small");
    const part = (text: string) => {
      const span = document.createElement("span");
      span.textContent = text;
      return span;
    };
    const state = busy?.address === d.address ? busy.text : d.paired ? (d.connected ? "connected" : "not connected") : "nearby";
    small.append(part(KINDS[d.kind] ?? KINDS.other), " · ", part(state));
    if (d.battery != null && d.connected) small.append(" · ", part("battery"), ` ${d.battery} %`);
    what.append(name, small);
    li.append(what);
    if (d.paired) {
      li.append(
        d.connected ? button("toggle", "Disconnect", () => void act("disconnect", d, "disconnecting…")) : button("toggle", "Connect", () => void act("connect", d, "connecting…")),
        button("forget", "Forget", () => void act("forget", d, "forgetting…")),
      );
    } else {
      li.append(button("toggle", "Pair", () => void act("pair", d, "pairing…")));
    }
    return li;
  }

  function render() {
    // Found devices stay in the order they turned up. How well each is
    // received changes all the time; sorted by that, the rows would keep
    // swapping places under the remote.
    for (const d of status.devices) if (!d.paired && !order.includes(d.address)) order.push(d.address);
    const key = JSON.stringify([{ ...status, devices: status.devices.map((d) => ({ ...d, signal: 0 })) }, busy]);
    if (key === shown) return;
    shown = key;

    // The rows are built anew. If the remote's highlight was on one of
    // them, it goes to the same device's button again, or to the search
    // button if the device is gone, rather than to the top of the page.
    const lit = card.querySelector<HTMLElement>(".tv-focus");
    const litRow = lit?.closest<HTMLElement>("li[data-address]");

    statusEl.textContent = !status.available
      ? (status.problem ?? "Bluetooth is not available here.")
      : !status.on
        ? "Bluetooth is switched off."
        : status.searching
          ? "Looking for devices nearby…"
          : "";
    statusEl.hidden = statusEl.textContent === "";
    const usable = status.available && status.on;
    powerBtn.hidden = !status.available;
    powerBtn.textContent = status.on ? "Switch Bluetooth off" : "Switch Bluetooth on";
    searchBtn.hidden = !usable;
    searchBtn.textContent = status.searching ? "Stop looking" : "Look for new devices";
    $("bt-how").hidden = !usable;

    const paired = usable ? status.devices.filter((d) => d.paired) : [];
    const nearby = usable ? status.devices.filter((d) => !d.paired).sort((a, b) => order.indexOf(a.address) - order.indexOf(b.address)) : [];
    known.replaceChildren(...paired.map(row));
    found.replaceChildren(...nearby.map(row));
    $("bt-none").hidden = !usable || paired.length > 0;
    $("bt-found-title").hidden = nearby.length === 0;

    if (litRow && lit && !lit.isConnected) {
      const again = card.querySelector<HTMLElement>(`li[data-address="${litRow.dataset.address}"]`);
      const target = again?.querySelector<HTMLElement>(`button[data-role="${lit.dataset.role}"]`) ?? again?.querySelector<HTMLElement>("button") ?? (searchBtn.hidden ? powerBtn : searchBtn);
      highlight(target);
    }
  }

  async function load() {
    try {
      const res = await fetch("/api/bluetooth", { cache: "no-store" });
      if (res.ok) status = (await res.json()) as Status;
    } catch {
      /* the next look may work */
    }
    render();
  }

  async function post(action: string, body: unknown): Promise<boolean> {
    try {
      const res = await send("POST", `/api/bluetooth/${action}`, body);
      if (!res.ok) {
        say(res.status === 401 ? "The PIN is needed to change this." : (await res.text()).trim(), "error");
        return false;
      }
      status = (await res.json()) as Status;
      return true;
    } catch {
      say("The Pi did not answer.", "error");
      return false;
    }
  }

  async function act(action: string, d: Device, doing: string) {
    if (busy) return;
    busy = { address: d.address, text: doing };
    say("");
    render();
    const ok = await post(action, { address: d.address });
    busy = null;
    if (ok) {
      const done: Record<string, string> = {
        pair: "{name} is paired. It connects by itself when it is switched on.",
        connect: "{name} is connected.",
        disconnect: "{name} is disconnected.",
        forget: "{name} was forgotten.",
      };
      say(t(done[action], { name: d.name }), "ok");
    } else void load();
    render();
  }

  searchBtn.addEventListener("click", async () => {
    say("");
    await post("search", { on: !status.searching });
    render();
  });
  powerBtn.addEventListener("click", async () => {
    say("");
    await post("power", { on: !status.on });
    render();
  });

  // Devices come and go by themselves: look again every few seconds, more
  // often while searching, and not at all while the page is not looked at.
  const tick = () => {
    window.setTimeout(
      async () => {
        if (!document.hidden && !busy) await load();
        tick();
      },
      status.searching ? 2000 : 5000,
    );
  };
  void load().then(tick);
}
