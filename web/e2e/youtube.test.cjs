// YouTube's TV interface without adverts: the small script PiDash loads into
// the browser for it (internal/apps/youtubetv).
//
// YouTube itself is not asked for anything here. The first test runs the
// script on its own; the second loads it into a real browser, the way the
// YouTube app does, with a stand-in for YouTube that answers on this machine.
const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const http = require("node:http");
const os = require("node:os");
const path = require("node:path");
const vm = require("node:vm");

const EXTENSION = path.join(__dirname, "..", "..", "internal", "apps", "youtubetv");

// Answers shaped like the ones YouTube's TV interface gets.
const video = (id) => ({ tileRenderer: { contentId: id, metadata: { tileMetadataRenderer: { title: { simpleText: "Video " + id } } } } });
const PLAYER = {
  responseContext: { visitorData: "x", serviceTrackingParams: [{ service: "GFEEDBACK", params: [{ key: "logged_in", value: "0" }] }] },
  playabilityStatus: { status: "OK", playableInEmbed: true },
  streamingData: { expiresInSeconds: "21540", formats: [{ itag: 18, url: "https://example.invalid/videoplayback?itag=18" }] },
  videoDetails: { videoId: "abc123", title: "A film about a river", lengthSeconds: "600" },
  adPlacements: [{ adPlacementRenderer: { config: { adPlacementConfig: { kind: "AD_PLACEMENT_KIND_START" } }, renderer: { adBreakServiceRenderer: {} } } }],
  adSlots: [{ adSlotRenderer: { adSlotMetadata: { slotId: "1" } } }],
  playerAds: [{ playerLegacyDesktopWatchAdsRenderer: {} }],
  adBreakHeartbeatParams: "Q0FBJTNE",
};
const HOME = {
  responseContext: { visitorData: "x" },
  contents: {
    tvBrowseRenderer: {
      content: {
        tvSurfaceContentRenderer: {
          content: {
            sectionListRenderer: {
              contents: [
                { tvMastheadRenderer: { video: { videoId: "advert" } } },
                { shelfRenderer: { title: { simpleText: "Recommended" }, content: { horizontalListRenderer: { items: [video("a"), { adSlotRenderer: { adSlotMetadata: { slotId: "2" } } }, video("b"), video("c")] } } } },
                { shelfRenderer: { title: { simpleText: "Music" }, content: { horizontalListRenderer: { items: [{ adSlotRenderer: {} }, video("d")] } } } },
              ],
            },
          },
        },
      },
    },
  },
};
const NEXT = { responseContext: {}, playerResponse: { ...PLAYER }, contents: { singleColumnWatchNextResults: { results: { results: { contents: [video("e"), { adSlotRenderer: {} }] } } } }, padding: "x".repeat(200) };

/** What the checks below expect of a JSON.parse that the script has taken over. */
function check(parse) {
  const player = parse(JSON.stringify(PLAYER));
  for (const plan of ["adPlacements", "adSlots", "playerAds", "adBreakHeartbeatParams"]) assert.equal(plan in player, false, `${plan} is gone`);
  assert.deepEqual([player.videoDetails.title, player.playabilityStatus.status, player.streamingData.formats[0].itag], ["A film about a river", "OK", 18], "the video itself is untouched");

  const home = parse(JSON.stringify(HOME));
  const rows = home.contents.tvBrowseRenderer.content.tvSurfaceContentRenderer.content.sectionListRenderer.contents;
  assert.deepEqual(rows.map((r) => Object.keys(r)[0]), ["shelfRenderer", "shelfRenderer"], "the banner above the rows is gone");
  const ids = rows.map((r) => r.shelfRenderer.content.horizontalListRenderer.items.map((i) => i.tileRenderer?.contentId ?? "ADVERT"));
  assert.deepEqual(ids, [["a", "b", "c"], ["d"]], "adverts among the videos are gone, the videos keep their order");

  const next = parse(JSON.stringify(NEXT));
  assert.equal("adPlacements" in next.playerResponse, false, "a video's answer inside another one is cleaned too");
  assert.equal(next.contents.singleColumnWatchNextResults.results.results.contents.length, 1);

  // Everything that is not such an answer comes out exactly as it went in.
  for (const other of [[1, 2, { adSlotRenderer: {} }], { a: 1 }, "text", 42, null, true, { settings: { items: [{ adSlotRenderer: 1 }] }, padding: "y".repeat(300) }]) {
    assert.deepEqual(parse(JSON.stringify(other)), other);
  }
  // A reviver still does its work, and broken text is still an error.
  assert.deepEqual(parse('{"a":1,"b":2}', (k, v) => (typeof v === "number" ? v * 2 : v)), { a: 2, b: 4 });
  assert.throws(() => parse("{not json"), (e) => e.name === "SyntaxError");
}

