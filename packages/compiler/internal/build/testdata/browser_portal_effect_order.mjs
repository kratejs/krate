// Clicks the buttons of the portal effect-order page and checks that every
// passive effect saw the value its component's insertion effect was handed
// (see TestUseClientIslandBrowserPortalEffectOrder).
import { chromium, firefox, webkit } from "playwright";
import { waitHydrated } from "./island_helpers.mjs";

const browserType = { chromium, firefox, webkit }[process.argv[2] || "chromium"];
const base = process.argv[3];
const browser = await browserType.launch({ headless: true });
let failed = false;
try {
  const page = await (await browser.newContext()).newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push("pageerror: " + e.message));
  await page.goto(base + "/");
  await waitHydrated(page, "#click");
  await page.waitForSelector("#in-portal");
  const settle = () => page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(() => setTimeout(r, 50)))));
  await settle();
  await page.evaluate(() => { window.__seen = []; });
  for (let i = 0; i < 3; i++) {
    await page.click("#click");
    await settle();
    await page.click("#later");
    await page.waitForFunction((n) => document.getElementById("before").textContent === String(n), 2 * i + 2);
    await settle();
  }
  const seen = await page.evaluate(() => window.__seen);
  console.log(JSON.stringify(seen));
  const stale = seen.filter((s) => { const [, v, handed] = s.match(/:(\d+)\/(\d+)$/); return v !== handed; });
  if (stale.length) {
    console.error("passive effects ran before their insertion effects: " + stale.join(", "));
    failed = true;
  }
  for (const name of ["before", "after"]) {
    for (let v = 1; v <= 6; v++) {
      if (!seen.includes(name + ":" + v + "/" + v)) {
        console.error("no passive effect of " + name + " saw " + v + ": " + JSON.stringify(seen));
        failed = true;
      }
    }
  }
  if (errors.length) {
    console.error(errors.join("\n"));
    failed = true;
  }
} finally {
  await browser.close();
}
process.exit(failed ? 1 : 0);
