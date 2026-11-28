// A keyed list of islands across refreshes that delete rows (see
// TestBrowserKeyedIslandsKeepStateAcrossRefresh). Prints JSON last.
import { chromium, firefox, webkit } from "playwright";
import { waitHydrated } from "./island_helpers.mjs";

const [base, browserName = "chromium"] = process.argv.slice(2);
const browser = await { chromium, firefox, webkit }[browserName].launch();
const errors = [];
const steps = {};
const order = [];
try {
  const context = await browser.newContext();
  const page = await context.newPage();
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => {
    if (m.type() === "error") errors.push(m.text());
  });
  const look = () =>
    page.evaluate(() => ({
      count: document.getElementById("count").textContent,
      shown: Array.from(document.querySelectorAll("#list .card")).map((e) => e.getAttribute("data-id")),
    }));
  const record = async (name) => {
    steps[name] = await look();
    order.push(name);
  };
  // A delete: wait for the refresh to bring the server's new count, then
  // for the islands to settle.
  const del = async (id, count) => {
    await page.click(`.card[data-id="${id}"] .del`);
    await page
      .waitForFunction((c) => document.getElementById("count").textContent === c, count, { timeout: 10000 })
      .catch(() => errors.push("no refresh after deleting " + id));
    await page.waitForFunction(() => (window.__krateIslands || []).every((r) => r.status !== "loading"), null, { timeout: 5000 }).catch(() => {});
    await page.waitForTimeout(300);
  };
  await page.goto(base + "/rows/");
  await waitHydrated(page, '.card[data-id="c"] .del');
  await record("load");
  await page.click('.card[data-id="c"] .hide');
  await page.waitForTimeout(100);
  await record("hide-c");
  await del("a", "2");
  await record("delete-a");
  await del("b", "1");
  await record("delete-b");
  await page.reload();
  await waitHydrated(page, '.card[data-id="c"] .del');
  await record("reload");
  await del("c", "0");
  await record("delete-c");
} catch (e) {
  errors.push(String(e && e.stack ? e.stack : e));
} finally {
  await browser.close();
}
console.log(JSON.stringify({ steps, order, errors }));
