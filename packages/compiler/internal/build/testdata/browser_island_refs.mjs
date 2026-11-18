// A Preact island whose Radix triggers (asChild) wrap a React 19 style
// function component without forwardRef must open, and a plain useRef on
// that component must reach the DOM element.
import { chromium, firefox, webkit } from "playwright";
import { waitHydrated } from "./island_helpers.mjs";

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
  await page.goto(base + "/");
  try {
    await waitHydrated(page, "#ref-kind");
  } catch (e) {
    failures.push("the island did not hydrate");
  }
  try {
    await page.waitForFunction(() => document.querySelector("#ref-kind")?.textContent === "button", null, { timeout: 5000 });
  } catch (e) {
    failures.push("useRef on the function component did not get an HTMLButtonElement: " + (await page.textContent("#ref-kind")));
  }
  try {
    await page.hover("#tip-trigger");
    await page.waitForSelector("[role=tooltip]", { state: "attached", timeout: 5000 });
  } catch (e) {
    failures.push("the tooltip did not open on hover");
  }
  await page.keyboard.press("Escape");
  await page.mouse.move(0, 0, { steps: 8 });
  for (const [trigger, role] of [["#menu-trigger", "menu"], ["#dialog-trigger", "dialog"]]) {
    try {
      await page.click(trigger);
      await page.waitForSelector("[role=" + role + "]", { state: "visible", timeout: 5000 });
    } catch (e) {
      failures.push(trigger + " did not open a " + role);
    }
    await page.keyboard.press("Escape");
    await page.waitForTimeout(300);
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
