// Submits island forms whose server action arrives as a prop: the FormData
// reaches the action, bound arguments survive, and the SPA router leaves the
// form to the island (no GET navigation).
import { chromium, firefox, webkit } from "playwright";

const [base, browserName = "chromium"] = process.argv.slice(2);
const browser = await { chromium, firefox, webkit }[browserName].launch();
const errors = [];
const result = {};
try {
  const page = await browser.newPage();
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto(base + "/");
  await page.waitForSelector("#plain");
  await page.waitForTimeout(800);
  for (const id of ["plain", "bound"]) {
    await page.fill(`form:has(#${id}) input`, "ada-" + id);
    await page.click("#" + id);
    await page.waitForSelector("#" + id + "-status", { timeout: 8000 });
    result[id] = await page.textContent("#" + id + "-status");
  }
  result.url = new URL(page.url()).pathname + new URL(page.url()).search;
  result.errors = errors;
  console.log(JSON.stringify(result));
} catch (e) {
  console.log(JSON.stringify({ failed: String(e.message).split("\n")[0], result, errors }));
  process.exitCode = 1;
} finally {
  await browser.close();
}
