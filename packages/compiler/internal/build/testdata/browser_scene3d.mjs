// Drives the @krate/3d fixture built by TestBrowserScene3D against the real
// Filament engine: it loads from the copied assets, renders, picks clicks and
// follows a signal.
import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { chromium, firefox, webkit } from "playwright";

const root = process.argv[2];
const browserType = { chromium, firefox, webkit }[process.argv[3] || "chromium"];
const mime = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".wasm": "application/wasm" };
const requests = [];

const server = http.createServer((req, res) => {
  let p = decodeURIComponent(req.url.split("?")[0]);
  requests.push(p);
  if (p.endsWith("/")) p += "index.html";
  else if (!path.extname(p)) p += "/index.html";
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
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const base = `http://127.0.0.1:${server.address().port}`;
const browser = await browserType.launch({ headless: true, args: ["--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--ignore-gpu-blocklist"] });
const failures = [];
const log = (...a) => console.log(...a);

const withShader = process.argv[4] === "shader";
let page = await browser.newPage({ viewport: { width: 800, height: 600 } });
const errors = [];
page.on("pageerror", (e) => errors.push(`pageerror: ${e.message}`));
page.on("console", (m) => {
  if (m.type() === "error" || m.type() === "warning") errors.push(`${m.type()}: ${m.text()}`);
});

// Pixel statistics of the canvas as composited on screen (a screenshot clip,
// decoded back through a 2D canvas), so it does not depend on
// preserveDrawingBuffer.
async function pixels(box) {
  const png = await page.screenshot({ clip: box });
  return page.evaluate(async (b64) => {
    const img = new Image();
    img.src = "data:image/png;base64," + b64;
    await img.decode();
    const c = document.createElement("canvas");
    c.width = img.width;
    c.height = img.height;
    const ctx = c.getContext("2d");
    ctx.drawImage(img, 0, 0);
    const d = ctx.getImageData(0, 0, c.width, c.height).data;
    const bg = [d[0], d[1], d[2]];
    let different = 0;
    const colors = new Set();
    for (let i = 0; i < d.length; i += 4) {
      if (Math.abs(d[i] - bg[0]) + Math.abs(d[i + 1] - bg[1]) + Math.abs(d[i + 2] - bg[2]) > 24) different++;
      colors.add((d[i] >> 4) + "," + (d[i + 1] >> 4) + "," + (d[i + 2] >> 4));
    }
    const at = (x, y) => Array.from(d.slice((y * c.width + x) * 4, (y * c.width + x) * 4 + 3));
    return { total: d.length / 4, different, colors: colors.size, bg, center: at(c.width >> 1, c.height >> 1), w: c.width, h: c.height };
  }, png.toString("base64"));
}
const until = (fn, arg, timeout = 15000) => page.waitForFunction(fn, arg, { timeout });

try {
  await page.goto(`${base}/`, { waitUntil: "load" });
  await until(() => window.__scene && window.__scene.isReady);
  log("engine ready");
  // Count rendered frames through the scene's own per-frame hook.
  await page.evaluate(() => {
    window.__frames = 0;
    window.__scene.onFrame(() => window.__frames++);
  });
  await until(() => window.__frames > 5);

  const loaded = requests.filter((r) => r.includes("/assets/3d/"));
  log("asset requests:", loaded.join(" "));
  if (!loaded.some((r) => /filament-[\d.]+\/filament\.js$/.test(r))) failures.push("filament.js was not requested from dist/assets/3d");
  if (!loaded.some((r) => /filament-[\d.]+\/filament\.wasm$/.test(r))) failures.push("filament.wasm was not requested next to filament.js");
  if (!loaded.some((r) => /materials-[\d.]+\/standard\.filamat$/.test(r))) failures.push("standard.filamat was not requested");

  const box = await page.locator("#scene").boundingBox();
  const px = await pixels(box);
  log("pixels:", JSON.stringify(px));
  const frac = px.different / px.total;
  if (!(frac > 0.05)) failures.push(`canvas looks empty: ${(frac * 100).toFixed(2)}% pixels differ from the background`);
  if (!(px.colors > 8)) failures.push(`canvas has only ${px.colors} distinct colors`);

  // A click on the sphere (canvas center) reaches its handler through view.pick.
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  try {
    await until(() => window.__clicks === 1, null, 8000);
    log("pick: click handler fired", await page.evaluate(() => window.__clicks));
  } catch {
    failures.push(`the sphere's onClick did not fire (clicks=${await page.evaluate(() => window.__clicks ?? 0)})`);
  }
  // A click on empty canvas must not fire it.
  await page.mouse.click(box.x + 6, box.y + 6);
  await page.waitForTimeout(400);
  if ((await page.evaluate(() => window.__clicks)) !== 1) failures.push(`a click on the background changed the click count to ${await page.evaluate(() => window.__clicks)}`);

  // The signal rotates the sphere: the dynamic slot's quaternion changes.
  const quat = () => page.evaluate(() => {
    const t = window.__scene.transforms;
    for (let s = 0; s < 8; s++) {
      const q = Array.from(t.data.slice(s * 10 + 3, s * 10 + 7));
      if (t.instances && t.instances[s]) return { s, q };
    }
    return null;
  });
  const before = await quat();
  const shotBefore = await pixels(box);
  await page.click("#spin");
  await page.click("#spin");
  await page.waitForTimeout(500);
  const after = await quat();
  log("quat before/after:", JSON.stringify(before), JSON.stringify(after));
  if (!before || !after || before.q.join() === after.q.join()) failures.push("the spin signal did not change the sphere transform");
  else if (Math.abs(after.q[1] - Math.sin(0.5)) > 0.02) failures.push(`unexpected rotation quaternion ${after.q}`);
  void shotBefore;
} catch (e) {
  failures.push(String(e).slice(0, 600));
}
if (errors.length) failures.push("console/page errors: " + errors.join(" | "));
await page.close();

if (withShader) {
  // A build-compiled shaderMaterial (real matc output) renders and follows a signal.
  errors.length = 0;
  page = await browser.newPage({ viewport: { width: 800, height: 600 } });
  page.on("pageerror", (e) => errors.push(`pageerror: ${e.message}`));
  page.on("console", (m) => {
    if (m.type() === "error" || m.type() === "warning") errors.push(`${m.type()}: ${m.text()}`);
  });
  try {
    await page.goto(`${base}/shader/`, { waitUntil: "load" });
    await until(() => window.__scene && window.__scene.isReady);
    await page.evaluate(() => {
      window.__frames = 0;
      window.__scene.onFrame(() => window.__frames++);
    });
    await until(() => window.__frames > 5);
    const box = await page.locator("#scene").boundingBox();
    const lit = await pixels(box);
    log("shader pixels:", JSON.stringify(lit.center));
    const [r, g, b] = lit.center;
    if (!(r > 200 && g < 40 && b > 200)) failures.push(`the shaderMaterial center pixel is ${lit.center}, want magenta`);
    await page.click("#dim");
    await page.waitForTimeout(500);
    const dim = await pixels(box);
    log("shader pixels after the uniform changed:", JSON.stringify(dim.center));
    if (!(dim.center[0] < 40 && dim.center[2] < 40)) failures.push(`the reactive uniform did not dim the plane: ${dim.center}`);
  } catch (e) {
    failures.push("shader page: " + String(e).slice(0, 600));
  }
  if (errors.length) failures.push("shader page console/page errors: " + errors.join(" | "));
  await page.close();
}
await browser.close();
server.close();

if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log("scene3d browser checks passed");
