// Loads a built page whose layout passes <Probe /> as a JSX prop and checks
// that the slot server-renders "ssr" and hydrates to "hydrated".
import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { chromium, firefox, webkit } from "playwright";

const [root, browserName = "chromium", route = "/", toggles = "0"] = process.argv.slice(2);
const browserType = { chromium, firefox, webkit }[browserName];
if (!root || !browserType) {
  console.error("usage: jsx_prop_slot_browser.mjs <dist-root> [chromium|firefox|webkit] [route] [1 to click #tog]");
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
await new Promise((r) => server.listen(0, "127.0.0.1", r));
const url = `http://127.0.0.1:${server.address().port}${route}`;

const browser = await browserType.launch();
const failures = [];
try {
  const html = await (await fetch(url)).text();
  if (!/id=["']?probe["']?[^>]*>(<!--[^>]*-->)?ssr</.test(html)) failures.push("server markup does not hold the initial state: " + html);
  const page = await browser.newPage();
  page.on("pageerror", (e) => failures.push("pageerror: " + e.message));
  await page.goto(url);
  try {
    await page.waitForFunction(() => document.getElementById("probe")?.textContent === "hydrated", null, { timeout: 5000 });
  } catch {
    failures.push("probe text is " + JSON.stringify(await page.textContent("#probe")) + ", want \"hydrated\"");
  }
  const cls = await page.getAttribute("#probe", "class");
  if (cls !== "probe is-on") failures.push("probe class is " + JSON.stringify(cls));
  if (toggles === "1") {
    await page.click("#tog");
    try {
      await page.waitForFunction(() => document.getElementById("tog")?.textContent === "open", null, { timeout: 5000 });
    } catch {
      failures.push("receiver text is " + JSON.stringify(await page.textContent("#tog")) + ", want \"open\"");
    }
    if ((await page.textContent("#probe")) !== "hydrated") failures.push("probe lost its state after the receiver re-rendered");
  }
} finally {
  await browser.close();
  server.close();
}
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log("ok");
