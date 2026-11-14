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

module.exports = { startPiDash, openPage, closeBrowser, remoteFor };
