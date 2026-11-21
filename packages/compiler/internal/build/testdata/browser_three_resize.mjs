// Resizes the three canvases of TestUseClientIslandBrowserThreeResize from
// timer tasks (as pointermove does while a divider is dragged) and checks the
// renderer events the page logs.
import { chromium, firefox, webkit } from "playwright";

const browserType = { chromium, firefox, webkit }[process.argv[2] || "chromium"];
const base = process.argv[3];
const browser = await browserType.launch({ headless: true });
const failures = [];
try {
  const page = await browser.newPage({ viewport: { width: 1200, height: 900 } });
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto(base + "/");
  if (!(await page.evaluate(() => !!document.createElement("canvas").getContext("webgl2")))) {
    console.log("NO_WEBGL");
    process.exit(0);
  }
  const loops = ["always", "demand", "never"];
  await page.waitForFunction((n) => Object.keys(window.__events || {}).length === n, loops.length, { timeout: 15000 });
  const result = await page.evaluate(async (loops) => {
    const boxes = loops.map((l) => document.getElementById("box-" + l));
    for (const l of loops) window.__events[l].length = 0;
    await new Promise((done) => {
      let i = 0;
      const t = setInterval(() => {
        boxes.forEach((b, k) => { b.style.width = 260 + ((i * 7 + k * 13) % 120) + "px"; });
        if (++i === 120) { clearInterval(t); done(); }
      }, 8);
    });
    await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(() => setTimeout(r, 100))));
    return Object.fromEntries(loops.map((l, k) => {
      const log = window.__events[l], canvas = boxes[k].querySelector("canvas");
      const gl = canvas.getContext("webgl2") || canvas.getContext("webgl");
      return [l, {
        resizes: log.filter((e) => e === "S").length,
        renders: log.filter((e) => e === "R").length,
        violations: window.__violations[l],
        endsWithRender: log[log.length - 1] === "R",
        buffer: gl ? gl.drawingBufferWidth : -1,
        width: canvas.width,
      }];
    }));
  }, loops);
  console.log(JSON.stringify(result));
  for (const [l, r] of Object.entries(result)) {
    if (r.resizes < 10) failures.push(l + ": the canvas was resized only " + r.resizes + " times");
    if (r.violations) failures.push(l + ": " + r.violations + " frames would show a canvas cleared by a resize");
    if (!r.endsWithRender) failures.push(l + ": the last resize was not followed by a render");
    if (r.buffer !== r.width) failures.push(l + ": drawing buffer " + r.buffer + " != canvas width " + r.width);
  }
  if (errors.length) failures.push(errors.join("\n"));
} finally {
  await browser.close();
}
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
