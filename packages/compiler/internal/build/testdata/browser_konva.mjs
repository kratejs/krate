// Drives the @krate/konva fixture built by TestBrowserKonva: the same
// react-konva-style editor compiled into a Krate page (/) and running as a
// Preact island on a server-rendered page (/island). Checks reactive props
// applied in place, click and hover hit-testing (listening, hitStrokeWidth,
// custom sceneFunc), drag with dragBoundFunc, the Transformer, double-click,
// keyed lists, conditionals, refs and context through the Stage, and that no
// React runtime loads.
import { chromium, firefox, webkit } from "playwright";

const browserType = { chromium, firefox, webkit }[process.argv[2] || "chromium"];
const base = process.argv[3];
const only = process.argv[4];
const browser = await browserType.launch({ headless: true });
const failures = [];
let step = "";

async function run(name, url, fn) {
  const page = await browser.newPage({ viewport: { width: 900, height: 700 } });
  const errors = [];
  const scripts = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => {
    if (m.type() === "error" || m.type() === "warning") errors.push(`${m.type()}: ${m.text()}`);
  });
  page.on("response", (r) => {
    if (r.request().resourceType() === "script") scripts.push(r.url());
  });
  try {
    await page.goto(`${base}${url}`, { waitUntil: "load" });
    const out = await fn(page, scripts);
    if (out !== true) failures.push(`${name}: ${out}`);
    if (errors.length) failures.push(`${name}: console: ${errors.join(" | ")}`);
  } catch (e) {
    const st = await page.locator("#status").textContent().catch(() => "");
    failures.push(`${name}: ${String(e).slice(0, 400)} while waiting for ${step} (status: ${st}; log: ${await page.evaluate(() => (window.__log || []).join(" ")).catch(() => "")}; console: ${errors.join(" | ") || "none"})`);
  }
  await page.close();
}

const until = (p, fn, arg) => {
  step = fn.toString().slice(0, 140) + (arg === undefined ? "" : ` (${JSON.stringify(arg)})`);
  return p.waitForFunction(fn, arg, { timeout: 10000 });
};
const status = (p) => p.locator("#status").textContent();
const waitStatus = (p, part) => until(p, (s) => document.querySelector("#status").textContent.includes(s), part);
// Page coordinates of a stage point.
async function at(p, x, y) {
  const box = await p.locator("#editor .konvajs-content").boundingBox();
  return [box.x + x, box.y + y];
}
async function click(p, x, y) {
  const [px, py] = await at(p, x, y);
  await p.mouse.click(px, py);
}
async function drag(p, from, to) {
  const [fx, fy] = await at(p, ...from);
  const [tx, ty] = await at(p, ...to);
  await p.mouse.move(fx, fy);
  await p.mouse.down();
  await p.mouse.move((fx + tx) / 2, (fy + ty) / 2, { steps: 4 });
  await p.mouse.move(tx, ty, { steps: 4 });
  await p.mouse.up();
}
// The RGB of a stage pixel on the layer canvas (pixel ratio aware).
const pixel = (p, x, y) =>
  p.evaluate(([x, y]) => {
    const layer = window.__stage.getLayers()[0];
    const canvas = layer.getCanvas();
    const r = canvas.getPixelRatio();
    const d = canvas.getContext()._context.getImageData(Math.round(x * r), Math.round(y * r), 1, 1).data;
    return [d[0], d[1], d[2]].join(",");
  }, [x, y]);
const logged = (p) => p.evaluate(() => (window.__log || []).join(" "));

