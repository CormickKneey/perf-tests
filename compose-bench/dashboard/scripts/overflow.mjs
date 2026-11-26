// Lists elements wider than the viewport (debug helper for layout checks).
//   node scripts/overflow.mjs <hash> <width>   (expects `npm run preview` on 4173)
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import puppeteer from "puppeteer-core";

const [hash = "#/", width = "390"] = process.argv.slice(2);
const browser = await puppeteer.launch({
  executablePath: process.env.CHROME_PATH ?? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  headless: true,
  userDataDir: mkdtempSync(join(tmpdir(), "dfperf-overflow-")),
});
const page = await browser.newPage();
await page.setViewport({ width: Number(width), height: 900 });
await page.goto("http://127.0.0.1:4173/" + hash, { waitUntil: "networkidle0" });
const res = await page.evaluate(() => {
  const vw = window.innerWidth;
  const out = [];
  for (const el of document.querySelectorAll("body *")) {
    const r = el.getBoundingClientRect();
    if (r.right > vw + 1 && r.width > 0 && !el.closest(".table-wrap, pre, .cmd code, .prose table")) {
      out.push(`${el.tagName.toLowerCase()}.${el.className} right=${Math.round(r.right)} w=${Math.round(r.width)}`);
    }
  }
  return out.slice(0, 25);
});
console.log(res.join("\n") || "no overflow");
await browser.close();
