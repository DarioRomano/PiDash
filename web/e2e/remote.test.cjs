// The TV remote must keep working. These tests press its buttons the three
// ways a press can reach a page (see harness.cjs) and check what happens on
// the dashboard, in the app row and on the settings page.
//
// Run with: make e2e
const { test, before, after } = require("node:test");
const assert = require("node:assert/strict");
const { startPiDash, openPage, closeBrowser, remoteFor, startTileServer, startRadarServer } = require("./harness.cjs");

const APPS = `
[[apps]]
id = "netflix"
name = "Netflix"
url = "https://www.netflix.com/browse"

[[apps]]
id = "youtube"
name = "YouTube"
url = "https://www.youtube.com/tv"
`;

let pi;
before(async () => {
  pi = await startPiDash(APPS);
});
after(async () => {
  await closeBrowser();
  await pi.stop();
});

// What the dashboard shows: "set" (as the settings say), "all" or "photo";
// and with it whether the caption and how much of the weather.
const view = (page) => page.evaluate(() => document.getElementById("overlay").dataset.view);
const shows = (page) => page.evaluate(() => { const o = document.getElementById("overlay").dataset; return [o.view, o.caption, o.weather]; });
// The bar that Up opens, with what can be done with the photo on screen.
const bar = (page) =>
  page.evaluate(() => {
    const b = document.getElementById("photo-bar");
    if (!b.classList.contains("open")) return null;
    const items = [...b.querySelectorAll(".pb-item")];
    return { items: items.map((e) => e.getAttribute("aria-label")), at: items.findIndex((e) => e.classList.contains("focused")), label: b.querySelector(".pb-label span").textContent, sub: b.querySelector(".pb-label small")?.textContent ?? "" };
  });
async function focusBar(page, remote, label) {
  for (let i = 0; i < 12; i++) {
    const now = await bar(page);
    assert.ok(now, "the photo bar is open");
    const want = now.items.indexOf(label);
    assert.ok(want >= 0, `the photo bar has no "${label}": ${now.items}`);
    if (now.at === want) return;
    await remote.press(now.at < want ? "right" : "left");
  }
  assert.fail(`the highlight never reached ${label}`);
}
const rowOpen = (page) => page.evaluate(() => document.getElementById("rail").classList.contains("open"));
const tile = (page) => page.evaluate(() => [...document.querySelectorAll(".tile")].findIndex((t) => t.classList.contains("focused")));
const photo = (page) =>
  page.evaluate(() => {
    const shown = [...document.querySelectorAll("#backdrop img.visible, #backdrop video.visible")].sort((a, b) => Number(b.style.zIndex) - Number(a.style.zIndex))[0];
    return shown ? shown.src : "";
  });

async function dashboard() {
  const page = await openPage(pi.base);
  await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
  await page.waitForTimeout(2000); // the first photo has faded in; the next press is not swallowed
  return page;
}

// Every way a press can arrive must do the same thing.
for (const how of ["press", "keyboard", "both"]) {
  const name = { press: "read by the backend only", keyboard: "as keys in the browser only", both: "reported twice, by browser and backend" }[how];

  test(`dashboard: Down steps through what is shown, Up opens the photo's bar (${name})`, async () => {
    const page = await dashboard();
    const remote = remoteFor(page, pi.base);
    assert.deepEqual(await shows(page), ["set", "off", "short"], "as the settings say: no caption, the weather in short");
    await remote[how]("down");
    assert.deepEqual(await shows(page), ["all", "on", "full"], "Down shows everything");
    await remote[how]("down");
    assert.equal(await view(page), "photo", "Down again leaves only the photo, by exactly one step");
    await remote[how]("down");
    assert.deepEqual(await shows(page), ["set", "off", "short"], "and round to what is set");

    // Up: what can be done with the photo. One press opens it, and it stays.
    await remote[how]("up");
    const opened = await bar(page);
    assert.deepEqual([opened?.items, opened?.at, opened?.label], [["Add to favourites", "About this photo", "More like this", "Hide this photo", "Time per photo", "Art instead of photos", "Bluetooth"], 0, "Add to favourites"]);
    assert.equal(await rowOpen(page), false, "the app row stays shut");
    await remote[how]("right");
    assert.deepEqual([(await bar(page)).at, (await bar(page)).label], [1, "About this photo"], "Right moves by exactly one");
    await remote[how]("left");
    await remote[how]("left"); // the end is the end
    assert.equal((await bar(page)).at, 0);
    await remote[how]("down");
    assert.equal(await bar(page), null, "any other button closes it");
    assert.equal(await view(page), "set", "and is not taken for a step through the views");
    assert.deepEqual(page.problems, []);
    await page.context().close();
  });

  test(`dashboard: Right and Left change the photo (${name})`, async () => {
    const page = await dashboard();
    const remote = remoteFor(page, pi.base);
    const first = await photo(page);
    await remote[how]("right");
    await page.waitForFunction((was) => [...document.querySelectorAll("#backdrop img.visible")].some((i) => i.src !== was), first, { timeout: 10000 });
    await page.context().close();
  });

  test(`app row: arrows move by exactly one tile (${name})`, async () => {
    const page = await dashboard();
    const remote = remoteFor(page, pi.base);
    await remote.press("select");
    assert.equal(await rowOpen(page), true, "OK opens the app row");
    const start = await tile(page);
    await remote[how]("right");
    assert.equal(await tile(page), start + 1, "Right moves one tile, not two and not none");
    await remote[how]("right");
    assert.equal(await tile(page), start + 2);
    await remote[how]("left");
    assert.equal(await tile(page), start + 1, "Left moves one tile back");
    await remote.press("back");
    assert.equal(await rowOpen(page), false, "Back closes the app row");
    assert.deepEqual(page.problems, []);
    await page.context().close();
  });
}

test("app row: OK on Settings opens the settings on the TV, Back returns", async () => {
  const page = await dashboard();
  const remote = remoteFor(page, pi.base);
  await remote.press("select");
  for (let i = 0; i < 12; i++) {
    if ((await page.evaluate(() => document.querySelector(".tile.focused .tile-label")?.textContent)) === "Settings") break;
    await remote.press("right");
  }
  await remote.press("select");
  await page.waitForURL(/\/settings\?from=tv/);
  await page.waitForSelector(".tv-focus");
  await remote.press("back");
  await page.waitForURL(pi.base + "/");
  await page.context().close();
});

const highlighted = (page) => page.evaluate(() => document.querySelector(".tv-focus")?.id ?? "");

// The settings on the TV open with the highlight on the bar of categories;
// Right goes into the one that is shown.
async function settingsOnTV(base, category) {
  const page = await openPage(base, "/settings?from=tv#" + category);
  const remote = remoteFor(page, base);
  await page.waitForSelector("#groups .tv-focus");
  await page.waitForTimeout(600);
  await remote.press("right");
  return { page, remote };
}

test("settings on the TV: categories at the left, shown as the highlight moves along them", async () => {
  const page = await openPage(pi.base, "/settings?from=tv");
  const remote = remoteFor(page, pi.base);
  await page.waitForSelector("#groups .tv-focus");
  await page.waitForTimeout(600);
  const category = () => page.evaluate(() => document.querySelector("#groups .tv-focus")?.textContent ?? "");
  const shown = () => page.evaluate(() => [...document.querySelectorAll("#main section.card")].filter((c) => c.getClientRects().length > 0).map((c) => c.querySelector("h2").textContent));
  assert.deepEqual(
    await page.evaluate(() => [...document.querySelectorAll("#groups button")].map((b) => b.textContent)),
    ["Screen", "Photos", "Weather and calendar", "Apps and radio", "Sound and Bluetooth", "Home and guests", "System"],
  );
  assert.equal(await category(), "Screen");
  assert.deepEqual(await shown(), ["Screen", "Performance", "Night"]);
  await remote.press("up"); // the top is the top
  assert.equal(await category(), "Screen");

  // Moving along the bar shows each category at once.
  await remote.press("down");
  assert.deepEqual([await category(), await shown()], ["Photos", ["Background photos"]]);
  await remote.press("down");
  assert.deepEqual([await category(), await shown()], ["Weather and calendar", ["Weather", "Calendar"]]);
  assert.match(page.url(), /#info$/, "the address remembers the category");
  for (let i = 0; i < 6; i++) await remote.press("down");
  assert.deepEqual([await category(), await shown()], ["System", ["Lock", "Updates", "Backup"]]);
  for (let i = 0; i < 4; i++) await remote.press("up");
  assert.deepEqual([await category(), await shown()], ["Weather and calendar", ["Weather", "Calendar"]]);

  // Right goes in. Up and Down stay inside, however close the bar is.
  await remote.press("right");
  assert.equal(await highlighted(page), "w-location");
  await remote.press("up");
  assert.equal(await highlighted(page), "w-location");
  await remote.press("down");
  assert.equal(await highlighted(page), "w-units");
  // On a list, Left means "the one before", not "out".
  await remote.press("right");
  assert.equal(await highlighted(page), "w-test");
  await remote.press("left");
  assert.equal(await highlighted(page), "w-units");
  const units = await page.inputValue("#w-units");
  await remote.press("left");
  assert.equal(await highlighted(page), "w-units");
  // Back comes out to the bar, to the category that is shown; Back again leaves.
  await remote.press("back");
  assert.equal(await category(), "Weather and calendar");
  assert.ok(page.url().includes("/settings"));
  // OK goes in as well.
  await remote.press("select");
  assert.equal(await highlighted(page), "w-location");
  // From a button at the left edge, Left leads to the bar.
  await page.selectOption("#w-units", units);
  await remote.press("back");
  await remote.press("down");
  await remote.press("down");
  assert.equal(await category(), "Sound and Bluetooth");
  await remote.press("right");
  await remote.press("down");
  assert.equal(await highlighted(page), "snd-down");
  await remote.press("left");
  assert.equal(await category(), "Sound and Bluetooth");
  await remote.press("back");
  await page.waitForURL(pi.base + "/");
  assert.deepEqual(page.problems, []);
  await page.context().close();
});

test("settings on a phone: the categories are a bar along the top", async () => {
  const { chromium } = require("playwright");
  const browser = await chromium.launch();
  try {
    const phone = await (await browser.newContext({ viewport: { width: 390, height: 844 } })).newPage();
    const problems = [];
    phone.on("pageerror", (e) => problems.push(String(e)));
    await phone.goto(pi.base + "/settings#bt-card"); // something in a category names the category
    await phone.waitForSelector("#main:not([hidden])");
    const shown = () => phone.evaluate(() => [...document.querySelectorAll("#main section.card")].filter((c) => c.getClientRects().length > 0).map((c) => c.querySelector("h2").textContent));
    assert.deepEqual(await shown(), ["Sound", "Bluetooth"]);
    const bar = await phone.evaluate(() => {
      const nav = document.getElementById("groups").getBoundingClientRect();
      const current = document.querySelector("#groups .current").getBoundingClientRect();
      return { height: nav.height, visible: current.left >= 0 && current.right <= innerWidth, wide: document.documentElement.scrollWidth <= innerWidth };
    });
    assert.ok(bar.height < 80, `the bar is one line, not ${bar.height}px high`);
    assert.ok(bar.visible, "the chosen category is scrolled into view");
    assert.ok(bar.wide, "the page does not scroll sideways");
    await phone.click('#groups button[data-group="system"]');
    assert.deepEqual(await shown(), ["Lock", "Updates", "Backup"]);
    assert.match(phone.url(), /#system$/);
    assert.deepEqual(problems, []);
  } finally {
    await browser.close();
  }
});

for (const how of ["press", "keyboard", "both"]) {
  const name = { press: "read by the backend only", keyboard: "as keys in the browser only", both: "reported twice" }[how];
  test(`settings on the TV: arrows move the highlight one control at a time (${name})`, async () => {
    const { page, remote } = await settingsOnTV(pi.base, "screen");
    assert.equal(await highlighted(page), "d-language", "the first control of the category is highlighted");
    await remote[how]("down");
    assert.equal(await highlighted(page), "d-position");
    await remote[how]("down");
    assert.equal(await highlighted(page), "d-size", "a second Down moves on by one, not two");
    await remote[how]("up");
    assert.equal(await highlighted(page), "d-position");
    await page.context().close();
    const weather = await settingsOnTV(pi.base, "info");
    await weather.remote[how]("down");
    assert.equal(await highlighted(weather.page), "w-units");
    await weather.remote[how]("right");
    assert.equal(await highlighted(weather.page), "w-test", "Right reaches the button beside the list");
    assert.deepEqual(weather.page.problems, []);
    await weather.page.context().close();
    return;
    assert.deepEqual(page.problems, []);
    await page.context().close();
  });
}

test("settings on the TV: the keyboard opens with OK, types, and closes with Back", async () => {
  const { page, remote } = await settingsOnTV(pi.base, "info");
  await remote.press("select");
  await page.waitForSelector(".tv-keyboard");
  const before = await page.evaluate(() => document.querySelector(".tv-keyboard .tv-focus").textContent);
  await remote.press("right");
  assert.notEqual(await page.evaluate(() => document.querySelector(".tv-keyboard .tv-focus").textContent), before, "arrows move across the keys");
  await remote.press("select");
  assert.equal((await page.inputValue("#w-location")).length, 1, "OK types the highlighted key once");
  await remote.press("back");
  assert.equal(await page.locator(".tv-keyboard").count(), 0, "Back closes the keyboard");
  assert.equal(page.url().includes("/settings"), true, "and does not leave the page");
  await page.context().close();
});

test("settings on the TV: a keyboard's arrows move the cursor in a text field, not the highlight", async () => {
  const { page, remote } = await settingsOnTV(pi.base, "info");
  await page.fill("#w-location", "Bern");
  await page.evaluate(() => document.getElementById("w-location").setSelectionRange(4, 4));
  await remote.both("left"); // a real keyboard: the browser gets the key and the backend reports it too
  assert.equal(await highlighted(page), "w-location", "the highlight stays on the field");
  assert.equal(await page.evaluate(() => document.getElementById("w-location").selectionStart), 3, "the cursor moved one letter");
  await page.context().close();
});

test("every page opens without errors in every language", async () => {
  const titles = { en: "Settings", de: "Einstellungen", fr: "Réglages", it: "Impostazioni" };
  for (const [language, title] of Object.entries(titles)) {
    // The language is a setting on the Pi, so each one gets a Pi of its own.
    const other = await startPiDash(`language = "${language}"\n` + APPS);
    try {
      for (const where of ["/", "/settings?from=tv", "/settings", "/photos?from=tv"]) {
        const page = await openPage(other.base, where);
        await page.waitForTimeout(900);
        assert.deepEqual(page.problems, [], `${where} in ${language}`);
        if (where.startsWith("/settings")) assert.equal(await page.locator("h1").innerText(), title, `${where} in ${language}`);
        await page.context().close();
      }
    } finally {
      await other.stop();
    }
  }
});

// --- Moving apps ---------------------------------------------------------------

const tiles = (page) => page.evaluate(() => [...document.querySelectorAll(".tile .tile-label")].map((t) => t.textContent));
const carried = (page) => page.evaluate(() => document.querySelector(".tile.moving .tile-label")?.textContent ?? "");
const savedOrder = async (base) => (await (await fetch(base + "/api/apps")).json()).apps.map((a) => a.name);

async function focusTile(page, remote, label) {
  for (let i = 0; i < 40; i++) {
    const [at, want] = await page.evaluate((label) => {
      const all = [...document.querySelectorAll(".tile")];
      return [all.findIndex((t) => t.classList.contains("focused")), all.findIndex((t) => t.querySelector(".tile-label")?.textContent === label)];
    }, label);
    if (want < 0) break;
    if (at === want) return;
    await remote.press(at < want ? "right" : "left");
  }
  assert.fail(`no tile called ${label}`);
}

for (const how of ["press", "keyboard", "both"]) {
  const name = { press: "read by the backend only", keyboard: "as keys in the browser only", both: "reported twice" }[how];

  test(`app row: Up picks an app up, the arrows carry it, OK puts it down (${name})`, async () => {
    // A Pi of its own: the order it ends up with must not matter to other tests.
    const own = await startPiDash(APPS);
    try {
      const page = await openPage(own.base);
      await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
      await page.waitForTimeout(2000);
      const remote = remoteFor(page, own.base);
      await remote.press("select");
      await focusTile(page, remote, "Netflix");
      const before = await tiles(page);
      const at = before.indexOf("Netflix");
      assert.equal(before[at + 1], "YouTube");

      // Up on a tile asks what to do with it; the first choice is to move it.
      await remote[how]("up");
      assert.deepEqual(await page.evaluate(() => [document.querySelector("#menu h2")?.textContent, ...[...document.querySelectorAll("#menu .menu-label")].map((e) => e.textContent)]), ["Netflix", "Move", "Remove from the app row", "Rename or change it"]);
      assert.equal(await carried(page), "");
      await remote.press("select");
      assert.equal(await carried(page), "Netflix", "Move picks the selected app up");
      await remote[how]("right");
      let now = await tiles(page);
      assert.deepEqual([now[at], now[at + 1]], ["YouTube", "Netflix"], "Right carries it one place, not two and not none");
      await remote[how]("right");
      assert.deepEqual(await tiles(page), now, "it cannot be carried past the last app");
      assert.deepEqual(await savedOrder(own.base), ["Netflix", "YouTube"], "nothing is saved while it is in the air");
      await remote.press("select");
      assert.equal(await carried(page), "", "OK puts it down");
      await page.waitForTimeout(400);
      assert.deepEqual(await savedOrder(own.base), ["YouTube", "Netflix"], "the new order is saved on the Pi");
      assert.equal(await rowOpen(page), true, "the app row stays open");

      // Back while carrying leaves everything where it was.
      await remote[how]("up");
      await remote.press("select");
      await remote[how]("left");
      now = await tiles(page);
      assert.deepEqual([now[at], now[at + 1]], ["Netflix", "YouTube"]);
      await remote.press("back");
      now = await tiles(page);
      assert.deepEqual([now[at], now[at + 1]], ["YouTube", "Netflix"], "Back returns it to where it was picked up");
      assert.equal(await rowOpen(page), true, "Back only ends the move; the row stays open");
      await page.waitForTimeout(300);
      assert.deepEqual(await savedOrder(own.base), ["YouTube", "Netflix"]);

      // Tiles that are not apps stay where they are.
      await focusTile(page, remote, "Settings");
      await remote[how]("up");
      assert.equal(await carried(page), "", "only apps can be picked up");

      // The order is still there after the page is loaded again.
      await page.reload();
      await page.waitForTimeout(1200);
      await remote.press("select");
      now = await tiles(page);
      assert.deepEqual([now[at], now[at + 1]], ["YouTube", "Netflix"]);
      assert.deepEqual(page.problems, []);
      await page.context().close();
    } finally {
      await own.stop();
    }
  });
}

// --- Status --------------------------------------------------------------------

test("app row: OK on Status shows how PiDash is doing; arrows leave it open, Back closes it", async () => {
  const page = await dashboard();
  const remote = remoteFor(page, pi.base);
  const open = () => page.evaluate(() => document.getElementById("status-panel").classList.contains("open"));
  await remote.press("select");
  await focusTile(page, remote, "Status");
  await remote.press("select");
  assert.equal(await open(), true, "OK on the Status tile opens the status page");
  assert.equal(await rowOpen(page), false, "the app row makes room for it");
  await page.waitForFunction(() => /\d+ in the app row/.test(document.getElementById("status-panel").innerText));
  const text = await page.evaluate(() => document.getElementById("status-panel").innerText);
  for (const part of ["This Pi", "PiDash", "TV and remote", "Photos", "Weather", "Recent problems"]) assert.ok(text.includes(part), `the status page lacks "${part}":\n${text}`);
  // The screen is measured while the page is open: its size, and how often it is drawn.
  await page.waitForFunction(() => /Screen\s+1920 × 1080 · drawn (only )?\d+ times a second/.test(document.getElementById("status-panel").innerText), null, { timeout: 8000 });
  // Buttons can be tried out without the page going away.
  for (const action of ["left", "right", "up", "down"]) await remote.press(action);
  assert.equal(await open(), true, "arrows leave the status page open");
  await remote.press("back");
  assert.equal(await open(), false, "Back closes it");
  assert.equal(await view(page), "set", "the arrows pressed meanwhile changed nothing underneath");
  assert.equal(await bar(page), null);
  assert.deepEqual(page.problems, []);
  await page.context().close();
});

// --- Emulators -----------------------------------------------------------------

test("settings: emulators are offered, with how to install one that is missing", async () => {
  const page = await openPage(pi.base, "/settings#apps");
  await page.waitForSelector("#add-kind option", { state: "attached" });
  const choices = await page.evaluate(() => [...document.querySelectorAll("#add-kind option")].map((o) => o.textContent));
  for (const emulator of ["RetroArch", "PPSSPP", "Dolphin", "ScummVM"]) assert.ok(choices.some((c) => c.startsWith(emulator)), `${emulator} is not offered: ${choices}`);
  // Nothing is installed on the machine running the tests.
  await page.selectOption("#add-kind", "__emulator:retroarch");
  assert.match(await page.locator("#add-desc").innerText(), /sudo apt install retroarch/);
  const before = await page.locator("#app-list li").count();
  assert.equal(before, 2, "the two apps of the test setup are listed");
  await page.click("#add-btn");
  assert.match(await page.locator("#add-result").innerText(), /not installed/);
  assert.equal(await page.locator("#app-list li").count(), before, "an emulator that is not installed is not added");
  assert.deepEqual(page.problems, []);
  await page.context().close();
});

// --- The picture reaches every edge ----------------------------------------------

test("burn-in protection moves texts and tiles, never the photo; the darkening always covers the screen", async () => {
  // A zoomed photo reaches past the screen's edge. If the darkening on top of
  // it moved away from an edge with the texts, a lighter strip of photo would
  // show there. It does move with them (the Pi then lays one picture over
  // the photo instead of two), so it has to reach past every edge.
  const page = await dashboard();
  const box = (id) =>
    page.evaluate((id) => {
      const r = document.getElementById(id).getBoundingClientRect();
      return [r.left, r.top, r.right, r.bottom].map((v) => Math.round(v * 100) / 100);
    }, id);
  const clockBefore = await box("clock");
  await page.evaluate(() => {
    const stage = document.getElementById("stage");
    stage.style.setProperty("--shift-x", "4px");
    stage.style.setProperty("--shift-y", "-3px");
  });
  await page.waitForTimeout(1600); // the move takes 1.2 s
  const screen = [0, 0, 1920, 1080];
  assert.deepEqual(await box("backdrop"), screen, "the photo layer still covers the whole screen");
  const scrim = await box("scrim");
  assert.ok(scrim[0] <= -10 && scrim[1] <= -10 && scrim[2] >= 1930 && scrim[3] >= 1090, `the darkening reaches well past every edge of the screen: ${scrim}`);
  const clockAfter = await box("clock");
  assert.deepEqual([clockAfter[0] - clockBefore[0], clockAfter[1] - clockBefore[1]], [4, -3], "the clock did move");
  // Pages that cover the screen move too, so their background reaches past their edges.
  for (const id of ["help", "status-panel", "weather-panel"]) {
    const spread = await page.evaluate((id) => getComputedStyle(document.getElementById(id)).boxShadow, id);
    assert.match(spread, /\b2[0-9]px$/, `${id} needs a background that reaches past the screen's edge, got "${spread}"`);
  }
  await page.context().close();
});

// --- Bluetooth -------------------------------------------------------------------

test("settings on the TV: Bluetooth devices are found, paired and forgotten with the remote", async () => {
  // A Pi of its own: the devices it ends up with must not matter to other tests.
  const own = await startPiDash(APPS);
  try {
    const { page, remote } = await settingsOnTV(own.base, "sound");
    await page.waitForSelector("#bt-list li");
    // What the highlight is on: "<button>@<device>".
    const lit = () =>
      page.evaluate(() => {
        const e = document.querySelector(".tv-focus");
        return e ? `${e.id || e.dataset.role}@${e.closest("li")?.dataset.address ?? ""}` : "";
      });
    const rows = (list) => page.evaluate((list) => [...document.querySelectorAll(`#${list} li`)].map((li) => li.innerText.replace(/\n+/g, " / ")), list);
    const until = async (press, reached, what) => {
      for (let i = 0; i < 90; i++) {
        if (reached(await lit())) return;
        await remote.press(press);
      }
      assert.fail(`the highlight never reached ${what}; it is on "${await lit()}"`);
    };

    assert.deepEqual(await rows("bt-list"), ["Living room speaker / Headphones or speaker · not connected / Connect / Forget", "Xbox Wireless Controller / Game controller · connected · battery 80 % / Disconnect / Forget"]);
    assert.deepEqual(await rows("bt-found"), [], "nothing is listed as nearby before searching");

    // Down the page to the paired devices, and on to the buttons below them.
    await until("down", (l) => l.includes("@AA:"), "a paired device");
    await until("down", (l) => l.startsWith("bt-"), "the buttons under the devices");
    if ((await lit()).startsWith("bt-power")) await remote.press("left");
    assert.equal(await lit(), "bt-search@");
    await remote.press("select");
    await page.waitForFunction(() => document.querySelectorAll("#bt-found li").length === 2);
    assert.equal(await page.locator("#bt-search").innerText(), "Stop looking");

    // The found devices are the next rows down, although their buttons sit
    // at the far right: no row is jumped over.
    await remote.press("down");
    assert.equal(await lit(), "toggle@AA:00:00:00:00:03", "Down from the search button goes to the first device found");
    await remote.press("select");
    await page.waitForFunction(() => /is paired/.test(document.getElementById("bt-result").textContent));
    assert.ok((await rows("bt-list")).includes("8BitDo Pro 2 / Game controller · connected / Disconnect / Forget"), `paired devices: ${await rows("bt-list")}`);
    assert.deepEqual(await rows("bt-found"), ["Keyboard K380 / Keyboard · nearby / Pair"]);
    // The row moved to the other list; the highlight went with it instead of
    // jumping to the top of the page.
    assert.equal(await lit(), "toggle@AA:00:00:00:00:03", "the highlight stays with the device that was paired");

    await remote.press("right");
    assert.equal(await lit(), "forget@AA:00:00:00:00:03");
    await remote.press("select");
    await page.waitForFunction(() => /was forgotten/.test(document.getElementById("bt-result").textContent));
    assert.equal((await rows("bt-list")).length, 2, "the device is gone from the paired ones");
    assert.equal(await lit(), "toggle@AA:00:00:00:00:03", "the highlight is still on that device, now among those nearby");

    // Switching Bluetooth off empties the lists and leaves a way back on.
    await until("up", (l) => l.startsWith("bt-"), "the buttons");
    if ((await lit()).startsWith("bt-search")) await remote.press("right");
    await remote.press("select");
    await page.waitForFunction(() => document.getElementById("bt-power").textContent === "Switch Bluetooth on");
    assert.deepEqual(await rows("bt-list"), []);
    assert.equal(await lit(), "bt-power@", "the highlight stays on the switch");
    assert.equal(await page.locator("#bt-status").innerText(), "Bluetooth is switched off.");
    assert.deepEqual(page.problems, []);
    await page.context().close();
  } finally {
    await own.stop();
  }
});

// --- Holding a direction -----------------------------------------------------------

// What the backend sends while a direction is held down on the remote.
async function held(page, base, action, times) {
  for (let i = 0; i < times; i++) {
    const res = await fetch(base + "/api/test/remote", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ action, repeat: true }) });
    assert.equal(res.status, 204);
    await page.waitForTimeout(120);
  }
}

