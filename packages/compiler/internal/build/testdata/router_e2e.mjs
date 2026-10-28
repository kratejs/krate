// Router e2e: soft navigation, intercepting routes, layout persistence and
// templates against a running Krate server. Every wait is a condition wait.
import { chromium, firefox, webkit } from "playwright";

const base = process.argv[2];
const browserName = process.argv[3] || "chromium";
const browserType = { chromium, firefox, webkit }[browserName];
if (!base || !browserType) {
  console.error("usage: router_e2e.mjs <base-url> [chromium|firefox|webkit]");
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
  // WebKit reports a prefetch that a hard navigation cancels mid-flight as a
  // page error ("Fetch API cannot load ... due to access control checks") even
  // though the router handles the rejection. That is the browser cancelling the
  // request, not a router failure.
  const cancelledFetch = /Fetch API cannot load .* due to access control checks/;
  page.on("pageerror", (e) => {
    if (browserName === "webkit" && cancelledFetch.test(String(e))) return;
    errors.push(String(e));
  });
  const text = (sel) => page.locator(sel).first().textContent();
  const path = () => new URL(page.url()).pathname;

  await page.goto(base + "/");
  await page.waitForFunction(() => window.__krate_mod !== undefined || document.readyState === "complete");
  // Layout state.
  await page.click("#layout-count");
  await page.click("#layout-count");
  await page.waitForFunction(() => document.querySelector("#layout-count").textContent === "2");
  // A marker on window proves no full document load happens below.
  await page.evaluate(() => (window.__spaMarker = 1));

  // Soft navigation to an intercepted URL opens the modal over the feed.
  await page.click("#p1");
  await page.waitForSelector("#modal");
  check(path() === "/photo/1", `intercept changes the URL to /photo/1 (got ${path()})`);
  check((await text("#modal")).includes("Modal photo 1"), "modal renders the param");
  check(await page.locator("#feed").count() === 1, "the feed stays rendered under the modal");
  check(await page.locator("#photo-page").count() === 0, "the real photo page is not rendered on soft navigation");
  check((await text("#layout-count")) === "2", "layout state persists across the intercept");

  // Chained intercept inside the modal.
  await page.click("#modal a");
  await page.waitForFunction(() => (document.querySelector("#modal") || {}).textContent?.includes("Modal photo 2"));
  check(path() === "/photo/2", "chained intercept updates the URL");

  // Back twice closes the modal and returns to the feed.
  await page.goBack();
  await page.waitForFunction(() => (document.querySelector("#modal") || {}).textContent?.includes("Modal photo 1"));
  await page.goBack();
  await page.waitForSelector("#modal", { state: "detached" });
  check(path() === "/", "back returns to /");
  check(await page.locator("#modal-default").count() === 1, "the slot shows its default content again");
  check((await text("#layout-count")) === "2", "layout state persists after back");

  // Forward reopens the modal.
  await page.goForward();
  await page.waitForSelector("#modal");
  check(path() === "/photo/1", "forward reopens the intercepted modal");
  await page.goBack();
  await page.waitForSelector("#modal", { state: "detached" });

  // Template remounts on navigation between sibling pages, layout persists.
  await page.click('nav a[href="/t/a"]');
  await page.waitForSelector("#ta");
  await page.click("#tpl-count");
  await page.waitForFunction(() => document.querySelector("#tpl-count").textContent === "1");
  await page.click("#to-b");
  await page.waitForSelector("#tb");
  await page.waitForFunction(() => document.querySelector("#tpl-count").textContent === "0");
  check(true, "template state resets on navigation");
  check((await text("#layout-count")) === "2", "layout state persists through template routes");
  await page.click("#tpl-count");
  await page.waitForFunction(() => document.querySelector("#tpl-count").textContent === "1");
  check(await page.evaluate(() => window.__spaMarker === 1), "no full page load happened during soft navigations");

  // Hard load of the intercepted URL renders the real page.
  await page.goto(base + "/photo/1");
  await page.waitForSelector("#photo-page");
  check((await text("#photo-page")).includes("Photo page 1"), "hard load renders the real photo page");
  check(await page.locator("#modal").count() === 0, "hard load does not render the modal");

  // Catch-all routing and specificity.
  await page.goto(base + "/docs/a/b");
  await page.waitForSelector("#docs");
  check((await text("#docs")).includes("docs a/b"), "catch-all captures the rest of the path");
  await page.goto(base + "/docs/intro");
  await page.waitForSelector("#intro");
  check(true, "a static route beats the catch-all");

  check(errors.length === 0, "no page errors: " + errors.join(" | "));
} finally {
  await browser.close();
}

console.log(log.join("\n"));
if (failures.length) {
  console.error("FAILURES:\n" + failures.join("\n"));
  process.exit(1);
}
