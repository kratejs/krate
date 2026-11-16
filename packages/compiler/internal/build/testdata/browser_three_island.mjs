// Drives the @krate/three island fixture built by TestBrowserThreeIsland: a
// "use client" viewer on a server-rendered page runs @krate/three and
// @krate/three/drei on Preact, with no React runtime on the page.
import { chromium, firefox, webkit } from "playwright";

const browserType = { chromium, firefox, webkit }[process.argv[2] || "chromium"];
const base = process.argv[3];
const browser = await browserType.launch({ headless: true, args: ["--use-angle=swiftshader", "--enable-unsafe-swiftshader"] });
const failures = [];

async function run(name, url, fn) {
  const page = await browser.newPage({ viewport: { width: 800, height: 600 } });
  const errors = [];
  const scripts = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => {
    if (m.type() === "error" && !/missing\.glb|404/.test(m.text())) errors.push(m.text());
  });
  page.on("response", (r) => {
    if (r.request().resourceType() === "script") scripts.push(r.url());
  });
  try {
    await page.goto(`${base}${url}`, { waitUntil: "load" });
    const out = await fn(page, scripts);
    if (out !== true) failures.push(`${name}: ${out}`);
    if (errors.length) failures.push(`${name}: page errors: ${errors.join(" | ")}`);
  } catch (e) {
    failures.push(`${name}: ${String(e).slice(0, 400)} while waiting for ${step} (page errors: ${errors.join(" | ") || "none"})`);
  }
  await page.close();
}

let step = "";
const until = (p, fn, arg) => {
  step = fn.toString().slice(0, 120);
  return p.waitForFunction(fn, arg, { timeout: 10000 });
};
const state = (p) => p.evaluate(() => {
  const s = window.__three.get();
  const box = s.scene.getObjectByName("box");
  return { controls: !!s.controls, min: s.controls?.minDistance, frameloop: s.frameloop, scale: box.scale.x, color: box.material.color.getHexString() };
});

try {
  await run("island scene", "/", async (p, scripts) => {
    // Only the canvas wrapper is server-rendered.
    const html = await (await fetch(`${base}/`)).text();
    if (!html.includes("<canvas") || !html.includes('aria-label="3D view"')) return "canvas wrapper missing from the server HTML";
    if (/<mesh|boxGeometry/i.test(html)) return "scene elements leaked into the HTML";

    await until(p, () => !!window.__three && !!window.__gltf && !!window.__three.get().controls);
    // A React package component renders in the same island.
    if (!(await p.locator(".badge").count())) return "the package component did not render";
    // No React runtime: the island runs on Preact.
    const react = scripts.filter((u) => /\/isr-|react-dom|react\.production/.test(u));
    if (react.length) return `React runtime loaded: ${react.join(", ")}`;

    // useGLTF: the model mounted after suspending; refs give three objects.
    const model = await p.evaluate(() => [Object.keys(window.__gltf.nodes).join(), window.__box?.type, window.__hasControls]);
    if (model.join() !== "body,Mesh,true") return `model and refs ${model.join()}`;
    // Environment from Lightformers, with drei's color and scale.
    const env = await p.evaluate(() => [window.__three.get().scene.environment?.isRenderTargetTexture, window.__envTop.material.color.r, window.__envTop.scale.toArray().join(":")]);
    if (env.join() !== "true,2.2,12:12:1") return `environment ${env.join()}`;

    // A click raycasts the box: its handler updates island state.
    const box = await p.locator("#scene canvas").boundingBox();
    const cx = box.x + box.width / 2, cy = box.y + box.height / 2;
    await p.mouse.click(cx, cy);
    await until(p, () => document.querySelector("#picked").textContent === "box");
    await until(p, () => window.__three.get().scene.getObjectByName("box").scale.x === 1.5);
    if ((await state(p)).color !== "ff69b4") return "the clicked box did not recolor";

    // Drag orbits and the wheel zooms (OrbitControls with makeDefault).
    const cam0 = await p.evaluate(() => window.__three.get().camera.position.toArray());
    await p.mouse.move(cx + 60, cy + 60);
    await p.mouse.down();
    await p.mouse.move(cx + 140, cy + 70, { steps: 5 });
    await p.mouse.up();
    await until(p, (c) => {
      const position = window.__three.get().camera.position;
      return position.distanceTo(position.clone().set(...c)) > 0.5;
    }, cam0);
    const dist = await p.evaluate(() => window.__three.get().camera.position.length());
    await p.mouse.move(cx, cy);
    await p.mouse.wheel(0, -300);
    await until(p, (d) => window.__three.get().camera.position.length() < d - 0.05, dist);

    // Island state reaches drei props, and pausing stops the frame loop.
    await p.click("#near");
    await until(p, () => window.__three.get().controls.minDistance === 2);
    await p.click("#pause");
    await until(p, () => window.__three.get().frameloop === "never");
    // A frame already scheduled may still land; then nothing renders.
    await p.waitForTimeout(400);
    const frames = await p.evaluate(() => window.__frames);
    await p.mouse.move(cx + 10, cy + 10);
    await p.mouse.wheel(0, -100);
    await p.waitForTimeout(400);
    if ((await p.evaluate(() => window.__frames)) !== frames) return "frames rendered while paused";
    await p.click("#pause");
    await until(p, () => window.__three.get().frameloop === "demand");
    return true;
  });

  await run("lazy viewer without a Suspense boundary", "/lazy/", async (p) => {
    await until(p, () => !!window.__three && !!window.__gltf);
    if (await p.locator("#viewer-loading").count()) return "the loading placeholder is still shown";
    return true;
  });

  await run("scene error reaches the boundary", "/broken/", async (p) => {
    await until(p, () => !!document.querySelector("#failed"));
    return true;
  });
} finally {
  await browser.close();
}

if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log("three island browser checks passed");