test("YouTube on the TV: adverts are taken out of YouTube's answers", () => {
  const script = fs.readFileSync(path.join(EXTENSION, "clean.js"), "utf8");
  const world = vm.createContext({});
  vm.runInContext("var window = globalThis;", world);
  vm.runInContext(script, world);
  vm.runInContext(script, world); // loaded twice by mistake: still one layer
  const parse = (...args) => {
    // Out of the other world and back, so that values compare as plain ones.
    world.args = args;
    try {
      const out = vm.runInContext("JSON.stringify({ v: JSON.parse(...args) })", world);
      return JSON.parse(out).v;
    } catch (e) {
      const err = new SyntaxError(e.message);
      if (e.name !== "SyntaxError") err.name = e.name;
      throw err;
    }
  };
  check(parse);
});

test("YouTube on the TV: the browser loads the script for the TV interface, and only for it", async () => {
  const { chromium } = require("playwright");
  // A stand-in for YouTube: a page that reads an answer the way YouTube's does.
  const page = `<!doctype html><title>stand-in</title><script>
    window.answers = { player: JSON.parse(${JSON.stringify(JSON.stringify(PLAYER))}), home: JSON.parse(${JSON.stringify(JSON.stringify(HOME))}) };
  </script>`;
  const server = http.createServer((req, res) => res.writeHead(200, { "Content-Type": "text/html" }).end(page));
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  const profile = fs.mkdtempSync(path.join(os.tmpdir(), "pidash-yt-"));
  const browser = await chromium.launchPersistentContext(profile, {
    channel: "chromium",
    args: [
      `--load-extension=${EXTENSION}`,
      `--disable-extensions-except=${EXTENSION}`,
      "--disable-features=DisableLoadExtensionCommandLineSwitch",
      `--host-resolver-rules=MAP www.youtube.com:80 127.0.0.1:${server.address().port}`,
      "--no-proxy-server",
    ],
  });
  try {
    const tab = await browser.newPage();
    const seen = async (address) => {
      await tab.goto(address);
      return tab.evaluate(() => ({
        plans: ["adPlacements", "adSlots", "playerAds"].filter((k) => k in window.answers.player),
        rows: window.answers.home.contents.tvBrowseRenderer.content.tvSurfaceContentRenderer.content.sectionListRenderer.contents.length,
        title: window.answers.player.videoDetails.title,
      }));
    };
    assert.deepEqual(await seen("http://www.youtube.com/tv"), { plans: [], rows: 2, title: "A film about a river" }, "on the TV interface the adverts are gone");
    assert.deepEqual(await seen("http://www.youtube.com/tv#/watch?v=abc123"), { plans: [], rows: 2, title: "A film about a river" });
    // The ordinary website is left alone: the script is made for the TV interface.
    assert.deepEqual(await seen("http://www.youtube.com/watch?v=abc123"), { plans: ["adPlacements", "adSlots", "playerAds"], rows: 3, title: "A film about a river" });
  } finally {
    await browser.close();
    server.close();
    fs.rmSync(profile, { recursive: true, force: true });
  }
});
