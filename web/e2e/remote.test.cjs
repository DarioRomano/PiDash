// The TV remote must keep working. These tests press its buttons the three
// ways a press can reach a page (see harness.cjs) and check what happens on
// the dashboard, in the app row and on the settings page.
//
// Run with: make e2e
const { test, before, after } = require("node:test");
const assert = require("node:assert/strict");
const { startPiDash, openPage, closeBrowser, remoteFor } = require("./harness.cjs");

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

const view = (page) => page.evaluate(() => document.getElementById("overlay").dataset.density);
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

  test(`dashboard: Up and Down change what is shown (${name})`, async () => {
    const page = await dashboard();
    const remote = remoteFor(page, pi.base);
    assert.equal(await view(page), "compact");
    await remote[how]("up");
    assert.equal(await view(page), "detailed", "Up shows more");
    await remote[how]("down");
    assert.equal(await view(page), "compact", "one Down goes back exactly one step");
    await remote[how]("down");
    assert.equal(await view(page), "photo", "Down again leaves only the photo");
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
  await page.waitForURL("**/settings?from=tv");
  await page.waitForSelector(".tv-focus");
  await remote.press("back");
  await page.waitForURL(pi.base + "/");
  await page.context().close();
});

const highlighted = (page) => page.evaluate(() => document.querySelector(".tv-focus")?.id ?? "");

for (const how of ["press", "keyboard", "both"]) {
  const name = { press: "read by the backend only", keyboard: "as keys in the browser only", both: "reported twice" }[how];
  test(`settings on the TV: arrows move the highlight one control at a time (${name})`, async () => {
    const page = await openPage(pi.base, "/settings?from=tv");
    const remote = remoteFor(page, pi.base);
    await page.waitForSelector(".tv-focus");
    await page.waitForTimeout(600);
    assert.equal(await highlighted(page), "w-location", "the first field is highlighted");
    await remote[how]("down");
    assert.equal(await highlighted(page), "w-units");
    await remote[how]("down");
    assert.equal(await highlighted(page), "p-source", "a second Down moves on by one, not two");
    await remote[how]("up");
    assert.equal(await highlighted(page), "w-units");
    await remote[how]("right");
    assert.equal(await highlighted(page), "w-test", "Right reaches the button beside the list");
    assert.deepEqual(page.problems, []);
    await page.context().close();
  });
}

test("settings on the TV: the keyboard opens with OK, types, and closes with Back", async () => {
  const page = await openPage(pi.base, "/settings?from=tv");
  const remote = remoteFor(page, pi.base);
  await page.waitForSelector(".tv-focus");
  await page.waitForTimeout(600);
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
  const page = await openPage(pi.base, "/settings?from=tv");
  const remote = remoteFor(page, pi.base);
  await page.waitForSelector(".tv-focus");
  await page.waitForTimeout(600);
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
  for (let i = 0; i < 14; i++) {
    if ((await page.evaluate(() => document.querySelector(".tile.focused .tile-label")?.textContent)) === label) return;
    await remote.press("right");
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

      await remote[how]("up");
      assert.equal(await carried(page), "Netflix", "Up picks the selected app up");
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
  // Buttons can be tried out without the page going away.
  for (const action of ["left", "right", "up", "down"]) await remote.press(action);
  assert.equal(await open(), true, "arrows leave the status page open");
  await remote.press("back");
  assert.equal(await open(), false, "Back closes it");
  assert.equal(await view(page), "compact", "the arrows pressed meanwhile changed nothing underneath");
  assert.deepEqual(page.problems, []);
  await page.context().close();
});

// --- Emulators -----------------------------------------------------------------

test("settings: emulators are offered, with how to install one that is missing", async () => {
  const page = await openPage(pi.base, "/settings");
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
