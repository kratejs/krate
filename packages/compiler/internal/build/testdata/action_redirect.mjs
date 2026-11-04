// Submits a form whose server action redirects, and checks the SPA router
// swaps the page without page errors.
import { chromium, firefox, webkit } from "playwright";

const [base, browserName = "chromium"] = process.argv.slice(2);
const browser = await { chromium, firefox, webkit }[browserName].launch();
const errors = [];
const result = {};
try {
  const page = await browser.newPage();
  page.on("pageerror", (e) => errors.push(e.message));
  const text = () => page.textContent("#page");

  await page.goto(base + "/go");
  await page.waitForSelector("#go");
  await page.waitForTimeout(500);
  await page.click("#go");
  await page.waitForFunction(() => document.querySelector("#page")?.textContent === "about", null, { timeout: 8000 });
  result.formRedirect = { path: new URL(page.url()).pathname, page: await text() };
  // The new page is live: its link navigates softly again.
  await page.click("#to-go");
  await page.waitForFunction(() => document.querySelector("#page")?.textContent === "go", null, { timeout: 8000 });
  result.afterNav = await text();

  await page.goto(base + "/");
  await page.waitForSelector("#login");
  await page.waitForTimeout(500);
  await page.click("#login");
  await page.waitForSelector("#error", { timeout: 8000 });
  await page.fill("#user", "ada");
  await page.click("#login");
  await page.waitForFunction(() => document.querySelector("#page")?.textContent === "account", null, { timeout: 8000 });
  result.actionState = { path: new URL(page.url()).pathname, page: await text() };
  result.errors = errors;
  console.log(JSON.stringify(result));
} catch (e) {
  console.log(JSON.stringify({ failed: String(e.message).split("\n")[0], result, errors }));
  process.exitCode = 1;
} finally {
  await browser.close();
}
