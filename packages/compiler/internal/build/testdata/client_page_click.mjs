// Drives a "use client" page under an async layout: waits for the island to
// hydrate, clicks the counter once and reports the result as JSON.
import { chromium, firefox, webkit } from "playwright";
import { waitHydrated } from "./island_helpers.mjs";

const [url, browserName = "chromium"] = process.argv.slice(2);
const browser = await { chromium, firefox, webkit }[browserName].launch();
const errors = [];
try {
  const page = await browser.newPage();
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => {
    if (m.type() === "error") errors.push(m.text());
  });
  await page.goto(url);
  await waitHydrated(page);
  const before = await page.textContent("#inc");
  await page.click("#inc");
  await page.waitForFunction(() => document.querySelector("#inc").textContent === "Count: 6");
  console.log(JSON.stringify({ before, after: await page.textContent("#inc"), errors }));
} finally {
  await browser.close();
}
