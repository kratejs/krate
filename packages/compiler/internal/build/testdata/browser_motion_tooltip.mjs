// Drives the motion + Radix tooltip sidebar (see
// TestUseClientIslandBrowserMotionTooltip): collapse it to the rail, open an
// item's tooltip, and while it is open expand the sidebar (the toggle) or pick
// the hovered item (which also moves the layoutId pill). Every label must
// animate back to opacity 1.
import { chromium, firefox, webkit } from "playwright";
import { waitHydrated } from "./island_helpers.mjs";

const browserType = { chromium, firefox, webkit }[process.argv[2] || "chromium"];
const base = process.argv[3];
const browser = await browserType.launch({ headless: true });
const failures = [];
const labels = ["customer", "designer", "dashboard", "orders"];
try {
  for (const scenario of ["no tooltip", "expand with a tooltip open", "pick the hovered item"]) {
    const page = await (await browser.newContext({ viewport: { width: 1200, height: 800 } })).newPage();
    const errors = [];
    page.on("pageerror", (e) => errors.push("pageerror: " + e.message));
    await page.goto(base + "/");
    await waitHydrated(page, "#toggle");
    const opacities = () => page.evaluate((ids) => ids.map((id) => id + "=" + getComputedStyle(document.getElementById("label-" + id)).opacity), labels);
    // The features load lazily; wait for them before animating.
    await page.waitForFunction(() => !!document.querySelector("[data-pill]") && performance.now() > 300);
    await page.click("#toggle");
    await page.waitForFunction(() => getComputedStyle(document.getElementById("label-orders")).opacity === "0", null, { timeout: 5000 });
    if (scenario !== "no tooltip") {
      await page.hover("#item-designer");
      await page.waitForSelector("#item-designer[data-state=delayed-open]", { timeout: 5000 });
    }
    // Without moving the pointer, so the tooltip is still open (or closing)
    // while the sidebar re-renders.
    await page.evaluate((s) => document.getElementById(s === "pick the hovered item" ? "item-designer" : "toggle").click(), scenario);
    const ok = await page
      .waitForFunction((ids) => ids.every((id) => getComputedStyle(document.getElementById("label-" + id)).opacity === "1"), labels, { timeout: 5000 })
      .then(() => true, () => false);
    if (!ok) failures.push(scenario + ": labels did not come back: " + (await opacities()).join(" "));
    if (scenario === "pick the hovered item") {
      const pill = await page.evaluate(() => document.querySelector("[data-pill]")?.closest("a")?.id);
      if (pill !== "item-designer") failures.push(scenario + ": the workspace pill is in " + pill);
    }
    if (errors.length) failures.push(scenario + ": " + errors.join("\n"));
    console.log(scenario + ": " + (await opacities()).join(" "));
    await page.context().close();
  }
} finally {
  await browser.close();
}
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
