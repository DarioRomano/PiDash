// A website app loads the browser extensions that were put into its folder.
//
// PiDash only names those folders to the browser when it starts the app.
// This checks, in a real browser, that the way it names them works: an
// extension from a folder is loaded and acts on the pages it is made for.
// Nothing leaves this machine: every request is answered here.
const nodeTest = require("node:test");
// Each test fails by name after three minutes, rather than holding up the whole run.
const test = (name, fn) => nodeTest.test(name, { timeout: 180_000 }, fn);
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

test("a browser extension from a folder is loaded, and acts only where it says it does", async () => {
  const { chromium } = require("playwright");
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "pidash-ext-"));
  const extension = path.join(dir, "extensions", "videos", "marker");
  fs.mkdirSync(extension, { recursive: true });
  fs.writeFileSync(
    path.join(extension, "manifest.json"),
    JSON.stringify({
      manifest_version: 3,
      name: "Marker",
      version: "1.0",
      content_scripts: [{ matches: ["*://videos.example/tv*"], js: ["mark.js"], run_at: "document_start", world: "MAIN" }],
    }),
  );
  fs.writeFileSync(path.join(extension, "mark.js"), "window.markedByTheExtension = true;");
  const browser = await chromium.launchPersistentContext(path.join(dir, "profile"), {
    channel: "chromium",
    // As PiDash starts an app's browser when its folder holds an extension.
    args: [`--load-extension=${extension}`, `--disable-extensions-except=${extension}`, "--disable-features=TranslateUI,Translate,DisableLoadExtensionCommandLineSwitch"],
  });
  try {
    let asked = 0;
    await browser.route("**/*", (route) => {
      asked++;
      return route.fulfill({ status: 200, contentType: "text/html", body: "<!doctype html><title>stand-in</title><script>window.sawItAtStart = window.markedByTheExtension === true;</script>" });
    });
    const tab = await browser.newPage();
    const seen = async (address) => {
      await tab.goto(address);
      return tab.evaluate(() => [window.markedByTheExtension === true, window.sawItAtStart]);
    };
    assert.deepEqual(await seen("https://videos.example/tv"), [true, true], "loaded before the page's own scripts ran");
    assert.deepEqual(await seen("https://videos.example/other"), [false, false], "not on pages it is not made for");
    assert.deepEqual(await seen("https://elsewhere.example/tv"), [false, false]);
    assert.ok(asked >= 3, "the pages came from the stand-in");
  } finally {
    await browser.close();
    fs.rmSync(dir, { recursive: true, force: true });
  }
});
