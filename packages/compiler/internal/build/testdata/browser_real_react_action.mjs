// Clicks the button of a real-React island that calls a server action (see
// TestBrowserRealReactIslandAction). sha256("hello").slice(0, 8) is 2cf24dba.
import { chromium, firefox, webkit } from "playwright";

const browserType = { chromium, firefox, webkit }[process.argv[2] || "chromium"];
const base = process.argv[3];
const browser = await browserType.launch({ headless: true });
let failure = "";
try {
  const page = await (await browser.newContext()).newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto(base + "/");
  await page.waitForSelector("#cv", { timeout: 15000 });
  await page.click("#save");
  try {
    await page.waitForFunction(() => document.querySelector("#save").textContent.includes("2cf24dba"), null, { timeout: 15000 });
  } catch (e) {
    failure = "the action result never reached the island: " + (await page.textContent("#save")) + " " + errors.join("; ");
  }
} finally {
  await browser.close();
}
if (failure) {
  console.error(failure);
  process.exit(1);
}
console.log("ok");