async function editorChecks(p) {
  await until(p, () => !!window.__stage && window.__stage.findOne(".box") && window.__stage.findOne(".themed"));
  const shape = await p.evaluate(() => {
    const s = window.__stage;
    const box = s.findOne(".box");
    window.__box0 = box;
    window.__items0 = ["a", "b", "c"].map((id) => s.findOne(".item-" + id));
    const wire = s.findOne(".wire");
    return [
      s.getClassName(),
      s.getLayers().length,
      box.getClassName(),
      box.fill(),
      box.shadowBlur(),
      box.draggable(),
      box.strokeWidth(),
      box.strokeScaleEnabled(),
      wire.dash().join(":"),
      wire.hitStrokeWidth(),
      s.findOne(".bg").listening(),
      s.findOne(".themed").text(),
      s.findOne(".label").text(),
    ].join();
  });
  if (shape !== "Stage,1,Rect,red,4,true,3,false,6:4,20,false,theme:dark,double-click me") return `initial tree ${shape}`;
  // Only the container div is rendered as HTML; the shapes are drawn.
  await until(p, () => document.querySelectorAll("#editor canvas").length === 1);
  await p.waitForTimeout(100);
  if ((await pixel(p, 80, 70)) !== "255,0,0") return `box pixel ${await pixel(p, 80, 70)}`;

  // Reactive props update the same node in place, and redraw.
  await p.click("#recolor");
  await until(p, () => window.__stage.findOne(".box").fill() === "blue");
  if (!(await p.evaluate(() => window.__stage.findOne(".box") === window.__box0))) return "recolor replaced the node";
  await until(p, () => {
    const c = window.__stage.getLayers()[0].getCanvas();
    const r = c.getPixelRatio();
    const d = c.getContext()._context.getImageData(80 * r, 70 * r, 1, 1).data;
    return d[2] === 255 && d[0] === 0;
  });
  await p.click("#move");
  await until(p, () => window.__stage.findOne(".box").x() === 50);
  await waitStatus(p, "box:50");

  // Click hit-testing: a shape's handler, a listening={false} shape lets the
  // click reach the stage, a thin line is hit within hitStrokeWidth, and a
  // custom sceneFunc shape is hit through its drawn path.
  await click(p, 300, 200);
  await waitStatus(p, "clicks:1");
  await click(p, 580, 380);
  await until(p, () => (window.__log || []).includes("stage"));
  await click(p, 300, 308);
  await until(p, () => (window.__log || []).includes("wire"));
  await click(p, 300, 330);
  if ((await logged(p)).split(" ").filter((e) => e === "wire").length !== 1) return "the wire was hit outside its hitStrokeWidth";
  await click(p, 415, 265);
  await until(p, () => (window.__log || []).includes("custom"));

  // Hover: mouseenter and mouseleave.
  const [hx, hy] = await at(p, 300, 200);
  await p.mouse.move(hx, hy);
  await waitStatus(p, "hover:true");
  await p.mouse.move(hx + 100, hy + 100);
  await waitStatus(p, "hover:false");

  // Drag a node; the drag end updates state, which keeps the node.
  await drag(p, [90, 70], [190, 120]);
  await waitStatus(p, "box:150");
  const dragged = await p.evaluate(() => {
    const b = window.__stage.findOne(".box");
    return [b === window.__box0, b.x(), b.y()].join();
  });
  if (dragged !== "true,150,90") return `dragged box ${dragged}`;
  // dragBoundFunc keeps the knob on its row.
  await drag(p, [500, 100], [450, 180]);
  const knob = await p.evaluate(() => [Math.round(window.__stage.findOne(".knob").x()), window.__stage.findOne(".knob").y()].join());
  if (knob !== "450,100" || !(await logged(p)).includes("knob:100")) return `knob ${knob} (${await logged(p)})`;

  // Transformer: selecting attaches it through nodes(), its props are
  // applied, and a corner drag resizes with keepRatio.
  await click(p, 190, 120);
  await waitStatus(p, "selected:true");
  await until(p, () => window.__stage.findOne("Transformer").nodes().length === 1);
  const tr = await p.evaluate(() => {
    const t = window.__stage.findOne("Transformer");
    return [t.nodes()[0] === window.__box0, t.keepRatio(), t.rotateEnabled(), t.anchorSize(), t.ignoreStroke()].join();
  });
  if (tr !== "true,true,true,10,true") return `transformer ${tr}`;
  // The bottom-right anchor sits at the box's corner (150+80, 90+60).
  await drag(p, [230, 150], [270, 180]);
  await until(p, () => (window.__log || []).includes("transformend"));
  if (!(await logged(p)).includes("transformstart")) return "no transformstart";
  const sized = (await status(p)).match(/size:(\d+)x(\d+)/);
  const w = Number(sized[1]), h = Number(sized[2]);
  if (!(w > 90) || Math.abs(w / h - 80 / 60) > 0.05) return `resize with keepRatio gave ${w}x${h}`;
  // boundBoxFunc rejects a box narrower than 20.
  await drag(p, [150 + w, 90 + h], [140, 80]);
  await p.waitForTimeout(100);
  const minW = await p.evaluate(() => {
    const b = window.__stage.findOne(".box");
    return b.width() * b.scaleX();
  });
  if (!(minW >= 19.5)) return `boundBoxFunc let the box shrink to ${minW}`;
  // A click on empty stage deselects.
  await click(p, 580, 380);
  await waitStatus(p, "selected:false");
  await until(p, () => window.__stage.findOne("Transformer").nodes().length === 0);

  // Double-click.
  const [lx, ly] = await at(p, 170, 207);
  await p.mouse.dblclick(lx, ly);
  await waitStatus(p, "editing:true");
  await until(p, () => window.__stage.findOne(".label").text() === "editing");

  // Keyed list: a reorder moves the same nodes, a removal destroys one.
  await p.click("#reorder");
  await until(p, () => window.__stage.findOne(".list").getChildren().map((n) => n.name()).join() === "item-c,item-b,item-a");
  const kept = await p.evaluate(() => {
    const s = window.__stage;
    return ["a", "b", "c"].every((id, i) => s.findOne(".item-" + id) === window.__items0[i]) && s.findOne(".item-c").x() === 20 && s.findOne(".item-a").x() === 80;
  });
  if (!kept) return "the reorder recreated the keyed nodes or did not update their props";
  await p.click("#drop");
  await until(p, () => window.__stage.findOne(".list").getChildren().map((n) => n.name()).join() === "item-b,item-a");
  if (!(await p.evaluate(() => !window.__items0[2].getStage()))) return "the dropped node is still attached";

  // Conditional: unmount and mount again, at the same z-position.
  await p.click("#toggle");
  await until(p, () => !window.__stage.findOne(".ball"));
  await p.click("#toggle");
  await until(p, () => !!window.__stage.findOne(".ball"));
  const order = await p.evaluate(() => window.__stage.getLayers()[0].getChildren().map((n) => n.name() || n.getClassName()).join());
  if (order !== "bg,box,ball,wire,knob,list,label,custom,themed,Transformer") return `layer order ${order}`;
  // Konva redraws the hit graph on the next frame, as with react-konva.
  await until(p, () => window.__stage.getIntersection({ x: 300, y: 200 })?.name() === "ball");
  await click(p, 300, 200);
  await waitStatus(p, "clicks:2");
  return true;
}

