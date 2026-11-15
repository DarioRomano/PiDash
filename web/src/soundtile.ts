import { t } from "./i18n";
import { postJSON, toast, UNREACHABLE } from "./notify";
import type { RailItem } from "./rail";
import { play } from "./sfx";
import { SOUND_GLYPH, SOUND_ID } from "./tiles";

interface SoundOutput {
  id: string;
  name: string;
  active: boolean;
  unplugged?: boolean;
}

/**
 * A tile that moves the sound to the next output: from the TV to the
 * headphones and back, without a trip into the settings. It is there when
 * there is a choice to make.
 */
export class SoundTile {
  private outputs: SoundOutput[] = [];

  constructor(private onChange: () => void) {}

  /** What the Pi says about where sound can go. */
  update(st: { available: boolean; outputs: SoundOutput[] }) {
    const usable = st.available ? st.outputs.filter((o) => !o.unplugged) : [];
    if (JSON.stringify(usable) === JSON.stringify(this.outputs)) return;
    this.outputs = usable;
    this.onChange();
  }

  /** The tile; null while there is nothing to choose between. */
  item(): RailItem | null {
    if (this.outputs.length < 2) return null;
    return { id: SOUND_ID, label: "Sound", glyph: SOUND_GLYPH, sub: this.outputs.find((o) => o.active)?.name ?? "" };
  }

  /** Moves the sound to the next output. */
  async next() {
    const at = this.outputs.findIndex((o) => o.active);
    const next = this.outputs[(at + 1) % this.outputs.length];
    if (!next) return;
    try {
      const res = await postJSON("/api/sound/use", { id: next.id });
      if (!res.ok) return toast((await res.text()).trim(), 6000);
      this.update(await res.json());
      toast(t("Sound now comes out of {name}.", { name: next.name }), 4000);
      // Heard from where the sound now is.
      window.setTimeout(() => play("select"), 350);
    } catch {
      toast(UNREACHABLE, 5000);
    }
  }
}