test("a held direction runs along the app row, but does not race through photos", async () => {
  const page = await dashboard();
  const remote = remoteFor(page, pi.base);
  const before = await photo(page);
  await held(page, pi.base, "right", 3);
  await page.waitForTimeout(600);
  assert.equal(await photo(page), before, "holding Right on the dashboard must not flip through photos");
  assert.equal(await view(page), "set");
  await held(page, pi.base, "down", 2);
  assert.equal(await view(page), "set", "holding Down changes nothing either");

  await remote.press("select");
  const start = await tile(page);
  await remote.press("right"); // the press itself
  await held(page, pi.base, "right", 3); // and three repeats while it is held
  assert.equal(await tile(page), start + 4, "each repeat moves one tile further");
  await held(page, pi.base, "left", 2);
  assert.equal(await tile(page), start + 2);
  assert.deepEqual(page.problems, []);
  await page.context().close();
});

test("settings on the TV: a held direction keeps the highlight moving", async () => {
  const { page, remote } = await settingsOnTV(pi.base, "screen");
  const seen = [await highlighted(page)];
  await remote.press("down");
  seen.push(await highlighted(page));
  for (let i = 0; i < 3; i++) {
    await held(page, pi.base, "down", 1);
    seen.push(await highlighted(page));
  }
  assert.equal(new Set(seen).size, 5, `the highlight should have been on five different controls: ${seen}`);
  // The keyboard's own repeats are left out once the backend sends them, so
  // a held key does not move at two speeds at once.
  const at = await highlighted(page);
  await page.keyboard.down("ArrowDown");
  await page.keyboard.down("ArrowDown"); // what a held key looks like to the page: the same key down again
  await page.keyboard.down("ArrowDown");
  await page.keyboard.up("ArrowDown");
  await page.waitForTimeout(200);
  const after = await highlighted(page);
  await remote.press("up");
  assert.equal(await highlighted(page), at, `three key-downs of one held key moved by one control only (it went from ${at} to ${after})`);
  await page.context().close();
});

// --- Sounds ------------------------------------------------------------------------

test("the remote is answered with small sounds, unless they are switched off", async () => {
  const sounds = (page) => page.evaluate(() => window.pidashSounds.join(" "));
  const page = await dashboard();
  const remote = remoteFor(page, pi.base);
  await page.evaluate(() => (window.pidashSounds = []));
  for (const action of ["select", "right", "left", "left", "back", "down", "down"]) await remote.press(action);
  // Open the row, move, move back, bump into its start, close; then two steps through what is shown.
  assert.equal(await sounds(page), "open move move edge back move move");
  assert.deepEqual(page.problems, []);
  await page.context().close();

  const quiet = await startPiDash("sounds = false\n" + APPS);
  try {
    const page = await openPage(quiet.base);
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
    await page.waitForTimeout(2000);
    await page.evaluate(() => (window.pidashSounds = []));
    const remote = remoteFor(page, quiet.base);
    for (const action of ["select", "right", "back"]) await remote.press(action);
    assert.equal(await rowOpen(page), false, "the buttons still work");
    assert.equal(await sounds(page), "", "no sounds with sounds = false");
    await page.context().close();
  } finally {
    await quiet.stop();
  }
});

// --- Sound output --------------------------------------------------------------------

test("settings: the sound output is chosen, and its volume set, at once", async () => {
  const own = await startPiDash(APPS);
  try {
    const page = await openPage(own.base, "/settings#sound");
    await page.waitForSelector("#snd-output option", { state: "attached" });
    const outputs = async () => Object.fromEntries((await (await fetch(own.base + "/api/sound")).json()).outputs.map((o) => [o.id, o]));
    assert.deepEqual(await page.evaluate(() => [...document.querySelectorAll("#snd-output option")].map((o) => o.textContent)), ["TV (HDMI)", "Living room speaker"]);
    assert.equal(await page.locator("#snd-level").innerText(), "100 %");

    await page.selectOption("#snd-output", "speaker");
    await page.waitForFunction(() => document.getElementById("snd-level").textContent === "40 %");
    assert.equal((await outputs()).speaker.active, true, "the speaker is in use without pressing Save");

    for (let i = 0; i < 3; i++) await page.click("#snd-up");
    await page.click("#snd-down");
    await page.waitForFunction(() => document.getElementById("snd-level").textContent === "50 %");
    await page.waitForTimeout(300);
    assert.equal((await outputs()).speaker.volume, 50);

    await page.click("#snd-mute");
    await page.waitForFunction(() => document.getElementById("snd-mute").textContent === "Sound on");
    assert.equal((await outputs()).speaker.muted, true);
    // Turning it up says "I want to hear something": that ends the muting.
    await page.click("#snd-up");
    await page.waitForFunction(() => document.getElementById("snd-level").textContent === "55 %");
    await page.waitForTimeout(300);
    const now = (await outputs()).speaker;
    assert.deepEqual([now.muted, now.volume], [false, 55]);

    await page.click("#snd-test");
    await page.waitForFunction(() => /Did you hear it/.test(document.getElementById("snd-result").textContent));
    assert.equal(await page.evaluate(() => document.getElementById("save").disabled), true, "nothing here waits for Save");
    assert.deepEqual(page.problems, []);
    await page.context().close();
  } finally {
    await own.stop();
  }
});

// --- Batteries ----------------------------------------------------------------------

test("a controller's battery is shown with the Bluetooth devices and on the status page", async () => {
  const page = await openPage(pi.base, "/settings#sound");
  await page.waitForSelector("#bt-list li");
  const rows = await page.evaluate(() => [...document.querySelectorAll("#bt-list li .what")].map((w) => w.innerText.replace(/\n+/g, " / ")));
  assert.ok(rows.includes("Xbox Wireless Controller / Game controller · connected · battery 80 %"), `rows: ${rows}`);
  assert.ok(rows.includes("Living room speaker / Headphones or speaker · not connected"), "a device that reports no battery shows none");
  await page.context().close();

  const status = await (await fetch(pi.base + "/api/status")).json();
  assert.deepEqual(
    status.bluetooth.map((d) => [d.name, d.battery]),
    [["Xbox Wireless Controller", 80]],
    "the status page lists what is connected",
  );
});

// --- Headphones ----------------------------------------------------------------------

test("the sound follows headphones, and the Sound tile moves it by hand", async () => {
  const own = await startPiDash(APPS);
  try {
    const headphones = async (connected) => {
      const res = await fetch(own.base + "/api/test/headphones", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ connected }) });
      assert.equal(res.status, 204);
    };
    const inUse = async () => (await (await fetch(own.base + "/api/sound")).json()).outputs.find((o) => o.active)?.name;
    await headphones(false); // the evening starts with the TV alone
    const page = await openPage(own.base);
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
    await page.waitForTimeout(2000);
    const remote = remoteFor(page, own.base);
    await remote.press("select");
    assert.ok(!(await tiles(page)).includes("Sound"), "with one output there is nothing to choose, and no Sound tile");

    // Headphones are switched on: the sound goes there by itself, and the tile appears.
    await headphones(true);
    await page.waitForFunction(() => [...document.querySelectorAll(".tile")].some((t) => t.querySelector(".tile-label").textContent === "Sound"));
    assert.equal(await inUse(), "Living room speaker");
    const sub = () => page.evaluate(() => [...document.querySelectorAll(".tile")].find((t) => t.querySelector(".tile-label").textContent === "Sound")?.querySelector(".tile-sub").textContent);
    assert.equal(await sub(), "Living room speaker", "the tile says where the sound is");

    // OK on the tile moves it to the TV, although the headphones are still on.
    await focusTile(page, remote, "Sound");
    await remote.press("select");
    await page.waitForFunction(() => /Sound now comes out of TV/.test(document.getElementById("toast").textContent));
    assert.equal(await inUse(), "TV (HDMI)");
    assert.equal(await sub(), "TV (HDMI)");
    assert.equal(await rowOpen(page), true, "the app row stays open");
    // And back again.
    await remote.press("select");
    await page.waitForFunction(() => /Sound now comes out of Living room speaker/.test(document.getElementById("toast").textContent));
    assert.equal(await inUse(), "Living room speaker");

    // Headphones off: the sound is on the TV again and the tile is gone.
    await headphones(false);
    await page.waitForFunction(() => ![...document.querySelectorAll(".tile .tile-label")].some((l) => l.textContent === "Sound"));
    assert.equal(await inUse(), "TV (HDMI)");
    assert.deepEqual(page.problems, []);
    await page.context().close();
  } finally {
    await own.stop();
  }
});

