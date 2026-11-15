// One request for a preloaded file (see TestBrowserResourceHints): the server
// HTML preloads /x.wasm, the island that calls preload() again in the browser
// adds nothing, and the island that fetches it reuses the preload.
import { chromium, firefox, webkit } from "playwright";

const browserType = { chromium, firefox, webkit }[process.argv[2] || "chromium"];
const base = process.argv[3];
const paths = process.argv.slice(4);
const browser = await browserType.launch({ headless: true });
const failures = [];
try {
  for (const path of paths) {
    const page = await (await browser.newContext()).newPage();
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    page.on("console", (m) => {
      if (m.type() === "error" || m.text().includes("preloaded")) errors.push(m.text());
    });
    let requests = 0;
    page.on("response", (r) => {
      if (new URL(r.url()).pathname === "/x.wasm") requests++;
    });
    await page.goto(base + path);
    try {
      await page.waitForFunction(() => document.getElementById("fetched")?.textContent === "8", null, { timeout: 15000 });
      await page.waitForFunction(() => document.getElementById("hinted")?.textContent === "ready", null, { timeout: 15000 });
    } catch {
      failures.push(path + ": the islands did not run; errors: " + errors.join("; "));
      await page.close();
      continue;
    }
    // A late preload() would start its request after the fetch finished.
    await page.waitForTimeout(500);
    const links = await page.evaluate(() => document.querySelectorAll('link[href="/x.wasm"]').length);
    if (requests !== 1) failures.push(path + ": want one request for /x.wasm, got " + requests);
    if (links !== 1) failures.push(path + ": want one link for /x.wasm in the document, got " + links);
    if (errors.length) failures.push(path + ": console errors: " + errors.join("; "));
    await page.close();
  }
} finally {
  await browser.close();
}
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log("ok");
