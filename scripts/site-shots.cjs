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

// Films and folders for the Videos page: names with nothing in them, and
// beside each film a picture under its name, which is its cover.
const FILMS_ROOT = fs.mkdtempSync(path.join(os.tmpdir(), "pidash-films-"));
const FILMS = path.join(FILMS_ROOT, "Videos"); // the page is headed by the folder's name
fs.mkdirSync(FILMS);
const SAMPLES = fs
  .readdirSync(PHOTOS, { recursive: true })
  .filter((f) => /\.jpe?g$/i.test(f))
  .sort()
  .map((f) => path.join(PHOTOS, f));
[["Big Buck Bunny.mkv", 1.4], ["Elephants Dream.mkv", 0.9], ["Sintel.mkv", 2.1], ["Spring.mkv", 0.6], ["Tears of Steel.mkv", 3.2], ["The Daily Dweebs.mp4", 0.2]].forEach(([name, gb], i) => {
  fs.closeSync(fs.openSync(path.join(FILMS, name), "w"));
  fs.truncateSync(path.join(FILMS, name), Math.round(gb * 2 ** 30));
  fs.copyFileSync(SAMPLES[(i * 5 + 2) % SAMPLES.length], path.join(FILMS, name.replace(/\.\w+$/, ".jpg")));
});
for (const folder of ["Concerts", "Holidays 2024"]) fs.mkdirSync(path.join(FILMS, folder));

const CONFIG = `
clock_position = "bottom-left"

[videos]
source = "folder"
folders = ["${FILMS}"]

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
      for (let i = 0; i < 40; i++) {
        if ((await page.evaluate(() => document.getElementById("caption").textContent)).includes(date)) return;
        await remote.press("right");
        await page.waitForTimeout(2200); // the fade
      }
      throw new Error("no photo dated " + date);
    };
    const focusTile = async (label) => {
      for (let i = 0; i < 24; i++) {
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
    await remote.press("back");

    // The Videos page, with one film begun: it was started, and the player
    // remembers where it stopped (here the script writes what mpv would).
    await fetch(pi.base + "/api/videos/play", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ path: "0:Sintel.mkv" }) });
    await fetch(pi.base + "/api/apps/stop", { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" });
    const later = path.join(pi.dir, "data", "pidash", "videos", "mpv", "watch_later");
    fs.mkdirSync(later, { recursive: true });
    const mark = require("node:crypto").createHash("md5").update(path.join(FILMS, "Sintel.mkv")).digest("hex").toUpperCase();
    fs.writeFileSync(path.join(later, mark), "start=2275.0\n");
    await page.evaluate(() => localStorage.removeItem("pidash-videos-at"));
    await remote.press("select");
    await focusTile("Videos");
    await remote.press("select");
    await page.waitForFunction(() => document.querySelectorAll("#grid-page .card").length >= 8);
    await page.waitForFunction(() => [...document.querySelectorAll("#grid-page .card-art img")].every((i) => i.complete), null, { timeout: 60000 });
    await remote.press("down");
    await remote.press("right");
    await remote.press("right");
    await shot(page, "videos");
    await remote.press("menu");

    // The photo quiz: a question about the year, answered.
    await remote.press("select");
    await focusTile("Photo quiz");
    await remote.press("select");
    await page.waitForSelector("#quiz .qz-answer");
    for (let i = 0; i < 9; i++) {
      if (await page.evaluate(() => /year/.test(document.querySelector("#quiz .qz-question").textContent))) break;
      await remote.press("1");
      await remote.press("select");
    }
    await page.waitForFunction(() => document.querySelector("#quiz .qz-picture img").complete);
    await remote.press("down");
    await remote.press("select");
    await shot(page, "quiz");
    await page.context().close();

    // The settings, as they look on the TV.
    const settings = await openPage(pi.base, "/settings?from=tv#sound");
    const tv = remoteFor(settings, pi.base);
    // The highlight starts on the categories; Right goes into the one that is shown.
    await settings.waitForSelector("#groups .tv-focus");
    await settings.waitForTimeout(600);
    await tv.press("right");
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

    await settings.goto(pi.base + "/settings?from=tv#info");
    await settings.reload();
    await settings.waitForSelector("#groups .tv-focus");
    await settings.waitForTimeout(600);
    await tv.press("right");
    await tv.press("select"); // the first field: the on-screen keyboard
    await settings.waitForTimeout(500);
    await shot(settings, "keyboard");
  } finally {
    await closeBrowser();
    await pi.stop();
    fs.rmSync(PHOTOS, { recursive: true, force: true });
    fs.rmSync(FILMS_ROOT, { recursive: true, force: true });
  }
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
