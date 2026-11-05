import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { chromium, firefox, webkit } from "playwright";

// Code that changes at run time, highlighted in the browser: plain text first,
// then the same markup the compiler writes, debounced, unchanged text left
// alone, hostile text left plain, and theme switching at run time.
// usage: code_dynamic.mjs <dist-root> [browser] <expected.json>
const root = process.argv[2];
const browserName = process.argv[3] || "chromium";
const expected = JSON.parse(fs.readFileSync(process.argv[4], "utf8"));
const browserType = { chromium, firefox, webkit }[browserName];
const mime = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".json": "application/json" };
const requests = [];
const server = http.createServer((req, res) => {
  let p = decodeURIComponent(req.url.split("?")[0]);
  requests.push(p);
  if (p.endsWith("/")) p += "index.html";
  fs.readFile(path.join(root, p), (err, body) => {
    if (err) { res.statusCode = 404; res.end("not found"); return; }
    res.setHeader("content-type", mime[path.extname(p)] || "application/octet-stream");
    res.end(body);
  });
});

const failures = [];
const check = (cond, message) => { if (!cond) failures.push(message); };
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const state = () => document.querySelector("code[data-krate-hl]");
const snap = () => {
  const c = document.querySelector("code[data-krate-hl]");
  return {
    lines: c.querySelectorAll(".line").length,
    text: c.textContent,
    html: c.innerHTML.replace(/<!--\/?k:[^>]*-->/g, ""),
    keyword: (() => { const k = c.querySelector(".kd, .kr, .k"); return k ? getComputedStyle(k).color : null; })(),
    markers: c.firstChild.nodeType === 8 && c.lastChild.nodeType === 8,
  };
};

try {
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const url = `http://127.0.0.1:${server.address().port}/`;
  const browser = await browserType.launch();
  const ctx = await browser.newContext({ colorScheme: "light" });
  const page = await ctx.newPage();
  page.on("pageerror", (e) => failures.push("page error: " + e.message));
  await page.goto(url);
  // The browser serialises innerHTML its own way (it writes ' for &#39;), so the
  // compiler's markup is compared after the same round trip.
  for (const k of Object.keys(expected)) {
    expected[k] = await page.evaluate((h) => { const t = document.createElement("template"); t.innerHTML = h; return t.innerHTML; }, expected[k]);
  }
  await sleep(500);

  // The initial text is highlighted once the chunk loads, with the same markup
  // the compiler writes for it.
  let s = await page.evaluate(snap);
  check(s.lines === 2 && s.html === expected.initial, `initial highlight differs from the compiler's:\n got ${s.html.slice(0, 200)}\nwant ${expected.initial.slice(0, 200)}`);
  check(s.markers, "slot markers were lost");
  const chunkRequests = requests.filter((r) => r.includes("/chunks/hl"));
  check(chunkRequests.length === 2, `expected the tokenizer and one language chunk, got ${chunkRequests}`);

  // Theme switching changes colours without touching the markup.
  const before = await page.evaluate(() => { window.__code = document.querySelector("code[data-krate-hl]"); window.__first = window.__code.querySelector(".line"); return getComputedStyle(window.__code.querySelector(".kr")).color; });
  await page.evaluate(() => document.documentElement.setAttribute("data-theme", "dark"));
  const dark = await page.evaluate(() => ({ kw: getComputedStyle(window.__code.querySelector(".kr")).color, same: window.__first === window.__code.querySelector(".line"), bg: getComputedStyle(window.__code.closest("pre")).backgroundColor }));
  check(before === "rgb(207, 34, 46)", `light keyword colour ${before}`);
  check(dark.kw === "rgb(255, 123, 114)" && dark.same && dark.bg === "rgb(23, 23, 23)", `dark mode: ${JSON.stringify(dark)}`);
  await page.evaluate(() => document.documentElement.setAttribute("data-theme", "light"));

  // An update shows plain text at once, then the highlighted markup.
  await page.evaluate(() => { window.__adds = 0; new MutationObserver((rs) => { for (const r of rs) for (const n of r.addedNodes) if (n.nodeType === 1 && n.classList.contains("line")) window.__adds++; }).observe(window.__code, { childList: true, subtree: true }); });
  await page.click("#more");
  s = await page.evaluate(snap);
  check(s.lines === 0 && s.text.endsWith("let z = 'x';"), `expected plain text right after the update, got ${s.lines} lines`);
  await sleep(500);
  s = await page.evaluate(snap);
  {
    let i = 0;
    while (i < s.html.length && s.html[i] === expected.more[i]) i++;
    check(s.html === expected.more, `updated highlight differs from the compiler's at ${i}:\n got ${JSON.stringify(s.html.slice(Math.max(0, i - 60), i + 80))}\nwant ${JSON.stringify(expected.more.slice(Math.max(0, i - 60), i + 80))}`);
  }

  // Fast updates are coalesced: only the last text is highlighted.
  const adds0 = await page.evaluate(() => window.__adds);
  for (let i = 0; i < 5; i++) { await page.click("#more"); }
  await sleep(600);
  s = await page.evaluate(snap);
  const burst = (await page.evaluate(() => window.__adds)) - adds0;
  check(s.lines === 8 && s.text.endsWith("let z = 'x';"), `after the burst: ${s.lines} lines`);
  check(burst === 8, `the burst was highlighted in ${burst} line additions, want one pass of 8`);

  // The same text is not highlighted again.
  const adds1 = await page.evaluate(() => window.__adds);
  await page.click("#same");
  await sleep(400);
  check((await page.evaluate(() => window.__adds)) === adds1, "unchanged text was highlighted again");

  // Text over the size or line limit stays plain, and the page stays usable.
  for (const [btn, label] of [["#big", "text over the size limit"], ["#long", "a line over the line limit"]]) {
    const t0 = Date.now();
    await page.click(btn);
    await sleep(400);
    s = await page.evaluate(snap);
    check(s.lines === 0, `${label} was highlighted`);
    check(Date.now() - t0 < 4000, `${label} stalled the page`);
  }

  // Hostile text within the limits finishes quickly, highlighted or not.
  for (const btn of ["#hostile1", "#hostile2"]) {
    const t0 = Date.now();
    await page.click(btn);
    await sleep(700);
    const ms = Date.now() - t0;
    check(ms < 3500, `${btn}: the page was busy for ${ms} ms`);
    check(await page.evaluate(() => document.getElementById("alive").textContent === "alive"), `${btn}: the page stopped responding`);
  }

  // Going back to normal code highlights again.
  await page.click("#reset");
  await sleep(500);
  s = await page.evaluate(snap);
  check(s.html === expected.initial, "highlighting did not resume after hostile text");
  await browser.close();
} catch (e) {
  failures.push(String((e && e.stack) || e));
} finally {
  server.close();
}
if (failures.length) { console.error(failures.join("\n")); process.exit(1); }
console.log("dynamic code browser checks passed");
