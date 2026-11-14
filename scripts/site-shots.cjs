// Takes the screenshots shown on the project page (site/img).
//
// They are pictures of the real pages, driven the way a TV remote drives
// them, on PiDash's development mode with its sample photos and devices.
//
// Run with: make site-shots   (after "make build")
const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { startPiDash, openPage, closeBrowser, remoteFor } = require("../web/e2e/harness.cjs");

const OUT = path.join(__dirname, "..", "site", "img");
// Pictures to show behind the clock: drawn for the purpose, see sample_photos.py.
const PHOTOS = fs.mkdtempSync(path.join(os.tmpdir(), "pidash-samples-"));
execFileSync("python3", [path.join(__dirname, "sample_photos.py"), PHOTOS], { stdio: "inherit" });

const CONFIG = `
clock_position = "bottom-left"

[photos]
source = "folder"
motion = "off"

[photos.folder]
path = "${PHOTOS}"

[[apps]]
id = "youtube"
name = "YouTube"
url = "https://www.youtube.com/tv"

[[apps]]
id = "netflix"
name = "Netflix"
url = "https://www.netflix.com/browse"

[[apps]]
id = "spotify"
name = "Spotify"
url = "https://open.spotify.com/"

[[apps]]
id = "synology-photos"
name = "Synology Photos"
url = "http://nas.local:5000"
`;

async function shot(page, name) {
  await page.waitForTimeout(700); // let movement settle
  await page.screenshot({ path: path.join(OUT, name + ".jpg"), type: "jpeg", quality: 84 });
  console.log("wrote", name + ".jpg");
}

(async () => {
  const pi = await startPiDash(CONFIG);
  try {
    // Use every album of the sample pictures.
    const folders = (await (await fetch(pi.base + "/api/photos/folders")).json()).folders;
    await fetch(pi.base + "/api/photos/selection", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ folders, recursive: true }) });

    const page = await openPage(pi.base);
    const remote = remoteFor(page, pi.base);
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 30000 });
    await page.waitForTimeout(2500);
    // Flips through the photos until the one from that day is showing.
    const showPhoto = async (date) => {
      for (let i = 0; i < 12; i++) {
        if ((await page.evaluate(() => document.getElementById("caption").textContent)).includes(date)) return;
        await remote.press("right");
        await page.waitForTimeout(2200); // the fade
      }
      throw new Error("no photo dated " + date);
    };
    const focusTile = async (label) => {
      for (let i = 0; i < 14; i++) {
        if ((await page.evaluate(() => document.querySelector(".tile.focused .tile-label")?.textContent)) === label) return;
        await remote.press("right");
      }
      throw new Error("no tile " + label);
    };

    await remote.press("up"); // weather and the photo's caption too
    await showPhoto("August 14, 2025");
    await shot(page, "dashboard");

    await showPhoto("August 15, 2025");
    await remote.press("select"); // the app row
    await focusTile("Netflix");
    await shot(page, "apps");

    await remote.press("back");
    await remote.press("weather");
    await page.waitForTimeout(600);
    await shot(page, "weather");
    await remote.press("back");

    await showPhoto("June 21, 2025");
    await remote.press("select");
    await focusTile("Status");
    await remote.press("select");
    await page.waitForTimeout(2600);
    await shot(page, "status");
    await page.context().close();

    // The settings, as they look on the TV.
    const settings = await openPage(pi.base, "/settings?from=tv");
    const tv = remoteFor(settings, pi.base);
    await settings.waitForSelector("#bt-list li");
    const lit = () => settings.evaluate(() => document.querySelector(".tv-focus")?.id ?? "");
    for (let i = 0; i < 90; i++) {
      if ((await lit()) === "bt-search") break;
      await tv.press("down");
      if ((await lit()) === "bt-power") await tv.press("left");
    }
    await tv.press("select"); // look for new devices
    await settings.waitForFunction(() => document.querySelectorAll("#bt-found li").length === 2);
    await shot(settings, "bluetooth");

    await settings.goto(pi.base + "/settings?from=tv");
    await settings.waitForSelector(".tv-focus");
    await tv.press("select"); // the first field: the on-screen keyboard
    await settings.waitForTimeout(500);
    await shot(settings, "keyboard");
  } finally {
    await closeBrowser();
    await pi.stop();
    fs.rmSync(PHOTOS, { recursive: true, force: true });
  }
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
