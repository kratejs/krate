// A Preact island and a real-React island validate with a tree-shaken zod
// (see TestBrowserIslandZod).
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
    await page.waitForSelector("#cv", { timeout: 15000 });
    await page.waitForFunction(
      () => ["real", "preact"].every((id) => (document.getElementById(id)?.textContent ?? "0") !== "0"),
      null,
      { timeout: 15000 },
    );
  } catch {
    throw new Error("the islands did not hydrate; errors: " + errors.join("; "));
  }
  for (const id of ["real", "preact"]) {
    const text = await page.textContent("#" + id);
    if (text !== "ok|false") failures.push(id + ": want zod to accept 'ok' and reject 7, got " + text);
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
