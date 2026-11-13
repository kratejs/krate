// Drives the real-React island fixture (see realreact_browser_test.go): a
// canvas painted through a react-reconciler renderer, updated by a Krate
// signal prop and by the island's own state.
import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { chromium, firefox, webkit } from "playwright";

const root = process.argv[2];
const browserType = { chromium, firefox, webkit }[process.argv[3] || "chromium"];
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
  const chunks = [];
  page.on("request", (r) => { if (/\/chunks\/(island_|isr-)/.test(r.url())) chunks.push(r.url()); });
  // The pixel at (x, y) of the island's canvas as "r,g,b,a".
  const pixel = (x, y) => page.evaluate(([x, y]) => {
    const d = document.getElementById("cv").getContext("2d").getImageData(x, y, 1, 1).data;
    return Array.from(d).join(",");
  }, [x, y]);
  const waitPixel = async (x, y, want) => {
    try {
      await page.waitForFunction(([x, y, want]) => {
        const c = document.getElementById("cv");
        return !!c && Array.from(c.getContext("2d").getImageData(x, y, 1, 1).data).join(",") === want;
      }, [x, y, want], { timeout: 5000 });
      return true;
    } catch {
      return false;
    }
  };

  await page.goto(base + "/", { waitUntil: "load" });
  check("the reconciler renderer paints the first frame", await waitPixel(7, 2, "255,0,0,255"), await pixel(7, 2).catch((e) => String(e)));
  check("the canvas is empty outside the shape", (await pixel(40, 10)) === "0,0,0,0", await pixel(40, 10));
  await page.click("#grow");
  check("island state moves the shape", (await waitPixel(12, 2, "255,0,0,255")) && (await pixel(7, 2)) === "0,0,0,0", `${await pixel(7, 2)} / ${await pixel(12, 2)}`);
  check("the button text follows state", (await page.textContent("#grow")) === "grow 2", await page.textContent("#grow"));
  await page.click("#recolor");
  check("a Krate signal prop reaches the island", await waitPixel(12, 2, "0,0,255,255"), await pixel(12, 2));
  check("the island kept its own state", (await page.textContent("#grow")) === "grow 2", await page.textContent("#grow"));
  check("no console errors", errors.length === 0, errors.join(" | "));
  const indexChunks = chunks.length;
  check("the page fetches the island entry and the React chunk", chunks.some((u) => u.includes("/chunks/isr-")) && chunks.some((u) => u.includes("/chunks/island_")), chunks.join(","));

  chunks.length = 0;
  await page.goto(base + "/plain/", { waitUntil: "load" });
  await page.waitForTimeout(300);
  check("a page without a real-React island loads no island chunk", chunks.length === 0 && indexChunks > 0, chunks.join(","));
  check("no errors on the plain page", errors.length === 0, errors.join(" | "));
  // "use client" Viewer from a sync page (static) and a "use client" parent.
  for (const route of ["/usync/", "/upanelsync/"]) {
    errors.length = 0;
    await page.goto(base + route, { waitUntil: "load" });
    check(route + " paints through the reconciler", await waitPixel(7, 2, "255,0,0,255"), await pixel(7, 2).catch((e) => String(e)));
    await page.click("#grow");
    check(route + " responds to island state", (await waitPixel(12, 2, "255,0,0,255")) && (await pixel(7, 2)) === "0,0,0,0", `${await pixel(7, 2)} / ${await pixel(12, 2)}`);
    check(route + " ran hooks and effects on real React", (await page.getAttribute("[data-ready]", "data-ready")) === "yes");
    check(route + " has no console errors", errors.length === 0, errors.join(" | "));
  }
  // Request-time pages: the server answers with the fallback and the island
  // mounts on real React (directly, through a wrapper module, in a layout).
  if (serverBase) {
    for (const route of ["/adirect/", "/awrapped/", "/alay/", "/uasync/", "/ulay/", "/upanelasync/"]) {
      errors.length = 0;
      await page.goto(serverBase + route, { waitUntil: "load" });
      check(route + " paints through the reconciler", await waitPixel(7, 2, "255,0,0,255"), await pixel(7, 2).catch((e) => String(e)));
      if (route.startsWith("/u")) check(route + " ran hooks and effects on real React", (await page.getAttribute("[data-ready]", "data-ready")) === "yes");
      check(route + " has no console errors", errors.length === 0, errors.join(" | "));
      if (route !== "/adirect/") {
        await page.click("#grow");
        check(route + " responds to island state", (await waitPixel(12, 2, "255,0,0,255")) && (await pixel(7, 2)) === "0,0,0,0", `${await pixel(7, 2)} / ${await pixel(12, 2)}`);
      }
    }
  }
} finally {
  await browser.close();
  server.close();
}
if (failures.length) {
  console.error("FAILED:\n- " + failures.join("\n- "));
  process.exit(1);
}
console.log("real-React island browser checks passed");
