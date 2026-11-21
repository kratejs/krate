// Loads pages whose client components read a localStorage store with
// useSyncExternalStore while "b" is saved and the server rendered "a" (see
// TestUseClientIslandBrowserSyncExternalStore). Every animation frame after
// a component hydrated must already show "b": React re-renders the
// components whose getSnapshot() differs from getServerSnapshot() before
// paint.
import { chromium, firefox, webkit } from "playwright";
import { waitHydrated } from "./island_helpers.mjs";

const browserType = { chromium, firefox, webkit }[process.argv[2] || "chromium"];
const base = process.argv[3];
const browser = await browserType.launch({ headless: true });
const failures = [];
const fail = (msg) => failures.push(msg);
const results = {};
// step runs one wait and, when it times out, says which and what the page shows.
async function step(page, label, fn) {
  try {
    return await fn();
  } catch (e) {
    const seen = await page.evaluate(() => ({
      islands: (window.__krateIslands || []).map((i) => i.module + "#" + i.export + "=" + i.status),
      main: (document.querySelector("main") || document.body).outerHTML.slice(0, 1500),
    })).catch(() => null);
    throw new Error(label + ": " + e.message.split("\n")[0] + "\n" + JSON.stringify(seen, null, 1));
  }
}
const p0 = (path) => path + ": ";

