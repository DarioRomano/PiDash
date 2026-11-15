/** The volume of headphones or a speaker: a bar, while it is being changed. */
export class VolumeBar {
  private timer = 0;

  constructor(private root: HTMLElement) {}

  show(v: { name: string; volume: number }) {
    this.root.querySelector(".vol-name")!.textContent = v.name;
    this.root.querySelector(".vol-number")!.textContent = `${v.volume} %`;
    (this.root.querySelector(".vol-level") as HTMLElement).style.transform = `scaleX(${Math.max(0, Math.min(100, v.volume)) / 100})`;
    this.root.classList.add("shown");
    clearTimeout(this.timer);
    this.timer = window.setTimeout(() => this.root.classList.remove("shown"), 1800);
  }
}
