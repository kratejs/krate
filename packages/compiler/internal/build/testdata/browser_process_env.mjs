// Unset process.env reads in Preact and real-React islands (see
// TestBrowserClientProcessEnv).
import { chromium, firefox, webkit } from "playwright";
import { waitHydrated } from "./island_helpers.mjs";

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
  await waitHydrated(page, "#cv");
  await waitHydrated(page, "#plain");
  const real = await page.textContent("#real");
  const plain = await page.textContent("#plain");
  if (real !== "x-fallback|foo-fallback|dyn-fallback|yval|lib-fallback|no-process") failures.push("real-React island read: " + real);
  if (plain !== "p1|lib-fallback|yval") failures.push("Preact island read: " + plain);
  if (errors.length) failures.push("console errors: " + errors.join("; "));
} finally {
  await browser.close();
}
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log("ok");
