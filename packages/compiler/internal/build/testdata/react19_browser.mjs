// Drives a production build of the React 19 fixture (react19_browser_test.go)
// in a real browser: useId pairs in children, nested children, lists, rows
// added at runtime and conditional branches; useEffectEvent; Activity;
// ViewTransition names; custom element properties.
import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { chromium, firefox, webkit } from "playwright";

const [root, browserName = "chromium"] = process.argv.slice(2);
const browserType = { chromium, firefox, webkit }[browserName];
if (!root || !browserType) {
  console.error("usage: react19_browser.mjs <dist-root> [chromium|firefox|webkit]");
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

const failures = [];
const errors = [];
const expect = (label, got, want) => {
  if (JSON.stringify(got) !== JSON.stringify(want)) failures.push(`${label}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
};

// Every label points at exactly one control, ids are unique, and clicking a
// label focuses its control.
const idReport = (page) =>
  page.evaluate(() => {
    const ids = [...document.querySelectorAll("[id]")].map((e) => e.id).filter((i) => i.startsWith("krate"));
    const dup = ids.filter((x, i) => ids.indexOf(x) !== i);
    const labels = [...document.querySelectorAll("label[for]")].map((l) => ({
      text: l.textContent,
      id: l.htmlFor,
      n: document.querySelectorAll('[id="' + CSS.escape(l.htmlFor) + '"]').length,
    }));
    const focus = [];
    for (const l of document.querySelectorAll("label[for]")) {
      l.click();
      focus.push(!!document.activeElement && document.activeElement.id === l.htmlFor);
    }
    return { dup, labels: labels.map((l) => l.text + ":" + l.n), focusOK: focus.every(Boolean), count: labels.length };
  });
const idMap = (page) =>
  page.evaluate(() => Object.fromEntries([...document.querySelectorAll("label[for]")].map((l) => [l.textContent, l.htmlFor])));

let browser;
try {
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  browser = await browserType.launch({ headless: true });
  const page = await (await browser.newContext()).newPage();
  page.on("pageerror", (e) => errors.push("pageerror: " + e.message));
  page.on("console", (m) => {
    if (m.type() === "error" && !m.text().startsWith("Failed to load resource")) errors.push("console: " + m.text());
  });
  const base = `http://127.0.0.1:${server.address().port}/`;
  // The static HTML, before any script runs.
  const raw = await (await fetch(base)).text();
  const visibleRaw = raw.replace(/<template[\s\S]*?<\/template>/g, "");
  const htmlIds = [...visibleRaw.matchAll(/<label for=([^ >]+)>([^<]+)</g)].map((m) => m[2] + "=" + m[1]).sort();

  await page.goto(base, { waitUntil: "networkidle" });
  const settle = () => page.waitForTimeout(80);

  // useId: static HTML ids equal the hydrated ids, pairs match, ids unique.
  const afterLoad = await idMap(page);
  const hydrated = Object.entries(afterLoad).map(([k, v]) => k + "=" + v).sort();
  expect("ids in HTML equal ids after hydration", hydrated, htmlIds);
  let rep = await idReport(page);
  expect("no duplicate ids", rep.dup, []);
  expect("every label has one control", rep.labels.every((l) => l.endsWith(":1")), true);
  expect("label click focuses the control", rep.focusOK, true);
  expect("label count at load", rep.count, 9);

  await page.click("#show");
  await settle();
  await page.click("#add");
  await settle();
  await page.click("#add");
  await settle();
  rep = await idReport(page);
  expect("no duplicate ids after a branch and two rows", rep.dup, []);
  expect("every label has one control after changes", rep.labels.every((l) => l.endsWith(":1")), true);
  expect("label click focuses after changes", rep.focusOK, true);
  expect("label count after changes", rep.count, 12);
  const after = await idMap(page);
  for (const k of ["r1", "r2"]) expect("existing row " + k + " keeps its id", after[k], afterLoad[k]);

  // useEffectEvent: reads the latest state, from an effect and a handler.
  expect("effect event from the mount effect", await page.locator("#evt-log").innerText(), "n=0");
  await page.click("#inc");
  await page.click("#inc");
  await settle();
  await page.click("#fire");
  await settle();
  expect("effect event sees the latest state", await page.locator("#evt-log").innerText(), "n=2");

  // Activity: hidden at load, effects wait until shown, state survives hiding.
  const log = () => page.evaluate(() => (window.__log || []).join(","));
  const probeShown = () => page.evaluate(() => document.getElementById("probe").getClientRects().length > 0);
  expect("activity hidden at load", await probeShown(), false);
  expect("activity effects do not run while hidden", await log(), "");
  expect("hidden static Activity keeps its content in the HTML", (await page.locator("#act-static-hidden").count()), 1);
  await page.click("#tog");
  await settle();
  expect("activity shown", await probeShown(), true);
  expect("activity effect set up on show", await log(), "setup");
  await page.click("#probe");
  await page.click("#probe");
  await page.fill("#act-input", "typed");
  await page.click("#tog");
  await settle();
  expect("activity hidden again", await probeShown(), false);
  expect("activity effect cleaned up on hide", await log(), "setup,cleanup");
  await page.click("#tog");
  await settle();
  expect("activity effect set up again", await log(), "setup,cleanup,setup");
  expect("hidden Activity keeps component state", await page.locator("#probe").innerText(), "probe 2");
  expect("hidden Activity keeps DOM state", await page.inputValue("#act-input"), "typed");

  // ViewTransition names its element for the View Transitions API.
  expect(
    "view transition name and class",
    await page.evaluate(() => {
      const el = document.getElementById("hero");
      return [el.style.viewTransitionName, el.style.getPropertyValue("view-transition-class")];
    }),
    ["hero", "slow-fade"],
  );

  // Custom elements: objects, arrays and functions are properties.
  const out = (id) => page.evaluate((i) => JSON.parse(document.getElementById(i).getAttribute("data-out")), id);
  const ce = await out("ce-a");
  expect("custom element object prop", ce.d, { a: 1 });
  expect("custom element array prop", ce.l, [1, 2]);
  expect("custom element function prop", ce.c, 42);
  expect("custom element string and number attributes", [ce.s, ce.n, ce.b, ce.f], ["str", "3", "", null]);
  expect("custom element has no attribute for an object", await page.evaluate(() => document.getElementById("ce-a").hasAttribute("data")), false);
  expect("custom element property from state", (await out("ce-dyn")).d, { x: 1 });
  await page.click("#bump");
  await settle();
  expect("custom element property follows state", (await out("ce-dyn")).d, { x: 2 });
  expect("server HTML has no object attribute", /\[object Object\]/.test(raw) || / data=/.test(raw.replace(/data-/g, "")), false);
} catch (e) {
  failures.push(String((e && e.stack) || e));
} finally {
  if (browser) await browser.close();
  server.close();
}
if (errors.length) failures.push("browser errors:\n  " + errors.join("\n  "));
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log("React 19 browser checks passed");
