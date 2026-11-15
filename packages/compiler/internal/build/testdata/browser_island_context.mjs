// Client components nested in the server children of a layout's client
// provider see its context (see TestBrowserIslandContextFromLayoutProvider): a
// Preact and a real-React island both open a Radix Tooltip under the layout's
// TooltipProvider and follow a counter context the provider owns.
import { chromium, firefox, webkit } from "playwright";

const browserType = { chromium, firefox, webkit }[process.argv[2] || "chromium"];
const base = process.argv[3];
const browser = await browserType.launch({ headless: true });
const failures = [];
try {
  const page = await (await browser.newContext()).newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push("pageerror: " + e.message));
  page.on("console", (m) => {
    if (m.type() === "error") errors.push("console: " + m.text());
  });
  page.on("response", (r) => {
    if (r.status() >= 400) errors.push("HTTP " + r.status() + " " + r.url());
  });
  page.on("requestfailed", (r) => errors.push("request failed: " + r.url()));
  await page.goto(base + "/");
  for (const id of ["#preact-n", "#real-n"]) {
    try {
      await page.waitForSelector("[data-krate-island-hydrated] " + id, { timeout: 15000 });
    } catch (e) {
      failures.push(id + " did not hydrate");
    }
  }
  for (const id of ["preact", "real"]) {
    try {
      await page.hover("#" + id + "-trigger");
      await page.waitForSelector("#" + id + "-content", { state: "visible", timeout: 5000 });
    } catch (e) {
      failures.push("the " + id + " tooltip did not open on hover");
    }
    // Dismiss it and wait out Radix's skip-delay window (as a user would), so
    // the next tooltip opens on its own.
    await page.keyboard.press("Escape");
    await page.mouse.move(0, 0, { steps: 8 });
    await page.waitForTimeout(600);
  }
  const counts = () => page.evaluate(() => [
    document.querySelector("#bump")?.textContent,
    document.querySelector("#preact-n")?.textContent,
    document.querySelector("#real-n")?.textContent,
  ]);
  const before = await counts();
  if (before.join("|") !== "bump 0|0|0") failures.push("initial context values: " + before.join("|"));
  await page.click("#bump");
  try {
    await page.waitForFunction(
      () => document.querySelector("#preact-n")?.textContent === "1" && document.querySelector("#real-n")?.textContent === "1",
      null,
      { timeout: 5000 },
    );
  } catch (e) {
    failures.push("the context update did not reach the nested islands: " + (await counts()).join("|"));
  }
  if (errors.length) failures.push(errors.join("\n"));
} finally {
  await browser.close();
}
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log("ok");