// --- A new version -----------------------------------------------------------------

test("a new version is pointed out on the Settings tile, and said once", async () => {
  const page = await openPage(pi.base, "/healthz"); // any page of this address, to set things up before the dashboard loads
  let available = true;
  await page.route("**/api/update", (route) =>
    route.fulfill({ json: { current: "v1.0.0", latest: "v9.9.9", available, auto: false, possible: true, installing: false } }),
  );
  await page.evaluate(() => localStorage.removeItem("pidash-update-told"));
  await page.goto(pi.base + "/");
  await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
  await page.waitForTimeout(2000);
  const remote = remoteFor(page, pi.base);
  const toast = () => page.evaluate(() => (document.getElementById("toast").classList.contains("visible") ? document.getElementById("toast").textContent : ""));
  const settingsSub = () => page.evaluate(() => [...document.querySelectorAll(".tile")].find((t) => t.querySelector(".tile-label").textContent === "Settings")?.querySelector(".tile-sub").textContent);

  assert.equal(await toast(), "", "nothing is said while nobody is using the TV");
  await remote.press("select");
  assert.match(await toast(), /PiDash v9\.9\.9 is available\. Install it under Settings, Updates\./);
  assert.equal(await settingsSub(), "Update available");
  assert.equal(await tile(page), 0, "the row still opens on its first tile");

  // Said once: opening the row again, or loading the page again, does not repeat it.
  await remote.press("back");
  await page.evaluate(() => document.getElementById("toast").classList.remove("visible"));
  await remote.press("select");
  assert.equal(await toast(), "");
  assert.equal(await settingsSub(), "Update available", "the tile keeps saying it");
  await page.reload();
  await page.waitForTimeout(1500);
  await remote.press("select");
  assert.equal(await toast(), "");

  // With automatic updates on there is nothing to point out.
  available = false;
  await page.reload();
  await page.waitForTimeout(1500);
  await remote.press("select");
  assert.equal(await settingsSub(), "");
  assert.deepEqual(page.problems, []);
  await page.context().close();
});

// --- Still alive ---------------------------------------------------------------------

test("the dashboard and the settings on the TV report in, so a frozen page can be noticed", async () => {
  for (const where of ["/", "/settings?from=tv"]) {
    const page = await openPage(pi.base, "/healthz");
    let reports = 0;
    await page.route("**/api/alive", (route) => {
      reports++;
      return route.fulfill({ status: 204 });
    });
    await page.goto(pi.base + where);
    await page.waitForTimeout(1200);
    assert.equal(reports, 1, `${where} reports in when it loads`);
    await page.context().close();
  }
  // The settings page on a phone is not the TV: it stays quiet.
  const phone = await openPage(pi.base, "/healthz");
  let reports = 0;
  await phone.route("**/api/alive", (route) => {
    reports++;
    return route.fulfill({ status: 204 });
  });
  await phone.goto(pi.base + "/settings");
  await phone.waitForTimeout(1200);
  assert.equal(reports, 0);
  await phone.context().close();
});

// --- Clock styles --------------------------------------------------------------------

test("the clock can be numbers or a clock with hands", async () => {
  const shown = (page, id) => page.evaluate((id) => getComputedStyle(document.getElementById(id)).display !== "none", id);
  const page = await dashboard();
  assert.deepEqual([await shown(page, "time"), await shown(page, "dial")], [true, false], "numbers unless chosen otherwise");
  await page.context().close();

  const own = await startPiDash('clock_style = "analog"\n' + APPS);
  try {
    const page = await openPage(own.base);
    await page.waitForFunction(() => document.getElementById("stage").dataset.clockStyle === "analog");
    assert.deepEqual([await shown(page, "time"), await shown(page, "dial")], [false, true], "hands instead of numbers");
    // The hands point at the time: the minute hand turns six degrees a minute.
    const [minute, now] = await page.evaluate(() => [document.getElementById("dial-minute").getAttribute("transform"), new Date().getMinutes()]);
    assert.ok([`rotate(${now * 6})`, `rotate(${((now + 59) % 60) * 6})`].includes(minute), `minute hand: ${minute} at minute ${now}`);
    assert.equal(await page.evaluate(() => document.querySelectorAll("#dial-marks line").length), 12);
    assert.equal(await shown(page, "date"), true, "the date stays");
    assert.deepEqual(page.problems, []);
    await page.context().close();
  } finally {
    await own.stop();
  }
});

// --- Wi-Fi for visitors ------------------------------------------------------------

test("the Guest Wi-Fi tile shows a code to scan, and forgets the password when closed", async () => {
  const page = await dashboard();
  const remote = remoteFor(page, pi.base);
  await remote.press("select");
  assert.ok(!(await tiles(page)).includes("Guest Wi-Fi"), "no tile without a network for visitors");
  await page.context().close();

  const own = await startPiDash(APPS + '\n[guest_wifi]\nname = "Guests"\npassword = "welcome123"\n');
  try {
    const page = await openPage(own.base);
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
    await page.waitForTimeout(2000);
    const remote = remoteFor(page, own.base);
    await remote.press("select");
    await focusTile(page, remote, "Guest Wi-Fi");
    await remote.press("select");
    await page.waitForSelector("#wifi-panel.open svg");
    const text = await page.evaluate(() => document.getElementById("wifi-panel").innerText);
    assert.ok(text.includes("Guests") && text.includes("welcome123"), `the page should name the network and its password:\n${text}`);
    // A code is a square of 21, 25, 29, … squares with a margin of four around it.
    const size = await page.evaluate(() => Number(document.querySelector("#wifi-panel svg").getAttribute("viewBox").split(" ")[2]) - 8);
    assert.ok(size >= 21 && (size - 17) % 4 === 0, `a code of ${size} squares`);
    assert.equal(await rowOpen(page), false);
    await remote.press("left"); // any button
    assert.equal(await page.evaluate(() => document.getElementById("wifi-panel").classList.contains("open")), false);
    assert.equal(await page.evaluate(() => document.getElementById("wifi-panel").textContent), "", "the password does not stay in the page");
    assert.deepEqual(page.problems, []);
    await page.context().close();
  } finally {
    await own.stop();
  }
});

// --- Now playing ----------------------------------------------------------------------

test("music that plays is shown, and the remote's play and skip buttons control it", async () => {
  const own = await startPiDash(APPS + '\n[[apps]]\nid = "spotify"\nname = "Spotify"\nurl = "https://open.spotify.com/"\nbackground = true\n');
  try {
    const post = (where, body) => fetch(own.base + where, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body ?? {}) });
    const page = await openPage(own.base);
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
    await page.waitForTimeout(2000);
    const remote = remoteFor(page, own.base);
    const card = () => page.evaluate(() => (document.getElementById("playing").hidden ? "" : document.getElementById("playing").innerText.replace(/\n+/g, " | ")));
    assert.equal(await card(), "", "nothing is shown while nothing plays");

    // Without music the skip button goes to the next photo.
    const before = await photo(page);
    await remote.press("next");
    await page.waitForFunction((b) => {
      const shown = [...document.querySelectorAll("#backdrop img.visible, #backdrop video.visible")].sort((x, y) => Number(y.style.zIndex) - Number(x.style.zIndex))[0];
      return shown && shown.src !== b;
    }, before);

    await post("/api/test/playing", { player: "Chromium", title: "Blue in Green", artist: "Miles Davis", album: "Kind of Blue", playing: true, canSkip: true });
    await page.waitForFunction(() => !document.getElementById("playing").hidden);
    assert.equal(await card(), "♪ | Blue in Green | Miles Davis");

    // With music, the same buttons belong to the music.
    await page.waitForTimeout(2200);
    const during = await photo(page);
    await remote.press("next");
    await page.waitForTimeout(600);
    assert.equal(await photo(page), during, "skip goes to the music, not to the photos");
    await remote.press("playpause");
    await page.waitForFunction(() => document.getElementById("playing").classList.contains("paused"));
    assert.equal(await card(), "♪ | Blue in Green | Miles Davis · paused");
    await remote.press("playpause");
    await page.waitForFunction(() => !document.getElementById("playing").classList.contains("paused"));

    // A music app that is left keeps playing behind the dashboard.
    assert.equal((await post("/api/apps/start", { id: "spotify" })).status, 200);
    assert.equal((await post("/api/test/leave")).status, 204); // Back held on the remote
    const apps = async () => (await (await fetch(own.base + "/api/apps")).json());
    assert.deepEqual([(await apps()).running, (await apps()).background], ["", "spotify"]);
    await remote.press("select");
    const sub = (label) => page.evaluate((label) => [...document.querySelectorAll(".tile")].find((t) => t.querySelector(".tile-label").textContent === label)?.querySelector(".tile-sub").textContent, label);
    assert.equal(await sub("Spotify"), "Playing · OK to open");
    assert.equal(await page.evaluate(() => document.querySelector(".tile.focused .tile-label").textContent), "Spotify", "the row opens on it");
    // OK brings it back to the front; it was never closed.
    await remote.press("select");
    await page.waitForTimeout(400);
    assert.deepEqual([(await apps()).running, (await apps()).background], ["spotify", undefined]);
    // Behind the dashboard again, and ended for good with its own tile.
    await post("/api/test/leave");
    await page.waitForTimeout(300);
    await remote.press("select");
    await focusTile(page, remote, "Stop Spotify");
    await remote.press("select");
    await page.waitForFunction(() => ![...document.querySelectorAll(".tile .tile-label")].some((l) => l.textContent === "Stop Spotify"));
    assert.deepEqual([(await apps()).running, (await apps()).background], ["", undefined]);

    // The music ends.
    await fetch(own.base + "/api/test/playing", { method: "POST", headers: { "Content-Type": "application/json" }, body: "null" });
    await page.waitForFunction(() => document.getElementById("playing").hidden);
    assert.deepEqual(page.problems, []);
    await page.context().close();
  } finally {
    await own.stop();
  }
});

// --- Games ------------------------------------------------------------------------------

test("the games of the PC, from Moonlight, can be tiles that start with one press", async () => {
  const fs = require("node:fs");
  const os = require("node:os");
  const path = require("node:path");
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "pidash-home-"));
  const conf = path.join(home, ".config", "Moonlight Game Streaming Project");
  fs.mkdirSync(conf, { recursive: true });
  fs.writeFileSync(
    path.join(conf, "Moonlight.conf"),
    "[hosts]\n1\\hostname=LIVING-PC\n1\\uuid=3F2A9C1E-77AB-4C0D-9E55-0A1B2C3D4E5F\n1\\mac=@ByteArray(\\x1c\\x1b\\r\\xaa\\xbb\\x42)\n1\\localaddress=192.168.1.20\n" +
      "1\\apps\\1\\name=Desktop\n1\\apps\\1\\id=1\n1\\apps\\2\\name=Hollow Knight\n1\\apps\\2\\id=2\n1\\apps\\3\\name=Hidden thing\n1\\apps\\3\\id=3\n1\\apps\\3\\hidden=true\n1\\apps\\size=3\nsize=1\n",
  );
  const own = await startPiDash(APPS + '\n[[apps]]\nid = "moonlight"\nname = "Moonlight"\ncommand = "moonlight-qt"\n[games]\npage = false\n', { HOME: home });
  try {
    const page = await openPage(own.base);
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
    await page.waitForTimeout(2000);
    const remote = remoteFor(page, own.base);
    await remote.press("select");
    const row = await tiles(page);
    const at = row.indexOf("Moonlight");
    assert.deepEqual(row.slice(at, at + 3), ["Moonlight", "Desktop", "Hollow Knight"], `the games follow the apps, the hidden one left out: ${row}`);

    // Games keep the order of Moonlight's list: they cannot be picked up.
    await focusTile(page, remote, "Hollow Knight");
    await remote.press("up");
    assert.equal(await carried(page), "");
    // And moving an app next to them still saves, with the games left out of it.
    await focusTile(page, remote, "Hollow Knight"); // stays
    for (let i = 0; i < 2; i++) await remote.press("left"); // Moonlight
    await remote.press("up");
    await remote.press("select"); // "Move"
    assert.equal(await carried(page), "Moonlight");
    await remote.press("left");
    await remote.press("select");
    await page.waitForTimeout(400);
    const status = await (await fetch(own.base + "/api/apps")).json();
    assert.deepEqual(status.apps.map((a) => a.name), ["Netflix", "Moonlight", "YouTube", "Desktop", "Hollow Knight"]);
    assert.equal(status.error, undefined);

    // OK starts the game.
    await focusTile(page, remote, "Hollow Knight");
    await remote.press("select");
    await page.waitForFunction(() => !document.getElementById("rail").classList.contains("open"));
    const running = (await (await fetch(own.base + "/api/apps")).json()).running;
    assert.match(running, /^game-[0-9a-f]{6}-2$/, "the game is what is running");
    assert.deepEqual(page.problems, []);
    await page.context().close();
  } finally {
    await own.stop();
    fs.rmSync(home, { recursive: true, force: true });
  }
});

// --- Rain soon, air and pollen ----------------------------------------------------------

test("the weather says when rain is about to start, and how the air is", async () => {
  const page = await dashboard();
  const news = await page.evaluate(() => document.querySelector("#weather .w-soon")?.textContent ?? "");
  assert.match(news, /^Rain (in \d+ min|any minute now)  ·  Lots of pollen: Birch$/, "rain on its way and a lot of pollen are worth a line");
  const remote = remoteFor(page, pi.base);
  await remote.press("weather");
  await page.waitForSelector("#weather-panel.open");
  const tilesText = await page.evaluate(() => [...document.querySelectorAll("#weather-panel .wp-tile")].map((t) => t.innerText.replace(/\s+/g, " ").trim()));
  assert.equal(tilesText.length, 9, "three more tiles for the air");
  assert.deepEqual(tilesText.slice(6), ["Air quality 34 Fair", "Pollen High Birch", "Fine dust 8.4 µg/m³ coarser dust 15"]);
  assert.match(await page.evaluate(() => document.querySelector("#weather-panel .wp-soon").textContent), /^Rain (in \d+ min|any minute now)$/);
  assert.deepEqual(page.problems, []);
  await page.context().close();
});

// --- Night mode and dimming by the sun --------------------------------------------------

test("night mode and the evening's dimming can follow sunset and sunrise", async () => {
  // The sample weather has the sun rise at 07:12 and set at 16:51, UTC. A
  // night that began five minutes ago and ends in an hour, said in terms of
  // whichever of the two is near enough to say it with.
  const weatherNow = (await (await fetch(pi.base + "/api/state")).json()).find((e) => e.provider === "weather").data;
  const minutes = (iso) => new Date(iso).getHours() * 60 + new Date(iso).getMinutes();
  const sun = { rise: minutes(weatherNow.daily[0].sunrise), set: minutes(weatherNow.daily[0].sunset) };
  const d = new Date();
  const now = d.getHours() * 60 + d.getMinutes();
  const spec = (target) => {
    for (const [name, at] of [["sunset", sun.set], ["sunrise", sun.rise]]) {
      for (const day of [0, 1440, -1440]) {
        const off = target + day - at;
        if (Math.abs(off) <= 999) return off === 0 ? name : `${name}${off > 0 ? "+" : ""}${off}`;
      }
    }
    throw new Error("no way to say " + target);
  };
  const own = await startPiDash(`night_start = "${spec(now - 5)}"\nnight_end = "${spec(now + 60)}"\nevening_dim = "strong"\n` + APPS);
  try {
    const page = await openPage(own.base);
    await page.waitForFunction(() => document.getElementById("stage").classList.contains("night"), null, { timeout: 20000 });
    // The dimming: none by day, all of it at night, rising in the hour after
    // sunset and falling in the half hour before sunrise.
    const since = (now - sun.set + 1440) % 1440;
    const night = (sun.rise - sun.set + 1440) % 1440;
    const evening = since >= night ? 0 : Math.max(0, Math.min(1, since / 60, (night - since) / 30));
    const dim = await page.evaluate(() => Number(document.getElementById("dim").style.opacity));
    assert.ok(Math.abs(dim - 0.42 * evening) < 0.03, `the sheet over the picture is ${dim}, want about ${(0.42 * evening).toFixed(3)}`);
    assert.deepEqual(page.problems, []);
    await page.context().close();
  } finally {
    await own.stop();
  }

  // A night that begins in ten minutes has not begun.
  const later = await startPiDash(`night_start = "${spec(now + 10)}"\nnight_end = "${spec(now + 90)}"\n` + APPS);
  try {
    const page = await openPage(later.base);
    await page.waitForSelector("#weather .w-temp");
    await page.waitForTimeout(500);
    assert.equal(await page.evaluate(() => document.getElementById("stage").classList.contains("night")), false);
    assert.equal(await page.evaluate(() => Number(document.getElementById("dim").style.opacity)), 0, "no dimming unless asked for");
    await page.context().close();
  } finally {
    await later.stop();
  }
});

// --- Restart and shut down --------------------------------------------------------------

