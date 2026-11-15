// A Preact island and a real-React island share one module instance (see
// TestBrowserIslandModuleSharedAcrossFlavours).
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
  await page.waitForSelector("[data-krate-island-hydrated] #cv", { timeout: 15000 });
  await page.waitForSelector("[data-krate-island-hydrated] #preact", { timeout: 15000 });
  await page.waitForFunction(
    () => ["real", "preact"].every((id) => (document.getElementById(id)?.textContent ?? "0") !== "0"),
    null,
    { timeout: 15000 },
  );
  const seen = [await page.textContent("#real"), await page.textContent("#preact")].sort();
  if (seen.join(",") !== "1,2") failures.push("want the islands to count 1 and 2 on one counter, got " + seen.join(","));
  if (errors.length) failures.push("console errors: " + errors.join("; "));
} finally {
  await browser.close();
}
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log("ok");
