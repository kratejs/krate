// Islands with no element of their own: the parent's child combinators see
// their root nodes, fragment and text roots hydrate and update in place, and
// an island nested in another island's server children keeps its nodes and
// state across a hide and show. Prints JSON last.
import { chromium, firefox, webkit } from "playwright";
import { waitHydrated } from "./island_helpers.mjs";

const [base, browserName = "chromium"] = process.argv.slice(2);
const browser = await { chromium, firefox, webkit }[browserName].launch();
const errors = [];
try {
  const page = await browser.newPage();
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => {
    if (m.type() === "error") errors.push(m.text());
  });
  await page.goto(base + "/");
  await waitHydrated(page, "#card");
  await waitHydrated(page, "#a");
  await waitHydrated(page, "#txt-btn");
  await waitHydrated(page, "#inner-a", { depth: 2 });
  const order = () => page.evaluate(() => Array.from(document.querySelector("#stack").children).map((e) => e.id).join(","));
  const r = {};
  r.hosts = await page.evaluate(() => document.querySelectorAll("krate-island").length);
  r.cardParent = await page.evaluate(() => document.querySelector("#card").parentElement.id);
  r.cardMargin = await page.evaluate(() => getComputedStyle(document.querySelector("#card")).marginBottom);
  r.order0 = await order();
  await page.click("#toggle-b");
  r.order1 = await order();
  await page.click("#toggle-b");
  r.order2 = await order();
  await page.click("#txt-btn");
  r.text = await page.evaluate(() => document.querySelector("#txt").textContent);
  // The nested island adds a root node, then the outer island hides and
  // shows its server children.
  await page.click("#inner-a");
  r.inner1 = await page.evaluate(() => Array.from(document.querySelector("#wrap").children).map((e) => e.id + ":" + e.textContent).join(","));
  await page.click("#outer-toggle");
  r.hidden = await page.evaluate(() => !document.querySelector("#wrap") && !document.querySelector("#inner-a") && !document.querySelector("#inner-extra"));
  await page.click("#outer-toggle");
  await page.waitForSelector("#inner-a");
  r.inner2 = await page.evaluate(() => Array.from(document.querySelector("#wrap").children).map((e) => e.id + ":" + e.textContent).join(","));
  await page.click("#inner-a");
  r.inner3 = await page.evaluate(() => Array.from(document.querySelector("#wrap").children).map((e) => e.id + ":" + e.textContent).join(","));
  r.after = await page.evaluate(() => document.querySelector("#stack").lastElementChild.id);
  console.log(JSON.stringify({ ...r, errors }));
} finally {
  await browser.close();
}
