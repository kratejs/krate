// Drives the @krate/three fixture built by TestBrowserThree: the scene mounts
// in a real WebGL context, renders, raycasts clicks, and follows page state.
import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { chromium, firefox, webkit } from "playwright";

const root = process.argv[2];
const browserType = { chromium, firefox, webkit }[process.argv[3] || "chromium"];
const mime = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css" };

const server = http.createServer((req, res) => {
  let p = decodeURIComponent(req.url.split("?")[0]);
  if (p.endsWith("/")) p += "index.html";
  else if (!path.extname(p)) p += "/index.html";
  fs.readFile(path.join(root, p), (err, body) => {
    if (err) {
      res.statusCode = 404;
      res.end("not found");
      return;
    }
    res.setHeader("content-type", mime[path.extname(p)] || "application/octet-stream");
    res.end(body);
  });
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const base = `http://127.0.0.1:${server.address().port}`;
const browser = await browserType.launch({ headless: true, args: ["--use-angle=swiftshader", "--enable-unsafe-swiftshader"] });
const failures = [];

async function run(name, url, fn) {
  const page = await browser.newPage({ viewport: { width: 800, height: 600 } });
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => {
    if (m.type() === "error") errors.push(m.text());
  });
  try {
    await page.goto(`${base}${url}`, { waitUntil: "load" });
    await page.waitForFunction(() => window.__three && window.__three.scene, null, { timeout: 10000 });
    const out = await fn(page);
    if (out !== true) failures.push(`${name}: ${out}`);
    if (errors.length) failures.push(`${name}: page errors: ${errors.join(" | ")}`);
  } catch (e) {
    const scene = await page
      .evaluate(() => window.__three?.scene?.children.map((c) => c.name).join() ?? "no scene")
      .catch(() => "unavailable");
    failures.push(`${name}: ${String(e).slice(0, 400)} (scene: ${scene}; page errors: ${errors.join(" | ") || "none"})`);
  }
  await page.close();
}

const names = (p) => p.evaluate(() => window.__three.scene.children.map((c) => c.name).filter(Boolean));
const until = (p, fn, arg) => p.waitForFunction(fn, arg, { timeout: 5000 });

try {
  await run("react-style scene", "/", async (p) => {
    // The scene is not in the server HTML; the fallback is.
    const html = await (await fetch(`${base}/`)).text();
    if (/<mesh|boxGeometry/i.test(html)) return "scene elements leaked into the HTML";
    if (!html.includes("WebGL is not available")) return "canvas fallback missing from the HTML";
    if (!html.includes("<canvas")) return "canvas missing from the HTML";

    let n = await names(p);
    if (n.join() !== "center,extra,item-1,item-2") return `initial scene ${n.join()}`;
    await until(p, () => window.__frames > 2);
    const rendered = await p.evaluate(() => window.__three.gl.info.render.triangles);
    if (!(rendered > 0)) return `nothing rendered (${rendered} triangles)`;
    if ((await p.evaluate(() => window.__width)) !== 400) return `useThree size ${await p.evaluate(() => window.__width)}`;
    const rotated = await p.evaluate(() => window.__three.scene.getObjectByName("center").rotation.y);
    if (!(rotated > 0)) return "useFrame did not rotate the box";

    // A click at the canvas center hits the box.
    const box = await p.locator("#scene canvas").boundingBox();
    await p.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
    await until(p, () => window.__clicks === 1);
    await until(p, () => window.__three.scene.getObjectByName("center").scale.x === 1.5);

    // Hover recolors through component state.
    await p.mouse.move(box.x + box.width / 2 + 1, box.y + box.height / 2 + 1);
    await until(p, () => window.__three.scene.getObjectByName("center").material.color.getHexString() === "ff69b4");
    await p.mouse.move(box.x + 2, box.y + 2);
    await until(p, () => window.__three.scene.getObjectByName("center").material.color.getHexString() === "0000ff");

    // Page state outside the canvas reaches props inside it.
    const material = await p.evaluate(() => window.__three.scene.getObjectByName("center").material.uuid);
    await p.click("#inc");
    await until(p, () => window.__three.scene.getObjectByName("center").material.color.getHexString() === "ff0000");
    if ((await p.evaluate(() => window.__three.scene.getObjectByName("center").material.uuid)) !== material) {
      return "a prop change recreated the material";
    }

    await p.click("#toggle");
    await until(p, () => !window.__three.scene.getObjectByName("extra"));
    await p.click("#toggle");
    await until(p, () => !!window.__three.scene.getObjectByName("extra"));
    await p.click("#add");
    await until(p, () => !!window.__three.scene.getObjectByName("item-3"));
    n = await names(p);
    if (n.join() !== "center,extra,item-1,item-2,item-3") return `scene after updates ${n.join()}`;
    return true;
  });

  await run("krate-style scene", "/native/", async (p) => {
    const n = await names(p);
    if (n.join() !== "mover") return `scene ${n.join()}`;
    const camera = await p.evaluate(() => [window.__three.camera.position.z, window.__three.camera.fov]);
    if (camera.join() !== "6,50") return `camera ${camera.join()}`;
    await until(p, () => !!window.__camera);
    await p.click("#move");
    await until(p, () => window.__three.scene.getObjectByName("mover").position.x === 1);
    const spinner = await p.evaluate(() => window.__three.scene.getObjectByName("spinner").geometry.type);
    if (spinner !== "TorusGeometry") return `spinner geometry ${spinner}`;
    return true;
  });
  await run("krate signals and create primitives", "/signals/", async (p) => {
    await until(p, () => !!window.__three.scene.getObjectByName("k-loaded:loaded-text"));
    if (await p.evaluate(() => !!window.__three.scene.getObjectByName("k-loading"))) return "Suspense fallback still mounted";
    if (!(await p.evaluate(() => !!window.__three.scene.getObjectByName("k-graph:body+wheel")))) return "createGraph nodes missing";
    // createThree: the spinner scales with the canvas width (300px).
    const scale = await p.evaluate(() => window.__three.scene.getObjectByName("k-spinner").scale.x);
    if (scale !== 1) return `createThree width scale ${scale}`;
    // createFrame runs every frame, and a signal it writes updates the material.
    await until(p, () => window.__spins > 3);
    await until(p, () => window.__three.scene.getObjectByName("k-spinner").material.color.getHexString() === "ffa500");
    // Signals from outside the canvas drive a prop, a conditional and a list.
    await p.click("#k-move");
    await until(p, () => window.__three.scene.getObjectByName("k-mover").position.x === 1);
    await p.click("#k-toggle");
    await until(p, () => !window.__three.scene.getObjectByName("k-extra"));
    await p.click("#k-add");
    await until(p, () => !!window.__three.scene.getObjectByName("k-item-c"));
    // createLoader follows an accessor input: a new URL loads the new file.
    await p.click("#k-file");
    await until(p, () => !!window.__three.scene.getObjectByName("k-loaded:other-text"));
    // A prop read inside createFrame is current: speed x10 spins faster.
    const before = await p.evaluate(() => window.__three.scene.getObjectByName("k-spinner").rotation.y);
    await p.click("#k-fast");
    await until(p, (b) => window.__three.scene.getObjectByName("k-spinner").rotation.y > b + 1, before);
    // The stop function returned by createFrame unsubscribes the callback.
    await p.evaluate(() => window.__stopSpinner());
    const stopped = await p.evaluate(() => window.__spins);
    await p.waitForTimeout(200);
    if ((await p.evaluate(() => window.__spins)) !== stopped) return "createFrame kept running after stop()";
    return true;
  });
  await run("extras", "/extras/", async (p) => {
    await until(p, () => !!window.__three.scene.getObjectByName("loaded:loaded-text"));
    if (await p.evaluate(() => !!window.__three.scene.getObjectByName("loading"))) return "Suspense fallback still mounted";
    const pos = await p.evaluate(() => window.__three.scene.getObjectByName("spread").position.toArray().join());
    if (pos !== "1,2,3") return `spread props position ${pos}`;
    const controls = await p.evaluate(() => !!window.__controls && window.__controls.object === window.__three.camera);
    if (!controls) return "extended OrbitControls did not get the camera";
    const portal = await p.evaluate(() => window.__portal.children.map((c) => c.name).join());
    if (portal !== "in-portal") return `portal children ${portal}`;
    // Context from outside the canvas reaches the scene and stays live.
    await until(p, () => !!window.__three.scene.getObjectByName("theme:red"));
    await p.click("#theme");
    await until(p, () => !!window.__three.scene.getObjectByName("theme:blue"));
    return true;
  });
} finally {
  await browser.close();
  server.close();
}

if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log("three browser checks passed");
