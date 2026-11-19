// A page with islands only (no page script): an island's router.replace()
// must be a soft navigation through the page's router, keeping the document
// (a marker on window survives) and the focused input. Prints JSON last.
import { chromium, firefox, webkit } from "playwright";
import { waitHydrated } from "./island_helpers.mjs";

const [base, browserName = "chromium"] = process.argv.slice(2);
const browser = await { chromium, firefox, webkit }[browserName].launch();
const errors = [];
try {
  const page = await browser.newPage();
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => {
    if (m.type() === "error") errors.push(m.text());
  });
  let documents = 0;
  page.on("request", (r) => {
    if (r.resourceType() === "document") documents++;
  });
  await page.goto(base + "/templates/?q=cv");
  await waitHydrated(page);
  // The router starts after the document's scripts ran, with no page script.
  await page.waitForFunction(() => !!globalThis[Symbol.for("krate.router")], null, { timeout: 5000 }).catch(() => {});
  const started = await page.evaluate(() => !!globalThis[Symbol.for("krate.router")]);
  await page.evaluate(() => {
    window.__marker = "kept";
    window.__navStarts = 0;
    window.addEventListener("krate:navigate-start", () => window.__navStarts++);
  });
  await page.focus("#search");
  await page.keyboard.press("Escape");
  await page.waitForFunction(() => location.search === "", null, { timeout: 5000 }).catch(() => {});
  await page.waitForTimeout(300);
  const after = await page.evaluate(() => ({
    marker: window.__marker || "",
    navStarts: window.__navStarts || 0,
    search: location.search,
    focused: document.activeElement ? document.activeElement.id : "",
  }));
  // A Link on the page (data-krate-link) is intercepted too.
  await page.click("#to-store");
  await page.waitForSelector("#store-page", { timeout: 5000 });
  const linkMarker = await page.evaluate(() => window.__marker || "");
  console.log(JSON.stringify({ started, documents, ...after, linkMarker, errors }));
} finally {
  await browser.close();
}