async function signalChecks(p) {
  await until(p, () => !!window.__k && !!window.__k.stage() && !!window.__k.rect());
  const first = await p.evaluate(() => {
    const s = window.__k.stage();
    window.__r0 = window.__k.rect();
    return [s.getClassName(), window.__k.rect() === s.findOne(".r"), s.findOne(".r").x(), s.findOne(".r").fill(), s.find(".dot").map((d) => d.x()).join(":"), !!s.findOne(".far")].join();
  });
  if (first !== "Stage,true,10,red,30:60:90,false") return `variable refs and initial tree ${first}`;
  await p.click("#inc");
  await until(p, () => window.__k.stage().findOne(".r").x() === 20 && window.__k.stage().findOne(".r").fill() === "blue");
  await p.click("#inc");
  await p.click("#inc");
  await until(p, () => !!window.__k.stage().findOne(".far"));
  await p.evaluate(() => (window.__dots0 = window.__k.stage().find(".dot")));
  await p.click("#rev");
  await until(p, () => window.__k.stage().find(".dot").map((d) => d.x()).join(":") === "90:60:30");
  if (!(await p.evaluate(() => window.__k.stage().find(".dot").every((d) => window.__dots0.includes(d))))) return "the keyed reorder recreated the components' nodes";
  // A click on the rect resets the signal: the conditional shape goes away.
  const box = await p.locator("#sig .konvajs-content").boundingBox();
  await p.mouse.click(box.x + 50, box.y + 20);
  await until(p, () => document.querySelector("#x").textContent === "0" && !window.__k.stage().findOne(".far"));
  if (!(await p.evaluate(() => window.__k.stage().findOne(".r") === window.__r0))) return "the rect was recreated";
  return true;
}

try {
  if (!only || only === "signals") {
    await run("krate signals", "/signals", (p) => signalChecks(p));
  }
  if (!only || only === "page") {
    await run("krate page", "/", async (p, scripts) => {
      const react = scripts.filter((u) => /\/isr-|react-dom|react\.production|preact/.test(u));
      if (react.length) return `a Krate page loaded React or Preact: ${react.join(", ")}`;
      return editorChecks(p);
    });
  }
  if (!only || only === "island") {
    await run("preact island", "/island", async (p, scripts) => {
      const html = await (await fetch(`${base}/island`)).text();
      if (/<Rect|<Layer|konvajs-content/i.test(html)) return "shapes leaked into the server HTML";
      const out = await editorChecks(p);
      const react = scripts.filter((u) => /\/isr-|react-dom|react\.production/.test(u));
      if (react.length) return `React runtime loaded: ${react.join(", ")}`;
      return out;
    });
  }
} finally {
  await browser.close();
}
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log("konva browser checks passed");
