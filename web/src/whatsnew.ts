import { t } from "./i18n";

const SEEN_VERSION = "pidash-seen-version";

/** After an update, says once what the new version brought. A first start says nothing. */
export class WhatsNew {
  private opened = false;

  constructor(
    private root: HTMLElement,
    private onToggle: (open: boolean) => void,
  ) {}

  get isOpen() {
    return this.opened;
  }

  /** Looks whether this version was seen on this TV before, and says what is new if not. */
  tell(version: string, news: string[]) {
    let seen: string | null = null;
    try {
      seen = localStorage.getItem(SEEN_VERSION);
      localStorage.setItem(SEEN_VERSION, version);
    } catch {
      return;
    }
    if (seen === null || seen === version || news.length === 0 || this.opened) return;
    const list = document.createElement("ul");
    for (const line of news) {
      const li = document.createElement("li");
      li.textContent = line;
      list.appendChild(li);
    }
    const title = document.createElement("h2");
    title.textContent = t("New in {version}", { version });
    title.dataset.noTranslate = "";
    const foot = document.createElement("footer");
    foot.className = "help-foot";
    foot.textContent = "Press any key or click to close";
    this.root.replaceChildren(title, list, foot);
    this.root.classList.add("open");
    this.opened = true;
    this.onToggle(true);
  }

  close() {
    if (!this.opened) return;
    this.opened = false;
    this.root.classList.remove("open");
    this.onToggle(false);
  }
}
