// Records a CDP screencast while navigating between the two pages of the
// glass sidebar app (see TestUseClientIslandBrowserGlassNavigation) and checks
// the sidebar's mean brightness in every frame, and that the navigations kept
// the layout and its client component mounted.
import { chromium } from "playwright";
import zlib from "node:zlib";

// Minimal PNG decoder (8-bit RGB/RGBA, not interlaced).
function decodePNG(buf) {
  let p = 8, w, h, ct, idat = [];
  while (p < buf.length) {
    const len = buf.readUInt32BE(p), type = buf.toString("ascii", p + 4, p + 8), data = buf.subarray(p + 8, p + 8 + len);
    if (type === "IHDR") { w = data.readUInt32BE(0); h = data.readUInt32BE(4); ct = data[9]; }
    else if (type === "IDAT") idat.push(data);
    p += 12 + len;
  }
  const bpp = ct === 6 ? 4 : 3, raw = zlib.inflateSync(Buffer.concat(idat)), stride = w * bpp, out = Buffer.alloc(h * stride);
  for (let y = 0; y < h; y++) {
    const f = raw[y * (stride + 1)], src = raw.subarray(y * (stride + 1) + 1, (y + 1) * (stride + 1)), row = out.subarray(y * stride, (y + 1) * stride), prev = y ? out.subarray((y - 1) * stride, y * stride) : null;
    for (let x = 0; x < stride; x++) {
      const a = x >= bpp ? row[x - bpp] : 0, b = prev ? prev[x] : 0, c = prev && x >= bpp ? prev[x - bpp] : 0;
      let v = src[x];
      if (f === 1) v += a; else if (f === 2) v += b; else if (f === 3) v += (a + b) >> 1;
      else if (f === 4) { const pa = Math.abs(b - c), pb = Math.abs(a - c), pc = Math.abs(a + b - 2 * c); v += pa <= pb && pa <= pc ? a : pb <= pc ? b : c; }
      row[x] = v & 255;
    }
  }
  return { w, h, bpp, px: out };
}
const base = process.argv[2];
const browser = await chromium.launch({ headless: true });
let failed = false;
try {
  const ctx = await browser.newContext({ viewport: { width: 1200, height: 700 } });
  const page = await ctx.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push("pageerror: " + e.message));
  const logs = [];
  page.on("console", (m) => logs.push(m.type() + ": " + m.text().slice(0, 200)));
  await page.goto(base + "/");
  await page.waitForFunction(() => typeof window.initRouter === "function");
  const cfg = await page.evaluate(() => ({ vt: JSON.parse(document.getElementById("__krate_cfg__")?.textContent || "{}").viewTransitions, rule: [...document.querySelectorAll("style")].some((s) => s.textContent.includes("@view-transition")) }));
  if (cfg.vt !== false || cfg.rule) {
    console.error("viewTransitions off must tell the router and drop the @view-transition rule: " + JSON.stringify(cfg));
    failed = true;
  }
  await page.click("#count");
  await page.evaluate(() => { window.__sameDocument = true; });
  await page.waitForTimeout(300);
  // The middle of the sidebar, below its links.
  const box = await page.evaluate(() => { const r = document.getElementById("glass").getBoundingClientRect(); return { x: r.x + 20, y: r.y + 160, w: r.width - 40, h: r.height - 220 }; });
  const cdp = await ctx.newCDPSession(page);
  const frames = [];
  cdp.on("Page.screencastFrame", (f) => { frames.push(f.data); cdp.send("Page.screencastFrameAck", { sessionId: f.sessionId }).catch(() => {}); });
  await cdp.send("Page.startScreencast", { format: "png", everyNthFrame: 1 });
  await page.waitForTimeout(300);
  for (let i = 0; i < 8; i++) {
    const to = i % 2 ? "#to-a" : "#to-b";
    await page.click(to);
    await page.waitForFunction((h) => document.querySelector("h1")?.textContent === h, i % 2 ? "Page A" : "Page B");
    await page.waitForTimeout(250);
  }
  const kept = await page.evaluate(() => ({ same: window.__sameDocument === true, count: document.getElementById("count").textContent }));
  if (!kept.same || kept.count !== "1") {
    console.error("the navigations did not keep the document and the layout's client component: " + JSON.stringify(kept) + "\n" + logs.slice(0, 20).join("\n"));
    failed = true;
  }
  // Full document loads too: the @view-transition rule animates those.
  for (let i = 0; i < 4; i++) {
    await page.evaluate((u) => location.assign(u), i % 2 ? "/" : "/b/");
    await page.waitForFunction((h) => document.querySelector("h1")?.textContent === h, i % 2 ? "Page A" : "Page B");
    await page.waitForTimeout(250);
  }
  await cdp.send("Page.stopScreencast");
  const means = frames.map((d) => {
    const { w, bpp, px } = decodePNG(Buffer.from(d, "base64"));
    let s = 0, n = 0;
    for (let y = Math.round(box.y); y < box.y + box.h; y += 4) for (let x = Math.round(box.x); x < box.x + box.w; x += 4) { const o = (y * w + x) * bpp; s += (px[o] + px[o + 1] + px[o + 2]) / 3; n++; }
    return s / n;
  });
  const ref = means[0];
  const off = means.filter((m) => Math.abs(m - ref) > 5);
  console.log(JSON.stringify({ frames: means.length, brightness: +ref.toFixed(1), changed: off.map((m) => +m.toFixed(1)) }));
  if (means.length < 8) {
    console.error("too few screencast frames: " + means.length);
    failed = true;
  }
  if (off.length) {
    console.error(off.length + " frames changed the sidebar's brightness from " + ref.toFixed(1));
    failed = true;
  }
  if (errors.length) {
    console.error(errors.join("\n"));
    failed = true;
  }
} finally {
  await browser.close();
}
process.exit(failed ? 1 : 0);
