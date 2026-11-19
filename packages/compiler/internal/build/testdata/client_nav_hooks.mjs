// Drives router hooks in client components of a server-rendered app: the nav
// marks the active link, follows client-side navigation, and the filters read
// the query. Prints the result as JSON on the last line.
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
  const current = () =>
    page.evaluate(() => Array.from(document.querySelectorAll("#nav a[aria-current]")).map((a) => a.id).join(","));
  await page.goto(base + "/");
  await waitHydrated(page);
  const home = await current();
  await page.click("#nav-store");
  await page.waitForFunction(() => document.querySelector("#nav-store")?.hasAttribute("aria-current"));
  const store = await current();
  await page.click("#nav-templates");
  await page.waitForSelector("#filters");
  await page.waitForFunction(() => document.querySelector("#nav-templates")?.hasAttribute("aria-current"));
  const templates = await current();
  const q1 = await page.textContent("#q");
  const router = await page.getAttribute("#filters", "data-router");
  await page.goto(base + "/templates/?q=direct");
  await waitHydrated(page);
  const q2 = await page.textContent("#q");
  console.log(JSON.stringify({ home, store, templates, q1, q2, router, errors }));
} finally {
  await browser.close();
}
