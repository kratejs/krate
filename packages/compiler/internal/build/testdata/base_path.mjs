import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { chromium, firefox, webkit } from "playwright";

// A site served under a base path, with nothing at the host root and no
// redirects: every asset, chunk, worker and link has to carry the prefix.
// usage: base_path.mjs <dist-root> [browser] <base>
const root = process.argv[2];
const browserName = process.argv[3] || "chromium";
const base = process.argv[4];
const browserType = { chromium, firefox, webkit }[browserName];
const mime = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".png": "image/png" };
const missed = [];
const served = [];
const server = http.createServer((req, res) => {
  const url = decodeURIComponent(req.url.split("?")[0]);
  served.push(url);
  if (url !== base && !url.startsWith(base + "/")) {
    missed.push(url);
    res.statusCode = 404;
    res.end("outside the base path");
    return;
  }
  let p = url.slice(base.length) || "/";
  if (p.endsWith("/")) p += "index.html";
  else if (!path.extname(p)) p += "/index.html";
  fs.readFile(path.join(root, p), (err, body) => {
    if (err) {
      // Prefetching a link whose page does not exist is fine; a file is not.
      if (path.extname(url)) missed.push(url);
      res.statusCode = 404;
      res.end("not found");
      return;
    }
    res.setHeader("content-type", mime[path.extname(p)] || "application/octet-stream");
    res.end(body);
  });
});

const failures = [];
const check = (cond, message) => { if (!cond) failures.push(message); };

try {
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const origin = `http://127.0.0.1:${server.address().port}`;
  const browser = await browserType.launch();
  const page = await browser.newPage();
  page.on("pageerror", (e) => failures.push("page error: " + e.message));
  const responses = [];
  page.on("response", (r) => responses.push(r));
  await page.goto(origin + base + "/");
  await page.waitForLoadState("networkidle");

  // Stylesheet, image and background all resolved.
  const css = await page.evaluate(() => {
    const el = document.createElement("div");
    el.className = "b";
    document.body.appendChild(el);
    return getComputedStyle(el).backgroundImage;
  });
  check(css.includes(base + "/pub.png"), "public file in css not under the base: " + css);

  // A dynamic import, then the element it reveals.
  await page.click("#load");
  await page.waitForFunction(() => document.getElementById("load").textContent === "7", null, { timeout: 5000 });
  check((await page.getAttribute("#cond", "href")) === base + "/cond", "conditional link lost the base path");

  // A worker chunk. The server log is the evidence: browsers disagree on what
  // the automation API reports as a worker's URL.
  await page.click("#spawn");
  const deadline = Date.now() + 5000;
  while (!served.some((u) => u.startsWith(base + "/workers/")) && Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 50));
  }
  check(served.some((u) => u.startsWith(base + "/workers/")), "worker was not requested under the base path");

  // SPA navigation out and back, without a document reload.
  await page.evaluate(() => { window.__sameDocument = true; });
  await page.click("#lit");
  await page.waitForSelector("#home", { timeout: 5000 });
  check(new URL(page.url()).pathname.replace(/\/$/, "") === base + "/about", "link went to " + page.url());
  check(await page.evaluate(() => window.__sameDocument === true), "link reloaded the document");
  await page.click("#home");
  await page.waitForSelector("#lit", { timeout: 5000 });
  check(new URL(page.url()).pathname.replace(/\/$/, "") === base, "home link went to " + page.url());

  // Programmatic navigation takes the same base-relative URL as <Link>.
  await page.evaluate(() => window.navigate("/about"));
  await page.waitForSelector("#home", { timeout: 5000 });
  check(new URL(page.url()).pathname.replace(/\/$/, "") === base + "/about", "navigate() went to " + page.url());

  await browser.close();
  const ignored = (u) => u === "/favicon.ico" || u.endsWith("/not-ours.png");
  const real = missed.filter((u) => !ignored(u));
  check(real.length === 0, "requests missed the site: " + real.join(", "));
} catch (e) {
  failures.push(String((e && e.stack) || e));
} finally {
  server.close();
}

if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log("base path ok");
