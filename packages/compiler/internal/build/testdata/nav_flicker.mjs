// Samples every animation frame of soft navigations (see
// nav_flicker_browser_test.go): #root opacity, whether the old and new page
// are in the DOM, html[data-krate-navigating], the loading UI, and the time
// until the new page's handlers work. Prints one JSON array (VT off, VT on).
import { chromium, firefox, webkit } from "playwright";

const base = process.argv[2];
const browserName = process.argv[3] || "chromium";
const browserType = { chromium, firefox, webkit }[browserName];
if (!base || !browserType) {
  console.error("usage: nav_flicker.mjs <base-url> [chromium|firefox|webkit]");
  process.exit(2);
}

const setDelay = (doc, css, js) => fetch(`${base}/__delay?doc=${doc}&css=${css}&js=${js}`);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function installProbes(vtOn) {
  if (!vtOn) window.__KRATE_CFG__ = { viewTransitions: false };
  window.__navFetches = [];
  const f = window.fetch;
  window.fetch = function (input) {
    try {
      const u = new URL(typeof input === "string" ? input : input.url, location.href);
      if (!/\.(js|css)$/.test(u.pathname)) window.__navFetches.push(u.pathname);
    } catch {}
    return f.apply(this, arguments);
  };
  window.__vts = 0;
  if (typeof document.startViewTransition === "function") {
    const start = document.startViewTransition.bind(document);
    document.startViewTransition = (cb) => {
      window.__vts++;
      return start(cb);
    };
  }
  window.__startSample = (sel) => {
    const s = { t0: performance.now(), frames: [], done: false, contentAt: null, interactiveAt: null };
    window.__sample = s;
    const tick = () => {
      if (s.done) return;
      const root = document.getElementById("root");
      const t = performance.now() - s.t0;
      const f = {
        t,
        op: root ? parseFloat(getComputedStyle(root).opacity) : 1,
        old: !!document.querySelector(sel.old),
        neu: !!document.querySelector(sel.neu),
        nav: document.documentElement.hasAttribute("data-krate-navigating"),
        loading: !!document.getElementById("loading-ui"),
      };
      s.frames.push(f);
      if (f.neu && s.contentAt === null) s.contentAt = t;
      if (sel.button && s.contentAt !== null && s.interactiveAt === null) {
        const btn = document.querySelector(sel.button);
        if (btn) {
          if (btn.textContent.trim() !== "0") s.interactiveAt = t;
          else btn.click();
        }
      }
      requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  };
}

// Starts sampling, runs act in the page, waits for the new page (and its
// working button), then summarizes the frames up to 300ms after it appeared.
async function measure(page, sel, act) {
  await page.evaluate((sel) => window.__startSample(sel), sel);
  const before = await page.evaluate(() => window.__navFetches.length);
  await page.evaluate(act);
  await page.waitForFunction(
    (hasButton) => window.__sample.contentAt !== null && (!hasButton || window.__sample.interactiveAt !== null),
    !!sel.button,
    { timeout: 10000 },
  );
  await sleep(300);
  return page.evaluate((before) => {
    const s = window.__sample;
    s.done = true;
    const frames = s.frames.filter((f) => f.t <= s.contentAt + 300);
    const fetches = window.__navFetches.slice(before);
    return {
      ContentMs: Math.round(s.contentAt),
      InteractiveMs: s.interactiveAt === null ? 0 : Math.round(s.interactiveAt),
      MinOpacity: Math.min(...frames.map((f) => f.op)),
      FadeFrames: frames.filter((f) => f.op < 0.99).length,
      BlankFrames: frames.filter((f) => !f.old && !f.neu).length,
      LoadingFrames: frames.filter((f) => f.loading).length,
      NavigatingFrames: frames.filter((f) => f.nav).length,
      Fetches: fetches.length,
      Frames: frames.length,
    };
  }, before);
}

async function run(browser, vtOn) {
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));
  await page.addInitScript(installProbes, vtOn);
  const out = { Mode: vtOn ? "vt-on" : "vt-off", Errors: errors };

  await setDelay(400, 150, 250);
  await page.goto(base + "/");
  await page.waitForLoadState("networkidle");
  await sleep(200);
  const click = (id) => `document.getElementById(${JSON.stringify(id)}).click()`;

  // Uncached navigation with a slow document, stylesheet and page script.
  out.Uncached = await measure(page, { old: "#page-home", neu: "#page-a", button: "#count-a" }, click("to-a"));
  await page.waitForLoadState("networkidle");

  // Island page, uncached.
  out.Island = await measure(page, { old: "#page-a", neu: "#page-b", button: "#count-b" }, click("to-b"));
  await page.waitForLoadState("networkidle");
  await sleep(1200); // idle-time work (the first entry's document) settles

  // Back to /a (rendered by the router), then to / (the hard-loaded entry).
  out.Back = await measure(page, { old: "#page-b", neu: "#page-a" }, "history.back()");
  out.BackInitial = await measure(page, { old: "#page-a", neu: "#page-home" }, "history.back()");

  // A fast document must not flash the loading UI.
  await setDelay(40, 0, 0);
  out.Fast = await measure(page, { old: "#page-home", neu: "#page-b" }, click("to-b"));
  await page.waitForLoadState("networkidle");

  // Hover starts the prefetch; the click lands while it is in flight.
  await setDelay(400, 150, 250);
  await page.evaluate(() => document.getElementById("to-a").dispatchEvent(new MouseEvent("mouseover", { bubbles: true })));
  await sleep(260);
  out.Inflight = await measure(page, { old: "#page-b", neu: "#page-a" }, click("to-a"));
  await page.waitForLoadState("networkidle");

  // View Transitions per kind of navigation.
  await setDelay(0, 0, 0);
  const vts = async (fn) => {
    const before = await page.evaluate(() => window.__vts);
    await page.evaluate(fn);
    await sleep(150);
    return (await page.evaluate(() => window.__vts)) - before;
  };
  const router = "globalThis[Symbol.for('krate.router')]";
  out.VT = {
    Query: await vts(`${router}.replace('/a/?q=1', { scroll: false })`),
    Refresh: await vts(`${router}.refresh()`),
    Page: await vts(`${router}.push('/b/')`),
  };
  await ctx.close();
  return out;
}

const browser = await browserType.launch();
try {
  const results = [];
  for (const vtOn of [false, true]) results.push(await run(browser, vtOn));
  console.log(JSON.stringify(results));
} finally {
  await browser.close();
}
