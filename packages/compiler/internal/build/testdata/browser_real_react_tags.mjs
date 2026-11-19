// A real-React island uses <Link> and <Image> without imports (see
// TestBrowserRealReactIslandTags).
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
  const go = await page.getAttribute("#go", "data-krate-link");
  if (go === null) failures.push("the island Link has no data-krate-link");
  if ((await page.getAttribute("#go", "data-prefetch")) === null) failures.push("the island Link has no data-prefetch");
  if ((await page.getAttribute("#go", "class")) !== "nav") failures.push("className did not reach the anchor");
  if ((await page.getAttribute("#ext", "data-krate-external")) === null) failures.push("an external Link lacks data-krate-external");
  if ((await page.getAttribute("#ext", "data-krate-link")) !== null) failures.push("an external Link must not navigate on the client");
  if ((await page.getAttribute("#pic", "src")) !== "/a.png") failures.push("Image did not render an img");
  await page.evaluate(() => {
    window.__marker = "alive";
  });
  await page.click("#go");
  try {
    await page.waitForSelector("#other", { timeout: 15000 });
  } catch (e) {
    failures.push("the click did not reach /other: " + page.url());
  }
  if (!/\/other\/?$/.test(page.url())) failures.push("url is " + page.url());
  if ((await page.evaluate(() => window.__marker)) !== "alive") failures.push("the click reloaded the page");
  if (errors.length) failures.push("console errors: " + errors.join("; "));
} finally {
  await browser.close();
}
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log("ok");
