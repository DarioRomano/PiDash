// The TV remote must keep working. These tests press its buttons the three
// ways a press can reach a page (see harness.cjs) and check what happens on
// the dashboard, in the app row and on the settings page.
//
// Run with: make e2e
const { test, before, after } = require("node:test");
const assert = require("node:assert/strict");
const { startPiDash, openPage, closeBrowser, remoteFor, startTileServer } = require("./harness.cjs");

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

// --- The picture reaches every edge ----------------------------------------------

test("burn-in protection moves texts and tiles, never the photo or its darkening", async () => {
  // A zoomed photo reaches past the screen's edge. If the darkening on top of
  // it moved with the texts, a lighter strip of photo would show at the edge.
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
  assert.deepEqual(await box("scrim"), screen, "the darkening still covers the whole screen");
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
    const page = await openPage(own.base, "/settings?from=tv");
    const remote = remoteFor(page, own.base);
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
  assert.equal(await view(page), "compact");
  await held(page, pi.base, "up", 2);
  assert.equal(await view(page), "compact", "holding Up changes nothing either");

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
  const page = await openPage(pi.base, "/settings?from=tv");
  const remote = remoteFor(page, pi.base);
  await page.waitForSelector(".tv-focus");
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
  // Open the row, move, move back, bump into its start, close; less detail, and no less than that.
  assert.equal(await sounds(page), "open move move edge back move edge");
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
    const page = await openPage(own.base, "/settings");
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
  const page = await openPage(pi.base, "/settings");
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

test("the games of the PC, from Moonlight, are tiles that start with one press", async () => {
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
  const own = await startPiDash(APPS + '\n[[apps]]\nid = "moonlight"\nname = "Moonlight"\ncommand = "moonlight-qt"\n', { HOME: home });
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
  assert.equal(await view(page) !== undefined, true);

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

    await remote.press("select");
    assert.ok(!(await tiles(page)).includes("Only favourites"), "no such tile without favourites");
    await focusTile(page, remote, "Add to favourites");
    await remote.press("select");
    await page.waitForFunction(() => [...document.querySelectorAll(".tile .tile-label")].some((t) => t.textContent === "Remove from favourites"));
    assert.equal((await status()).favourites, 1);
    assert.match(await page.evaluate(() => document.getElementById("caption").textContent), /^★/, "a star before the caption");
    assert.equal(await rowOpen(page), true, "the row stays, to take it back at once");

    // Only favourites: the slideshow has that one photo and nothing else.
    await focusTile(page, remote, "Only favourites");
    assert.equal(await page.evaluate(() => document.querySelector(".tile.focused .tile-sub")?.textContent), "1 photo");
    await remote.press("select");
    await page.waitForFunction(() => /Showing only your favourites/.test(document.getElementById("toast").textContent));
    let list = (await (await fetch(own.base + "/api/photos/playlist?limit=50")).json()).items;
    assert.deepEqual(list.map((i) => [i.key, i.favourite]), [[shown(), true]]);
    assert.equal((await status()).onlyFavourites, true);

    // Taking the last favourite away brings all photos back.
    await remote.press("select");
    await page.waitForFunction(() => [...document.querySelectorAll(".tile .tile-label")].some((t) => t.textContent === "Remove from favourites"));
    await focusTile(page, remote, "Remove from favourites");
    await remote.press("select");
    await page.waitForFunction(() => [...document.querySelectorAll(".tile .tile-label")].some((t) => t.textContent === "Add to favourites"));
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
    await remote.press("select");
    await focusTile(page, remote, "About this photo");
    return { page, remote };
  };
  try {
    // Photos from the coast know where they were taken.
    await choose("Holidays/Coast");
    let { page, remote } = await open();
    assert.equal(await page.evaluate(() => document.querySelector(".tile.focused .tile-sub")?.textContent), "Cascais, Portugal");
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
    assert.ok(map.asked.length >= 4 && map.asked.every((u) => u.startsWith("/10/")), `tiles asked for: ${map.asked}`);
    assert.ok(map.asked.includes("/10/485/392.png"), `the tile with Cascais on it: ${map.asked}`);
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
    assert.equal(await zoom(), "13");
    await remote.press("up");
    assert.equal(await zoom(), "13");
    for (let i = 0; i < 4; i++) await remote.press("down");
    assert.equal(await zoom(), "4");
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

  const own = await startPiDash(APPS + '\n[[apps]]\nid = "retroarch"\nname = "RetroArch"\ncommand = "retroarch -f"\n', { HOME: home });
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
