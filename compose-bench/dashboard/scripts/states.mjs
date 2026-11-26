// Captures interaction / data states of the dashboard for review: chart hover
// tooltip, filter focus, a filter change mid-transition and settled, an
// expanded stack row, plus loading, empty and error. Empty and loading are
// produced by intercepting the data requests; no invented runs are shown.
//   npm run build && node scripts/states.mjs
import { spawn } from "node:child_process";
import { mkdirSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import puppeteer from "puppeteer-core";

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "..");
const out = join(root, "review", "states");
mkdirSync(out, { recursive: true });
const port = 4175;
const base = `http://127.0.0.1:${port}/`;
const server = spawn("npx", ["vite", "preview", "--host", "127.0.0.1", "--port", String(port), "--strictPort"], { cwd: root, stdio: "pipe" });
await new Promise((res) => server.stdout.on("data", (d) => String(d).includes(String(port)) && res()));
const browser = await puppeteer.launch({
  executablePath: process.env.CHROME_PATH ?? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  headless: true,
  userDataDir: mkdtempSync(join(tmpdir(), "dfperf-states-")),
});
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
const shot = async (page, name, clip) => {
  await page.screenshot({ path: join(out, name), ...(clip ? { clip } : {}) });
  console.log(join(out, name));
};
const intercept = async (page, handler) => {
  await page.setRequestInterception(true);
  page.on("request", (req) => (req.url().includes("/data/") ? handler(req) : req.continue()));
};
const emptyIndex = JSON.stringify({ schema: "dfperf/v1", generated_at: null, runs: [] });

try {
  for (const width of [1440, 390]) {
    const page = await browser.newPage();
    await page.setViewport({ width, height: 900 });
    await page.goto(base + "#/", { waitUntil: "networkidle0" });
    await page.evaluate(() => document.fonts.ready);
    await wait(1400); // entrance animations

    // chart hover: tooltip with stack breakdown and platform metadata
    const hit = await page.$(".hit");
    await hit.hover();
    await wait(450);
    await shot(page, `hover-${width}.png`, { x: 0, y: 0, width, height: 900 });
    await page.mouse.move(2, 2);

    // filter focus
    await page.focus(".filter select");
    await page.keyboard.press("Shift");
    await wait(200);
    await shot(page, `focus-${width}.png`, { x: 0, y: 0, width, height: 200 });

    // metric change: a frame mid-tween, then settled
    const selects = await page.$$(".filter select");
    await selects[2].select("mibs");
    await wait(140);
    await shot(page, `metric-tween-${width}.png`, { x: 0, y: 0, width, height: 560 });
    await wait(700);
    await shot(page, `metric-settled-${width}.png`, { x: 0, y: 0, width, height: 560 });

    // expanded stack row
    await page.click(".row");
    await wait(450);
    const tbl = await (await page.$(".table-wrap")).boundingBox();
    await shot(page, `expanded-${width}.png`, { x: 0, y: Math.max(0, tbl.y - 20), width, height: tbl.height + 40 });
    await page.close();

    // loading: delay the data requests
    const slow = await browser.newPage();
    await slow.setViewport({ width, height: 900 });
    await intercept(slow, async (req) => {
      await wait(4000);
      req.continue().catch(() => {});
    });
    await slow.goto(base + "#/", { waitUntil: "domcontentloaded" });
    await wait(700);
    await shot(slow, `loading-${width}.png`);
    await slow.close();

    // empty: a schema-valid empty index, as produced when results/ is empty
    const empty = await browser.newPage();
    await empty.setViewport({ width, height: 900 });
    await intercept(empty, (req) => (req.url().endsWith("/data/index.json") ? req.respond({ status: 200, contentType: "application/json", body: emptyIndex }) : req.continue()));
    await empty.goto(base + "#/", { waitUntil: "networkidle0" });
    await wait(400);
    await shot(empty, `empty-${width}.png`);
    await empty.close();

    // error: index unavailable
    const err = await browser.newPage();
    await err.setViewport({ width, height: 900 });
    await intercept(err, (req) => (req.url().endsWith("/data/index.json") ? req.respond({ status: 503, body: "unavailable" }) : req.continue()));
    await err.goto(base + "#/", { waitUntil: "networkidle0" });
    await wait(400);
    await shot(err, `error-${width}.png`);
    await err.close();
  }
} finally {
  await browser.close();
  server.kill();
}
