// Two short lists the dashboard asks with: Bluetooth devices to connect or
// disconnect, and restarting or shutting down.
import { t } from "./i18n";
import type { Menu } from "./menu";
import { postJSON, sayRefusal, toast, UNREACHABLE } from "./notify";
import type { RailItem } from "./rail";
import { BLUETOOTH_GLYPH, BLUETOOTH_ID } from "./tiles";

interface BluetoothDevice {
  address: string;
  name: string;
  paired: boolean;
  connected: boolean;
  battery?: number;
}
interface BluetoothStatus {
  available: boolean;
  on: boolean;
  devices: BluetoothDevice[];
}

/**
 * The devices the Pi knows, to connect or disconnect one without a trip
 * through the settings. Pairing a new one is done there.
 */
export class QuickBluetooth {
  private status: BluetoothStatus | null = null;
  private asked = 0;
  /** The address of the device that is being connected or disconnected right now. */
  private busy = "";
  private showing = false;

  constructor(
    private menu: Menu,
    /** What is known has changed: the tiles that show it are to be drawn anew. */
    private onChange: () => void,
    private openSettings: (category: string) => void,
  ) {}

  get available() {
    return !!this.status?.available;
  }

  /** The menu was closed, by whatever means. */
  closed() {
    this.showing = false;
  }

  private paired() {
    return (this.status?.devices ?? []).filter((d) => d.paired);
  }

  /** The Bluetooth button, for the app row and the photo's bar: it names what is connected. */
  item(): RailItem {
    const on = this.paired()
      .filter((d) => d.connected)
      .map((d) => (d.battery !== undefined ? `${d.name} · ${d.battery} %` : d.name));
    return { id: BLUETOOTH_ID, label: "Bluetooth", glyph: BLUETOOTH_GLYPH, sub: !this.status?.on ? t("Off") : on.length ? on.join(", ") : undefined };
  }

  /** Asks the Pi for its Bluetooth devices, at most every few seconds. */
  async look(force = false) {
    if (!force && Date.now() - this.asked < 8000) return;
    this.asked = Date.now();
    try {
      const res = await fetch("/api/bluetooth", { cache: "no-store" });
      if (!res.ok) return;
      const before = JSON.stringify(this.status);
      this.status = (await res.json()) as BluetoothStatus;
      if (JSON.stringify(this.status) !== before) this.onChange();
    } catch {
      /* the next look will tell */
    }
  }

  /** Opens the list, and brings it up to date once the Pi has been asked. */
  open() {
    this.show();
    void this.look(true).then(() => {
      if (this.showing && !this.busy) this.show(this.menu.selected);
    });
  }

  private show(selected = 0) {
    const busy = this.busy;
    const st = this.status;
    if (!st) return;
    // More than fit on the screen are found in the settings.
    const devices = this.paired().slice(0, 5);
    const items = devices.map((d) => ({
      label: d.name,
      hint:
        d.address === busy
          ? t(d.connected ? "Disconnecting…" : "Connecting…")
          : d.connected
            ? t("Connected") + (d.battery !== undefined ? "  ·  " + t("battery {n} %", { n: d.battery }) : "") + "  ·  " + t("OK disconnects")
            : t("Not connected") + "  ·  " + t("OK connects"),
    }));
    if (!st.on) items.push({ label: t("Switch Bluetooth on"), hint: "" });
    items.push({ label: t("Pair a new device…"), hint: t("Opens the settings, where devices are also forgotten.") });
    this.menu.open(t("Bluetooth"), items, selected, async (i) => {
      if (i === items.length - 1) return this.openSettings("sound");
      const device = devices[i];
      if (this.busy) return this.show(i); // one thing at a time
      // Shown again at once, so that it is plain something is happening.
      this.busy = device ? device.address : "power";
      this.show(i);
      try {
        const res = device
          ? await postJSON(`/api/bluetooth/${device.connected ? "disconnect" : "connect"}`, { address: device.address })
          : await postJSON("/api/bluetooth/power", { on: true });
        if (!res.ok) await sayRefusal(res);
        else {
          this.status = (await res.json()) as BluetoothStatus;
          this.onChange();
        }
      } catch {
        toast(UNREACHABLE, 6000);
      }
      this.busy = "";
      if (this.showing) this.show(this.menu.selected);
    });
    // Opening a list closes the one before it, which says so: mark this one after.
    this.showing = true;
  }
}

/**
 * Restart and shut down. Things that take the screen away are asked about
 * twice: once to choose, once to be sure.
 */
export function openPowerMenu(menu: Menu) {
  const choices = [
    { action: "restart", label: "Restart PiDash", hint: "The dashboard is back in a few seconds.", sure: "Restart PiDash?", yes: "Yes, restart PiDash" },
    { action: "reboot", label: "Restart the Pi", hint: "Takes about a minute. An app that is open is closed.", sure: "Restart the Pi?", yes: "Yes, restart the Pi" },
    { action: "off", label: "Shut the Pi down", hint: "To start it again, switch its power off and on.", sure: "Shut the Pi down?", yes: "Yes, shut it down" },
  ];
  menu.open("Power", choices, 0, (i) => {
    const c = choices[i];
    // "No" is what is selected: a second OK by habit changes nothing.
    menu.open(c.sure, [{ label: "No, go back" }, { label: c.yes }], 0, async (answer) => {
      if (answer !== 1) return;
      try {
        const res = await postJSON(`/api/power/${c.action}`, {});
        if (!res.ok) return sayRefusal(res, 12000);
        toast(t(c.action === "off" ? "Shutting down…" : "Restarting…"), 30000);
      } catch {
        toast(UNREACHABLE, 6000);
      }
    });
  });
}
