import { t } from "./i18n";
import { toast } from "./notify";

const TOLD_KEY = "pidash-update-told";

/**
 * When updates are not installed automatically, a new version is pointed out
 * where someone will see it: on the Settings tile, and once in words the
 * first time the app row is opened after it appeared. Not at the moment it is
 * found, which is as likely to be three in the morning.
 */
export class UpdateNotice {
  /** The version that waits to be installed; "" when none does. */
  version = "";

  constructor(private onChange: () => void) {
    void this.look();
    window.setInterval(() => void this.look(), 60 * 60 * 1000);
  }

  private async look() {
    try {
      const res = await fetch("/api/update", { cache: "no-store" });
      if (!res.ok) return;
      const st = (await res.json()) as { latest?: string; available: boolean; auto: boolean; installing: boolean };
      const found = st.available && !st.auto && !st.installing ? (st.latest ?? "") : "";
      if (found === this.version) return;
      this.version = found;
      this.onChange();
    } catch {
      /* the next look may work */
    }
  }

  /** Says it in words, once for each version. */
  tell() {
    if (!this.version) return;
    try {
      if (localStorage.getItem(TOLD_KEY) === this.version) return;
      localStorage.setItem(TOLD_KEY, this.version);
    } catch {
      return; // without a memory it would be said every time
    }
    toast(t("PiDash {version} is available. Install it under Settings, Updates.", { version: this.version }), 10000);
  }
}
