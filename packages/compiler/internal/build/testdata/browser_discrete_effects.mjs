// Clicks two buttons in quick succession (separate tasks, before the next
// frame) and checks that a react-colorful style controlled widget never calls
// onChange for changes of its own color prop (see TestBrowserDiscreteEventEffects).
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
  await waitHydrated(page, "#b1");
  const result = await page.evaluate(async () => {
    const tick = () => new Promise((r) => setTimeout(r, 0));
    const frames = () => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(() => setTimeout(r, 50))));
    for (let i = 0; i < 10; i++) {
      document.getElementById("b1").click();
      await tick();
      document.getElementById("b2").click();
      await frames();
      document.getElementById("reset").click();
      await frames();
    }
    return { calls: window.__onChange || [], shown: document.getElementById("shown").textContent };
  });
  console.log(JSON.stringify(result));
  if (result.calls.length > 0) {
    console.error("onChange was called for prop-driven changes: " + JSON.stringify(result.calls));
    failed = true;
  }
  if (errors.length) {
    console.error(errors.join("\n"));
    failed = true;
  }
} finally {
  await browser.close();
}
process.exit(failed ? 1 : 0);
