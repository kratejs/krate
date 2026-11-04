// Per-segment not-found and error pages, driven in a real browser against the
// real Go server: a nested 404 inside the layouts on hard load and on soft
// navigation (layout state kept), a client error boundary with reset(), nested
// boundaries (an error page that fails hands over to the next one up), the
// server-rendered error page, and soft navigation away clearing an error.
// Every wait is a condition wait.
import { chromium, firefox, webkit } from "playwright";

const [base, browserName = "chromium"] = process.argv.slice(2);
const browserType = { chromium, firefox, webkit }[browserName];
if (!base || !browserType) {
  console.error("usage: segments_e2e.mjs <base-url> <chromium|firefox|webkit>");
  process.exit(2);
}

const failures = [];
const log = [];
function check(cond, msg) {
  if (!cond) failures.push(msg);
  else log.push("ok: " + msg);
}

const browser = await browserType.launch();
try {
  const page = await browser.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));
  // The browser logs expected 404/500 responses, and React-style boundaries log
  // the errors they catch; neither is an app error.
  const benign = /Failed to load resource|Prefetch request denied|exploded|failed after|The above error|React will try/;
  page.on("console", (m) => {
    if (m.type() === "error" && !benign.test(m.text())) errors.push("console: " + m.text());
  });
  const path = () => new URL(page.url()).pathname;
  const text = (sel) => page.locator(sel).first().textContent();
  const count = (sel) => page.locator(sel).count();

  // A click before the page script has hydrated is lost, so retry each click
  // until its effect is visible.
  async function clickUntil(sel, done, arg) {
    for (let i = 0; i < 40; i++) {
      await page.click(sel);
      try {
        await page.waitForFunction(done, arg, { timeout: 1000 });
        return;
      } catch {}
    }
    throw new Error("click on " + sel + " never took effect; page errors: " + errors.join(" | "));
  }
  const textIs = ([sel, want]) => document.querySelector(sel) && document.querySelector(sel).textContent === want;
  const visible = (sel) => !!document.querySelector(sel);
  const bumpLayout = async (to) => clickUntil("#layout-count", textIs, ["#layout-count", String(to)]);

  // 1. Hard load of an unmatched URL under /dash: the dash not-found page,
  // inside both layouts, with status 404.
  const hard = await page.goto(base + "/dash/missing");
  check(hard.status() === 404, `a hard load of /dash/missing answers 404 (got ${hard.status()})`);
  check((await count("#dash-404")) === 1, "the nearest not-found page is the dash one");
  check((await count("#dash-layout")) === 1 && (await count("#nav")) === 1, "the not-found page renders inside the dash and root layouts");
  check((await count("#root-404")) === 0, "the root 404 page is not used under /dash");
  const robots = await page.locator('meta[name="robots"]').getAttribute("content");
  check(robots === "noindex", "segment pages are noindex");
  check((await count('link[rel="canonical"]')) === 0, "segment pages have no canonical link");

  // A URL outside /dash gets the root page.
  const root404 = await page.goto(base + "/nowhere");
  check(root404.status() === 404 && (await count("#root-404")) === 1, "a URL outside /dash gets the root 404 page");

  // 2. Soft navigation to a not-found URL keeps the layout state.
  await page.goto(base + "/dash/");
  await bumpLayout(1);
  await bumpLayout(2);
  await page.evaluate(() => {
    window.__spa = 1;
    document.getElementById("dash-layout").dataset.mark = "kept";
  });
  const missingResponse = page.waitForResponse((r) => new URL(r.url()).pathname.replace(/\/$/, "") === "/dash/missing" && r.status() !== 301);
  await page.click("#to-dash-missing");
  await page.waitForSelector("#dash-404");
  check((await missingResponse).status() === 404, "the soft navigation was answered with 404");
  check(path().startsWith("/dash/missing"), "the address bar keeps the requested URL");
  check((await text("#layout-count")) === "2", "root layout state survives the soft navigation to a not-found page");
  check((await page.locator("#dash-layout").getAttribute("data-mark")) === "kept", "the dash layout DOM is kept");
  check((await page.evaluate(() => window.__spa)) === 1, "no document reload happened");
  // ... and to the root not-found page, which shares only the root layout.
  await page.click("#to-root-missing");
  await page.waitForSelector("#root-404");
  check((await text("#layout-count")) === "2", "the root layout also survives a soft navigation to the root 404");
  check((await count("#dash-layout")) === 0, "the dash layout is gone on the root 404");

  // 3. Client error boundary: dash/crash/error.tsx. A render error after a
  // click shows its error page in place; reset() mounts the children fresh.
  await page.click("#to-crash");
  await page.waitForSelector("#crash");
  check((await text("#layout-count")) === "2", "layout state survives navigation into a segment with an error boundary");
  await clickUntil("#crash-count", textIs, ["#crash-count", "1"]);
  for (let i = 0; i < 40; i++) {
    await page.click("#arm");
    try {
      await page.waitForSelector("#inner-error", { timeout: 1000 });
      break;
    } catch {}
  }
  check((await text("#inner-msg")) === "crash page exploded", "the nearest boundary shows its error page with the thrown error");
  check((await count("#dash-layout")) === 1 && (await text("#layout-count")) === "2", "the layouts stay alive while the error page shows");
  check((await count("#outer-error")) === 0, "the outer boundary is not involved");
  await page.click("#inner-reset");
  await page.waitForSelector("#crash-ok");
  check((await text("#crash-count")) === "0", "reset() mounts the children again with fresh state");
  check((await count("#inner-error")) === 0, "the error page is gone after reset()");
  check((await page.evaluate(() => window.__spa)) === 1, "client errors do not reload the document");

  // 4. Soft navigation away from a failed segment clears the error.
  for (let i = 0; i < 40; i++) {
    await page.click("#arm");
    try {
      await page.waitForSelector("#inner-error", { timeout: 1000 });
      break;
    } catch {}
  }
  await page.click("#to-dash");
  await page.waitForSelector("#dash-home");
  check((await count("#inner-error")) === 0, "navigating away clears the error page");
  await page.click("#to-crash");
  await page.waitForSelector("#crash-ok");
  check((await count("#inner-error")) === 0, "coming back mounts the page fresh");

  // 5. Nested boundaries: dash/broken/error.tsx throws while rendering, so the
  // error goes to dash/error.tsx.
  await page.click("#to-broken");
  await page.waitForSelector("#broken");
  for (let i = 0; i < 40; i++) {
    await page.click("#arm-broken");
    try {
      await page.waitForSelector("#outer-error", { timeout: 1000 });
      break;
    } catch {}
  }
  check((await text("#outer-msg")) === "error page failed after: broken page exploded", "an error page that fails hands the error to the next boundary up: " + (await text("#outer-msg")));
  check((await text("#layout-count")) === "2", "layouts are alive after the hand-over");
  await page.click("#outer-reset");
  await page.waitForSelector("#broken-ok");
  check((await count("#outer-error")) === 0, "reset() on the outer boundary mounts the segment again");
  errors.length = 0;

  // 6. Server error page: dash/error.tsx answers an uncaught server error.
  const ssr = await page.goto(base + "/dash/ssr/");
  check(ssr.status() === 500, `an uncaught server error answers 500 (got ${ssr.status()})`);
  check(ssr.headers()["x-krate-error"] === "render", "X-Krate-Error stays on the response");
  await page.waitForFunction(() => (document.querySelector("#outer-msg") || {}).textContent);
  check((await text("#outer-msg")) === "Internal Server Error", "the message is sanitized: " + (await text("#outer-msg")));
  check(/^[0-9a-f]{10}$/.test((await text("#outer-digest")) || ""), "the error carries a digest: " + (await text("#outer-digest")));
  check((await page.content()).indexOf("secret server failure") < 0, "the real message never reaches the page");
  check((await count("#dash-layout")) === 1 && (await count("#nav")) === 1, "the server error page renders inside the layouts");
  errors.length = 0;
  // Soft navigation to it swaps it in place.
  await page.goto(base + "/dash/");
  await bumpLayout(1);
  await page.evaluate(() => (window.__spa = 2));
  await page.click("#to-ssr");
  await page.waitForFunction(() => (document.querySelector("#outer-msg") || {}).textContent);
  check((await text("#layout-count")) === "1", "layout state survives a soft navigation to the server error page");
  check((await page.evaluate(() => window.__spa)) === 2, "no document reload for the soft navigation");
  check((await text("#outer-msg")) === "Internal Server Error" && /^[0-9a-f]{10}$/.test(await text("#outer-digest")), "the soft-navigated error page is sanitized with a digest");
  errors.length = 0;

  // 7. No boundary code on pages without an error boundary.
  const homeHtml = await (await fetch(base + "/")).text();
  const scripts = [...homeHtml.matchAll(/<script[^>]+src=["']?([^"' >]+)/g)].map((m) => m[1]);
  let shipped = homeHtml;
  for (const src of scripts) shipped += await (await fetch(new URL(src, base))).text();
  check(!/KrateSegmentBoundary/.test(shipped), "a page outside any error boundary ships no boundary code");
  const crashHtml = await (await fetch(base + "/dash/crash/")).text();
  check(/KrateSegmentBoundary/.test(crashHtml + (await (await fetch(new URL(crashHtml.match(/<script[^>]+src=["']?(\/dash\/crash\/index[^"' >]+)/)[1], base))).text())), "a page inside one ships it");

  check(errors.length === 0, "no page errors: " + errors.join(" | "));
} catch (e) {
  failures.push("script error: " + (e && e.stack ? e.stack : e));
} finally {
  await browser.close();
}
console.log(log.join("\n"));
if (failures.length) {
  console.error("FAILURES:\n" + failures.join("\n"));
  process.exit(1);
}
