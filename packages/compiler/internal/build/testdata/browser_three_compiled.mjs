// Drives the pages built by TestBrowserThreeCompiled: "/" (the compile-time
// optimizer on) and "/off/" (<Canvas compile={false}>) in a real WebGL context.
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
const until = (p, fn, arg) => p.waitForFunction(fn, arg, { timeout: 8000 });

// Helpers installed in the page: a color histogram of the rendered canvas and
// the world positions of the boxes of a given size, from either path.
const installHelpers = () => {
  window.__hist = () => {
    const { gl, scene, camera } = window.__three;
    gl.render(scene, camera);
    const src = gl.domElement;
    const c = document.createElement("canvas");
    c.width = src.width;
    c.height = src.height;
    const ctx = c.getContext("2d");
    ctx.drawImage(src, 0, 0);
    const d = ctx.getImageData(0, 0, c.width, c.height).data;
    const bins = {};
    let lit = 0;
    for (let i = 0; i < d.length; i += 4) {
      if (d[i + 3] > 0) lit++;
      const k = (d[i] >> 5) + "," + (d[i + 1] >> 5) + "," + (d[i + 2] >> 5) + "," + (d[i + 3] >> 6);
      bins[k] = (bins[k] || 0) + 1;
    }
    return { bins, lit, total: d.length / 4 };
  };
  window.__boxes = (w, h, dd) => {
    const { scene } = window.__three;
    scene.updateMatrixWorld(true);
    const out = [];
    const near = (a, b) => Math.abs(a - b) < 1e-6;
    scene.traverse((o) => {
      const p = o.geometry?.parameters;
      if (o.geometry?.type !== "BoxGeometry" || !near(p.width, w) || !near(p.height, h) || !near(p.depth, dd)) return;
      if (o.isInstancedMesh) {
        for (let i = 0; i < o.count; i++) {
          const m = new o.matrixWorld.constructor().fromArray(o.instanceMatrix.array, i * 16).premultiply(o.matrixWorld);
          out.push(Array.from(m.elements));
        }
      } else out.push(Array.from(o.matrixWorld.elements));
    });
    const key = (m) => m.map((v) => Math.round(v * 1e4) / 1e4 + 0).join(",");
    return out.map(key).sort();
  };
  window.__screen = (name) => {
    const { scene, camera, gl } = window.__three;
    const v = scene.getObjectByName(name).getWorldPosition(new scene.position.constructor()).project(camera);
    return [((v.x + 1) / 2) * gl.domElement.clientWidth, ((1 - v.y) / 2) * gl.domElement.clientHeight];
  };
};

async function open(url, errors) {
  const page = await browser.newPage({ viewport: { width: 800, height: 600 } });
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => {
    if (m.type() === "error") errors.push(m.text());
  });
  await page.goto(`${base}${url}`, { waitUntil: "load" });
  try {
    await page.waitForFunction(() => window.__three && window.__three.scene, null, { timeout: 10000 });
    await page.evaluate(installHelpers);
    if (url !== "/react/") await until(page, () => window.__frames > 2);
  } catch (e) {
    throw new Error(`${url}: ${String(e).slice(0, 200)} (page errors: ${errors.join(" | ") || "none"})`);
  }
  return page;
}

