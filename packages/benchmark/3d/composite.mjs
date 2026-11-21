// Builds side-by-side composites (filament | three-compiled | three-runtime)
// from the screenshots run.mjs wrote, using a canvas page in Playwright.
//   node composite.mjs SHOTS_DIR
import fs from "node:fs";
import path from "node:path";
import { chromium } from "playwright";

const dir = path.resolve(process.argv[2] || ".work/shots");
const names = ["filament", "three-compiled", "three-runtime"];
const labels = ["A: @krate/3d (Filament)", "B: @krate/three compiled", "C: @krate/three compile={false}"];
const browser = await chromium.launch({ headless: true });
try {
  for (const mode of ["static", "animated"]) {
    const imgs = names.map((n) => "data:image/png;base64," + fs.readFileSync(path.join(dir, `${n}-${mode}.png`)).toString("base64"));
    const page = await browser.newPage();
    const b64 = await page.evaluate(
      async ({ imgs, labels }) => {
        const W = 1280, H = 720, bar = 40;
        const c = document.createElement("canvas");
        c.width = W * imgs.length;
        c.height = H + bar;
        const g = c.getContext("2d");
        g.fillStyle = "#111";
        g.fillRect(0, 0, c.width, c.height);
        g.font = "bold 22px sans-serif";
        g.fillStyle = "#fff";
        for (let i = 0; i < imgs.length; i++) {
          const im = new Image();
          im.src = imgs[i];
          await im.decode();
          g.drawImage(im, i * W, bar);
          g.fillText(labels[i], i * W + 12, 28);
        }
        return c.toDataURL("image/png").split(",")[1];
      },
      { imgs, labels },
    );
    fs.writeFileSync(path.join(dir, `compare-${mode}.png`), Buffer.from(b64, "base64"));
    await page.close();
  }
} finally {
  await browser.close();
}
