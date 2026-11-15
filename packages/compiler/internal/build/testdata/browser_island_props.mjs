// An island nested two server components below a layout's client provider
// (see TestIslandPropsShipOnce) hydrates, reads the provider's context, and
// keeps working when the provider re-renders and hides then shows its
// server children: the provider adopts the server-rendered nodes in place.
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
  await page.goto(base + "/");
  try {
    await page.waitForSelector("[data-krate-island-hydrated] [data-krate-island-hydrated] #blob", { timeout: 15000 });
  } catch (e) {
    failures.push("the nested island did not hydrate");
  }
  const text = () => page.evaluate(() => [
    document.querySelector("#prov-n")?.textContent,
    document.querySelector("#blob")?.textContent,
    document.querySelectorAll("#blob").length,
    document.querySelectorAll("#inner").length,
    document.querySelector("#menu-link")?.textContent,
  ].join("|"));
  const want = (n) => `prov ${n}|blob 50000 ${n}|1|1|menu item`;
  if ((await text()) !== want(0)) failures.push("initial: " + (await text()));
  await page.click("#blob");
  try {
    await page.waitForFunction((w) => [
      document.querySelector("#prov-n")?.textContent,
      document.querySelector("#blob")?.textContent,
      document.querySelectorAll("#blob").length,
      document.querySelectorAll("#inner").length,
      document.querySelector("#menu-link")?.textContent,
    ].join("|") === w, want(1), { timeout: 5000 });
  } catch (e) {
    failures.push("after a click in the nested island: " + (await text()));
  }
  await page.click("#toggle");
  await page.waitForSelector("#hidden", { timeout: 5000 }).catch(() => failures.push("the provider did not hide its children"));
  await page.click("#toggle");
  try {
    await page.waitForSelector("#page #inner #blob", { timeout: 5000 });
  } catch (e) {
    failures.push("the server children did not come back after hide/show: " + (await text()));
  }
  await page.click("#blob");
  try {
    await page.waitForFunction((w) => [
      document.querySelector("#prov-n")?.textContent,
      document.querySelector("#blob")?.textContent,
      document.querySelectorAll("#blob").length,
      document.querySelectorAll("#inner").length,
      document.querySelector("#menu-link")?.textContent,
    ].join("|") === w, want(2), { timeout: 5000 });
  } catch (e) {
    failures.push("after hide/show and a click: " + (await text()));
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
