import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { chromium, firefox, webkit } from "playwright";

// Compares what a page shows before hydration (JavaScript disabled) with what
// it shows after, for the text of one selector. Static HTML must never be
// wrong; a value that is deferred to runtime may be empty before hydration.
// usage: evalfix_browser.mjs <dist-root> <browser> <checks-json>
// each check: { path, selector, static?, hydrated }; static defaults to hydrated.
const root = process.argv[2];
const browserName = process.argv[3] || "chromium";
const checks = JSON.parse(process.argv[4] || "[]");
const browserType = { chromium, firefox, webkit }[browserName];
if (!root || !browserType) {
  console.error("usage: evalfix_browser.mjs <dist-root> [chromium|firefox|webkit] <checks-json>");
  process.exit(2);
}
const mime = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".json": "application/json" };
const server = http.createServer((req, res) => {
  let p = decodeURIComponent(req.url.split("?")[0]);
  if (p.endsWith("/")) p += "index.html";
  fs.readFile(path.join(root, p), (err, body) => {
    if (err) { res.statusCode = 404; res.end("not found"); return; }
    res.setHeader("content-type", mime[path.extname(p)] || "application/octet-stream");
    res.end(body);
  });
});

const failures = [];
const texts = (page, selector) =>
  page.evaluate((sel) => [...document.querySelectorAll(sel)].map((e) => e.textContent.trim()).join("|"), selector);

try {
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const base = `http://127.0.0.1:${server.address().port}`;
  const browser = await browserType.launch();
  const plain = await browser.newContext({ javaScriptEnabled: false });
  const live = await browser.newContext();
  for (const c of checks) {
    const want = { static: c.static ?? c.hydrated, hydrated: c.hydrated };
    const a = await plain.newPage();
    await a.goto(base + c.path);
    const gotStatic = await texts(a, c.selector);
    if (gotStatic !== want.static) failures.push(`${c.path} ${c.selector} static: got ${JSON.stringify(gotStatic)}, want ${JSON.stringify(want.static)}`);
    await a.close();

    const b = await live.newPage();
    const errors = [];
    b.on("pageerror", (e) => errors.push(String(e)));
    // The runtime reports a failed hydration scope through console.error.
    b.on("console", (m) => { if (m.type() === "error") errors.push("console: " + m.text()); });
    await b.goto(base + c.path);
    await b.waitForLoadState("networkidle");
    if (c.click) {
      await b.click(c.click);
    }
    await b.waitForTimeout(150);
    const gotHydrated = await texts(b, c.selector);
    const wantAfter = c.afterClick ?? want.hydrated;
    if (gotHydrated !== wantAfter) failures.push(`${c.path} ${c.selector} hydrated: got ${JSON.stringify(gotHydrated)}, want ${JSON.stringify(wantAfter)}`);
    if (errors.length) failures.push(`${c.path} page errors: ${errors.join("; ")}`);
    await b.close();
  }
  await browser.close();
} catch (e) {
  failures.push(String(e && e.stack || e));
} finally {
  server.close();
}
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log(`evalfix browser checks passed (${checks.length})`);