test("the Power tile asks twice before the Pi is shut down", async () => {
  const page = await dashboard();
  const remote = remoteFor(page, pi.base);
  const menu = () =>
    page.evaluate(() => {
      const m = document.getElementById("menu");
      if (!m.classList.contains("open")) return null;
      return { title: m.querySelector("h2").textContent, items: [...m.querySelectorAll(".menu-label")].map((e) => e.textContent), at: [...m.querySelectorAll(".menu-item")].findIndex((e) => e.classList.contains("focused")) };
    });
  const asked = async () => (await fetch(pi.base + "/api/test/power")).text();

  await remote.press("select");
  await focusTile(page, remote, "Power");
  await remote.press("select");
  assert.deepEqual(await menu(), { title: "Power", items: ["Restart PiDash", "Restart the Pi", "Shut the Pi down"], at: 0 });
  assert.equal(await rowOpen(page), false);
  await remote.press("up"); // the top is the top
  await remote.press("down");
  await remote.press("down");
  await remote.press("down");
  assert.equal((await menu()).at, 2);
  await remote.press("select");
  assert.deepEqual(await menu(), { title: "Shut the Pi down?", items: ["No, go back", "Yes, shut it down"], at: 0 }, "the second question, with No selected");
  await remote.press("select"); // OK by habit: nothing happens
  assert.equal(await menu(), null);
  assert.equal(await asked(), "[]");

  // Back leaves at either question.
  await remote.press("select");
  await focusTile(page, remote, "Power");
  await remote.press("select");
  await remote.press("back");
  assert.equal(await menu(), null);
  assert.equal(await view(page), "set");

  // And yes means yes.
  await remote.press("select");
  await focusTile(page, remote, "Power");
  await remote.press("select");
  await remote.press("down");
  await remote.press("select");
  assert.equal((await menu()).title, "Restart the Pi?");
  await remote.press("down");
  await remote.press("select");
  await page.waitForFunction(() => /Restarting…/.test(document.getElementById("toast").textContent));
  assert.equal(await asked(), "[reboot]");
  assert.deepEqual(page.problems, []);
  await page.context().close();
});

// --- Favourites, and where a photo was taken --------------------------------------------

test("a favourite photo is marked, and the slideshow can show favourites only", async () => {
  const own = await startPiDash(APPS);
  try {
    const page = await openPage(own.base);
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
    await page.waitForTimeout(2000);
    const remote = remoteFor(page, own.base);
    const status = async () => (await fetch(own.base + "/api/photos")).json();
    const shown = () => decodeURIComponent(new URL(photoNow).pathname.split("/").pop());
    let photoNow = await photo(page);

    // The photo's actions are in the bar that Up opens, not among the apps.
    await remote.press("select");
    const row = await tiles(page);
    for (const gone of ["Add to favourites", "Hide this photo", "About this photo", "More like this"]) assert.ok(!row.includes(gone), `"${gone}" is no longer in the app row: ${row}`);
    await remote.press("back");

    await remote.press("up");
    assert.ok(!(await bar(page)).items.includes("Only favourites"), "not offered without favourites");
    await remote.press("select"); // the star is first
    await page.waitForFunction(() => document.querySelector("#photo-bar .pb-item")?.getAttribute("aria-label") === "Remove from favourites");
    assert.equal((await status()).favourites, 1);
    assert.match(await page.evaluate(() => document.getElementById("caption").textContent), /^★/, "a star before the caption");
    assert.equal((await bar(page))?.at, 0, "the bar stays, to take it back at once");
    assert.equal(await photo(page), photoNow, "and the photo waits while the bar is open");

    // Only favourites: the slideshow has that one photo and nothing else.
    await focusBar(page, remote, "Only favourites");
    assert.equal((await bar(page)).sub, "1 photo");
    await remote.press("select");
    assert.equal(await bar(page), null, "choosing something else closes the bar");
    await page.waitForFunction(() => /Showing only your favourites/.test(document.getElementById("toast").textContent));
    let list = (await (await fetch(own.base + "/api/photos/playlist?limit=50")).json()).items;
    assert.deepEqual(list.map((i) => [i.key, i.favourite]), [[shown(), true]]);
    assert.equal((await status()).onlyFavourites, true);

    // Taking the last favourite away brings all photos back.
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"));
    for (let i = 0; i < 40 && !(await bar(page))?.items.includes("Remove from favourites"); i++) {
      if (await bar(page)) await remote.press("back");
      await page.waitForTimeout(250);
      await remote.press("up");
    }
    assert.ok((await bar(page)).items.includes("Show all photos"), "the bar says the slideshow is narrowed");
    await focusBar(page, remote, "Remove from favourites");
    await remote.press("select");
    await page.waitForFunction(() => document.querySelector("#photo-bar .pb-item")?.getAttribute("aria-label") === "Add to favourites");
    const st = await status();
    assert.deepEqual([st.favourites, st.onlyFavourites], [0, false]);
    list = (await (await fetch(own.base + "/api/photos/playlist?limit=50")).json()).items;
    assert.ok(list.length > 5, `all photos again, got ${list.length}`);
    assert.deepEqual(page.problems, []);
    await page.context().close();
  } finally {
    await own.stop();
  }
});

test("About this photo says when and where it was taken, with a map", async () => {
  const map = await startTileServer();
  const own = await startPiDash(`${APPS}\n[photos]\nmap_tiles = "${map.url}"\n`);
  const choose = async (folder) => {
    const label = folder.replaceAll("/", " / ");
    await fetch(own.base + "/api/photos/selection", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ folders: [{ id: folder, name: folder.split("/").pop(), path: label }], recursive: false }),
    });
    // The folder is read in the background: wait until the slideshow is made of it.
    for (let i = 0; i < 100; i++) {
      const list = (await (await fetch(own.base + "/api/photos/playlist?limit=50")).json()).items;
      if (list.length > 0 && list.every((it) => it.folder === label)) return;
      await new Promise((r) => setTimeout(r, 100));
    }
    assert.fail(`the slideshow never showed ${label}`);
  };
  const open = async () => {
    const page = await openPage(own.base);
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
    await page.waitForTimeout(2000);
    const remote = remoteFor(page, own.base);
    await remote.press("up");
    await focusBar(page, remote, "About this photo");
    return { page, remote };
  };
  try {
    // Photos from the coast know where they were taken.
    await choose("Holidays/Coast");
    let { page, remote } = await open();
    assert.equal((await bar(page)).sub, "Cascais, Portugal");
    await remote.press("select");
    await page.waitForSelector("#photo-info.open .pi-map img.loaded");
    const text = await page.evaluate(() => document.getElementById("photo-info").innerText);
    for (const want of ["About this photo", "Cascais, Portugal", "Holidays / Coast", "2024", "12:00", "years ago", "© OpenStreetMap contributors"]) {
      assert.ok(text.includes(want), `the page should say "${want}":\n${text}`);
    }
    assert.equal(await rowOpen(page), false);
    assert.equal(await page.evaluate(() => document.getElementById("stage").classList.contains("panel-open")), true);
    // The map: pieces at the regional zoom around Cascais (38.70° N, 9.42° W), fetched by the Pi.
    await page.waitForFunction(() => [...document.querySelectorAll(".pi-map img")].every((i) => i.classList.contains("loaded")));
    assert.ok(map.asked.length >= 12 && map.asked.every((u) => u.startsWith("/11/")), `tiles asked for: ${map.asked}`);
    assert.ok(map.asked.includes("/11/970/784.png"), `the tile with Cascais on it: ${map.asked}`);
    assert.ok([...map.agents].every((a) => /^PiDash\//.test(a)), `the map service is told who asks: ${[...map.agents]}`);
    const pin = await page.evaluate(() => {
      const box = document.querySelector(".pi-map").getBoundingClientRect();
      const p = document.querySelector(".pi-pin").getBoundingClientRect();
      return [Math.round(p.left + p.width / 2 - box.left - box.width / 2), Math.round(p.bottom - box.top - box.height / 2)];
    });
    assert.ok(Math.abs(pin[0]) <= 1 && Math.abs(pin[1]) <= 1, `the pin's tip is in the middle of the map, not ${pin} away from it`);

    // Up looks closer, Down from further away, and no further than there is.
    const zoom = () => page.evaluate(() => document.querySelector(".pi-map").dataset.zoom);
    await remote.press("up");
    assert.equal(await zoom(), "14");
    await remote.press("up");
    assert.equal(await zoom(), "14");
    for (let i = 0; i < 4; i++) await remote.press("down");
    assert.equal(await zoom(), "5");
    // A piece of map is drawn at its own size, which is what keeps it sharp.
    assert.deepEqual(await page.evaluate(() => { const i = document.querySelector(".pi-map img"); return [i.offsetWidth, i.naturalWidth]; }), [256, 256]);
    // A second look at the same place asks the map service for nothing new.
    await page.waitForFunction(() => [...document.querySelectorAll(".pi-map img")].every((i) => i.classList.contains("loaded")));
    const before = map.asked.length;
    await remote.press("up");
    await remote.press("up");
    await remote.press("down");
    await page.waitForFunction(() => [...document.querySelectorAll(".pi-map img")].every((i) => i.classList.contains("loaded")));
    assert.equal(map.asked.length, before, "pieces of map are kept on the Pi");
    await remote.press("back");
    assert.equal(await page.evaluate(() => document.getElementById("photo-info").classList.contains("open")), false);
    assert.deepEqual(page.problems, []);
    await page.context().close();

    // Photos of the family do not: no map, and the page says so.
    await choose("Family/Portraits");
    ({ page, remote } = await open());
    await remote.press("select");
    await page.waitForSelector("#photo-info.open .pi-body");
    const plain = await page.evaluate(() => document.getElementById("photo-info").innerText);
    assert.ok(plain.includes("This photo does not say where it was taken.") && plain.includes("Family / Portraits"), plain);
    assert.equal(await page.evaluate(() => document.querySelector(".pi-map")), null);
    await remote.press("select"); // any button closes
    assert.equal(await page.evaluate(() => document.getElementById("photo-info").classList.contains("open")), false);
    assert.deepEqual(page.problems, []);
    await page.context().close();
  } finally {
    await own.stop();
    await map.stop();
  }
});

// --- Games from RetroArch ---------------------------------------------------------------

test("RetroArch's favourite games are tiles, and the one played last comes first", async () => {
  const fs = require("node:fs");
  const os = require("node:os");
  const path = require("node:path");
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "pidash-home-"));
  const conf = path.join(home, ".config", "retroarch");
  const put = (file, text) => {
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, text);
  };
  const core = path.join(conf, "cores", "mgba_libretro.so");
  const game = (name) => ({ path: path.join(home, "ROMs", name + ".gba"), label: name, core_path: core, db_name: "Nintendo - Game Boy Advance.lpl" });
  put(core, "x");
  for (const name of ["Kart", "Quest", "Tennis"]) put(path.join(home, "ROMs", name + ".gba"), "x");
  put(path.join(conf, "content_favorites.lpl"), JSON.stringify({ version: "1.5", default_core_path: "DETECT", items: [game("Kart"), game("Quest")] }));
  put(path.join(conf, "content_history.lpl"), JSON.stringify({ version: "1.5", items: [game("Tennis"), game("Kart"), game("Gone")] }));

  const own = await startPiDash(APPS + '\n[[apps]]\nid = "retroarch"\nname = "RetroArch"\ncommand = "retroarch -f"\n[games]\npage = false\n', { HOME: home });
  try {
    const page = await openPage(own.base);
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
    await page.waitForTimeout(2000);
    const remote = remoteFor(page, own.base);
    await remote.press("select");
    let row = await tiles(page);
    let at = row.indexOf("RetroArch");
    assert.deepEqual(row.slice(at, at + 4), ["RetroArch", "Kart", "Quest", "Tennis"], `favourites, then what was played, without what is gone: ${row}`);

    await focusTile(page, remote, "Tennis");
    await remote.press("select");
    await page.waitForFunction(() => !document.getElementById("rail").classList.contains("open"));
    const status = await (await fetch(own.base + "/api/apps")).json();
    assert.match(status.running, /^game-ra-[0-9a-f]{8}$/, "the game is what is running");
    assert.deepEqual(status.apps.slice(-3).map((a) => a.name), ["Tennis", "Kart", "Quest"], "what was played last comes first");
    assert.deepEqual(page.problems, []);
    await page.context().close();
  } finally {
    await own.stop();
    fs.rmSync(home, { recursive: true, force: true });
  }
});

// --- Bluetooth from the app row ---------------------------------------------------------

