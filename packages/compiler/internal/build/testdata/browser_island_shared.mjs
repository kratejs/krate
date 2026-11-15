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
  const seen = [await page.textContent("#real"), await page.textContent("#preact")].sort();
  if (seen.join(" ") !== "1,1,1,1 2,2,2,2") failures.push("want the islands to count 1 and 2 on each shared module, got " + seen.join(" "));
  for (const id of ["real-extra", "preact-extra"]) {
    const text = await page.textContent("#" + id);
    if (text !== '["x"]|kind-shadow,bevel|1|2') failures.push(id + ": want A = [\"x\"] and the cycle evaluated in order, got " + text);
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
