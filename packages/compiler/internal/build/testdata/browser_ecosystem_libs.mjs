// Drives the real npm React libraries built by TestBrowserEcosystemLibraries
// (sonner, cmdk, embla-carousel-react, @dnd-kit, @tanstack/react-table,
// next-themes, motion/react) in a real browser.
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
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const base = `http://127.0.0.1:${server.address().port}`;
const browser = await browserType.launch({ headless: true });
const failures = [];

async function run(name, fn) {
  const page = await browser.newPage({ viewport: { width: 800, height: 600 } });
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  try {
    await page.goto(`${base}/${name}/`, { waitUntil: "load" });
    // Islands are display:contents, so they are attached but never "visible".
    await page.waitForSelector("[data-krate-island-hydrated]", { state: "attached", timeout: 10000 });
    const out = await fn(page);
    if (out !== true) failures.push(`${name}: ${out} (page errors: ${errors.join(" | ") || "none"})`);
  } catch (e) {
    failures.push(`${name}: ${String(e).slice(0, 300)} (page errors: ${errors.join(" | ") || "none"})`);
  }
  await page.close();
}

try {
  await run("sonner", async (p) => {
    await p.click("#t");
    await p.waitForSelector("[data-sonner-toast]", { timeout: 4000 });
    const txt = await p.textContent("[data-sonner-toast]");
    return txt.includes("Saved!") && txt.includes("all good") ? true : `toast text ${txt}`;
  });
  await run("toastlayout", async (p) => {
    await p.click("#save");
    await p.waitForSelector("[data-sonner-toast]", { timeout: 4000 });
    const first = await p.textContent("[data-sonner-toast]");
    if (!first.includes("Saved from page")) return `toast text ${first}`;
    await p.click("#to-b");
    await p.waitForFunction(() => document.querySelector("h1")?.textContent === "b", null, { timeout: 5000 });
    await p.waitForTimeout(300);
    await p.click("#save");
    await p.waitForSelector("[data-sonner-toast]", { timeout: 4000 });
    const toasters = await p.locator("[data-sonner-toaster]").count();
    return toasters === 1 ? true : `toasters after navigation=${toasters}`;
  });
  await run("cmdk", async (p) => {
    const total = await p.locator("[cmdk-item]").count();
    await p.fill("[cmdk-input]", "ban");
    await p.waitForTimeout(200);
    const shown = await p.locator("[cmdk-item]:visible").count();
    await p.fill("[cmdk-input]", "zzz");
    await p.waitForTimeout(200);
    const empty = await p.locator("[cmdk-empty]").count();
    return total === 3 && shown === 1 && empty === 1 ? true : `items=${total} filtered=${shown} empty=${empty}`;
  });
  await run("embla", async (p) => {
    await p.waitForFunction(() => document.querySelector("#count")?.textContent === "3", null, { timeout: 5000 });
    const before = await p.evaluate(() => document.querySelector("#viewport > div").style.transform);
    await p.click("#next");
    await p.waitForTimeout(800);
    const after = await p.evaluate(() => document.querySelector("#viewport > div").style.transform);
    return before !== after ? true : `transform unchanged: ${before}`;
  });
  await run("dnd", async (p) => {
    const a = await p.locator("#row-a").boundingBox();
    const c = await p.locator("#row-c").boundingBox();
    await p.mouse.move(a.x + 5, a.y + 5);
    await p.mouse.down();
    await p.mouse.move(a.x + 5, a.y + 20, { steps: 5 });
    await p.mouse.move(c.x + 5, c.y + c.height, { steps: 10 });
    await p.mouse.up();
    await p.waitForTimeout(300);
    const order = await p.textContent("#order");
    return order !== "a,b,c" ? true : `order unchanged: ${order}`;
  });
  await run("table", async (p) => {
    const first = () => p.locator("tbody tr td").first().textContent();
    const before = await first();
    await p.click("#th-age");
    await p.waitForTimeout(200);
    const after = await first();
    // Number columns sort descending first in TanStack Table.
    return before === "Ada" && after === "Cy" ? true : `${before} -> ${after}`;
  });
  await run("themes", async (p) => {
    await p.click("#dark");
    await p.waitForTimeout(200);
    const cls = await p.evaluate(() => document.documentElement.className);
    const txt = await p.textContent("#theme");
    return cls.includes("dark") && txt.startsWith("dark") ? true : `class=${cls} text=${txt}`;
  });
  await run("themelayout", async (p) => {
    await p.click("#dark");
    await p.waitForTimeout(200);
    const cls = await p.evaluate(() => document.documentElement.className);
    const txt = await p.textContent("#theme");
    return cls.includes("dark") && txt.startsWith("dark") ? true : `class=${cls} text=${txt}`;
  });
  await run("motion", async (p) => {
    await p.waitForTimeout(400);
    const opacity = await p.evaluate(() => getComputedStyle(document.querySelector("#box")).opacity);
    const transform = await p.evaluate(() => document.querySelector("#box").style.transform);
    await p.click("#toggle");
    await p.waitForTimeout(600);
    const present = await p.locator("#pres").count();
    return opacity === "1" && transform.includes("40") && present === 0 ? true : `opacity=${opacity} transform=${transform} present after exit=${present}`;
  });
} finally {
  await browser.close();
  server.close();
}
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log("ecosystem libraries ok");