test("the Bluetooth tile connects and disconnects devices without a trip through the settings", async () => {
  const own = await startPiDash(APPS);
  try {
    const page = await openPage(own.base);
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
    await page.waitForTimeout(2000);
    const remote = remoteFor(page, own.base);
    const menu = () =>
      page.evaluate(() => {
        const m = document.getElementById("menu");
        if (!m.classList.contains("open")) return null;
        return { title: m.querySelector("h2").textContent, at: [...m.querySelectorAll(".menu-item")].findIndex((e) => e.classList.contains("focused")), items: [...m.querySelectorAll(".menu-item")].map((e) => [e.querySelector(".menu-label").textContent, (e.querySelector(".menu-hint")?.textContent ?? "").replace(/\s+/g, " ")]) };
      });
    const connected = async () => (await (await fetch(own.base + "/api/bluetooth")).json()).devices.filter((d) => d.connected).map((d) => d.name);

    await remote.press("select");
    await focusTile(page, remote, "Bluetooth");
    assert.equal(await page.evaluate(() => document.querySelector(".tile.focused .tile-sub")?.textContent), "Xbox Wireless Controller · 80 %", "the tile names what is connected, with its battery");
    await remote.press("select");
    assert.equal(await rowOpen(page), false);
    assert.deepEqual(await menu(), {
      title: "Bluetooth",
      at: 0,
      items: [
        ["Living room speaker", "Not connected · OK connects"],
        ["Xbox Wireless Controller", "Connected · battery 80 % · OK disconnects"],
        ["Pair a new device…", "Opens the settings, where devices are also forgotten."],
      ],
    });

    // OK connects the speaker; the menu stays, with the highlight where it was.
    await remote.press("select");
    await page.waitForFunction(() => /^Connected/.test(document.querySelector("#menu .menu-item .menu-hint")?.textContent ?? ""));
    assert.deepEqual(await connected(), ["Living room speaker", "Xbox Wireless Controller"]);
    assert.equal((await menu()).at, 0);
    // And disconnects the controller.
    await remote.press("down");
    await remote.press("select");
    await page.waitForFunction(() => /^Not connected/.test(document.querySelectorAll("#menu .menu-item .menu-hint")[1]?.textContent ?? ""));
    assert.deepEqual(await connected(), ["Living room speaker"]);
    assert.equal((await menu()).at, 1);

    // Back closes; the tile now names the speaker.
    await remote.press("back");
    assert.equal(await menu(), null);
    await remote.press("select");
    await focusTile(page, remote, "Bluetooth");
    assert.equal(await page.evaluate(() => document.querySelector(".tile.focused .tile-sub")?.textContent), "Living room speaker");

    // The same list is one press from the photo: Up, and the last button of the bar.
    await remote.press("back");
    await remote.press("up");
    await focusBar(page, remote, "Bluetooth");
    assert.equal((await bar(page)).sub, "Living room speaker", "the button names what is connected");
    await remote.press("select");
    assert.equal(await bar(page), null);
    assert.equal((await menu())?.title, "Bluetooth");
    assert.deepEqual((await menu()).items.map((i) => i[0]), ["Living room speaker", "Xbox Wireless Controller", "Pair a new device…"]);

    // Pairing is done in the settings, which open at the right category.
    await remote.press("down");
    await remote.press("down");
    await remote.press("select");
    await page.waitForURL(/\/settings\?from=tv#sound$/);
    await page.waitForSelector("#groups .tv-focus");
    assert.equal(await page.evaluate(() => document.querySelector("#groups .tv-focus").textContent), "Sound and Bluetooth");
    assert.deepEqual(page.problems, []);
    await page.context().close();
  } finally {
    await own.stop();
  }
});

// --- Performance ------------------------------------------------------------------------

test("the performance preset sets how often the moving background is drawn", async () => {
  // Counts the screen refreshes in which the moving background was painted.
  const counting = () => {
    window.__painted = new Set();
    let frame = 0;
    const raf = window.requestAnimationFrame.bind(window);
    window.requestAnimationFrame = (cb) =>
      raf((t) => {
        frame = t;
        cb(t);
      });
    for (const name of ["drawImage", "fillRect", "putImageData"]) {
      const original = CanvasRenderingContext2D.prototype[name];
      CanvasRenderingContext2D.prototype[name] = function (...args) {
        if (this.canvas.id === "ambient") window.__painted.add(frame);
        return original.apply(this, args);
      };
    }
  };
  const perSecond = async (config) => {
    const own = await startPiDash(config + APPS);
    let browser;
    try {
      // No photos: the moving background is what is on screen. The sample
      // photos are hidden one by one until none is left.
      const json = { "Content-Type": "application/json" };
      for (let i = 0; i < 100; i++) {
        const list = (await (await fetch(own.base + "/api/photos/playlist?limit=4")).json()).items;
        const status = await (await fetch(own.base + "/api/photos")).json();
        if (list.length === 0 && !status.indexing && i > 2) break;
        if (list.length > 0) await fetch(own.base + "/api/photos/hide", { method: "POST", headers: json, body: JSON.stringify({ keys: list.map((it) => it.key) }) });
        else await new Promise((r) => setTimeout(r, 100));
      }
      browser = await require("playwright").chromium.launch();
      const context = await browser.newContext({ viewport: { width: 1920, height: 1080 } });
      const page = await context.newPage();
      await page.addInitScript(counting);
      await page.goto(own.base);
      await page.waitForFunction(() => document.getElementById("stage").dataset.performance);
      await page.waitForTimeout(2500); // settled: nothing reacts to a button any more
      assert.equal(await page.evaluate(() => document.getElementById("backdrop").classList.contains("has-photo")), false, "no photo is showing");
      await page.evaluate(() => window.__painted.clear());
      await page.waitForTimeout(3000);
      const rate = (await page.evaluate(() => window.__painted.size)) / 3;
      const preset = await page.evaluate(() => document.getElementById("stage").dataset.performance);
      const height = await page.evaluate(() => document.getElementById("ambient").height);
      // A button: the app row opens and the background lifts with it. How
      // many pictures does that movement get in its first 800 ms?
      await page.evaluate(() => window.__painted.clear());
      await fetch(own.base + "/api/test/remote", { method: "POST", headers: json, body: JSON.stringify({ action: "select" }) });
      await page.waitForFunction(() => document.getElementById("rail").classList.contains("open"));
      await page.waitForTimeout(800);
      const reacting = await page.evaluate(() => window.__painted.size);
      return { rate, preset, height, reacting };
    } finally {
      await browser?.close();
      await own.stop();
    }
  };
  // Never more often than asked for; and not much less, or it would stutter.
  const about = (got, want, what) => assert.ok(got.rate <= want * 1.15 + 0.5 && got.rate >= want * 0.5, `${what}: drawn ${got.rate.toFixed(1)} times a second, want about ${want}`);

  const balanced = await perSecond("");
  assert.equal(balanced.preset, "balanced");
  about(balanced, 12, "balanced");
  const saver = await perSecond('performance = "saver"\n');
  about(saver, 5, "power saver");
  // However slowly the background drifts, what a button sets in motion is
  // fluid: far more pictures than the four or five the drift would get.
  for (const [name, got] of [["balanced", balanced], ["power saver", saver]]) {
    assert.ok(got.reacting >= 20, `${name}: the background's reaction to a button got ${got.reacting} pictures in 0.8 s, want it fluid`);
  }
  assert.ok(saver.height <= 360 && saver.height < balanced.height, `the power saver draws a coarser picture: ${saver.height} against ${balanced.height} lines`);
  about(await perSecond('performance = "smooth"\n'), 30, "smooth");
  // A rate of one's own counts for more than the preset.
  about(await perSecond('performance = "saver"\nbackground_fps = 20\n'), 20, "20 a second by hand");
});

test("power saver: photos stand still; the settings offer the presets", async () => {
  const own = await startPiDash('performance = "saver"\n' + APPS + '\n[photos]\nmotion = "fill"\n');
  try {
    const page = await openPage(own.base);
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
    await page.waitForTimeout(2500);
    const moving = await page.evaluate(() => document.getAnimations().filter((a) => a.effect?.target?.closest?.("#backdrop") && a.playState === "running" && (a.effect.getKeyframes?.() ?? []).some((k) => "transform" in k)).length);
    assert.equal(moving, 0, "no photo is drifting");
    await page.context().close();

    const settings = await openPage(own.base, "/settings#screen");
    await settings.waitForSelector("#main:not([hidden])");
    assert.equal(await settings.inputValue("#d-performance"), "saver");
    assert.match(await settings.locator("#d-performance-hint").innerText(), /5 times a second/);
    assert.match(await settings.locator("#main").innerText(), /Menus are not slowed down by this/);
    await settings.selectOption("#d-performance", "smooth");
    assert.match(await settings.locator("#d-performance-hint").innerText(), /30 times a second/);
    await settings.selectOption("#d-fps", "24");
    assert.equal(await settings.inputValue("#d-tiles"), "glow");
    await settings.selectOption("#d-tiles", "plain");
    await settings.click("#save");
    await settings.waitForFunction(() => document.getElementById("save").disabled);
    // The backend starts again with what was saved.
    let display = {};
    for (let i = 0; i < 50 && !(display.performance === "smooth" && display.background_fps === 24); i++) {
      await new Promise((r) => setTimeout(r, 200));
      try {
        display = (await (await fetch(own.base + "/api/config")).json()).display ?? {};
      } catch {
        /* restarting */
      }
    }
    assert.deepEqual([display.performance, display.background_fps, display.tiles], ["smooth", 24, "plain"]);
    // Plain tiles: no glow, no shadow, nothing see-through.
    const plain = await openPage(own.base);
    await plain.waitForFunction(() => document.getElementById("stage").dataset.tiles === "plain");
    await remoteFor(plain, own.base).press("select");
    assert.deepEqual(
      await plain.evaluate(() => {
        const tile = document.querySelector(".tile");
        return [getComputedStyle(tile).boxShadow, getComputedStyle(tile.querySelector(".tile-glow")).display, getComputedStyle(tile).backgroundImage];
      }),
      ["none", "none", "none"],
    );
    await plain.context().close();
    assert.deepEqual(settings.problems, []);
    await settings.context().close();
  } finally {
    await own.stop();
  }
});

// --- What the dashboard shows, set and kept --------------------------------------------

test("the caption and the weather's detail are settings, and can be kept from the remote", async () => {
  const own = await startPiDash('caption = "on"\nweather_detail = "off"\nlock_view = true\n' + APPS);
  try {
    const page = await openPage(own.base);
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
    await page.waitForTimeout(2000);
    const remote = remoteFor(page, own.base);
    const seen = () =>
      page.evaluate(() => ({
        caption: Number(getComputedStyle(document.getElementById("caption")).opacity) > 0.5 && document.getElementById("caption").textContent.length > 3,
        weather: document.getElementById("weather").getClientRects().length > 0,
        clock: Number(getComputedStyle(document.getElementById("overlay")).opacity) > 0.5,
      }));
    assert.deepEqual(await shows(page), ["set", "on", "off"]);
    assert.deepEqual(await seen(), { caption: true, weather: false, clock: true }, "the caption is there, the weather is not");
    // Kept: Down changes nothing, however often.
    for (let i = 0; i < 3; i++) await remote.press("down");
    assert.deepEqual(await shows(page), ["set", "on", "off"]);
    assert.deepEqual(await seen(), { caption: true, weather: false, clock: true });
    // Up still opens the photo's bar, which makes room where the caption was.
    await remote.press("up");
    assert.ok(await bar(page));
    await remote.press("back");
    await page.context().close();

    // The settings page has the three, as they are; and saves them.
    const settings = await openPage(own.base, "/settings#screen");
    await settings.waitForSelector("#main:not([hidden])");
    assert.deepEqual([await settings.inputValue("#d-caption"), await settings.inputValue("#d-weather"), await settings.isChecked("#d-lock")], ["on", "off", true]);
    await settings.selectOption("#d-weather", "full");
    await settings.uncheck("#d-lock");
    await settings.click("#save");
    let display = {};
    for (let i = 0; i < 50 && !(display.weather_detail === "full" && display.lock_view === false); i++) {
      await new Promise((r) => setTimeout(r, 200));
      try {
        display = (await (await fetch(own.base + "/api/config")).json()).display ?? {};
      } catch {
        /* restarting */
      }
    }
    assert.deepEqual([display.caption, display.weather_detail, display.lock_view], ["on", "full", false]);
    assert.deepEqual(settings.problems, []);
    await settings.context().close();

    // No longer kept: Down goes straight to "only the photo", since everything is what is set.
    const again = await openPage(own.base);
    await again.waitForFunction(() => document.getElementById("overlay").dataset.weather === "full");
    await remoteFor(again, own.base).press("down");
    assert.equal(await view(again), "photo");
    await again.context().close();
  } finally {
    await own.stop();
  }
});

// --- Other websites -------------------------------------------------------------------

test("requests from other websites, and under names that are not the Pi's, are refused", async () => {
  const http = require("node:http");
  const call = (method, path, headers) =>
    new Promise((resolve, reject) => {
      const u = new URL(pi.base);
      const req = http.request({ host: u.hostname, port: u.port, method, path, headers: { "Content-Type": "application/json", ...headers } }, (res) => {
        let body = "";
        res.on("data", (d) => (body += d));
        res.on("end", () => resolve({ status: res.statusCode, body, headers: res.headers }));
      });
      req.on("error", reject);
      req.end(method === "GET" ? undefined : "{}");
    });
  const running = async () => (await (await fetch(pi.base + "/api/apps")).json()).running;

  // A page somewhere else tries to start an app, the way a hidden form or script would.
  for (const headers of [{ Origin: "https://evil.example" }, { Origin: "null" }, { "Sec-Fetch-Site": "cross-site" }, { Origin: "https://www.youtube.com", "Sec-Fetch-Site": "cross-site" }]) {
    const res = await call("POST", "/api/apps/start", { ...headers });
    assert.equal(res.status, 403, `refused: ${JSON.stringify(headers)}`);
    assert.match(res.body, /another website/);
  }
  assert.equal(await running(), "", "nothing was started");
  // A name from the internet, pointed at the Pi's address.
  const rebound = await call("GET", "/api/config", { Host: "pi.evil.example:8080" });
  assert.equal(rebound.status, 403);
  assert.match(rebound.body, /does not answer to this name/);
  // PiDash's own pages, and programs that are no browser, are served; and the
  // pages say what they may load.
  const own = await call("GET", "/", {});
  assert.equal(own.status, 200);
  assert.match(own.headers["content-security-policy"], /script-src 'self'/);
  assert.equal(own.headers["x-frame-options"], "DENY");
  const u = new URL(pi.base);
  const started = await call("POST", "/api/apps/start", { Origin: pi.base, "Sec-Fetch-Site": "same-origin", Host: u.host, "Content-Length": "2" });
  assert.equal(started.status, 404, "its own page gets through to the handler (which knows no app without a name)");
});

// --- Two photos side by side ----------------------------------------------------------

test("two photos side by side: the caption speaks for both, and the map goes to either", async () => {
  const map = await startTileServer();
  const own = await startPiDash(`caption = "on"\n${APPS}\n[photos]\npair_portraits = true\nmap_tiles = "${map.url}"\n`);
  try {
    await fetch(own.base + "/api/photos/selection", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ folders: [{ id: "Holidays/Coast", name: "Coast", path: "Holidays / Coast" }], recursive: false }) });
    // Upright photos find their partner once their shape is known.
    let pair;
    for (let i = 0; i < 100 && !pair; i++) {
      const list = (await (await fetch(own.base + "/api/photos/playlist?limit=50")).json()).items;
      pair = list.every((it) => it.folder === "Holidays / Coast") ? list.find((it) => it.parts) : undefined;
      if (!pair) await new Promise((r) => setTimeout(r, 200));
    }
    assert.ok(pair, "the sample photos include two upright ones, shown together");
    assert.deepEqual(pair.parts.map((p) => [p.folder, p.place]), [["Holidays / Coast", "Cascais, Portugal"], ["Holidays / Coast", "Cascais, Portugal"]]);
    assert.notEqual(pair.parts[0].taken, pair.parts[1].taken);

    const page = await openPage(own.base);
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
    await page.waitForTimeout(2000);
    const remote = remoteFor(page, own.base);
    for (let i = 0; i < 12 && !decodeURIComponent(await photo(page)).includes("+"); i++) {
      await remote.press("right");
      await page.waitForTimeout(2200);
    }
    assert.ok(decodeURIComponent(await photo(page)).includes("+"), "a pair is on screen");
    // What both share is said once; what differs, for each: the left one first.
    const caption = await page.evaluate(() => document.getElementById("caption").textContent);
    assert.match(caption, /^Coast {2}· {2}Cascais, Portugal\s+◀ \S.*2024\s+▶ \S.*2024$/, `the caption: ${caption}`);
    assert.notEqual(caption.split("◀")[1].split("▶")[0].trim(), caption.split("▶")[1].trim(), "two different days");

    // About: the map starts with the left photo; Right takes it to the right one.
    await remote.press("up");
    await focusBar(page, remote, "About this photo");
    await remote.press("select");
    await page.waitForSelector("#photo-info.open .pi-map img.loaded");
    const state = () =>
      page.evaluate(() => {
        const box = document.querySelector(".pi-map").getBoundingClientRect();
        const pin = document.querySelector(".pi-pin:not(.other)").getBoundingClientRect();
        return {
          chosen: [...document.querySelectorAll(".pi-photo")].findIndex((b) => b.classList.contains("chosen")),
          centred: Math.abs(pin.left + pin.width / 2 - box.left - box.width / 2) <= 1 && Math.abs(pin.bottom - box.top - box.height / 2) <= 1,
          pins: document.querySelectorAll(".pi-pin").length,
          others: document.querySelectorAll(".pi-pin.other").length,
        };
      });
    assert.deepEqual(await state(), { chosen: 0, centred: true, pins: 2, others: 1 });
    assert.match(await page.evaluate(() => document.getElementById("photo-info").innerText), /◀ ▶ the left or the right photo/);
    await remote.press("left"); // already there
    assert.equal((await state()).chosen, 0);
    await remote.press("right");
    assert.deepEqual(await state(), { chosen: 1, centred: true, pins: 2, others: 1 }, "the map moved to the right photo's place");
    await remote.press("left");
    assert.equal((await state()).chosen, 0);
    assert.equal(await page.evaluate(() => document.getElementById("photo-info").classList.contains("open")), true, "Left and Right do not close the page");
    await remote.press("back");
    assert.deepEqual(page.problems, []);
    await page.context().close();
  } finally {
    await own.stop();
    await map.stop();
  }
});

// --- Small things that make it nicer to use ---------------------------------------------

test("a hidden photo can be taken back with OK; a held slideshow says so; the row opens on the last app", async () => {
  const own = await startPiDash(APPS);
  try {
    const page = await openPage(own.base);
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
    await page.waitForTimeout(2000);
    const remote = remoteFor(page, own.base);
    const hidden = async () => (await (await fetch(own.base + "/api/photos")).json()).hidden;
    const toast = () => page.evaluate(() => document.getElementById("toast").textContent);

    // Hide, then OK at once: it is back.
    await remote.press("up");
    await focusBar(page, remote, "Hide this photo");
    await remote.press("select");
    await page.waitForFunction(() => /Press OK now to take that back/.test(document.getElementById("toast").textContent));
    assert.equal(await hidden(), 1);
    await remote.press("select");
    await page.waitForFunction(() => /It is back/.test(document.getElementById("toast").textContent));
    assert.equal(await hidden(), 0);
    assert.equal(await rowOpen(page), false, "that OK did not also open the app row");

    // Hide, then any other button: it stays hidden, and OK means OK again.
    await page.waitForTimeout(1500);
    await remote.press("up");
    assert.equal((await bar(page)).at, 0, "the bar does not open on Hide, which was used last");
    await focusBar(page, remote, "Hide this photo");
    await remote.press("select");
    await page.waitForFunction(() => /Press OK now to take that back/.test(document.getElementById("toast").textContent));
    await remote.press("right");
    await remote.press("select");
    assert.equal(await rowOpen(page), true);
    assert.equal(await hidden(), 1);
    await remote.press("back");

    // The bar opens on what was used last (other than Hide).
    await remote.press("up");
    await focusBar(page, remote, "More like this");
    await remote.press("select");
    // More of what: its album, the day it was taken, or that year.
    assert.deepEqual(await page.evaluate(() => [...document.querySelectorAll("#menu.open .menu-label")].map((e) => e.textContent)), ["From this album", "From this day", "From this year"]);
    await remote.press("down");
    await remote.press("down");
    await remote.press("select");
    await page.waitForFunction(() => /pictures are from 2024|no other photos from 2024/.test(document.getElementById("toast").textContent));
    await page.waitForTimeout(600);
    await remote.press("up");
    assert.equal((await bar(page)).label, "More like this");
    await remote.press("back");

    // Play/pause holds the photo, and a mark says so for as long as it is held.
    const held = () => page.evaluate(() => Number(getComputedStyle(document.getElementById("held")).opacity) > 0.5);
    assert.equal(await held(), false);
    await remote.press("playpause");
    await page.waitForTimeout(500);
    assert.equal(await held(), true);
    assert.match(await page.evaluate(() => document.getElementById("held").textContent), /Photo held/);
    await remote.press("playpause");
    await page.waitForTimeout(500);
    assert.equal(await held(), false);

    // The app row opens on the first tile; after an app was used, on that app.
    await remote.press("select");
    assert.equal(await tile(page), 0);
    await focusTile(page, remote, "YouTube");
    await remote.press("select");
    await page.waitForFunction(() => !document.getElementById("rail").classList.contains("open"));
    await fetch(own.base + "/api/apps/stop", { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" });
    await page.waitForTimeout(800);
    await remote.press("select");
    assert.equal(await page.evaluate(() => document.querySelector(".tile.focused .tile-label")?.textContent), "YouTube");
    await remote.press("back");
    // Also after the page was loaded again.
    await page.reload();
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
    await page.waitForTimeout(1500);
    await remote.press("select");
    assert.equal(await page.evaluate(() => document.querySelector(".tile.focused .tile-label")?.textContent), "YouTube");
    assert.deepEqual(page.problems, []);
    await page.context().close();
  } finally {
    await own.stop();
  }
});

test("new photos are announced once and shown from the bar; channel keys are the volume for headphones", async () => {
  const own = await startPiDash(APPS);
  try {
    const page = await openPage(own.base);
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
    await page.waitForTimeout(2000);
    const remote = remoteFor(page, own.base);
    const json = { "Content-Type": "application/json" };
    const press = (action) => fetch(own.base + "/api/test/remote", { method: "POST", headers: json, body: JSON.stringify({ action }) });

    // Three photos "arrive".
    await remote.press("up");
    assert.ok(!(await bar(page)).items.includes("Show the new photos"));
    await remote.press("back");
    await fetch(own.base + "/api/test/new-photos?n=3", { method: "POST" });
    await page.waitForFunction(() => /3 new photos, most of them in “.+”\. ▲ shows them\./.test(document.getElementById("toast").textContent));
    await remote.press("up");
    let now = await bar(page);
    assert.deepEqual([now.items[0], now.at, now.sub], ["Show the new photos", 0, "3 in Holidays"], "the bar offers them first");
    await remote.press("select");
    await page.waitForFunction(() => /Here are the 3 new photos/.test(document.getElementById("toast").textContent));
    const status = await (await fetch(own.base + "/api/photos")).json();
    assert.equal(status.new, 0, "looked at: new no longer");
    await page.waitForTimeout(600);
    await remote.press("up");
    assert.ok(!(await bar(page)).items.includes("Show the new photos"));
    await remote.press("back");

    // The sound is on the TV: channel up is the next photo, as ever.
    await page.waitForTimeout(1600);
    const before = await photo(page);
    await press("channel-up");
    await page.waitForFunction((b) => {
      const shown = [...document.querySelectorAll("#backdrop img.visible, #backdrop video.visible")].sort((x, y) => Number(y.style.zIndex) - Number(x.style.zIndex))[0];
      return shown && shown.src !== b;
    }, before);
    // Headphones connect and the sound follows: now the same button is their volume.
    // (The sample speaker is there from the start: it is switched off and on again.)
    await fetch(own.base + "/api/test/headphones", { method: "POST", headers: json, body: JSON.stringify({ connected: false }) });
    await new Promise((r) => setTimeout(r, 400));
    await fetch(own.base + "/api/test/headphones", { method: "POST", headers: json, body: JSON.stringify({ connected: true }) });
    const active = async () => (await (await fetch(own.base + "/api/sound")).json()).outputs.find((o) => o.active);
    for (let i = 0; i < 40 && (await active()).kind !== "bluetooth"; i++) await new Promise((r) => setTimeout(r, 100));
    const start = await active();
    assert.equal(start.kind, "bluetooth");
    await page.waitForTimeout(1700);
    const held = await photo(page);
    await press("channel-down");
    // A bar shows the new level for a moment.
    await page.waitForFunction(() => document.getElementById("volume").classList.contains("shown"));
    assert.deepEqual(await page.evaluate(() => [document.querySelector("#volume .vol-name").textContent, document.querySelector("#volume .vol-number").textContent]), ["Living room speaker", `${Math.max(0, start.volume - 5)} %`]);
    assert.equal((await active()).volume, Math.max(0, start.volume - 5));
    await press("channel-up");
    await press("channel-up");
    for (let i = 0; i < 30 && (await active()).volume < Math.min(100, start.volume + 5); i++) await new Promise((r) => setTimeout(r, 100));
    assert.equal((await active()).volume, Math.min(100, start.volume + 5));
    assert.equal(await photo(page), held, "and the photo stays");
    assert.deepEqual(page.problems, []);
    await page.context().close();
  } finally {
    await own.stop();
  }
});

test("the status page saves a diagnostics file; the map has a switch for larger lettering", async () => {
  const fs = require("node:fs");
  const os = require("node:os");
  const path = require("node:path");
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "pidash-home-"));
  const map = await startTileServer();
  const own = await startPiDash(`${APPS}\n[weather]\nlocation = "Zurich"\n[photos]\nmap_tiles = "${map.url}"\n[guest_wifi]\nname = "Guests"\npassword = "s3cret-wifi-pass"\n`, { HOME: home });
  try {
    await fetch(own.base + "/api/photos/selection", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ folders: [{ id: "Holidays/Coast", name: "Coast", path: "Holidays / Coast" }], recursive: false }) });
    for (let i = 0; i < 100; i++) {
      const list = (await (await fetch(own.base + "/api/photos/playlist?limit=50")).json()).items;
      if (list.length > 0 && list.every((it) => it.folder === "Holidays / Coast")) break;
      await new Promise((r) => setTimeout(r, 100));
    }
    const page = await openPage(own.base);
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
    await page.waitForTimeout(2000);
    const remote = remoteFor(page, own.base);

    await remote.press("select");
    await focusTile(page, remote, "Status");
    await remote.press("select");
    await page.waitForFunction(() => /OK saves all this to a file/.test(document.getElementById("status-panel").innerText));
    await remote.press("select");
    await page.waitForFunction(() => /Saved on the Pi as .*pidash-diagnostics-.*\.txt/.test(document.getElementById("status-panel").innerText), null, { timeout: 30000 });
    assert.equal(await page.evaluate(() => document.getElementById("status-panel").classList.contains("open")), true, "OK no longer closes the page");
    const files = fs.readdirSync(home).filter((f) => f.startsWith("pidash-diagnostics-"));
    assert.equal(files.length, 1);
    const text = fs.readFileSync(path.join(home, files[0]), "utf8");
    for (const part of ["PiDash diagnostics", "Version:", "=== Status", "=== Settings (no passwords)", "=== Log of the services", "1920 x 1080"]) assert.ok(text.includes(part), `the file lacks "${part}"`);
    assert.ok(!text.includes("s3cret-wifi-pass"), "no password is in the file");
    assert.equal(fs.statSync(path.join(home, files[0])).mode & 0o077, 0, "the file is for its owner only");
    await remote.press("back");
    assert.equal(await page.evaluate(() => document.getElementById("status-panel").classList.contains("open")), false);

    // The map: OK switches between a sharp map and one with large lettering.
    await remote.press("up");
    await focusBar(page, remote, "About this photo");
    await remote.press("select");
    await page.waitForSelector("#photo-info.open .pi-map img.loaded");
    const drawn = () => page.evaluate(() => [document.querySelector(".pi-map").dataset.lettering, document.querySelector(".pi-map").dataset.zoom, document.querySelector(".pi-map img").offsetWidth]);
    assert.deepEqual(await drawn(), ["sharp", "11", 256]);
    await remote.press("select");
    assert.deepEqual(await drawn(), ["large", "10", 512], "pieces from one step further away, at twice the size");
    assert.equal(await page.evaluate(() => document.getElementById("photo-info").classList.contains("open")), true);
    // Remembered for the next photo.
    await remote.press("back");
    await remote.press("up");
    await focusBar(page, remote, "About this photo");
    await remote.press("select");
    await page.waitForSelector("#photo-info.open .pi-map img.loaded");
    assert.equal((await drawn())[0], "large");
    await remote.press("select");
    assert.equal((await drawn())[0], "sharp");
    await remote.press("back");
    assert.deepEqual(page.problems, []);
    await page.context().close();
  } finally {
    await own.stop();
    await map.stop();
    fs.rmSync(home, { recursive: true, force: true });
  }
});