// Runs in the page before its scripts: saves "b" and records, at every
// frame, what the hydrated components show.
const init = () => {
  localStorage.setItem("mode", "b");
  window.__noServer = "b";
  window.__frames = [];
  const hydrated = (el) => {
    if (!el) return false;
    if ((window.__krateIslands || []).some((i) => i.status === "hydrated" && i.contains(el))) return true;
    for (let p = el; p; p = p.parentElement) if (p.hasAttribute && p.hasAttribute("data-krate-island-hydrated")) return true;
    return false;
  };
  const tick = () => {
    const mode = document.getElementById("mode");
    const swA = document.getElementById("sw-a");
    const early = document.getElementById("early");
    const f = {};
    if (hydrated(mode)) f.mode = mode.tagName + ":" + mode.getAttribute("data-mode") + ":" + document.querySelectorAll("[data-mode]").length;
    if (hydrated(swA)) f.sw = swA.getAttribute("aria-checked") + "/" + document.getElementById("sw-b").getAttribute("aria-checked");
    if (hydrated(early)) f.early = early.textContent;
    if (Object.keys(f).length) window.__frames.push(f);
    if (window.__frames.length < 400) requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
};

try {
  for (const path of ["/", "/islands"]) {
    const context = await browser.newContext();
    await context.addInitScript(init);
    const page = await context.newPage();
    const errors = [];
    page.on("pageerror", (e) => errors.push("pageerror: " + e.message));
    page.on("console", (m) => { if (m.type() === "error") errors.push("console: " + m.text()); });
    const html = await (await page.request.get(base + path)).text();
    // The server renders getServerSnapshot's value.
    if (!/data-mode="?a/.test(html) || !/id="?sw-a"?[^>]*aria-checked="?true/.test(html)) fail(p0(path) + "the server did not render getServerSnapshot's value: " + (html.match(/<main[\s\S]{0,600}/) || [html.slice(0, 600)])[0]);
    await page.goto(base + path);
    for (const sel of ["#mode", "#sw-a", "#early", "#noserver", "#in"]) await step(page, path + " hydrate " + sel, () => waitHydrated(page, sel));
    await step(page, path + " #late", () => page.waitForSelector("#late"));
    await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
    const state = await page.evaluate(() => ({
      frames: window.__frames,
      mode: document.getElementById("mode").tagName + ":" + document.getElementById("mode").getAttribute("data-mode"),
      modes: document.querySelectorAll("[data-mode]").length,
      sw: document.getElementById("sw-a").getAttribute("aria-checked") + "/" + document.getElementById("sw-b").getAttribute("aria-checked"),
      early: document.getElementById("early").textContent,
      late: document.getElementById("late").textContent,
      noserver: document.getElementById("noserver").textContent,
    }));
    const p = path + ": ";
    const frames = state.frames;
    if (!frames.some((f) => f.mode) || !frames.some((f) => f.sw)) fail(p + "no frame was recorded after hydration: " + JSON.stringify(frames.slice(0, 5)));
    for (const f of frames) {
      if (f.mode && f.mode !== "SECTION:b:1") { fail(p + "a frame showed the server's mode after hydration: " + f.mode); break; }
      if (f.sw && f.sw !== "false/true") { fail(p + "a frame showed the server's switch after hydration: " + f.sw); break; }
      if (f.early && f.early !== "b") { fail(p + "a frame showed the server's value in #early: " + f.early); break; }
    }
    if (state.mode !== "SECTION:b" || state.modes !== 1) fail(p + "mode after hydration: " + state.mode + " (" + state.modes + " [data-mode] elements)");
    if (state.sw !== "false/true") fail(p + "switch after hydration: " + state.sw);
    if (state.early !== "b" || state.late !== "b") fail(p + "the hydrated reader and the one mounted later disagree: " + state.early + " / " + state.late);
    // Without getServerSnapshot the hydrating render reads getSnapshot, as before.
    if (state.noserver !== "b") fail(p + "a store without getServerSnapshot: " + state.noserver);

    // useId: the server's ids survive hydration and a re-render.
    const serverId = (html.match(/id="?in"? data-id="?([^"\s>]+)/) || [])[1];
    await page.click("#rerender");
    await step(page, path + " rerender", () => page.waitForFunction(() => document.getElementById("rerender").textContent === "1"));
    const ids = await page.evaluate(() => ({ input: document.getElementById("in").getAttribute("data-id"), label: document.getElementById("lbl").getAttribute("for") }));
    if (!serverId || ids.input !== serverId || ids.label !== serverId) fail(p + "useId: server " + serverId + ", client input " + ids.input + ", label " + ids.label);

    // The store still drives every reader after hydration.
    await page.click("#sw-a");
    await step(page, path + " choose A", () => page.waitForFunction(() => document.getElementById("mode").getAttribute("data-mode") === "a", null, { timeout: 5000 }));
    const after = await page.evaluate(() => ({
      mode: document.getElementById("mode").tagName, sw: document.getElementById("sw-a").getAttribute("aria-checked"),
      early: document.getElementById("early").textContent, late: document.getElementById("late").textContent,
    }));
    if (after.mode !== "DIV" || after.sw !== "true" || after.early !== "a" || after.late !== "a") fail(p + "after choosing A: " + JSON.stringify(after));
    if (errors.length) fail(p + errors.join("\n"));
    results[path] = { frames: frames.length, ...state, frames: undefined, ids };
    await context.close();
  }

  // A real-React island: React's own useSyncExternalStore, untouched.
  {
    const context = await browser.newContext();
    await context.addInitScript(init);
    const page = await context.newPage();
    const errors = [];
    page.on("pageerror", (e) => errors.push("pageerror: " + e.message));
    await page.goto(base + "/real");
    await step(page, "/real #real", () => page.waitForSelector("#real", { timeout: 15000 }));
    await page.waitForFunction(() => document.getElementById("real").getAttribute("data-mode") === "b", null, { timeout: 15000 }).catch(() => {});
    const real = await page.evaluate(() => ({
      mode: document.getElementById("real").tagName + ":" + document.getElementById("real").getAttribute("data-mode"),
      modes: document.querySelectorAll("[data-mode]").length,
    }));
    if (real.mode !== "SECTION:b" || real.modes !== 1) fail("/real: " + JSON.stringify(real));
    if (errors.length) fail("/real: " + errors.join("\n"));
    results["/real"] = real;
    await context.close();
  }
} finally {
  await browser.close();
}
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log(JSON.stringify(results));
