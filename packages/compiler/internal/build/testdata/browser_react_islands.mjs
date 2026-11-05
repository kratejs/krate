// Drives the React-library island fixtures (see reactisland_browser_test.go)
// in a real browser: store hooks shared by islands, a form hook, a CommonJS
// hook, client:visible deferral and a CSS-in-JS island.
import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { chromium, firefox, webkit } from "playwright";

const root = process.argv[2];
const browserType = { chromium, firefox, webkit }[process.argv[3] || "chromium"];
// The base URL of the real server, for pages rendered per request ("" when
// the test could not start one).
const serverBase = process.argv[4] || "";
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
  page.on("request", (r) => { if (r.url().includes("/chunks/island_")) islandRequests.push(r.url()); });
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

  // Pages built per generateStaticParams entry hydrate their islands.
  for (const id of ["a", "b"]) {
    errors.length = 0;
    const html = await (await page.request.get(`${base}/gsp/${id}/`)).text();
    check(`gsp ${id}: the island is in the HTML`, html.includes("data-krate-island"), html.slice(0, 300));
    await page.goto(`${base}/gsp/${id}/`, { waitUntil: "load" });
    await hydrated();
    check(`gsp ${id}: the param reaches the page`, (await text("#gid")) === `entry ${id}`, await text("#gid"));
    check(`gsp ${id}: the store island renders`, (await text("#inc")) === `gsp-${id} 1`, await text("#inc"));
    await page.click("#inc");
    check(`gsp ${id}: the store island is interactive`, (await text("#inc")) === `gsp-${id} 2`, await text("#inc"));
    check(`gsp ${id}: a second island shares the store`, (await text("#disp")) === "shown 2", await text("#disp"));
    check(`gsp ${id}: no console errors`, errors.length === 0, errors.join(" | "));
  }
  errors.length = 0;

  await page.goto(base + "/styled/", { waitUntil: "load" });
  await hydrated();
  check("CSS-in-JS island has no server markup", (await page.evaluate(() => document.documentElement.outerHTML)).includes("fs-div"));
  const color = await page.evaluate(() => getComputedStyle(document.querySelector("#o")).color);
  check("CSS-in-JS styles apply after hydration", color === "rgb(255, 0, 0)", color);
  check("no console errors on the other pages", errors.length === 0, errors.join(" | "));

  // Providers a layout wraps around its children reach every island of the
  // page, and the modules of the islands live on across client-side navigation.
  const q = () => page.evaluate(() => [...document.querySelectorAll(".q")].map((e) => e.textContent));
  const clients = () => page.evaluate(() => (globalThis.__clients ? globalThis.__clients.size : 0));
  const observers = () => page.evaluate(() => [...(globalThis.__clients || [])].reduce((n, c) => n + c.subs.size, 0));
  const h1 = (t) => page.waitForFunction((want) => document.querySelector("h1") && document.querySelector("h1").textContent === want, t, { timeout: 10000 });
  const mounted = () => page.waitForFunction(() => {
    const all = document.querySelectorAll("[data-krate-island]").length;
    return all > 0 && all === document.querySelectorAll("[data-krate-island-hydrated]").length;
  }, null, { timeout: 10000 });
  const goBack = async () => { await page.evaluate(() => history.back()); };
  const goForward = async () => { await page.evaluate(() => history.forward()); };
  errors.length = 0;

  const appHTML = await (await page.request.get(base + "/app/")).text();
  check("islands are server-rendered inside the layout's providers", /<i id="?t"?>light<\/i>/.test(appHTML), appHTML.slice(0, 600));
  check("the layout keeps its own markup", /id="?app-shell"?/.test(appHTML));
  await page.goto(base + "/app/", { waitUntil: "load" });
  await mounted();
  await page.waitForFunction(() => [...document.querySelectorAll(".q")].length === 2 && [...document.querySelectorAll(".q")].every((e) => e.textContent === "loaded:k"), null, { timeout: 10000 });
  check("two islands use one provider's client", (await clients()) === 1, String(await clients()));
  check("the provider's theme reaches an island", (await text("#t")) === "light", await text("#t"));
  check("no console errors with providers", errors.length === 0, errors.join(" | "));
  const before = await text("#inc");
  await page.evaluate(() => { window.__marker = 1; });
  await page.click("#inc");
  await page.click("#inc");
  const bumped = await text("#inc");
  check("the store island updates", bumped !== before, `${before} -> ${bumped}`);

  await page.click('a[href="/app/second/"]');
  await h1("two");
  await mounted();
  check("the router navigates without a reload", (await page.evaluate(() => window.__marker)) === 1);
  check("the store state survives navigation", (await text("#inc")) === bumped, `${await text("#inc")} vs ${bumped}`);
  check("the provider's client survives navigation", (await clients()) === 1 && (await q()).every((v) => v === "loaded:k"), `${await clients()} ${await q()}`);

  await page.click('a[href="/app/nested/"]');
  await h1("nested");
  await mounted();
  check("a nested layout's provider wins", (await text("#t")) === "dark", await text("#t"));
  check("a nested layout reuses the outer provider's client", (await clients()) === 1, String(await clients()));

  await goBack();
  await h1("two");
  await mounted();
  check("back restores the page with the same state", (await text("#inc")) === bumped, await text("#inc"));
  const requestsBefore = islandRequests.length;
  await page.click('a[href="/app/plain/"]');
  await h1("plain");
  check("a page without islands has no island markup", (await page.locator("[data-krate-island]").count()) === 0);
  check("a page without islands loads no island code", islandRequests.length === requestsBefore, islandRequests.slice(requestsBefore).join(","));
  check("a leaving island unmounts and releases its subscriptions", (await observers()) === 0, String(await observers()));
  await goBack();
  await h1("two");
  await mounted();
  check("coming back mounts again with the same state", (await text("#inc")) === bumped && (await observers()) === 1, `${await text("#inc")} ${await observers()}`);
  await goBack();
  await h1("one");
  await mounted();
  check("back to the first page keeps the state", (await text("#inc")) === bumped && (await observers()) === 2, `${await text("#inc")} ${await observers()}`);
  await goForward();
  await h1("two");
  await mounted();
  check("forward works", (await text("#inc")) === bumped, await text("#inc"));
  check("navigation keeps one document", (await page.evaluate(() => window.__marker)) === 1);
  check("no console errors while navigating", errors.length === 0, errors.join(" | "));

  // A page outside the layout is untouched by its providers.
  await page.goto(base + "/", { waitUntil: "load" });
  await hydrated();
  check("a page outside the layout still works", (await text("#inc")) === "count 1", await text("#inc"));

  if (serverBase) {
    for (const route of ["ssr", "isr"]) {
      const url = `${serverBase}/app/${route}/`;
      const first = await (await page.request.get(url)).text();
      check(`${route}: islands are server-rendered`, first.includes("data-krate-island-ssr") && /<i id="?t"?>light<\/i>/.test(first), first.slice(0, 500));
      check(`${route}: no region error fallback`, !first.includes("suspense-error"));
      if (route === "ssr") {
        await new Promise((r) => setTimeout(r, 1100));
        const second = await (await page.request.get(url)).text();
        const stamp = (h) => (/id="?stamp"?>(\d+)</.exec(h) || [])[1];
        check("ssr: the page is rendered per request", stamp(first) && stamp(second) && stamp(first) !== stamp(second), `${stamp(first)} ${stamp(second)}`);
      }
      errors.length = 0;
      await page.goto(url, { waitUntil: "load" });
      await mounted();
      check(`${route}: the provider reaches the island`, (await text("#t")) === "light", await text("#t"));
      check(`${route}: the store island is interactive`, await page.click("#inc").then(() => text("#inc")) === "count 2", await text("#inc"));
      await page.click('a[href="/app/second/"]');
      await h1("two");
      await mounted();
      check(`${route}: state carries into a static page`, (await text("#inc")) === "count 2", await text("#inc"));
      check(`${route}: no console errors`, errors.length === 0, errors.join(" | "));
    }
  }
} finally {
  await browser.close();
  server.close();
}
if (failures.length) {
  console.error("react islands browser checks failed:\n- " + failures.join("\n- "));
  process.exit(1);
}
console.log("react islands browser checks passed");