// --- Nothing to draw, nothing drawn -----------------------------------------------------

test("a still photo costs nothing: no pictures are asked for, and the screen is one sheet", async () => {
  const context = await (await require("playwright").chromium.launch()).newContext({ viewport: { width: 1920, height: 1080 } });
  try {
    const page = await context.newPage();
    // Counts every time the page asks to be woken for the next picture.
    await page.addInitScript(() => {
      window.__asked = 0;
      const raf = window.requestAnimationFrame.bind(window);
      window.requestAnimationFrame = (cb) => {
        window.__asked++;
        return raf(cb);
      };
    });
    await page.goto(pi.base);
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
    await page.waitForTimeout(6000); // the moving background has gone to sleep under the photo
    await page.evaluate(() => (window.__asked = 0));
    await page.waitForTimeout(3000);
    assert.equal(await page.evaluate(() => window.__asked), 0, "with a still photo the page asks for no pictures at all");
    assert.equal(await page.evaluate(() => document.getAnimations().filter((a) => a.playState === "running").length), 0, "and nothing is animating");

    // What the screen is put together from: sheets as large as the screen
    // are what costs at 4K. The page (behind everything) and one more.
    const cdp = await context.newCDPSession(page);
    let layers = [];
    cdp.on("LayerTree.layerTreeDidChange", (e) => {
      if (e.layers) layers = e.layers;
    });
    await cdp.send("LayerTree.enable");
    await page.evaluate(() => (document.getElementById("stage").style.outline = "0px solid transparent"));
    await page.waitForTimeout(800);
    const large = layers.filter((l) => l.drawsContent && l.width >= 1800 && l.height >= 1000);
    assert.ok(layers.length > 0, "the browser reported its layers");
    assert.ok(large.length <= 2, `${large.length} sheets as large as the screen: ${large.map((l) => `${l.width}x${l.height}`)}`);
  } finally {
    await context.browser().close();
  }
});

test("the status page says what the browser draws with; the TV can be sent a smaller picture", async () => {
  const own = await startPiDash(APPS);
  try {
    const page = await openPage(own.base);
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
    await page.waitForTimeout(1500);
    const remote = remoteFor(page, own.base);
    await remote.press("select");
    await focusTile(page, remote, "Status");
    await remote.press("select");
    // On the machine that runs the tests there is no graphics chip, and the page says so.
    await page.waitForFunction(() => /Graphics\s+\S/.test(document.getElementById("status-panel").innerText));
    assert.match(await page.evaluate(() => document.getElementById("status-panel").innerText), /Graphics\s+(.*much slower|\S.*)/);
    await page.context().close();

    const settings = await openPage(own.base, "/settings#screen");
    await settings.waitForSelector("#main:not([hidden])");
    assert.equal(await settings.inputValue("#sys-resolution"), "desktop", "the TV gets what the desktop is set to unless asked otherwise");
    await settings.selectOption("#sys-resolution", "1080p");
    await settings.click("#save");
    let saved = "";
    for (let i = 0; i < 50 && saved !== "1080p"; i++) {
      await new Promise((r) => setTimeout(r, 200));
      try {
        saved = (await (await fetch(own.base + "/api/settings")).json()).system?.resolution ?? "";
      } catch {
        /* restarting */
      }
    }
    assert.equal(saved, "1080p");
    assert.deepEqual(settings.problems, []);
    await settings.context().close();
  } finally {
    await own.stop();
  }
});

// --- The pages that fill the screen: games, videos, places ------------------------------

// What a page of cards shows: its title, the cards, and the one the highlight is on.
const grid = (page) =>
  page.evaluate(() => {
    const g = document.getElementById("grid-page");
    if (!g.classList.contains("open")) return null;
    const cards = [...g.querySelectorAll(".card")];
    return {
      title: g.querySelector("h2").textContent,
      cards: cards.map((c) => c.querySelector(".card-title").textContent),
      subs: cards.map((c) => c.querySelector(".card-sub")?.textContent ?? ""),
      at: cards.findIndex((c) => c.classList.contains("focused")),
      note: g.querySelector(".grid-note")?.textContent ?? "",
    };
  });
const toastText = (page) => page.evaluate(() => (document.getElementById("toast").classList.contains("visible") || document.getElementById("toast").classList.contains("show") ? document.getElementById("toast").textContent : document.getElementById("toast").textContent));

async function ready(base) {
  const page = await openPage(base);
  await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
  await page.waitForTimeout(2000);
  return { page, remote: remoteFor(page, base) };
}

test("the games are on a page of their own: one tile, covers to walk through, OK plays", async () => {
  const fs = require("node:fs");
  const os = require("node:os");
  const path = require("node:path");
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "pidash-home-"));
  const conf = path.join(home, ".config", "Moonlight Game Streaming Project");
  fs.mkdirSync(conf, { recursive: true });
  const names = ["Desktop", "Hollow Knight", "Celeste", "Stardew Valley", "Hades", "Portal 2", "Rocket League", "Outer Wilds"];
  let apps = "";
  names.forEach((n, i) => (apps += `1\\apps\\${i + 1}\\name=${n}\n1\\apps\\${i + 1}\\id=${i + 1}\n`));
  fs.writeFileSync(
    path.join(conf, "Moonlight.conf"),
    "[hosts]\n1\\hostname=LIVING-PC\n1\\uuid=3F2A9C1E-77AB-4C0D-9E55-0A1B2C3D4E5F\n1\\mac=@ByteArray(\\x1c\\x1b\\r\\xaa\\xbb\\x42)\n1\\localaddress=192.168.1.20\n" + apps + `1\\apps\\size=${names.length}\nsize=1\n`,
  );
  const own = await startPiDash(APPS + '\n[[apps]]\nid = "moonlight"\nname = "Moonlight"\ncommand = "moonlight-qt"\n', { HOME: home });
  try {
    const { page, remote } = await ready(own.base);
    await remote.press("select");
    const row = await tiles(page);
    assert.ok(row.includes("Games") && !row.includes("Hollow Knight"), `one tile for all the games: ${row}`);
    assert.equal(row[row.indexOf("Moonlight") + 1], "Games", "it follows the apps");
    await focusTile(page, remote, "Games");
    assert.equal(await page.evaluate(() => document.querySelector(".tile.focused .tile-sub").textContent), "8 games");
    // It is no app: it cannot be picked up, and has nothing to offer on Up.
    await remote.press("up");
    assert.equal(await carried(page), "");
    assert.equal(await page.evaluate(() => document.getElementById("menu").classList.contains("open")), false);

    await remote.press("select");
    let g = await grid(page);
    assert.deepEqual([g.title, g.cards, g.at], ["Games", names, 0]);
    assert.equal(await rowOpen(page), false, "the app row made way for the page");
    assert.equal(await page.evaluate(() => document.getElementById("stage").classList.contains("panel-open")), true, "the clock is out of the way");

    // Six to a row: Down goes to the card below, and from a full row to the last card.
    await remote.press("right");
    await remote.press("down");
    assert.equal((await grid(page)).at, 7);
    await remote.press("down");
    assert.equal((await grid(page)).at, 7, "there is no third row");
    await remote.press("up");
    assert.equal((await grid(page)).at, 1);
    await remote.press("left");
    await remote.press("left");
    assert.equal((await grid(page)).at, 0, "the first card is the end");
    for (let i = 0; i < 4; i++) await remote.press("right");
    await remote.press("down");
    assert.equal((await grid(page)).at, 7, "Down from above an empty place goes to the last card");

    // A held direction keeps moving, as in the app row.
    await fetch(own.base + "/api/test/remote", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ action: "left", repeat: true }) });
    await page.waitForTimeout(200);
    assert.equal((await grid(page)).at, 6);

    // Back closes the page; the next time it opens it is there again.
    await remote.press("back");
    assert.equal(await grid(page), null);
    assert.equal(await page.evaluate(() => document.getElementById("stage").classList.contains("panel-open")), false);
    await remote.press("select");
    await focusTile(page, remote, "Games");
    await remote.press("select");
    await remote.press("right"); // Hollow Knight
    await remote.press("select");
    await page.waitForFunction(() => !document.getElementById("grid-page").classList.contains("open"));
    let status = await (await fetch(own.base + "/api/apps")).json();
    assert.match(status.running, /^game-[0-9a-f]{6}-2$/, "the game is what is running");

    // Back at the dashboard: the row opens on Games, which says what runs; the page marks it.
    await remote.press("select");
    assert.equal(await page.evaluate(() => document.querySelector(".tile.focused .tile-label").textContent), "Games");
    assert.equal(await page.evaluate(() => document.querySelector(".tile.focused .tile-sub").textContent), "Hollow Knight");
    await remote.press("select");
    g = await grid(page);
    assert.equal(g.cards[g.at], "Hollow Knight", "the highlight is on the game that runs, now the first");
    assert.equal(g.subs[g.at], "Running · OK to stop");
    await remote.press("select");
    await page.waitForFunction(async () => (await (await fetch("/api/apps")).json()).running === "");
    assert.deepEqual(page.problems, []);
    await page.context().close();

    // The setting: every game a tile again.
    const res = await fetch(own.base + "/api/settings", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ system: { autoUpdate: false, games: true, gamesPage: false } }) });
    assert.equal(res.status, 200, await res.text());
    assert.equal((await (await fetch(own.base + "/api/settings")).json()).system.gamesPage, false);
  } finally {
    await own.stop();
    fs.rmSync(home, { recursive: true, force: true });
  }
});

test("videos: folders to walk through, a video plays in the player, Back goes up a folder", async () => {
  const fs = require("node:fs");
  const os = require("node:os");
  const path = require("node:path");
  const films = fs.mkdtempSync(path.join(os.tmpdir(), "pidash-films-"));
  const put = (rel) => {
    fs.mkdirSync(path.dirname(path.join(films, rel)), { recursive: true });
    fs.writeFileSync(path.join(films, rel), "x".repeat(2048));
  };
  for (const f of ["Series/Episode 10.mkv", "Series/Episode 2.mkv", "Series/Episode 2.srt", "Series/notes.txt", "Alien.mp4", "@eaDir/x.mp4"]) put(f);
  const own = await startPiDash(APPS + `\n[videos]\nsource = "folder"\nfolders = ["${films}"]\n`);
  try {
    const { page, remote } = await ready(own.base);
    await remote.press("select");
    await focusTile(page, remote, "Videos");
    await remote.press("select");
    await page.waitForFunction(() => document.getElementById("grid-page").classList.contains("open"));
    let g = await grid(page);
    assert.deepEqual([g.cards, g.at], [["Series", "Alien.mp4"], 0], "folders first; what a NAS keeps for itself is left out");
    assert.match(g.subs[1], /MB/);

    await remote.press("select");
    await page.waitForFunction(() => document.querySelector("#grid-page h2").textContent === "Series");
    g = await grid(page);
    assert.deepEqual(g.cards, ["Episode 2.mkv", "Episode 10.mkv"], "episodes in the order people count, without the subtitles and the notes");
    assert.match(await page.evaluate(() => document.querySelector("#grid-page .help-foot").textContent), /the folder above/);

    // OK plays: the player takes the screen like an app, with the subtitles that lie beside the video.
    await remote.press("select");
    await page.waitForFunction(async () => (await (await fetch("/api/apps")).json()).running === "pidash-video");
    const argv = JSON.parse(fs.readFileSync(path.join(own.dir, "run", "pidash-video.json"), "utf8"));
    assert.equal(argv[0], "/usr/bin/mpv");
    assert.equal(argv.at(-1), path.join(films, "Series", "Episode 2.mkv"));
    assert.ok(argv.includes("--sub-file=" + path.join(films, "Series", "Episode 2.srt")), argv.join(" "));
    assert.ok(!(await (await fetch(own.base + "/api/apps")).json()).apps.some((a) => a.id === "pidash-video"), "the player is no tile");
    // The video ends (here: is stopped); the page is still where it was.
    await fetch(own.base + "/api/apps/stop", { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" });
    await page.waitForFunction(async () => (await (await fetch("/api/apps")).json()).running === "");
    assert.equal((await grid(page)).title, "Series");

    // Back goes to the folder above, with the highlight on where one came from; then closes.
    await remote.press("back");
    await page.waitForFunction(() => document.querySelector("#grid-page h2").textContent !== "Series");
    g = await grid(page);
    assert.equal(g.cards[g.at], "Series");
    await remote.press("select");
    await page.waitForFunction(() => document.querySelector("#grid-page h2").textContent === "Series");
    await remote.press("menu"); // closes from anywhere
    assert.equal(await grid(page), null);

    // Opened again, it is in the folder that was open last.
    await remote.press("select");
    await focusTile(page, remote, "Videos");
    await remote.press("select");
    await page.waitForFunction(() => document.querySelector("#grid-page h2")?.textContent === "Series");
    // A folder that has gone since: back to the top instead of an error.
    fs.rmSync(path.join(films, "Series"), { recursive: true });
    await remote.press("menu");
    await remote.press("select");
    await focusTile(page, remote, "Videos");
    await remote.press("select");
    await page.waitForFunction(() => document.querySelectorAll("#grid-page .card").length === 1);
    assert.deepEqual((await grid(page)).cards, ["Alien.mp4"]);
    assert.deepEqual(page.problems, []);
    await page.context().close();

    // The settings page has them: saved there, the tile goes.
    const view = (await (await fetch(own.base + "/api/settings")).json()).videos;
    assert.deepEqual(view, { source: "folder", folders: [films] });
    let res = await fetch(own.base + "/api/settings", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ videos: { source: "folder", folders: ["Videos"] } }) });
    assert.equal(res.status, 422, "a folder without a full path is refused");
  } finally {
    await own.stop();
    fs.rmSync(films, { recursive: true, force: true });
  }
});

