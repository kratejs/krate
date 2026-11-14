// Drives a production build (trimmed runtime) of a small page in a real
// browser. Static and unit checks passed while the trimmed runtime lacked the
// list feature and krateParam, so this checks what the page does.
// mode "main" is a page without Suspense (the runtime stays trimmed); a page
// with Suspense streams, which keeps the whole runtime, so it is built apart.
import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { chromium, firefox, webkit } from "playwright";

const [root, browserName = "chromium", mode = "main"] = process.argv.slice(2);
const browserType = { chromium, firefox, webkit }[browserName];
if (!root || !browserType) {
  console.error("usage: trimmed_runtime_browser.mjs <dist-root> [chromium|firefox|webkit] [main|suspense]");
  process.exit(2);
}
const mime = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css" };
const server = http.createServer((req, res) => {
  let p = decodeURIComponent(req.url.split("?")[0]);
  if (p.endsWith("/")) p += "index.html";
  fs.readFile(path.join(root, p), (err, body) => {
    if (err) {
      res.statusCode = 404;
      res.end("not found");
      return;
    }
    res.setHeader("content-type", mime[path.extname(p)] || "application/octet-stream");
    res.end(body);
  });
});

const failures = [];
const errors = [];
const expect = (label, got, want) => {
  if (got !== want) failures.push(`${label}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
};

const checks = {
  async main(page, text, settle) {
    // Keyed list: every later change used to be ignored.
    expect("list initial", await text("#list"), "abc");
    await page.click("#rev");
    await settle();
    expect("list reversed", await text("#list"), "cba");
    await page.click("#add");
    await settle();
    expect("list appended", await text("#list"), "cbaz");

    // A custom hook that takes an argument.
    expect("hook initial", await text("#hook"), "5");
    await page.click("#hook");
    await settle();
    expect("hook after click", await text("#hook"), "6");

    // A branch whose guard is false must not evaluate its slots.
    expect("guard initial", await text("#guard"), "");
    await page.click("#login");
    await settle();
    expect("guard shown", await text("#guard"), "Annhas2tags");
    await page.click("#logout");
    await settle();
    expect("guard hidden", await text("#guard"), "");

    // Destructured locals without a build-time value.
    expect("destructure", await text("#destruct"), "T3Y");

    // A function prop called from a slot.
    expect("function prop", await text("#fprop"), "X");

    // createPortal inside a logical child.
    expect("portal closed", await page.locator("body > #modal").count(), 0);
    await page.click("#open");
    await settle();
    expect("portal open", await page.locator("body > #modal").count(), 1);
    await page.click("#open");
    await settle();
    expect("portal closed again", await page.locator("body > #modal").count(), 0);
  },
  async twice(page, text, settle) {
    expect("first copy hydrated", await text("#first .probe"), "hydrated");
    expect("second copy hydrated", await text("#second .probe"), "hydrated");
  },
  async suspense(page, text, settle) {
    expect("suspense initial", await text("#sus"), "count0");
    await page.click("#inc");
    await settle();
    expect("suspense after click", await text("#sus"), "count1");
    expect("suspense branch initial", await text("#pick"), "A");
    await page.click("#tog");
    await settle();
    expect("suspense branch toggled", await text("#pick"), "B");
  },
};

let browser;
try {
  if (!checks[mode]) throw new Error(`unknown mode ${mode}`);
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  browser = await browserType.launch({ headless: true });
  const page = await (await browser.newContext()).newPage();
  page.on("pageerror", (e) => errors.push("pageerror: " + e.message));
  page.on("console", (m) => {
    if (m.type() === "error" && !m.text().startsWith("Failed to load resource")) errors.push("console: " + m.text());
  });
  await page.goto(`http://127.0.0.1:${server.address().port}/`, { waitUntil: "networkidle" });
  const text = async (sel) => (await page.locator(sel).innerText()).replace(/\s+/g, "");
  await checks[mode](page, text, () => page.waitForTimeout(80));
} catch (e) {
  failures.push(String((e && e.stack) || e));
} finally {
  if (browser) await browser.close();
  server.close();
}
if (errors.length) failures.push("browser errors:\n  " + errors.join("\n  "));
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log(`trimmed runtime browser checks passed (${mode})`);
