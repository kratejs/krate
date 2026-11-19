// next/dynamic loaders that resolve to a named export or a bare component, in
// a Preact island and a real-React island (see TestBrowserMigratedDynamicLoaders).
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
  for (const kind of ["real", "preact"]) {
    for (const [id, text] of [["named", "NAMED"], ["bare", "BARE"], ["default", "DEFAULT"]]) {
      const sel = `#${id}-${kind}`;
      try {
        await page.waitForSelector(sel, { timeout: 10000 });
        const got = await page.textContent(sel);
        if (got !== text) failures.push(`${sel}: ${got}`);
      } catch {
        failures.push(`${sel} never rendered`);
      }
    }
  }
  if (errors.length) failures.push("console errors: " + errors.join("; "));
} finally {
  await browser.close();
}
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log("ok");