test("places: every place the photos were taken, on a map; choosing one shows its photos", async () => {
  const map = await startTileServer();
  const own = await startPiDash(`${APPS}\n[photos]\nmap_tiles = "${map.url}"\n`);
  const list = (page) =>
    page.evaluate(() => {
      const p = document.getElementById("places");
      if (!p.classList.contains("open")) return null;
      const rows = [...p.querySelectorAll(".pl-row")];
      const m = p.querySelector(".pl-map");
      return {
        names: rows.map((r) => r.querySelector(".pl-name").textContent),
        subs: rows.map((r) => r.querySelector(".pl-sub").textContent),
        at: rows.findIndex((r) => r.classList.contains("focused")),
        zoom: m ? Number(m.dataset.zoom) : null,
        pins: m ? m.querySelectorAll(".pi-pin").length : 0,
        chosen: m ? m.querySelectorAll(".pi-pin:not(.other)").length : 0,
        tiles: m ? m.querySelectorAll(".pi-tiles img.loaded").length : 0,
        picture: p.querySelector(".pl-picture img")?.getAttribute("src") ?? "",
      };
    });
  try {
    const { page, remote } = await ready(own.base);
    await remote.press("select");
    await focusTile(page, remote, "Places");
    await remote.press("select");
    await page.waitForSelector("#places .pl-map .pi-tiles img.loaded");
    let now = await list(page);
    assert.deepEqual(now.names, ["Cascais, Portugal", "Zermatt, Switzerland"]);
    assert.match(now.subs[0], /^6 photos\s+·\s+2024$/);
    assert.deepEqual([now.at, now.zoom, now.chosen], [0, 6, 1]);
    assert.match(now.picture, /^\/photos\/m-holidays-coast-/, "a photo from the place is shown beside the list");
    assert.equal(await page.evaluate(() => document.querySelector("#places .pi-credit").textContent), "© OpenStreetMap contributors");

    // Down: the next place, and the map follows it.
    await remote.press("down");
    await page.waitForFunction(() => document.querySelector("#places .pl-picture img").getAttribute("src").includes("mountains"));
    await page.waitForTimeout(500);
    now = await list(page);
    assert.equal(now.at, 1);
    await remote.press("down");
    assert.equal((await list(page)).at, 1, "the last place is the end");

    // Left looks from further away: at the scale of a continent both places are in sight.
    await remote.press("left");
    await page.waitForTimeout(300);
    now = await list(page);
    assert.deepEqual([now.zoom, now.pins, now.chosen], [4, 2, 1]);
    await remote.press("left");
    await remote.press("left");
    assert.equal((await list(page)).zoom, 2, "the whole world is as far as it goes");
    await remote.press("right");
    assert.equal((await list(page)).zoom, 4);
    assert.ok(map.asked.some((u) => u.startsWith("/4/")), "the map was fetched through the Pi");

    // OK: the photos from there come next, and the page makes way for them.
    await remote.press("select");
    await page.waitForFunction(() => !document.getElementById("places").classList.contains("open"));
    await page.waitForFunction(() => /The next 6 pictures are from Zermatt, Switzerland\./.test(document.getElementById("toast").textContent));
    await page.waitForFunction(() => [...document.querySelectorAll("#backdrop img.visible")].some((i) => i.src.includes("m-holidays-mountains-")), null, { timeout: 15000 });
    const seen = new Set();
    for (let i = 0; i < 5; i++) {
      const now = await photo(page);
      seen.add(now.split("/photos/")[1].split("?")[0]);
      // A press while one photo still fades into the next is not counted, so
      // let it finish; and the next one is prepared on the Pi first.
      await page.waitForTimeout(1600);
      await remote.press("right");
      await page.waitForFunction((was) => [...document.querySelectorAll("#backdrop img.visible")].sort((a, b) => Number(b.style.zIndex) - Number(a.style.zIndex))[0]?.src !== was, now, { timeout: 15000 });
    }
    assert.ok([...seen].every((k) => k.startsWith("m-holidays-mountains-")), `only photos from there: ${[...seen]}`);
    assert.ok(seen.size >= 4, `and not the same one over and over: ${[...seen]}`);
    assert.deepEqual(page.problems, []);
    await page.context().close();

    // Without the map there is still the list.
    await fetch(own.base + "/api/settings", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ photos: { source: "", intervalSeconds: 30, map: false } }) });
  } finally {
    await own.stop();
    await map.stop();
  }
});

// --- Conveniences -------------------------------------------------------------------------

test("a tile of PiDash's own is hidden with Up, and brought back in the settings; an app is removed from the row", async () => {
  const own = await startPiDash(APPS);
  try {
    const { page, remote } = await ready(own.base);
    await remote.press("select");
    assert.ok((await tiles(page)).includes("Status"));
    await focusTile(page, remote, "Status");
    await remote.press("up");
    assert.deepEqual(await page.evaluate(() => [...document.querySelectorAll("#menu .menu-label")].map((e) => e.textContent)), ["Hide this tile"]);
    await remote.press("select");
    await page.waitForFunction(() => ![...document.querySelectorAll(".tile .tile-label")].some((t) => t.textContent === "Status"));
    assert.deepEqual((await (await fetch(own.base + "/api/config")).json()).display.hidden_tiles, ["status"]);
    // Settings and the apps' own tiles offer no hiding: there would be no way back.
    await focusTile(page, remote, "Settings");
    await remote.press("up");
    assert.equal(await page.evaluate(() => document.getElementById("menu").classList.contains("open")), false);

    // An app: moved, removed, or changed.
    await focusTile(page, remote, "Netflix");
    await remote.press("up");
    assert.deepEqual(await page.evaluate(() => [...document.querySelectorAll("#menu .menu-label")].map((e) => e.textContent)), ["Move", "Remove from the app row", "Rename or change it"]);
    await remote.press("down");
    await remote.press("select");
    await page.waitForFunction(() => ![...document.querySelectorAll(".tile .tile-label")].some((t) => t.textContent === "Netflix"));
    assert.deepEqual(await savedOrder(own.base), ["YouTube"]);
    // It stays hidden when the page is loaded anew.
    await page.reload();
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
    await page.waitForTimeout(1500);
    await remote.press("select");
    const row = await tiles(page);
    assert.ok(!row.includes("Status") && !row.includes("Netflix") && row.includes("YouTube"), String(row));
    assert.deepEqual(page.problems, []);
    await page.context().close();

    // The settings: a box for every tile, the hidden one unticked; ticking it brings it back.
    const settings = await openPage(own.base, "/settings#apps");
    await settings.waitForSelector("input.own-tile[data-tile=status]");
    await settings.waitForFunction(() => !document.querySelector("#main").hidden);
    assert.equal(await settings.evaluate(() => document.querySelector("input.own-tile[data-tile=status]").checked), false);
    assert.equal(await settings.evaluate(() => document.querySelector("input.own-tile[data-tile=weather]").checked), true);
    assert.ok(await settings.evaluate(() => !!document.querySelector("input.own-tile[data-tile=places]") && !!document.querySelector("input.own-tile[data-tile=videos]")), "the new tiles can be hidden too");
    await settings.evaluate(() => {
      const box = document.querySelector("input.own-tile[data-tile=status]");
      box.checked = true;
      box.dispatchEvent(new Event("change", { bubbles: true }));
    });
    await settings.click("#save");
    for (let i = 0; i < 100; i++) {
      try {
        const cfg = await (await fetch(own.base + "/api/config")).json();
        if (cfg.display.hidden_tiles.length === 0) break;
      } catch {
        /* restarting */
      }
      assert.ok(i < 99, "the tile never came back");
      await new Promise((r) => setTimeout(r, 100));
    }
    assert.deepEqual(settings.problems, []);
    await settings.context().close();
  } finally {
    await own.stop();
  }
});

test("how long a photo stays is set in the bar; more like this goes by album, day or year", async () => {
  const own = await startPiDash(APPS);
  try {
    const { page, remote } = await ready(own.base);
    await remote.press("up");
    await focusBar(page, remote, "Time per photo");
    assert.equal((await bar(page)).sub, "30 seconds");
    await remote.press("select");
    await page.waitForFunction(() => document.querySelector("#photo-bar .pb-label small")?.textContent === "1 minute");
    assert.ok(await bar(page), "the bar stays open, to press again");
    await remote.press("select");
    await page.waitForFunction(() => document.querySelector("#photo-bar .pb-label small")?.textContent === "2 minutes");
    assert.equal((await (await fetch(own.base + "/api/photos")).json()).intervalSeconds, 120);
    // The settings page shows the same.
    assert.equal((await (await fetch(own.base + "/api/settings")).json()).photos.intervalSeconds, 120);

    // More like this: a short list to choose from.
    await focusBar(page, remote, "More like this");
    await remote.press("select");
    const choices = await page.evaluate(() => [...document.querySelectorAll("#menu .menu-item")].map((e) => [e.querySelector(".menu-label").textContent, e.querySelector(".menu-hint")?.textContent ?? ""]));
    assert.deepEqual(choices.map((c) => c[0]), ["From this album", "From this day", "From this year"]);
    assert.equal(choices[2][1], "2024");
    assert.match(choices[1][1], /2024$/);
    await remote.press("down");
    await remote.press("down");
    await remote.press("select");
    await page.waitForFunction(() => /The next \d+ pictures are from 2024\./.test(document.getElementById("toast").textContent));
    assert.deepEqual(page.problems, []);
    await page.context().close();
  } finally {
    await own.stop();
  }
});

test("after an update the TV says once what is new; a first start says nothing", async () => {
  const own = await startPiDash(APPS);
  try {
    const { page, remote } = await ready(own.base);
    const open = () => page.evaluate(() => document.getElementById("whatsnew").classList.contains("open"));
    assert.equal(await open(), false, "nothing is said on a first start");
    const version = (await (await fetch(own.base + "/api/config")).json()).version;
    assert.equal(await page.evaluate(() => localStorage.getItem("pidash-seen-version")), version);

    // As if an older version had run here before.
    await page.evaluate(() => localStorage.setItem("pidash-seen-version", "v0.0.1"));
    await page.reload();
    await page.waitForFunction(() => document.getElementById("whatsnew").classList.contains("open"));
    const said = await page.evaluate(() => ({ title: document.querySelector("#whatsnew h2").textContent, lines: [...document.querySelectorAll("#whatsnew li")].map((l) => l.textContent) }));
    assert.equal(said.title, `New in ${version}`);
    assert.ok(said.lines.length >= 5 && said.lines.some((l) => /Trips/.test(l)) && said.lines.some((l) => /quiz/.test(l)), String(said.lines));
    // Any button closes it, and does nothing else.
    await remote.press("select");
    assert.equal(await open(), false);
    assert.equal(await rowOpen(page), false, "the button that closed it did not also open the app row");
    await page.reload();
    await page.waitForFunction(() => document.getElementById("backdrop").classList.contains("has-photo"), null, { timeout: 20000 });
    await page.waitForTimeout(800);
    assert.equal(await open(), false, "it is said once");
    assert.deepEqual(page.problems, []);
    await page.context().close();
  } finally {
    await own.stop();
  }
});

test("settings: the TV can be switched off when nothing was pressed for a while; odd pictures and Bluetooth have their switches", async () => {
  const own = await startPiDash(APPS);
  try {
    const page = await openPage(own.base, "/settings#screen");
    await page.waitForFunction(() => !document.querySelector("#main").hidden);
    const idle = await page.evaluate(() => [...document.getElementById("tv-idle").options].map((o) => o.value));
    assert.deepEqual(idle, ["0", "5", "10", "15", "30", "60", "120", "240"], "from five minutes");
    assert.equal(await page.evaluate(() => document.getElementById("tv-idle").value), "0");
    assert.deepEqual(await page.evaluate(() => ["bt-off", "p-odd", "sys-games-page"].map((id) => document.getElementById(id).checked)), [true, true, true]);
    await page.selectOption("#tv-idle", "5");
    await page.evaluate(() => {
      for (const id of ["bt-off", "p-odd"]) {
        const box = document.getElementById(id);
        box.checked = false;
        box.dispatchEvent(new Event("change", { bubbles: true }));
      }
    });
    await page.click("#save");
    for (let i = 0; i < 100; i++) {
      try {
        const s = await (await fetch(own.base + "/api/settings")).json();
        if (s.system.idleOffMinutes === 5) {
          assert.deepEqual([s.system.bluetoothOff, s.photos.skipOdd, s.system.gamesPage], [false, false, true]);
          break;
        }
      } catch {
        /* restarting */
      }
      assert.ok(i < 99, "the settings were never saved");
      await new Promise((r) => setTimeout(r, 100));
    }
    assert.deepEqual(page.problems, []);
    await page.context().close();

    // The videos: what is asked for follows where they are.
    const apps = await openPage(own.base, "/settings#apps");
    await apps.waitForFunction(() => !document.querySelector("#main").hidden);
    const shown = () => apps.evaluate(() => ["v-folders-row", "v-hint-folder", "v-hint-synology"].map((id) => !document.getElementById(id).hidden));
    assert.deepEqual(await shown(), [false, false, false]);
    await apps.selectOption("#v-source", "folder");
    assert.deepEqual(await shown(), [true, true, false]);
    await apps.selectOption("#v-source", "synology");
    assert.deepEqual(await shown(), [true, false, true]);
    assert.equal(await apps.evaluate(() => document.getElementById("v-folders-label").textContent), "Shared folder on the NAS");
    // Without the NAS of the photos it cannot be saved, and the page says why.
    await apps.click("#save");
    await apps.waitForFunction(() => /NAS account of the photos/i.test(document.body.textContent));
    assert.deepEqual(apps.problems, []);
    await apps.context().close();
  } finally {
    await own.stop();
  }
});

// --- Trips, the quiz, other devices on the TV, controllers, the rain radar, art -----------

// Chooses folders of the sample photos and waits until the slideshow is made of them.
async function chooseFolders(base, ids) {
  await fetch(base + "/api/photos/selection", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ folders: ids.map((id) => ({ id, name: id.split("/").pop(), path: id.replaceAll("/", " / ") })), recursive: true }),
  });
  for (let i = 0; i < 100; i++) {
    const list = (await (await fetch(base + "/api/photos/playlist?limit=100")).json()).items;
    if (list.length > 0 && list.every((it) => ids.some((id) => it.folder.startsWith(id.replaceAll("/", " / "))))) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  assert.fail(`the slideshow never showed ${ids}`);
}

test("trips: the journeys the photos tell of, worked out from places and dates; choosing one plays it", async () => {
  const map = await startTileServer();
  const own = await startPiDash(`${APPS}\n[photos]\nmap_tiles = "${map.url}"\n`);
  try {
    // The usual sample photos tell of no trip, so there is no tile.
    let { page, remote } = await ready(own.base);
    await remote.press("select");
    assert.ok(!(await tiles(page)).includes("Trips"), "no Trips tile without trips");
    await page.context().close();

    await chooseFolders(own.base, ["Journeys"]);
    ({ page, remote } = await ready(own.base));
    await remote.press("select");
    await focusTile(page, remote, "Trips");
    assert.equal(await page.evaluate(() => document.querySelector(".tile.focused .tile-sub").textContent), "2 trips");
    await remote.press("select");
    await page.waitForSelector("#places .pl-map .pi-tiles img.loaded");
    const list = () =>
      page.evaluate(() => ({
        title: document.querySelector("#places h2").textContent,
        names: [...document.querySelectorAll("#places .pl-name")].map((e) => e.textContent),
        subs: [...document.querySelectorAll("#places .pl-sub")].map((e) => e.textContent),
        at: [...document.querySelectorAll("#places .pl-row")].findIndex((r) => r.classList.contains("focused")),
        foot: document.querySelector("#places .help-foot").textContent,
      }));
    let now = await list();
    assert.deepEqual([now.title, now.names, now.at], ["Trips", ["Lisbon, Portugal", "Reykjavik, Iceland"], 0], "the latest first; the photos at home are no trip");
    assert.match(now.subs[0], /^September 2 – 6, 2023\s+·\s+9 photos$/);
    assert.match(now.foot, /choose a trip/);

    await remote.press("down");
    await remote.press("select");
    await page.waitForFunction(() => !document.getElementById("places").classList.contains("open"));
    await page.waitForFunction(() => /The next 7 pictures are from Reykjavik, Iceland\./.test(document.getElementById("toast").textContent));
    // In the order they were taken: the first one first.
    await page.waitForFunction(() => [...document.querySelectorAll("#backdrop img.visible")].some((i) => i.src.includes("m-journeys-iceland-0")), null, { timeout: 15000 });
    assert.deepEqual(page.problems, []);
    await page.context().close();
  } finally {
    await own.stop();
    await map.stop();
  }
});

