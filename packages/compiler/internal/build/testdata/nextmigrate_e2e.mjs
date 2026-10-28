// Migrated Next.js app, driven in a real browser against the real Go server:
// async server components with tagged fetch, a client island inside an async
// page, revalidateTag from a server action refreshing the open route, layout
// persistence, template remount, notFound(), and a parallel + intercepting
// modal route. Every wait is a condition wait.
import { chromium, firefox, webkit } from "playwright";

const [base, browserName = "chromium", dataBase] = process.argv.slice(2);
const browserType = { chromium, firefox, webkit }[browserName];
if (!base || !dataBase || !browserType) {
  console.error("usage: nextmigrate_e2e.mjs <base-url> <chromium|firefox|webkit> <data-url>");
  process.exit(2);
}

const failures = [];
const log = [];
function check(cond, msg) {
  if (!cond) failures.push(msg);
  else log.push("ok: " + msg);
}

const gets = async () => (await (await fetch(dataBase + "/__count")).json()).gets;

const browser = await browserType.launch();
try {
  const page = await browser.newPage();
  const errors = (globalThis.__pageErrors = []);
  page.on("pageerror", (e) => errors.push(String(e)));
  // WebKit refuses <link rel=prefetch> on plain-http origins and says so on the
  // console; that is the browser declining a hint, not an app error.
  const benign = /Prefetch request denied/;
  page.on("console", (m) => {
    if (m.type() === "error" && !benign.test(m.text())) errors.push("console: " + m.text());
  });
  await page.addInitScript(() => {
    window.addEventListener("krate:navigate-error", (e) => console.error("navigate-error " + JSON.stringify(e.detail || {})));
  });
  const path = () => new URL(page.url()).pathname;
  const text = (sel) => page.locator(sel).first().textContent();

  // A click before the page script has hydrated is lost, so retry each click
  // until its effect is visible (a condition wait, not a timer).
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
  const countIs = (n) => ([sel, want]) => document.querySelector(sel).textContent === want;

  await page.goto(base + "/");
  await clickUntil("#layout-count", countIs(), ["#layout-count", "1"]);
  await clickUntil("#layout-count", countIs(), ["#layout-count", "2"]);
  await page.evaluate(() => (window.__spaMarker = 1));
  const mountsBefore = await page.evaluate(() => window.__tplMounts || 0);

  // Soft navigation into an async server component page.
  await page.click("#nav-posts");
  await page.waitForSelector("#posts li");
  check(path() === "/posts", "soft navigation reaches /posts");
  check((await page.locator("#posts li").count()) === 2, "the async page rendered the fetched posts");
  check((await text("#layout-count")) === "2", "layout state persists across the navigation" + (errors.length ? " (page errors: " + errors.join(" | ") + ")" : ""));
  check((await page.evaluate(() => window.__spaMarker)) === 1, "navigation did not reload the document");
  await page.waitForFunction((m) => (window.__tplMounts || 0) > m, mountsBefore);
  check(true, "the template remounted on navigation");

  // "use client" island inside the async page hydrates.
  await clickUntil("#like-first-post", countIs(), ["#like-first-post", "1 likes"]);
  check(true, "client component inside an async server page is interactive");

  // The tagged fetch is cached: a reload does not hit the data server.
  const g1 = await gets();
  await page.reload();
  await page.waitForSelector("#posts li");
  check((await gets()) === g1, "a reload is served from the cache without refetching");

  // revalidateTag from a server action refreshes the open route in place.
  await page.evaluate(() => (window.__spaMarker = 2));
  // The page hydrated above (the like button works after the reload), so this
  // click is not retried: a retry could add the post twice.
  await clickUntil("#like-first-post", countIs(), ["#like-first-post", "1 likes"]);
  await page.click("#add-post");
  await page.waitForFunction(() => document.querySelectorAll("#posts li").length === 3);
  check(true, "revalidateTag re-rendered the page with the new post");
  check((await page.evaluate(() => window.__spaMarker)) === 2, "the refresh swapped content without a document reload");
  check((await gets()) > g1, "the data was refetched after revalidation");

  // notFound() in an async server component answers 404.
  const missing = await page.goto(base + "/posts/missing");
  check(missing.status() === 404, `notFound() answers 404 (got ${missing.status()})`);
  // The browser logs the expected 404 as a console error; it is not a page error.
  errors.length = 0;
  const known = await page.goto(base + "/posts/first-post");
  check(known.status() === 200, "an existing post renders");
  check((await text("#post h1")) === "First post", "params are awaited in the async page");

  // Parallel + intercepting route.
  await page.goto(base + "/");
  await page.evaluate(() => (window.__spaMarker = 3));
  await page.click("#photo-1");
  await page.waitForSelector("#modal");
  check(path() === "/photo/1", "intercepted soft navigation changes the URL");
  check((await page.locator("#feed").count()) === 1, "the feed stays under the modal");
  check((await page.locator("#photo-page").count()) === 0, "the full photo page is not rendered on soft navigation");
  await page.goto(base + "/photo/1");
  check((await page.locator("#photo-page").count()) === 1, "a hard load renders the real photo page");
  check((await page.locator("#modal").count()) === 0, "a hard load does not render the modal");

  check(errors.length === 0, "no page errors: " + errors.join(" | "));
} catch (e) {
  failures.push("script error: " + (e && e.stack ? e.stack : e) + (globalThis.__pageErrors && globalThis.__pageErrors.length ? "\npage errors: " + globalThis.__pageErrors.join(" | ") : ""));
} finally {
  await browser.close();
}
console.log(log.join("\n"));
if (failures.length) {
  console.error("FAILURES:\n" + failures.join("\n"));
  process.exit(1);
}
