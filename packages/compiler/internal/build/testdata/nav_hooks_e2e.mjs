// The router hooks (usePathname, useParams, useSearchParams, useRouter,
// useSelectedLayoutSegment(s)) in a real browser against the real Go server:
// right values in the raw HTML (before any script), live after SPA navigation,
// router.push/replace/back/forward/refresh, browser back and forward, a
// catch-all param and a request-time page. Every wait is a condition wait.
import { chromium, firefox, webkit } from "playwright";

const [base, browserName = "chromium"] = process.argv.slice(2);
const browserType = { chromium, firefox, webkit }[browserName];
if (!base || !browserType) {
  console.error("usage: nav_hooks_e2e.mjs <base-url> <chromium|firefox|webkit>");
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
  // A click that starts a navigation while a view transition runs skips it.
  page.on("pageerror", (e) => { if (!/Transition was skipped/.test(String(e))) errors.push(String(e)); });
  page.on("console", (m) => {
    if (m.type() === "error" && !/Failed to load resource/.test(m.text())) errors.push("console: " + m.text());
  });
  const text = (sel) => page.locator(sel).first().textContent();
  const path = () => new URL(page.url()).pathname + new URL(page.url()).search;
  const waitText = (sel, want) =>
    page.waitForFunction(([s, w]) => (document.querySelector(s) || {}).textContent === w, [sel, want]);
  const waitPath = (want) => page.waitForFunction((w) => location.pathname + location.search === w, want);

  // 1. Raw HTML, as a crawler or a slow connection sees it: no script has run.
  const raw = async (p) => (await fetch(base + p)).text();
  const a = await raw("/blog/a/");
  check(/id="?post-path"?[^>]*>(<!--[^>]*-->)?\/blog\/a(<!--[^>]*-->)?</.test(a), "static /blog/a carries its pathname in the raw HTML");
  check(/id="?post-slug"?[^>]*>(<!--[^>]*-->)?a(<!--[^>]*-->)?</.test(a), "static /blog/a carries its param in the raw HTML");
  const b = await raw("/blog/b/");
  check(/id="?post-path"?[^>]*>(<!--[^>]*-->)?\/blog\/b(<!--[^>]*-->)?</.test(b), "static /blog/b carries its own pathname");
  const zzz = await raw("/blog/zzz/");
  check(/id="?post-path"?[^>]*>(<!--[^>]*-->)?\/blog\/zzz(<!--[^>]*-->)?</.test(zzz), "an unbaked param gets its pathname from the template");
  const ssrRaw = await raw("/ssr/42/?tab=info");
  check(/id="?ssr-path"?[^>]*>(<!--[^>]*-->)?\/ssr\/42(<!--[^>]*-->)?</.test(ssrRaw), "an SSR page renders its pathname per request");
  check(/id="?ssr-id"?[^>]*>(<!--[^>]*-->)?42(<!--[^>]*-->)?</.test(ssrRaw), "an SSR page renders its param per request");
  check(/id="?ssr-tab"?[^>]*>(<!--[^>]*-->)?info(<!--[^>]*-->)?</.test(ssrRaw), "an SSR page renders its query per request");
  const ssrOther = await raw("/ssr/7/?tab=x");
  check(/id="?ssr-id"?[^>]*>(<!--[^>]*-->)?7(<!--[^>]*-->)?</.test(ssrOther) && /id="?ssr-tab"?[^>]*>(<!--[^>]*-->)?x(<!--[^>]*-->)?</.test(ssrOther), "a different SSR request renders different values");

  // 2. Hard load, then client navigation keeps the hooks live.
  await page.goto(base + "/");
  await page.waitForSelector("#home");
  await waitText("#nav-path", "/");
  check((await page.locator("#to-home").getAttribute("class")) === "active", "the active link follows the pathname on the home page");
  await page.evaluate(() => (window.__spa = 1));
  await page.click("#layout-count");
  await waitText("#layout-count", "1");

  await page.click("#to-dash");
  await page.waitForSelector("#dash-home");
  await waitText("#nav-path", "/dash");
  check((await page.locator("#to-dash").getAttribute("class")) === "active", "the active link moves with client navigation");
  check((await page.locator("#to-home").getAttribute("class")) === "idle", "and the previous link goes idle");
  check((await text("#nav-seg")) === "dash", "the root layout's useSelectedLayoutSegment is dash");
  check((await text("#dash-seg")) === "null", "the dash layout on its own page has no selected segment");

  await page.click("#to-settings");
  await page.waitForSelector("#dash-settings");
  await waitText("#dash-seg", "settings");
  check((await text("#dash-segs")) === "settings", "the dash layout counts from its own depth: " + (await text("#dash-segs")));
  check((await text("#nav-segs")) === "dash|settings", "the root layout counts from the root: " + (await text("#nav-segs")));
  check((await text("#layout-count")) === "1" && (await page.evaluate(() => window.__spa)) === 1, "layout state and the document survive");

  // 3. Dynamic route params, baked and unbaked, and a catch-all.
  await page.click("#to-a");
  await page.waitForSelector("#post");
  await waitText("#post-slug", "a");
  check((await text("#post-path")) === "/blog/a", "client navigation to a baked page: pathname");
  await page.click("#to-b");
  await waitText("#post-slug", "b");
  check((await text("#post-path")) === "/blog/b", "and to the next: " + (await text("#post-path")));
  await page.click("#to-zzz");
  await waitText("#post-slug", "zzz");
  check((await text("#post-path")) === "/blog/zzz", "client navigation to an unbaked param");
  await page.click("#to-docs");
  await page.waitForSelector("#docs-rest");
  await waitText("#docs-rest", "x+y+z");
  check(true, "a catch-all param is an array");

  // 4. Back and forward through all of it.
  await page.goBack();
  await waitText("#post-slug", "zzz");
  check((await text("#nav-path")) === "/blog/zzz", "back: the layout hook follows");
  await page.goBack();
  await waitText("#post-slug", "b");
  await page.goBack();
  await waitText("#post-slug", "a");
  check((await text("#post-path")) === "/blog/a", "back twice: params and pathname");
  await page.goBack();
  await waitText("#dash-seg", "settings");
  await page.goForward();
  await waitText("#post-slug", "a");
  await page.goForward();
  await waitText("#post-slug", "b");
  check((await text("#nav-path")) === "/blog/b", "forward: pathname");
  check((await page.evaluate(() => window.__spa)) === 1 && (await text("#layout-count")) === "1", "back and forward never reloaded the document");

  // 5. useRouter: push (with a query), replace, back, forward, refresh, prefetch.
  await page.goto(base + "/");
  await page.waitForSelector("#home");
  await page.evaluate(() => (window.__spa = 2));
  await page.click("#push");
  await waitPath("/blog/a/?q=pushed");
  await waitText("#post-slug", "a");
  check((await page.evaluate(() => window.__spa)) === 2, "router.push navigates without a reload");
  await page.goBack();
  await page.waitForSelector("#home");
  await waitText("#q", "null");
  // replace swaps the current entry: back from it must skip the home page.
  await page.click("#to-dash");
  await page.waitForSelector("#dash-home");
  await page.goBack();
  await page.waitForSelector("#home");
  await page.click("#replace");
  await waitPath("/blog/b/");
  await waitText("#post-slug", "b");
  await page.goForward();
  await page.waitForSelector("#dash-home");
  await page.goBack();
  await waitText("#post-slug", "b");
  check(path() === "/blog/b/", "router.replace replaced the history entry: " + path());
  await page.goto(base + "/");
  await page.waitForSelector("#home");
  await page.evaluate(() => (window.__spa = 3));
  await page.click("#to-dash");
  await page.waitForSelector("#dash-home");
  await page.goBack();
  await page.waitForSelector("#home");
  await page.click("#forward");
  await page.waitForSelector("#dash-home");
  check(path().startsWith("/dash"), "router.forward");
  await page.goBack();
  await page.waitForSelector("#home");
  await page.evaluate(() => window.history.pushState({}, "", "/?q=typed"));
  await page.click("#prefetch");
  await page.click("#refresh");
  await page.waitForSelector("#home");
  check((await page.evaluate(() => window.__spa)) === 3, "router.refresh and router.prefetch keep the document");

  // 6. A request-time page: the browser takes over after the server render.
  await page.goto(base + "/ssr/42/?tab=info");
  await page.waitForSelector("#ssr-path");
  await waitText("#nav-path", "/ssr/42");
  check((await text("#ssr-path")) === "/ssr/42" && (await text("#ssr-id")) === "42" && (await text("#ssr-tab")) === "info", "the SSR page after hydration");

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