test("the photo quiz asks which year or where, counts what was right, and offers another round", async () => {
  const own = await startPiDash(APPS);
  try {
    let { page, remote } = await ready(own.base);
    await remote.press("select");
    assert.ok(!(await tiles(page)).includes("Photo quiz"), "no quiz about photos from one year and two places");
    await page.context().close();

    await chooseFolders(own.base, ["Journeys", "Holidays"]);
    ({ page, remote } = await ready(own.base));
    await remote.press("select");
    await focusTile(page, remote, "Photo quiz");
    await remote.press("select");
    await page.waitForSelector("#quiz .qz-answer");
    const now = () =>
      page.evaluate(() => ({
        count: document.querySelector("#quiz .qz-count")?.textContent ?? "",
        question: document.querySelector("#quiz .qz-question")?.textContent ?? "",
        answers: [...document.querySelectorAll("#quiz .qz-text")].map((e) => e.textContent),
        at: [...document.querySelectorAll("#quiz .qz-answer")].findIndex((e) => e.classList.contains("focused")),
        right: [...document.querySelectorAll("#quiz .qz-answer")].findIndex((e) => e.classList.contains("right")),
        wrong: [...document.querySelectorAll("#quiz .qz-answer")].findIndex((e) => e.classList.contains("wrong")),
        score: document.querySelector("#quiz .qz-score")?.textContent ?? "",
        result: document.querySelector("#quiz .qz-result")?.textContent ?? "",
        picture: document.querySelector("#quiz .qz-picture img")?.getAttribute("src") ?? "",
      }));
    let q = await now();
    assert.equal(q.count, "Photo 1 of 10");
    assert.ok(["Which year was this?", "Where was this?"].includes(q.question), q.question);
    assert.equal(q.answers.length, 4);
    assert.equal(new Set(q.answers).size, 4, "four different answers");
    assert.match(q.picture, /^\/photos\/m-/);
    assert.equal(await page.evaluate(() => document.getElementById("stage").classList.contains("panel-open")), true, "the caption that would give it away is out of sight");

    // Down moves, OK answers; the right answer is shown either way.
    await remote.press("down");
    assert.equal((await now()).at, 1);
    await remote.press("select");
    q = await now();
    assert.ok(q.right >= 0, "the right answer is marked");
    assert.equal(q.wrong >= 0, q.right !== 1, "a wrong answer is marked as such");
    assert.equal(q.score === "Right!", q.right === 1);
    // OK goes on without waiting; the number buttons answer directly.
    await remote.press("select");
    assert.equal((await now()).count, "Photo 2 of 10");
    let right = q.right === 1 ? 1 : 0;
    for (let i = 2; i <= 10; i++) {
      await remote.press("1");
      q = await now();
      assert.ok(q.right >= 0, `question ${i} was answered by its number`);
      if (q.right === 0) right++;
      await remote.press("select");
    }
    q = await now();
    assert.equal(q.result, `${right} of 10 right`);
    // Another round, then Back leaves.
    await remote.press("select");
    await page.waitForFunction(() => document.querySelector("#quiz .qz-count")?.textContent === "Photo 1 of 10");
    await remote.press("back");
    assert.equal(await page.evaluate(() => document.getElementById("quiz").classList.contains("open")), false);
    assert.deepEqual(page.problems, []);
    await page.context().close();
  } finally {
    await own.stop();
  }
});

test("other devices on the TV are tiles that switch the TV's input; a controller's buttons are named on screen", async () => {
  const fs = require("node:fs");
  const os = require("node:os");
  const path = require("node:path");
  const home = fs.mkdtempSync(path.join(os.tmpdir(), "pidash-home-"));
  const conf = path.join(home, ".config", "Moonlight Game Streaming Project");
  fs.mkdirSync(conf, { recursive: true });
  fs.writeFileSync(path.join(conf, "Moonlight.conf"), "[hosts]\n1\\hostname=LIVING-PC\n1\\uuid=3F2A9C1E-77AB-4C0D-9E55-0A1B2C3D4E5F\n1\\mac=@ByteArray(\\x1c\\x1b\\r\\xaa\\xbb\\x42)\n1\\localaddress=192.168.1.20\n1\\apps\\1\\name=Desktop\n1\\apps\\1\\id=1\n1\\apps\\2\\name=Hollow Knight\n1\\apps\\2\\id=2\n1\\apps\\size=2\nsize=1\n");
  const config = APPS + '\n[[apps]]\nid = "moonlight"\nname = "Moonlight"\ncommand = "moonlight-qt"\n[[tv.inputs]]\nname = "PlayStation"\nport = 2\n[[tv.inputs]]\nname = "Set-top box"\naddress = "1.2.0.0"\n';
  const own = await startPiDash(config, { HOME: home, PIDASH_PRETEND_TV: "1" });
  const pad = (action, kind) => fetch(own.base + "/api/test/remote", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ action, pad: kind }) });
  try {
    const { page, remote } = await ready(own.base);
    await remote.press("select");
    const row = await tiles(page);
    assert.ok(row.includes("PlayStation") && row.includes("Set-top box"), String(row));
    const hint = () => page.evaluate(() => document.getElementById("rail-hint").textContent);
    assert.match(await hint(), /OK open .* Back close/);

    // A PlayStation controller in the hand: the line names its buttons. The TV's remote brings OK and Back back.
    await pad("right", "playstation");
    await page.waitForFunction(() => /✕ open .* ○ close/.test(document.getElementById("rail-hint").textContent));
    await pad("left", "xbox");
    await page.waitForFunction(() => /Ⓐ open .* Ⓑ close/.test(document.getElementById("rail-hint").textContent));
    await pad("right", "nintendo");
    await page.waitForFunction(() => /Ⓑ open .* Ⓐ close/.test(document.getElementById("rail-hint").textContent));
    await remote.press("left");
    await page.waitForFunction(() => /OK open .* Back close/.test(document.getElementById("rail-hint").textContent));

    // OK on a device: the TV is told to show it.
    await focusTile(page, remote, "PlayStation");
    await remote.press("select");
    await page.waitForFunction(() => /Switching the TV to PlayStation/.test(document.getElementById("toast").textContent));
    await remote.press("select");
    await focusTile(page, remote, "Set-top box");
    await remote.press("select");
    await page.waitForFunction(() => /Set-top box/.test(document.getElementById("toast").textContent));
    await page.waitForTimeout(300);
    assert.equal(await (await fetch(own.base + "/api/test/tv")).text(), "show 2.0.0.0\nshow 1.2.0.0");

    // A controller switches the TV on: what comes up is the games, whatever was open.
    await remote.press("select");
    assert.equal(await rowOpen(page), true);
    await fetch(own.base + "/api/test/remote", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ open: "games" }) });
    await page.waitForFunction(() => document.getElementById("grid-page").classList.contains("open"));
    assert.equal((await grid(page)).title, "Games");
    assert.equal(await rowOpen(page), false);
    // With a controller, the page's own line names its buttons too.
    await pad("right", "playstation");
    await page.waitForFunction(() => /✕ play .* ○ close/.test(document.querySelector("#grid-page .help-foot").textContent));
    assert.deepEqual(page.problems, []);
    await page.context().close();

    // The settings: the devices are listed; the TV is asked what else is connected.
    const settings = await openPage(own.base, "/settings#apps");
    await settings.waitForFunction(() => !document.querySelector("#main").hidden);
    const rows = () => settings.evaluate(() => [...document.querySelectorAll("#ti-list li .what")].map((e) => e.textContent));
    assert.deepEqual(await rows(), ["PlayStationHDMI 2", "Set-top box1.2.0.0"]);
    await settings.click("#ti-find");
    await settings.waitForFunction(() => /1 device added/.test(document.getElementById("ti-result").textContent));
    assert.deepEqual(await rows(), ["PlayStationHDMI 2", "Set-top box1.2.0.0", "HDMI 3HDMI 3"], "what is known already is not added twice; a device without a name is named by its socket");
    await settings.fill("#ti-name", "Console");
    await settings.selectOption("#ti-port", "2");
    await settings.click("#ti-add");
    await settings.waitForFunction(() => /HDMI 2 already has a tile/.test(document.getElementById("ti-result").textContent));
    assert.equal(await settings.evaluate(() => document.getElementById("sys-games-pad").checked), true);
    assert.deepEqual(settings.problems, []);
    await settings.context().close();
  } finally {
    await own.stop();
    fs.rmSync(home, { recursive: true, force: true });
  }
});

test("the rain radar opens from the forecast: the last hour over a map, running up to now", async () => {
  const map = await startTileServer();
  const rain = await startRadarServer();
  const own = await startPiDash(`${APPS}\n[photos]\nmap_tiles = "${map.url}"\n[weather]\nradar_url = "${rain.url}"\n`);
  try {
    const { page, remote } = await ready(own.base);
    await remote.press("weather");
    await page.waitForFunction(() => document.getElementById("weather-panel").classList.contains("open"));
    assert.match(await page.evaluate(() => document.querySelector("#weather-panel .wp-foot").textContent), /▶ rain radar/);
    await remote.press("right");
    await page.waitForSelector("#radar .rd-sheet.shown img.loaded");
    const now = () =>
      page.evaluate(() => {
        const b = document.querySelector("#radar .rd-map");
        return { zoom: Number(b.dataset.zoom), frame: Number(b.dataset.frame), sheets: document.querySelectorAll("#radar .rd-sheet").length, shown: document.querySelectorAll("#radar .rd-sheet.shown").length, clock: document.querySelector("#radar .rd-clock").textContent, title: document.querySelector("#radar h2").textContent };
      });
    let r = await now();
    assert.deepEqual([r.zoom, r.sheets, r.shown], [6, 7, 1], "the last hour: seven pictures, one at a time");
    assert.match(r.title, /Rain radar\s+Sampleville/);
    assert.match(await page.evaluate(() => document.querySelector("#radar .pi-credit").textContent), /OpenStreetMap.*RainViewer/);

    // They run by themselves; an arrow stops them and steps by hand.
    const first = r.frame;
    await page.waitForFunction((was) => Number(document.querySelector("#radar .rd-map").dataset.frame) !== was, first, { timeout: 5000 });
    await remote.press("left");
    r = await now();
    assert.match(r.clock, /❚❚/, "held");
    const held = r.frame;
    await page.waitForTimeout(1500);
    assert.equal((await now()).frame, held, "it stays on the picture that was chosen");
    await remote.press("select");
    await page.waitForFunction((was) => Number(document.querySelector("#radar .rd-map").dataset.frame) !== was, held, { timeout: 5000 });

    // Up looks closer, as close as the radar goes; Down further away.
    await remote.press("up");
    assert.equal((await now()).zoom, 7);
    await remote.press("up");
    assert.equal((await now()).zoom, 7);
    await remote.press("down");
    await remote.press("down");
    assert.equal((await now()).zoom, 5);
    assert.ok(rain.asked.some((u) => /\/256\/5\//.test(u)) && rain.asked.every((u) => !/\/256\/(8|9|1\d)\//.test(u)), "the pieces come through the Pi, none closer than the radar offers");

    // Back returns to the forecast, and Back again to the dashboard.
    await remote.press("back");
    assert.equal(await page.evaluate(() => document.getElementById("radar").classList.contains("open")), false);
    assert.equal(await page.evaluate(() => document.getElementById("weather-panel").classList.contains("open")), true);
    await remote.press("back");
    assert.equal(await page.evaluate(() => document.getElementById("weather-panel").classList.contains("open")), false);
    assert.deepEqual(page.problems, []);
    await page.context().close();

    // Switched off in the settings, the forecast offers nothing.
    const res = await fetch(own.base + "/api/settings", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ weather: { location: "", name: "", units: "metric", radar: false } }) });
    assert.equal(res.status, 200, await res.text());
    for (let i = 0; i < 100; i++) {
      try {
        if ((await (await fetch(own.base + "/api/config")).json()).radar === false) break;
      } catch {
        /* restarting */
      }
      assert.ok(i < 99, "the radar was never switched off");
      await new Promise((r) => setTimeout(r, 100));
    }
    assert.equal((await fetch(own.base + "/api/radar")).status, 404);
  } finally {
    await own.stop();
    await map.stop();
    await rain.stop();
  }
});

test("art mode: paintings in place of the photos, with their label, and back again", async () => {
  const own = await startPiDash(APPS);
  try {
    const { page, remote } = await ready(own.base);
    await remote.press("up");
    await focusBar(page, remote, "Art instead of photos");
    await remote.press("select");
    await page.waitForFunction(() => [...document.querySelectorAll("#backdrop img.visible")].some((i) => new URL(i.src).pathname.startsWith("/art/art-")), null, { timeout: 20000 });
    await page.waitForFunction(() => /Paintings from the Art Institute of Chicago/.test(document.getElementById("toast").textContent));
    // The label: title, painter, year, and whose it is; shown though captions are off.
    await page.waitForFunction(() => /·\s+Sample paintings$/.test(document.getElementById("caption").textContent));
    const label = await page.evaluate(() => document.getElementById("caption").textContent);
    assert.match(label, /^[A-Z][A-Za-z ]+\s+·\s+[A-Z][a-z]+ [A-Z][a-z]+\s+·\s+(c\. )?\d{4}\s+·\s+Sample paintings$/);
    assert.equal(await page.evaluate(() => document.getElementById("overlay").dataset.caption), "on");
    assert.equal((await (await fetch(own.base + "/api/config")).json()).art_on, true);

    // The bar has the way back and how long a painting stays, and nothing about photos.
    await remote.press("up");
    assert.deepEqual((await bar(page)).items, ["Back to your photos", "Time per photo", "Bluetooth"]);
    await remote.press("back");
    // Left and right step through the paintings.
    const before = await photo(page);
    await page.waitForTimeout(1600);
    await remote.press("right");
    await page.waitForFunction((was) => [...document.querySelectorAll("#backdrop img.visible")].sort((a, b) => Number(b.style.zIndex) - Number(a.style.zIndex))[0]?.src !== was, before, { timeout: 15000 });
    assert.match(new URL(await photo(page)).pathname, /^\/art\/art-/);

    // It is kept: a page loaded anew is in art mode still.
    await page.reload();
    await page.waitForFunction(() => [...document.querySelectorAll("#backdrop img.visible")].some((i) => new URL(i.src).pathname.startsWith("/art/")), null, { timeout: 20000 });

    // Back to the photos, at once: also while a painting is still fading in.
    await remote.press("up");
    await focusBar(page, remote, "Back to your photos");
    await remote.press("select");
    await page.waitForFunction(() => [...document.querySelectorAll("#backdrop img.visible")].some((i) => new URL(i.src).pathname.startsWith("/photos/")), null, { timeout: 20000 });
    assert.equal(await page.evaluate(() => document.getElementById("overlay").dataset.caption), "off");
    assert.equal((await (await fetch(own.base + "/api/config")).json()).art_on, false);
    assert.deepEqual(page.problems, []);
    await page.context().close();

    // Not offered when the settings say so.
    const res = await fetch(own.base + "/api/settings", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ photos: { source: "", intervalSeconds: 30, art: false } }) });
    assert.equal(res.status, 200, await res.text());
    for (let i = 0; i < 100; i++) {
      try {
        if ((await (await fetch(own.base + "/api/config")).json()).art === false) break;
      } catch {
        /* restarting */
      }
      assert.ok(i < 99, "art mode was never taken away");
      await new Promise((r) => setTimeout(r, 100));
    }
    assert.equal((await fetch(own.base + "/api/art/playlist")).status, 404);
  } finally {
    await own.stop();
  }
});

test("in a container only the dashboard is served: no apps, nothing of the machine, and the settings say so", async () => {
  // The settings name apps, another device on the TV and a time to switch it off: none of it applies there.
  const own = await startPiDash(APPS + '\n[tv]\noff = "23:00"\n[[tv.inputs]]\nname = "Console"\nport = 2\n', {}, ["-standalone"]);
  try {
    const cfg = await (await fetch(own.base + "/api/config")).json();
    assert.equal(cfg.standalone, true);
    assert.deepEqual(cfg.tv_inputs, []);
    assert.deepEqual((await (await fetch(own.base + "/api/apps")).json()).apps, []);

    const { page, remote } = await ready(own.base);
    await remote.press("select");
    await page.waitForFunction(() => document.getElementById("rail").classList.contains("open"));
    const row = await tiles(page);
    for (const gone of ["Netflix", "YouTube", "Console", "Desktop", "Power", "Bluetooth", "Sound"]) assert.ok(!row.includes(gone), `${gone} is offered: ${row}`);
    for (const kept of ["Weather", "Status", "Settings"]) assert.ok(row.includes(kept), `${kept} is missing: ${row}`);
    assert.deepEqual(page.problems, []);
    await page.context().close();

    // The settings page leaves out what is about the machine, and says how a container is updated.
    const settings = await openPage(own.base, "/settings");
    await settings.waitForSelector("#main:not([hidden])");
    await settings.waitForFunction(() => /container/.test(document.getElementById("up-status").textContent));
    const shown = await settings.evaluate(() => ({
      groups: [...document.querySelectorAll("#groups button")].filter((b) => !b.hidden).map((b) => b.dataset.group),
      cards: [...document.querySelectorAll("#main section.card")].filter((c) => !c.hidden).map((c) => c.querySelector("h2").textContent),
      fields: ["sys-resolution", "tv-idle"].map((id) => document.getElementById(id).closest("label").hidden),
      update: document.getElementById("up-buttons").hidden,
    }));
    assert.deepEqual(shown.groups, ["screen", "photos", "info", "apps", "home", "system"]);
    for (const gone of ["Apps", "Other devices on the TV", "Videos", "Sound", "Bluetooth"]) assert.ok(!shown.cards.includes(gone), `${gone} is offered: ${shown.cards}`);
    for (const kept of ["Weather", "Background photos", "Radio", "Night", "Updates", "Backup"]) assert.ok(shown.cards.includes(kept), `${kept} is missing: ${shown.cards}`);
    assert.deepEqual(shown.fields, [true, true]);
    assert.equal(shown.update, true);
    assert.equal((await fetch(own.base + "/api/update/install", { method: "POST" })).status >= 400, true);
    assert.deepEqual(settings.problems, []);
    await settings.context().close();
  } finally {
    await own.stop();
  }
});
