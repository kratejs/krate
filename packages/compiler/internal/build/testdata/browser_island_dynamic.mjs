// A Preact island and a real-React island share counter.ts with the module
// they import() (see TestBrowserIslandDynamicImportShared).
import { chromium, firefox, webkit } from "playwright";

const browserType = { chromium, firefox, webkit }[process.argv[2] || "chromium"];
const base = process.argv[3];
const browser = await browserType.launch({ headless: true });
const failures = [];
try {
  const page = await (await browser.newContext()).newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => {
    if (m.type() === "error") errors.push(m.text());
  });
  await page.goto(base + "/");
  try {
    await page.waitForFunction(() => ["real", "preact"].every((id) => (document.getElementById(id)?.textContent ?? "") !== ""), null, { timeout: 15000 });
  } catch {
    throw new Error("the islands did not finish; errors: " + errors.join("; "));
  }
  const calls = [await page.textContent("#real"), await page.textContent("#preact")].flatMap((v) => v.split(",")).map(Number).sort();
  if (calls.join(",") !== "1,2,3,4") failures.push("want the four next() calls to count 1 to 4 on one counter, got " + calls.join(","));
  if (errors.length) failures.push("console errors: " + errors.join("; "));
} finally {
  await browser.close();
}
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log("ok");
