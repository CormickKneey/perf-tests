// Full-page screenshots of the built dashboard at 1440 and 390 px using the
// locally installed Chrome (isolated temporary profile). Usage:
//   npm run build && npm run shots [-- --only landing]
import { spawn } from "node:child_process";
import { mkdirSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import puppeteer from "puppeteer-core";

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "..");
const out = join(root, "review");
mkdirSync(out, { recursive: true });
const chrome = process.env.CHROME_PATH ?? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const port = Number(process.env.PORT ?? 4173);
const base = `http://127.0.0.1:${port}/`;
const only = process.argv.includes("--only") ? process.argv[process.argv.indexOf("--only") + 1] : null;

const pages = [
  { name: "dashboard", hash: "#/" },
].filter((p) => !p.hash.endsWith("undefined") && (!only || p.name === only));

const server = spawn("npx", ["vite", "preview", "--host", "127.0.0.1", "--port", String(port), "--strictPort"], { cwd: root, stdio: "pipe" });
await new Promise((res, rej) => {
  const t = setTimeout(() => rej(new Error("preview did not start")), 15000);
  server.stdout.on("data", (d) => {
    if (String(d).includes(String(port))) {
      clearTimeout(t);
      res();
    }
  });
});

const browser = await puppeteer.launch({
  executablePath: chrome,
  headless: true,
  userDataDir: mkdtempSync(join(tmpdir(), "dfperf-shots-")),
  args: ["--no-first-run", "--no-default-browser-check", "--hide-scrollbars"],
});
try {
  for (const width of [1440, 390]) {
    const page = await browser.newPage();
    await page.setViewport({ width, height: width === 1440 ? 900 : 844, deviceScaleFactor: 1 });
    for (const p of pages) {
      await page.goto(base + p.hash, { waitUntil: "networkidle0" });
      await page.evaluate(() => document.fonts.ready);
      await new Promise((r) => setTimeout(r, 400));
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
      const file = join(out, `${p.name}-${width}.png`);
      await page.screenshot({ path: file, fullPage: true });
      console.log(`${file}${overflow > 0 ? `  WARNING horizontal overflow ${overflow}px` : ""}`);
      if (process.env.SEGMENTS_DIR) {
        // Viewport-sized slices for close review of long pages.
        const h = await page.evaluate(() => document.documentElement.scrollHeight);
        const vh = width === 1440 ? 900 : 844;
        for (let y = 0, i = 0; y < h; y += vh, i++) {
          await page.screenshot({ path: join(process.env.SEGMENTS_DIR, `${p.name}-${width}-${String(i).padStart(2, "0")}.png`), clip: { x: 0, y, width, height: Math.min(vh, h - y) } });
        }
      }
      if (p.name === "landing" && width === 1440) {
        await page.screenshot({ path: join(out, "first-screen.png") });
        console.log(join(out, "first-screen.png"));
      }
    }
    await page.close();
  }
} finally {
  await browser.close();
  server.kill();
}
