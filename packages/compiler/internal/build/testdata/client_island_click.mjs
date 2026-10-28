// Drives a client component rendered inside an async server page: waits for
// the island to hydrate, clicks it twice and reports the result as JSON.
import { chromium, firefox, webkit } from "playwright";

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
  await page.waitForSelector("krate-island[data-krate-island-hydrated]", { timeout: 15000 });
  const before = await page.textContent("#inc");
  await page.click("#inc");
  await page.waitForFunction(() => document.querySelector("#inc").textContent === "Count: 6");
  await page.click("#inc");
  await page.waitForFunction(() => document.querySelector("#inc").textContent === "Count: 7");
  const after = await page.textContent("#inc");
  const children = await page.textContent("krate-children");
  console.log(JSON.stringify({ before, after, children, errors }));
} finally {
  await browser.close();
}
