// Starts PiDash for the end-to-end tests and opens pages on it.
//
// The backend runs in its development mode (-mock): sample photos, sample
// weather, apps that only pretend to start. Nothing outside a temporary
// directory is touched.
const { spawn } = require("node:child_process");
const fs = require("node:fs");
const net = require("node:net");
const os = require("node:os");
const path = require("node:path");
const { chromium } = require("playwright");

const BINARY = process.env.PIDASH_BIN || path.join(__dirname, "..", "..", "bin", "pidash");

function freePort() {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.once("error", reject);
    srv.listen(0, "127.0.0.1", () => {
      const { port } = srv.address();
      srv.close(() => resolve(port));
    });
  });
}

/** Starts a backend with the given config.toml text; returns its address and a stop function. */
async function startPiDash(config = "", extraEnv = {}) {
  if (!fs.existsSync(BINARY)) throw new Error(`${BINARY} is missing: run "make build" first`);
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "pidash-e2e-"));
  const cfg = path.join(dir, "config", "config.toml");
  fs.mkdirSync(path.dirname(cfg), { recursive: true });
  fs.writeFileSync(cfg, "[display]\ndebug_keys = false\n" + config);
  const port = await freePort();
  const env = { ...process.env, XDG_CONFIG_HOME: path.join(dir, "xdg-config"), XDG_DATA_HOME: path.join(dir, "data"), XDG_CACHE_HOME: path.join(dir, "cache"), XDG_RUNTIME_DIR: path.join(dir, "run"), XDG_DATA_DIRS: path.join(dir, "share"), ...extraEnv };
  fs.mkdirSync(env.XDG_RUNTIME_DIR, { recursive: true });
  let log = "";
  const child = spawn(BINARY, ["-config", cfg, "-listen", `127.0.0.1:${port}`, "-mock"], { env, stdio: ["ignore", "pipe", "pipe"] });
  child.stdout.on("data", (d) => (log += d));
  child.stderr.on("data", (d) => (log += d));
  const base = `http://127.0.0.1:${port}`;
  for (let i = 0; ; i++) {
    try {
      if ((await fetch(base + "/healthz")).ok) break;
    } catch {
      /* not up yet */
    }
    if (i > 100) throw new Error("PiDash did not start:\n" + log);
    await new Promise((r) => setTimeout(r, 100));
  }
  return {
    base,
    dir,
    log: () => log,
    stop: async () => {
      child.kill();
      await new Promise((r) => child.once("exit", r));
      fs.rmSync(dir, { recursive: true, force: true });
    },
  };
}

let browser;
async function openPage(base, where = "/") {
  browser ??= await chromium.launch({ args: ["--autoplay-policy=no-user-gesture-required"] });
  const context = await browser.newContext({ viewport: { width: 1920, height: 1080 } });
  const page = await context.newPage();
  page.problems = [];
  page.on("pageerror", (e) => page.problems.push(String(e)));
  // Something the pages' own rules about what they may load refused.
  page.on("console", (m) => {
    if (m.type() === "error" && /Content Security Policy/i.test(m.text())) page.problems.push(m.text());
  });
  await page.goto(base + where);
  return page;
}
async function closeBrowser() {
  await browser?.close();
  browser = undefined;
}

/**
 * The buttons of a TV remote, as PiDash gets them.
 *
 * remote.press sends a button the way the backend reports it after reading
 * the remote itself; this works whether or not the browser has the keyboard.
 * keyboard presses the matching key in the browser. both does the two at
 * once, which is what happens on a Pi whose browser window has the keyboard:
 * one press, reported twice.
 */
const KEYS = { up: "ArrowUp", down: "ArrowDown", left: "ArrowLeft", right: "ArrowRight" };
function remoteFor(page, base) {
  const press = async (action) => {
    const res = await fetch(base + "/api/test/remote", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ action }) });
    if (!res.ok) throw new Error(`remote ${action}: ${res.status}`);
    await page.waitForTimeout(150);
  };
  return {
    press,
    keyboard: async (action) => {
      await page.keyboard.press(KEYS[action]);
      await page.waitForTimeout(150);
    },
    both: async (action) => {
      await Promise.all([page.keyboard.press(KEYS[action]), fetch(base + "/api/test/remote", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ action }) })]);
      await page.waitForTimeout(400); // longer than the time within which two reports count as one press
    },
  };
}

/**
 * A map service for the tests: every tile is a pale square with a frame, so a
 * screenshot shows where the pieces lie. asked lists the tiles requested.
 */
async function startTileServer() {
  const http = require("node:http");
  const zlib = require("node:zlib");
  const crc = (buf) => {
    let c = ~0;
    for (const b of buf) {
      c ^= b;
      for (let k = 0; k < 8; k++) c = (c >>> 1) ^ (0xedb88320 & -(c & 1));
    }
    return ~c >>> 0;
  };
  const chunk = (type, data) => {
    const head = Buffer.alloc(4);
    head.writeUInt32BE(data.length);
    const body = Buffer.concat([Buffer.from(type), data]);
    const sum = Buffer.alloc(4);
    sum.writeUInt32BE(crc(body));
    return Buffer.concat([head, body, sum]);
  };
  const size = 256;
  const tile = (x, y) => {
    const rows = Buffer.alloc(size * (1 + size * 3));
    for (let py = 0; py < size; py++) {
      const row = py * (1 + size * 3);
      for (let px = 0; px < size; px++) {
        const edge = px < 2 || py < 2;
        const road = Math.abs(px - py) < 3 || Math.abs(px + py - size) < 2;
        const water = (x + y) % 3 === 0 && px > 150 && py > 120;
        const [r, g, b] = edge ? [150, 160, 170] : road ? [255, 255, 255] : water ? [170, 211, 223] : [(x + y) % 2 ? 226 : 236, 234, 214];
        rows.set([r, g, b], row + 1 + px * 3);
      }
    }
    const header = Buffer.alloc(13);
    header.writeUInt32BE(size, 0);
    header.writeUInt32BE(size, 4);
    header.set([8, 2, 0, 0, 0], 8);
    return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk("IHDR", header), chunk("IDAT", zlib.deflateSync(rows)), chunk("IEND", Buffer.alloc(0))]);
  };
  const asked = [];
  const agents = new Set();
  const server = http.createServer((req, res) => {
    const m = /^\/(\d+)\/(\d+)\/(\d+)\.png$/.exec(req.url);
    if (!m) return res.writeHead(404).end();
    asked.push(req.url);
    agents.add(req.headers["user-agent"]);
    res.writeHead(200, { "Content-Type": "image/png" }).end(tile(Number(m[2]), Number(m[3])));
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  return {
    url: `http://127.0.0.1:${server.address().port}/{z}/{x}/{y}.png`,
    asked,
    agents,
    stop: () => new Promise((r) => server.close(r)),
  };
}

module.exports = { startPiDash, openPage, closeBrowser, remoteFor, startTileServer };
