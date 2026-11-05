// Drives the React-library island fixtures (see reactisland_browser_test.go)
// in a real browser: store hooks shared by islands, a form hook, a CommonJS
// hook, client:visible deferral and a CSS-in-JS island.
import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { chromium, firefox, webkit } from "playwright";

const root = process.argv[2];
const browserType = { chromium, firefox, webkit }[process.argv[3] || "chromium"];
const mime = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css" };

const server = http.createServer((req, res) => {
  let p = decodeURIComponent(req.url.split("?")[0]);
  if (p.endsWith("/")) p += "index.html";
  else if (!path.extname(p)) p += "/index.html";
  fs.readFile(path.join(root, p), (err, body) => {
    if (err) { res.statusCode = 404; res.end("not found"); return; }
    res.setHeader("content-type", mime[path.extname(p)] || "application/octet-stream");
    res.end(body);
  });
});

const failures = [];
const check = (name, ok, detail = "") => { if (!ok) failures.push(`${name}${detail ? ": " + detail : ""}`); };

await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const base = `http://127.0.0.1:${server.address().port}`;
const browser = await browserType.launch({ headless: true });
try {
  const context = await browser.newContext({ viewport: { width: 800, height: 600 } });
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => { if (m.type() === "error") errors.push(m.text()); });
  const islandRequests = [];
  page.on("request", (r) => { if (r.url().includes("/chunks/island.")) islandRequests.push(r.url()); });
  const text = (sel) => page.textContent(sel);
  // Islands are display:contents, so they are attached but never "visible".
  const hydrated = async (timeout = 10000) => {
    try {
      await page.waitForSelector("[data-krate-island-hydrated]", { state: "attached", timeout });
    } catch {
      throw new Error(`no island hydrated on ${page.url()}\nerrors: ${errors.join(" | ") || "(none)"}\n${(await page.content()).slice(0, 1500)}`);
    }
  };

  await page.goto(base + "/", { waitUntil: "load" });
  await hydrated();
  check("store hook renders the initial state", (await text("#inc")) === "count 1", await text("#inc"));
  await page.click("#inc");
  await page.click("#inc");
  check("two islands share one store", (await text("#inc")) === "count 3" && (await text("#disp")) === "shown 3", `${await text("#inc")} / ${await text("#disp")}`);
  await page.click("#rename");
  check("a page signal reaches the island", (await text("#inc")) === "clicks 3", await text("#inc"));
  check("a CommonJS hook library runs", (await text("#hello")) === "hello krate", await text("#hello"));
  await page.fill("#name", "Ada");
  check("a form hook updates the island", (await text("#echo")) === "Ada", await text("#echo"));
  check("no console errors on the hooks page", errors.length === 0, errors.join(" | "));

  islandRequests.length = 0;
  await page.goto(base + "/vis/", { waitUntil: "load" });
  await page.waitForTimeout(500);
  check("client:visible keeps the server markup", (await page.locator("#deferred").count()) === 1);
  check("client:visible does not fetch the chunk at load", islandRequests.length === 0, islandRequests.join(","));
  check("client:visible does not mount at load", (await page.locator("[data-krate-island-hydrated]").count()) === 0);
  await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
  await hydrated(5000).catch(() => {});
  check("client:visible mounts after scrolling", (await page.locator("[data-krate-island-hydrated]").count()) === 1);
  check("the island chunk loads on demand", islandRequests.length === 1, String(islandRequests.length));

  await page.goto(base + "/idle/", { waitUntil: "load" });
  await hydrated(5000).catch(() => {});
  check("client:idle mounts once the browser is idle", (await page.locator("[data-krate-island-hydrated]").count()) === 1);

  await page.goto(base + "/styled/", { waitUntil: "load" });
  await hydrated();
  check("CSS-in-JS island has no server markup", (await page.evaluate(() => document.documentElement.outerHTML)).includes("fs-div"));
  const color = await page.evaluate(() => getComputedStyle(document.querySelector("#o")).color);
  check("CSS-in-JS styles apply after hydration", color === "rgb(255, 0, 0)", color);
  check("no console errors on the other pages", errors.length === 0, errors.join(" | "));
} finally {
  await browser.close();
  server.close();
}
if (failures.length) {
  console.error("react islands browser checks failed:\n- " + failures.join("\n- "));
  process.exit(1);
}
console.log("react islands browser checks passed");
