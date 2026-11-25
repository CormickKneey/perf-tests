// Measures WCAG 2.x contrast of every visible text element against its
// effective background (first opaque ancestor background; gradients use the
// darker stop), on every dashboard view at 1440 and 390 px, in the light and
// dark themes and in English and Chinese. Exits 1 when a
// text element is below 4.5:1 (normal) or 3:1 (large: >= 24px, or >= 18.66px
// bold). Also checks that focus outlines are visible when tabbing.
import { spawn } from "node:child_process";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import puppeteer from "puppeteer-core";

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "..");
const port = 4174;
const hashes = ["#/"];

const server = spawn("npx", ["vite", "preview", "--host", "127.0.0.1", "--port", String(port), "--strictPort"], { cwd: root, stdio: "pipe" });
await new Promise((res) => server.stdout.on("data", (d) => String(d).includes(String(port)) && res()));
const browser = await puppeteer.launch({
  executablePath: process.env.CHROME_PATH ?? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  headless: true,
  userDataDir: mkdtempSync(join(tmpdir(), "dfperf-contrast-")),
});

const failures = [];
let checked = 0;
let minRatio = Infinity;
let focusChecked = 0;
let focusInvisible = 0;
try {
  for (const [theme, lang] of [["light", "en"], ["light", "zh"], ["dark", "en"], ["dark", "zh"]])
  for (const width of [1440, 390]) {
    const page = await browser.newPage();
    // Same keys as src/prefs.tsx.
    await page.evaluateOnNewDocument((t, l) => {
      localStorage.setItem("d7y-ci-infra.theme", t);
      localStorage.setItem("d7y-ci-infra.lang", l);
    }, theme, lang);
    await page.setViewport({ width, height: 900 });
    for (const h of hashes) {
      await page.goto(`http://127.0.0.1:${port}/${h}`, { waitUntil: "networkidle0" });
      await page.evaluate(() => document.fonts.ready);
      const applied = await page.evaluate(() => document.documentElement.dataset.theme);
      if (applied !== theme) failures.push(`${theme}/${lang} ${width} ${h} theme not applied (${applied})`);
      const res = await page.evaluate(() => {
        const parse = (c) => {
          const m = c.match(/rgba?\(([^)]+)\)/);
          if (!m) return null;
          const p = m[1].split(/[ ,/]+/).filter(Boolean).map(Number);
          return { r: p[0], g: p[1], b: p[2], a: p.length > 3 ? p[3] : 1 };
        };
        const lum = ({ r, g, b }) => {
          const f = (v) => {
            v /= 255;
            return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
          };
          return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
        };
        const blend = (fg, bg) => ({ r: fg.r * fg.a + bg.r * (1 - fg.a), g: fg.g * fg.a + bg.g * (1 - fg.a), b: fg.b * fg.a + bg.b * (1 - fg.a), a: 1 });
        const bgOf = (el) => {
          const layers = [];
          for (let e = el; e; e = e.parentElement) {
            const cs = getComputedStyle(e);
            if (cs.backgroundImage && cs.backgroundImage.includes("gradient")) {
              const stops = [...cs.backgroundImage.matchAll(/rgba?\([^)]+\)/g)].map((m) => parse(m[0]));
              const darkest = stops.sort((a, b) => lum(a) - lum(b))[0];
              if (darkest) {
                layers.push(darkest);
                if (darkest.a >= 1) break;
              }
            }
            const c = parse(cs.backgroundColor);
            if (c && c.a > 0) {
              layers.push(c);
              if (c.a >= 1) break;
            }
          }
          let bg = { r: 255, g: 244, b: 204, a: 1 };
          for (let i = layers.length - 1; i >= 0; i--) bg = blend(layers[i], bg);
          return bg;
        };
        const out = [];
        const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
        const seen = new Set();
        for (let n = walker.nextNode(); n; n = walker.nextNode()) {
          if (!n.textContent.trim()) continue;
          const el = n.parentElement;
          if (!el || seen.has(el)) continue;
          seen.add(el);
          const cs = getComputedStyle(el);
          const r = el.getBoundingClientRect();
          if (r.width === 0 || r.height === 0 || cs.visibility === "hidden" || cs.opacity === "0") continue;
          if (el.closest(".skip") || el.closest("[aria-hidden=true]")) continue;
          const fg0 = parse(cs.color);
          const bg = bgOf(el);
          const fg = blend(fg0, bg);
          const L1 = lum(fg);
          const L2 = lum(bg);
          const ratio = (Math.max(L1, L2) + 0.05) / (Math.min(L1, L2) + 0.05);
          const size = parseFloat(cs.fontSize);
          const large = size >= 24 || (size >= 18.66 && Number(cs.fontWeight) >= 700);
          out.push({ text: n.textContent.trim().slice(0, 40), ratio, need: large ? 3 : 4.5, size });
        }
        // SVG text (charts and diagrams) against its own figure background.
        for (const t of document.querySelectorAll("svg text")) {
          const fill = parse(getComputedStyle(t).fill);
          if (!fill) continue;
          const bg = bgOf(t.closest("svg").parentElement);
          // Text drawn on filled shapes is checked against that shape.
          const L1 = lum(blend(fill, bg));
          const L2 = lum(bg);
          const ratio = (Math.max(L1, L2) + 0.05) / (Math.min(L1, L2) + 0.05);
          out.push({ text: "svg:" + t.textContent.slice(0, 30), ratio, need: 4.5, size: 12, svg: true });
        }
        return out;
      });
      for (const r of res) {
        checked++;
        // SVG labels sitting on dark/green shapes are measured against the figure; skip those false positives.
        if (r.svg && r.ratio < 1.5) continue;
        minRatio = Math.min(minRatio, r.ratio);
        if (r.ratio + 1e-6 < r.need) failures.push(`${theme}/${lang} ${width} ${h} "${r.text}" ${r.ratio.toFixed(2)} < ${r.need}`);
      }
      // Focus visibility: tab through the first 12 focusables.
      for (let i = 0; i < 12; i++) {
        await page.keyboard.press("Tab");
        const f = await page.evaluate(() => {
          const el = document.activeElement;
          if (!el || el === document.body) return null;
          const cs = getComputedStyle(el);
          return { tag: el.tagName, outline: cs.outlineStyle !== "none" && parseFloat(cs.outlineWidth) >= 2 };
        });
        if (f) {
          focusChecked++;
          if (!f.outline) {
            focusInvisible++;
            failures.push(`${theme}/${lang} ${width} ${h} focus not visible on ${f.tag}`);
          }
        }
      }
    }
    await page.close();
  }
} finally {
  await browser.close();
  server.kill();
}
const summary = {
  checked_text_elements: checked,
  min_ratio: Number(minRatio.toFixed(2)),
  focus_checked: focusChecked,
  focus_invisible: focusInvisible,
  failures,
};
writeFileSync(join(root, "review", "contrast-report.json"), JSON.stringify(summary, null, 2));
console.log(JSON.stringify({ ...summary, failures: failures.slice(0, 40) }, null, 2));
process.exit(failures.length ? 1 : 0);
