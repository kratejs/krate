import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { chromium, firefox, webkit } from "playwright";

// Light/dark behaviour of highlighted code: system preference, an explicit
// data-theme, and switching at runtime, for the Code component and a bare
// highlighted block. usage: code_theme.mjs <dist-root> [browser]
const root = process.argv[2];
const browserName = process.argv[3] || "chromium";
const browserType = { chromium, firefox, webkit }[browserName];
if (!root || !browserType) {
  console.error("usage: code_theme.mjs <dist-root> [chromium|firefox|webkit]");
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
const check = (cond, message) => { if (!cond) failures.push(message); };
const rgb = (s) => (s.match(/[\d.]+/g) || []).slice(0, 3).map((v) => Math.round(Number(s.startsWith("color(") ? v * 255 : v))).join(",");
const lum = ([r, g, b]) => { const f = (c) => { c /= 255; return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4; }; return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b); };
const contrast = (a, b) => { let x = lum(a), y = lum(b); if (x < y) [x, y] = [y, x]; return (x + 0.05) / (y + 0.05); };
const parts = (s) => rgb(s).split(",").map(Number);

const probe = () => {
  const g = (e, p) => getComputedStyle(e)[p];
  const blk = document.querySelector(".krate-code-block");
  const code = blk.querySelector("code");
  const bare = document.querySelector("pre.chroma:not(.krate-code-block pre)");
  const kw = code.querySelector(".kr, .kd, .k");
  return {
    blockBg: g(blk, "backgroundColor"), preBg: g(blk.querySelector("pre"), "backgroundColor"),
    text: g(code, "color"), keyword: g(kw, "color"),
    title: g(blk.querySelector(".krate-code-title"), "color"), headerBg: g(blk.querySelector(".krate-code-header"), "backgroundColor"),
    bareBg: g(bare, "backgroundColor"), bareText: g(bare, "color"), bareKeyword: g(bare.querySelector(".kr, .kd, .k"), "color"),
    spans: [...code.querySelectorAll("span")].filter((s) => s.className === "kr" || s.className === "kd").length,
  };
};
const PAIRS = { light: { bg: "247,247,247", text: "31,35,40", keyword: "207,34,46" }, dark: { bg: "13,17,23", text: "230,237,243", keyword: "255,123,114" } };

try {
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const url = `http://127.0.0.1:${server.address().port}/`;
  const browser = await browserType.launch();
  for (const system of ["light", "dark"]) {
    const ctx = await browser.newContext({ colorScheme: system });
    const page = await ctx.newPage();
    await page.goto(url);
    const steps = [[system, null], ...["light", "dark", "light"].map((t) => [t, t])];
    for (const [mode, forced] of steps) {
      if (forced) await page.evaluate((t) => document.documentElement.setAttribute("data-theme", t), forced);
      const r = await page.evaluate(probe);
      const label = `system=${system} data-theme=${forced || "none"}`;
      const want = PAIRS[mode];
      check(rgb(r.blockBg) === want.bg, `${label}: block background ${rgb(r.blockBg)}, want ${want.bg}`);
      check(rgb(r.preBg) === want.bg, `${label}: pre background ${rgb(r.preBg)}, want ${want.bg}`);
      check(rgb(r.bareBg) === want.bg, `${label}: bare block background ${rgb(r.bareBg)}, want ${want.bg}`);
      check(rgb(r.text) === want.text, `${label}: text ${rgb(r.text)}, want ${want.text}`);
      check(rgb(r.bareText) === want.text, `${label}: bare text ${rgb(r.bareText)}, want ${want.text}`);
      check(rgb(r.keyword) === want.keyword, `${label}: keyword ${rgb(r.keyword)}, want ${want.keyword}`);
      check(rgb(r.bareKeyword) === want.keyword, `${label}: bare keyword ${rgb(r.bareKeyword)}, want ${want.keyword}`);
      check(contrast(parts(r.text), parts(r.blockBg)) >= 7, `${label}: untinted text contrast too low`);
      check(contrast(parts(r.title), parts(r.headerBg)) >= 4.5, `${label}: title contrast ${contrast(parts(r.title), parts(r.headerBg)).toFixed(2)} below 4.5`);
      check(r.spans > 0, `${label}: no keyword spans`);
    }
    await ctx.close();
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
console.log("code theme browser checks passed");