const pages = {};
const results = {};
try {
  for (const [name, url] of [["on", "/"], ["off", "/off/"]]) {
    const errors = [];
    const p = await open(url, errors);
    pages[name] = p;
    const r = (results[name] = { errors });

    r.instanced = await p.evaluate(() => {
      const out = [];
      window.__three.scene.traverse((o) => o.isInstancedMesh && out.push(o.count));
      return out.sort((a, b) => a - b);
    });
    r.triangles = await p.evaluate(() => window.__three.gl.info.render.triangles);
    r.grid = await p.evaluate(() => window.__boxes(1, 1, 1));
    r.arm = await p.evaluate(() => {
      const o = window.__three.scene.getObjectByName("arm");
      return { world: Array.from(o.matrixWorld.elements), pos: o.position.toArray(), auto: o.matrixAutoUpdate, worldAuto: o.matrixWorldAutoUpdate };
    });
    r.inner = await p.evaluate(() => {
      const o = window.__three.scene.getObjectByName("inner");
      o.updateWorldMatrix(true, false);
      return { world: Array.from(o.matrixWorld.elements), pos: o.position.toArray(), auto: o.matrixAutoUpdate, worldAuto: o.matrixWorldAutoUpdate };
    });
    r.shared = await p.evaluate(() => {
      const s = window.__three.scene;
      const clickable = s.getObjectByName("clickable");
      let other = null;
      s.traverse((o) => {
        if (o !== clickable && o.geometry?.type === "SphereGeometry") other = o;
      });
      return { geometry: other.geometry === clickable.geometry, material: other.material === clickable.material };
    });
    r.hist0 = await p.evaluate(() => window.__hist());

    // A click on the mesh with a handler reaches it (compiled or not).
    const box = await p.locator("#scene canvas").boundingBox();
    const [sx, sy] = await p.evaluate(() => window.__screen("clickable"));
    await p.mouse.click(box.x + sx, box.y + sy);
    await until(p, () => window.__clicks === 1);

    // A signal drives a group rotation and the rotation of mapped meshes.
    await p.click("#spin");
    await until(p, () => window.__three.scene.getObjectByName("spinner").rotation.y === 0.5);
    r.ring = await p.evaluate(() => window.__boxes(0.3, 0.3, 0.3));
    r.arm1 = await p.evaluate(() => {
      const o = window.__three.scene.getObjectByName("arm");
      o.updateWorldMatrix(true, false);
      return Array.from(o.matrixWorld.elements);
    });
    r.hist1 = await p.evaluate(() => window.__hist());
    r.frames = await p.evaluate(() => window.__frames);
  }

  // React-style state reaches compiled reactive transforms and handlers.
  {
    const errors = [];
    const p = await open("/react/", errors).catch((e) => {
      failures.push(`react: ${e.message}`);
      return null;
    });
    if (p) {
      const rot = () => p.evaluate(() => window.__three.scene.getObjectByName("turner").rotation.y);
      if ((await rot()) !== 0) failures.push(`react: initial rotation ${await rot()}`);
      await p.click("#turn");
      await until(p, () => window.__three.scene.getObjectByName("turner").rotation.y === 0.5).catch(() => {});
      if ((await rot()) !== 0.5) failures.push(`react: rotation after the button ${await rot()}, want 0.5`);
      const box = await p.locator("#scene canvas").boundingBox();
      await p.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
      await until(p, () => window.__three.scene.getObjectByName("turner").rotation.y === 1.5).catch(() => {});
      if ((await rot()) !== 1.5) failures.push(`react: rotation after the mesh click ${await rot()}, want 1.5 (stale state in the handler?)`);
      if (errors.length) failures.push(`react: page errors: ${errors.join(" | ")}`);
    }
  }

  const on = results.on;
  const off = results.off;
  const f = (name, msg) => failures.push(`${name}: ${msg}`);
  for (const [name, r] of Object.entries(results)) if (r.errors.length) f(name, `page errors: ${r.errors.join(" | ")}`);

  if (on.instanced.join() !== "2,6,9") f("on", `InstancedMesh counts ${on.instanced.join()}, want 2,6,9`);
  if (off.instanced.length) f("off", `compile={false} still instances: ${off.instanced.join()}`);
  if (!(on.triangles > 0) || !(off.triangles > 0)) f("render", `triangles on=${on.triangles} off=${off.triangles}`);
  if (!on.shared.geometry || !on.shared.material) f("on", "the clickable sphere does not share geometry and material with the instanced spheres");
  if (off.shared.geometry || off.shared.material) f("off", "the runtime path shares geometry or material");
  if (on.inner.auto !== false || on.inner.worldAuto !== false) f("on", `static matrices not frozen: ${on.inner.auto}/${on.inner.worldAuto}`);
  if (off.inner.auto !== true || off.inner.worldAuto !== true) f("off", "compile={false} froze matrices");
  if (on.inner.pos.join() !== "0.5,0,0") f("on", `inner position ${on.inner.pos}`);
  if (on.arm.auto !== false || on.arm.worldAuto !== true) f("on", `arm under a reactive group: auto=${on.arm.auto} worldAuto=${on.arm.worldAuto}`);
  const close = (a, b) => a.length === b.length && a.every((v, i) => Math.abs(v - b[i]) < 1e-5);
  if (!close(on.inner.world, off.inner.world)) f("matrix", `inner world matrix differs: ${on.inner.world} vs ${off.inner.world}`);
  if (!close(on.arm.world, off.arm.world)) f("matrix", `arm world matrix differs`);
  if (on.grid.length !== 9 || on.grid.join("|") !== off.grid.join("|")) f("matrix", "the instanced grid does not match the reconciler's boxes");
  if (on.ring.length !== 6 || on.ring.join("|") !== off.ring.join("|")) f("matrix", `the reactive instances do not match the reconciler's meshes after the signal changed:\n${on.ring.join("\n")}\n${off.ring.join("\n")}`);
  if (!close(on.arm1, off.arm1)) f("matrix", "arm world matrix differs after the signal changed");
  for (const [name, r] of Object.entries(results)) if (!(r.frames > 2)) f(name, "useFrame did not run");

  // The two paths draw the same picture: compare the color histograms.
  const diff = (a, b) => {
    let d = 0;
    for (const k of new Set([...Object.keys(a.bins), ...Object.keys(b.bins)])) d += Math.abs((a.bins[k] || 0) - (b.bins[k] || 0));
    return d / a.total;
  };
  for (const h of ["hist0", "hist1"]) {
    if (!(on[h].lit / on[h].total > 0.05)) f(h, `the compiled page drew almost nothing (${on[h].lit} of ${on[h].total} pixels)`);
    const d = diff(on[h], off[h]);
    if (d > 0.03) f(h, `pictures differ: ${(d * 100).toFixed(2)}% of pixels fall in other color bins`);
  }

  // One draw call for the 9 instanced boxes: hide everything else and render.
  const calls = await pages.on.evaluate(() => {
    const { scene, camera, gl } = window.__three;
    let target = null;
    scene.traverse((o) => o.isInstancedMesh && o.count === 9 && (target = o));
    const keep = new Set();
    for (let o = target; o; o = o.parent) keep.add(o);
    const hidden = [];
    scene.traverse((o) => {
      if (!keep.has(o) && o.visible) {
        o.visible = false;
        hidden.push(o);
      }
    });
    gl.render(scene, camera);
    const n = gl.info.render.calls;
    for (const o of hidden) o.visible = true;
    return n;
  });
  if (calls !== 1) f("on", `9 instanced boxes took ${calls} draw calls, want 1`);
  const offCalls = await pages.off.evaluate(() => {
    const { scene, camera, gl } = window.__three;
    gl.render(scene, camera);
    return gl.info.render.calls;
  });
  const onCalls = await pages.on.evaluate(() => {
    const { scene, camera, gl } = window.__three;
    gl.render(scene, camera);
    return gl.info.render.calls;
  });
  if (!(onCalls < offCalls)) f("calls", `compiled scene made ${onCalls} draw calls, the reconciler's ${offCalls}`);
  console.log(`draw calls: compiled ${onCalls}, compile={false} ${offCalls}; instanced ${on.instanced.join("+")}`);
} catch (e) {
  failures.push(String(e).slice(0, 600));
}
await browser.close();
server.close();
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log("ok");
