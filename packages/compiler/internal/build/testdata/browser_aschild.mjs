// A Radix DropdownMenu trigger with asChild clones a server-rendered Button
// (see TestBrowserAsChildServerChild): clicking it opens the menu, and the
// client Avatar inside it hydrates in the same island. A top-level asChild
// client component merges its click handler into a server Button.
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
  page.on("response", (r) => {
    if (r.status() >= 400) errors.push("HTTP " + r.status() + " " + r.url());
  });
  page.on("requestfailed", (r) => errors.push("request failed: " + r.url()));
  await page.goto(base + "/");
  try {
    await waitHydrated(page, "#avatar[data-ready=yes]");
  } catch (e) {
    failures.push("the Avatar did not hydrate inside the menu island");
  }
  const info = await page.evaluate(() => {
    const b = document.querySelector("#account");
    return {
      tag: b?.tagName,
      popup: b?.getAttribute("aria-haspopup"),
      label: b?.getAttribute("aria-label"),
      slot: b?.getAttribute("data-slot"),
      nested: b ? b.querySelectorAll("krate-island").length + document.evaluate(".//comment()[starts-with(., 'krate-island:')]", b, null, 7, null).snapshotLength : -1,
      avatar: !!b?.querySelector("#avatar"),
    };
  });
  if (info.tag !== "BUTTON") failures.push("the trigger is not the Button: " + JSON.stringify(info));
  if (info.popup !== "menu") failures.push("the trigger props did not reach the Button: " + JSON.stringify(info));
  if (info.label !== "Account menu") failures.push("the Button lost its own props: " + JSON.stringify(info));
  if (info.nested !== 0) failures.push("the trigger holds a nested island: " + JSON.stringify(info));
  if (!info.avatar) failures.push("the Avatar is not inside the Button: " + JSON.stringify(info));
  await page.click("#account");
  try {
    await page.waitForSelector("[role=menu]", { state: "visible", timeout: 5000 });
    await page.waitForSelector("#item-profile", { state: "visible", timeout: 5000 });
  } catch (e) {
    failures.push("clicking the account button did not open the menu");
  }
  const expanded = await page.getAttribute("#account", "aria-expanded");
  if (expanded !== "true") failures.push("aria-expanded after click: " + expanded);
  await page.keyboard.press("Escape");
  await page.click("#press");
  try {
    await page.waitForSelector("#press[data-pressed='1']", { timeout: 5000 });
  } catch (e) {
    failures.push("the top-level asChild handler did not reach the Button: " + (await page.getAttribute("#press", "data-pressed")));
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
