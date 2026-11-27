// The page callbacks read innerText on purpose: the checks are about rendered,
// visible text, which textContent does not reflect.
/* oxlint-disable unicorn/prefer-dom-node-text-content */
// End-to-end test for the /playground/ page (Chromium, headless).
//
//   pnpm build && node scripts/playground-e2e.mjs
//
// Serves packages/web/dist with a tiny static server on a free 127.0.0.1 port,
// drives the real page (compiler wasm in a worker, preview through a service
// worker) and writes screenshots to $PLAYGROUND_E2E_SHOTS (default
// .playground-build/shots). Install a browser once with
// `pnpm exec playwright install chromium`.

import assert from "node:assert/strict";
import { existsSync, mkdirSync, readFileSync, statSync } from "node:fs";
import http from "node:http";
import { dirname, extname, join, normalize, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { loadExamples } from "./playground-examples.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const dist = resolve(here, "../dist");
const shots = resolve(process.env.PLAYGROUND_E2E_SHOTS || join(here, "../.playground-build/shots"));

if (!existsSync(join(dist, "playground/index.html"))) {
  console.error("playground-e2e: dist/playground/index.html is missing; run `pnpm build` first");
  process.exit(1);
}
if (!existsSync(join(dist, "playground/manifest.json"))) {
  console.error("playground-e2e: the site was built without the playground engine");
  process.exit(process.env.KRATE_REQUIRE_PLAYGROUND === "1" ? 1 : 0);
}
mkdirSync(shots, { recursive: true });

const TYPES = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".woff2": "font/woff2",
  ".gz": "application/octet-stream",
  ".webmanifest": "application/manifest+json",
  ".xml": "application/xml",
};

const server = http.createServer((req, res) => {
  const pathname = decodeURIComponent(new URL(req.url, "http://x").pathname);
  let file = join(dist, normalize(pathname));
  if (!file.startsWith(dist)) {
    res.writeHead(403).end();
    return;
  }
  if (existsSync(file) && statSync(file).isDirectory()) file = join(file, "index.html");
  if (!existsSync(file) || !statSync(file).isFile()) {
    res.writeHead(404, { "content-type": "text/plain" }).end("not found");
    return;
  }
  res.writeHead(200, { "content-type": TYPES[extname(file)] || "application/octet-stream" });
  res.end(readFileSync(file));
});
await new Promise((r) => server.listen(0, "127.0.0.1", r));
const base = `http://127.0.0.1:${server.address().port}`;

const { examples } = await loadExamples();
const browser = await chromium.launch();
let step = "start";
const log = (msg) => console.log(`  ok  ${msg}`);

async function newPlayground(context, url = base + "/playground/") {
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push("pageerror: " + e.message));
  page.on("console", (m) => {
    if (m.type() === "error") errors.push("console.error: " + m.text());
  });
  page.on("dialog", (d) => d.accept());
  await page.goto(url);
  return { page, errors };
}

const waitReady = (page) => page.waitForSelector('#pg[data-stage="ready"]', { timeout: 120000 });

// Waits for the next build and for the preview iframe to finish loading it.
async function afterEdit(page, action) {
  const before = await page.evaluate(() => ({ b: +(document.getElementById("pg").dataset.builds || 0), f: +(document.getElementById("pg").dataset.frames || 0) }));
  await action();
  await page.waitForFunction((b) => +document.getElementById("pg").dataset.builds > b, before.b, { timeout: 30000 });
  await page.waitForFunction((f) => +document.getElementById("pg").dataset.frames > f, before.f, { timeout: 30000 });
}
const preview = (page) => page.frameLocator("iframe.pg-frame");
const setCode = (page, text) => page.locator(".ed-ta").fill(text);
// The pickers are Krate select listboxes (a combobox button and role=option items).
const pickOption = async (page, id, value) => {
  await page.click(`#${id} .krate-select-trigger`);
  await page.locator(`#${id}`).getByRole("option", { name: value, exact: true }).click();
};
const pickerValue = (page, id) => page.evaluate((i) => document.getElementById(i).value, id);
const openExample = (page, id) => afterEdit(page, () => pickOption(page, "pg-example", examples.find((e) => e.id === id)?.title ?? id));
const tab = (page, name) => page.click(`#pg-tab-${name}`);

try {
  // ---- Load, engine progress, version ----
  step = "load";
  const context = await browser.newContext({
    viewport: { width: 1360, height: 860 },
    colorScheme: "light",
    permissions: ["clipboard-read", "clipboard-write"],
  });
  // The fetch example talks to a public API; answer it locally.
  await context.route("https://jsonplaceholder.typicode.com/posts/*", (route) => {
    const n = route.request().url().split("/").pop();
    route.fulfill({
      status: 200,
      headers: { "access-control-allow-origin": "*", "content-type": "application/json" },
      body: JSON.stringify({ id: Number(n), title: "title for " + n, body: "body for " + n }),
    });
  });
  const { page, errors } = await newPlayground(context);
  await page.waitForSelector("#pg-boot:not([hidden])", { timeout: 5000 }).catch(() => {});
  await waitReady(page);
  const versionText = await page.locator(".pg-engine").innerText();
  assert.match(versionText, /Krate compiler \S+/, "the compiler version is shown");
  assert.ok(!/^Krate compiler 0\.1\.0$/.test(versionText.trim()), "the version is stamped: " + versionText);
  log(`engine ready: ${versionText.trim()}`);

  // ---- Counter preview and interaction ----
  step = "counter";
  await page.waitForFunction(() => +document.getElementById("pg").dataset.frames >= 1, null, { timeout: 30000 });
  const frame = preview(page);
  await frame.locator(".value").waitFor();
  assert.equal((await frame.locator(".value").innerText()).trim(), "0");
  await frame.getByRole("button", { name: "+" }).click();
  await frame.getByRole("button", { name: "+" }).click();
  await frame.locator(".value", { hasText: "2" }).waitFor({ timeout: 5000 });
  log("counter preview renders and increments inside the iframe");
  assert.match(await page.locator(".pg-statusbar").innerText(), /Built in \d+ ms/);
  assert.match(await page.locator(".pg-statusbar").innerText(), /JS [\d.]+ (KB|B) gzip/);
  log("status bar shows build time and JS gzip size");

  // ---- Editing: live rebuild, syntax error diagnostic, fix ----
  step = "edit";
  const counterSrc = examples.find((e) => e.id === "counter").files["src/pages/index.tsx"];
  await afterEdit(page, () => setCode(page, counterSrc.replace("<h1>Counter</h1>", "<h1>Edited title</h1>")));
  await preview(page).locator("h1", { hasText: "Edited title" }).waitFor();
  log("an edit rebuilds the preview automatically");

  await page.evaluate(() => (window.__b = +document.getElementById("pg").dataset.builds));
  await setCode(page, counterSrc.replace("<h1>Counter</h1>", "<h1>{</h1>"));
  await page.waitForFunction(() => +document.getElementById("pg").dataset.builds > window.__b, null, { timeout: 30000 });
  await page.waitForSelector(".pg-badge-error");
  assert.ok((await page.locator(".ed-pre .l-error").count()) > 0, "the failing line is highlighted");
  await tab(page, "diagnostics");
  const diagText = await page.locator("#pg-diags").innerText();
  assert.match(diagText, /Error/);
  assert.match(diagText, /index\.tsx:\d+:\d+/);
  await page.locator("[data-diag]").first().click();
  const caretLine = await page.evaluate(() => {
    const ta = document.querySelector(".ed-ta");
    return ta.value.slice(0, ta.selectionStart).split("\n").length;
  });
  assert.equal(caretLine, 9, "clicking a diagnostic puts the caret on its line");
  assert.match(await page.locator(".pg-statusbar").innerText(), /1 error/);
  // The last good preview stays visible while the build is broken.
  await tab(page, "preview");
  await preview(page).locator("h1").waitFor();
  log("a syntax error shows a diagnostic, highlights the line, and jumps to it");

  await afterEdit(page, () => setCode(page, counterSrc));
  assert.equal(await page.locator(".pg-badge-error").count(), 0);
  await tab(page, "preview");
  log("fixing the error clears the diagnostics");

  // ---- Editor keys ----
  step = "editor keys";
  const ta = page.locator(".ed-ta");
  await ta.fill("function a() {");
  await ta.press("End");
  await ta.press("Enter");
  assert.equal(await ta.inputValue(), "function a() {\n  ", "Enter auto-indents inside a block");
  await ta.press("Tab");
  assert.equal(await ta.inputValue(), "function a() {\n    ", "Tab inserts two spaces");
  await ta.press("Shift+Tab");
  assert.equal(await ta.inputValue(), "function a() {\n  ", "Shift+Tab outdents");
  await afterEdit(page, () => setCode(page, counterSrc));
  log("auto-indent, Tab and Shift+Tab");

  // ---- Output tabs ----
  step = "output tabs";
  await tab(page, "html");
  assert.match(await page.locator("#pg-code-html").innerText(), /<h1>Counter<\/h1>/);
  await tab(page, "js");
  assert.match(await page.locator("#pg-code-js").innerText(), /createSignal|kbind/);
  await tab(page, "css");
  assert.match(await page.locator("#pg-code-css").innerText(), /\.value/);
  await tab(page, "files");
  const filesText = await page.locator("#pg-files-out").innerText();
  assert.match(filesText, /index\.html/);
  assert.match(filesText, /\.css/);
  await page.waitForFunction(() => /KB|B/.test(document.querySelector("#pg-files-out tbody tr:nth-child(2) td:last-child")?.textContent || ""));
  assert.match(filesText, /chunks\//, "the shared runtime chunk is in the output");
  log("HTML, JS, CSS and Files tabs show the output");

  // Console relay.
  step = "console";
  await afterEdit(page, () => setCode(page, counterSrc.replace("const [count", 'console.log("hello-e2e", 42);\n  console.error("boom-e2e");\n  const [count')));
  await tab(page, "console");
  await page.locator("#pg-console", { hasText: "hello-e2e 42" }).waitFor();
  await page.locator(".pg-logline-error", { hasText: "boom-e2e" }).waitFor();
  assert.match(await page.locator("#pg-tab-console").innerText(), /2/);
  await page.click("#pg-clear");
  assert.equal(await page.locator(".pg-logline").count(), 0);
  await afterEdit(page, () => setCode(page, counterSrc));
  await tab(page, "preview");
  log("console.log and console.error are relayed from the preview");

  // ---- Files: add, edit, rename, delete ----
  step = "files";
  await page.click("#pg-add");
  await page.fill("#pg-name", "components/greeting.tsx");
  await page.press("#pg-name", "Enter");
  assert.match(await page.locator("#pg-files").innerText(), /components\/greeting\.tsx/);
  await setCode(page, 'export function Greeting() {\n  return <p class="greeting">Hello from a new file</p>;\n}\n');
  await page.click('#pg-files [role="tab"]:has-text("pages/index.tsx")');
  await afterEdit(page, () =>
    setCode(
      page,
      counterSrc.replace('import "../styles.css";', 'import "../styles.css";\nimport { Greeting } from "../components/greeting";').replace("<h1>Counter</h1>", "<h1>Counter</h1>\n      <Greeting />"),
    ),
  );
  await preview(page).locator(".greeting", { hasText: "Hello from a new file" }).waitFor();
  await page.click('#pg-files [role="tab"]:has-text("components/greeting.tsx")');
  await page.click("#pg-rename");
  await page.fill("#pg-name", "components/hello.tsx");
  await page.press("#pg-name", "Enter");
  assert.match(await page.locator("#pg-files").innerText(), /components\/hello\.tsx/);
  await page.click("#pg-delete");
  assert.doesNotMatch(await page.locator("#pg-files").innerText(), /components\/hello/);
  await page.click('#pg-files [role="tab"]:has-text("pages/index.tsx")');
  await afterEdit(page, () => setCode(page, counterSrc));
  log("add, rename and delete a file");

  // ---- Reload keeps the project (localStorage) ----
  step = "persist";
  await page.click('#pg-files [role="tab"]:has-text("pages/index.tsx")');
  await afterEdit(page, () => setCode(page, counterSrc.replace("<h1>Counter</h1>", "<h1>Saved title</h1>")));
  await page.waitForTimeout(700);
  await page.reload();
  await waitReady(page);
  await preview(page).locator("h1", { hasText: "Saved title" }).waitFor({ timeout: 30000 });
  assert.match(await page.locator(".ed-ta").inputValue(), /Saved title/);
  log("work is autosaved and restored on reload");

  // ---- Reset ----
  step = "reset";
  await afterEdit(page, () => page.click("#pg-reset"));
  assert.match(await page.locator(".ed-ta").inputValue(), /<h1>Counter<\/h1>/);
  log("reset restores the example");

  // ---- Share round trip ----
  step = "share";
  await afterEdit(page, () => setCode(page, counterSrc.replace("<h1>Counter</h1>", "<h1>Shared title</h1>")));
  await page.click("#pg-share");
  await page.waitForFunction(() => /Link copied/.test(document.querySelector(".pg-stat-note")?.textContent || ""));
  const link = await page.evaluate(() => navigator.clipboard.readText());
  assert.match(link, /\/playground\/#code=[\w-]+$/);
  const shared = await newPlayground(context, link);
  await waitReady(shared.page);
  await shared.page.waitForFunction(() => +document.getElementById("pg").dataset.builds >= 1, null, { timeout: 30000 });
  assert.match(await shared.page.locator(".ed-ta").inputValue(), /Shared title/);
  assert.equal(await shared.page.locator("#pg-gate").isVisible(), true, "a shared project does not auto-run");
  assert.equal(await shared.page.locator("iframe.pg-frame").count(), 0, "no preview frame before the user agrees");
  await shared.page.click("#pg-gate-run");
  await shared.page.frameLocator("iframe.pg-frame").locator("h1", { hasText: "Shared title" }).waitFor({ timeout: 30000 });
  assert.ok(!new URL(shared.page.url()).hash, "the hash is cleared after loading");
  assert.deepEqual(shared.errors, [], "no errors on the shared page");
  await shared.page.close();
  log("share link round-trips and the preview waits for consent");

  // Example hash link.
  const ex = await newPlayground(context, base + "/playground/#example=stopwatch");
  await waitReady(ex.page);
  await ex.page.waitForFunction(() => +document.getElementById("pg").dataset.frames >= 1, null, { timeout: 30000 });
  assert.match(await ex.page.locator(".ed-ta").inputValue(), /Stopwatch/);
  assert.equal(await pickerValue(ex.page, "pg-example"), "stopwatch");
  await ex.page.close();
  log("#example=<id> opens an example");

  // ---- Every example ----
  step = "examples";
  await page.click("#pg-reset");
  const checks = {
    counter: async (f) => {
      await f.getByRole("button", { name: "+" }).click();
      await f.locator(".value", { hasText: "1" }).waitFor({ timeout: 5000 });
    },
    todo: async (f) => {
      await f.getByLabel("New todo").fill("write e2e");
      await f.getByRole("button", { name: "Add" }).click();
      await f.getByText("write e2e").waitFor({ timeout: 5000 });
      await f.getByText("1 item(s) left").waitFor({ timeout: 5000 });
      await f.getByLabel("write e2e").first().check();
      await f.getByText("0 item(s) left").waitFor({ timeout: 5000 });
      await f.getByRole("button", { name: "Remove write e2e" }).click();
    },
    "css-signals": async (f) => {
      assert.equal(await f.locator("script[src]").count(), 0, "the CSS signals page loads no script");
      await f.getByRole("tab", { name: "Details" }).click();
      await f.getByText("Details panel.").waitFor({ state: "visible", timeout: 5000 });
      await f.getByRole("button", { name: "Does it need JavaScript?" }).click();
      await f.getByText("No. The compiler erases it.").waitFor({ state: "visible", timeout: 5000 });
    },
    components: async (f) => {
      await f.getByRole("tab", { name: "Password" }).click();
      await f.getByText("Change your password here.").waitFor({ state: "visible", timeout: 5000 });
      await f.getByRole("button", { name: "Open dialog" }).click();
      await f.getByText("This dialog is pure CSS").waitFor({ state: "visible", timeout: 5000 });
      await f.getByRole("button", { name: "Close" }).first().click();
      await f.getByText("This dialog is pure CSS").waitFor({ state: "hidden", timeout: 5000 });
      assert.ok(await f.getByRole("switch").count(), "the switch renders");
      await f.getByText("Volume: 40").waitFor({ timeout: 5000 });
    },
    tailwind: async (f) => {
      const btn = f.getByRole("button", { name: "Like" });
      assert.notEqual(await btn.evaluate((el) => getComputedStyle(el).backgroundColor), "rgba(0, 0, 0, 0)", "tailwind utilities are applied");
      await btn.click();
      await f.getByRole("button", { name: "Liked" }).waitFor({ timeout: 5000 });
    },
    "css-modules": async (f) => {
      const card = f.locator("h2").first().locator("..");
      assert.notEqual(await card.evaluate((el) => getComputedStyle(el).borderTopWidth), "0px", "the module class is applied");
      assert.ok(!(await card.getAttribute("class")).split(" ").includes("card"), "the class name is scoped");
    },
    "multi-page": async (f, page) => {
      await f.getByRole("link", { name: "About" }).click();
      await f.locator("h1", { hasText: "About" }).waitFor({ timeout: 5000 });
      await page.waitForFunction(() => document.querySelector("#pg-url").textContent === "/about/", null, { timeout: 5000 });
      await f.getByRole("link", { name: "Contact" }).click();
      await f.locator("h1", { hasText: "Contact" }).waitFor({ timeout: 5000 });
    },
    markdown: async (f) => {
      await f.locator("h1", { hasText: "Notes" }).waitFor({ timeout: 5000 });
      assert.equal(await f.locator("table").count(), 1, "the Markdown table renders");
    },
    fetch: async (f) => {
      await f.getByText("title for 1").waitFor({ timeout: 8000 });
      await f.getByRole("button", { name: "Next" }).click();
      await f.getByText("title for 2").waitFor({ timeout: 8000 });
    },
    stopwatch: async (f) => {
      await f.getByRole("button", { name: "Start" }).click();
      await f.getByRole("button", { name: "Stop" }).waitFor({ timeout: 5000 });
      await page.waitForTimeout(400);
      await f.getByRole("button", { name: "Stop" }).click();
      const t = (await f.locator(".time").innerText()).trim();
      assert.notEqual(t, "00:00.00", "the stopwatch ran");
    },
  };
  // The demo-* examples are the gallery's demos: a few get a real interaction,
  // the rest must build, render their heading and log no errors.
  checks["demo-reactivity"] = async (f) => {
    await f.getByRole("button", { name: "a + 1" }).click();
    await f.locator('[data-node="0"]', { hasText: "3" }).waitFor({ timeout: 5000 });
  };
  checks["demo-css-signals"] = async (f) => {
    assert.equal(await f.locator("script[src]").count(), 0, "the CSS signals demo loads no script");
    await f.locator("label", { hasText: "Yearly" }).click();
  };
  checks["demo-data"] = async (f) => {
    await f.locator(".dt-state strong", { hasText: "ready" }).waitFor({ timeout: 15000 });
  };
  for (const e of examples) {
    if (e.id.startsWith("demo-") && !checks[e.id]) {
      checks[e.id] = async (f) => {
        await f.locator("h1", { hasText: e.title.replace(/^Demo: /, "") }).waitFor({ timeout: 5000 });
      };
    }
    await openExample(page, e.id);
    await tab(page, "diagnostics");
    assert.equal(await page.locator(".pg-badge-error").count(), 0, `${e.id}: no error diagnostics`);
    await tab(page, "preview");
    const f = preview(page);
    await f.locator("body").waitFor();
    assert.ok((await f.locator("body").innerText()).trim().length > 0, `${e.id}: the preview renders text`);
    assert.ok(checks[e.id], `${e.id}: has an interaction check`);
    await checks[e.id](f, page);
    // Console errors from inside the preview (the fetch example may hit the network).
    const bad = await page.locator(".pg-logline-error").allInnerTexts();
    assert.deepEqual(bad, [], `${e.id}: no errors in the preview console`);
    log(`example "${e.id}" builds, renders and works`);
  }

  // Offline fetch degrades to a message.
  await context.route("https://jsonplaceholder.typicode.com/posts/*", (route) => route.abort());
  await openExample(page, "fetch");
  await preview(page).getByRole("alert").waitFor({ timeout: 8000 });
  log("the fetch example degrades gracefully when offline");

  // ---- Pickers and options: Krate select, Krate checkboxes ----
  step = "pickers";
  assert.equal(await page.locator("select").count(), 0, "the playground has no native select");
  assert.equal(await page.locator('.pg input[type="checkbox"]:not(.krate-checkbox-input)').count(), 0, "the checkboxes are Krate checkboxes");
  await openExample(page, "counter");
  assert.equal(await pickerValue(page, "pg-example"), "counter");
  // Keyboard: ArrowDown opens the list and focuses the search; type, Enter chooses.
  await page.focus("#pg-example .krate-select-trigger");
  await page.keyboard.press("ArrowDown");
  assert.equal(await page.getAttribute("#pg-example .krate-select-trigger", "aria-expanded"), "true");
  await afterEdit(page, async () => {
    await page.keyboard.type("stopw");
    await page.keyboard.press("Enter");
  });
  assert.equal(await pickerValue(page, "pg-example"), "stopwatch");
  assert.equal(await page.getAttribute("#pg-example .krate-select-trigger", "aria-expanded"), "false");
  await page.focus("#pg-example .krate-select-trigger");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("Escape");
  assert.equal(await page.getAttribute("#pg-example .krate-select-trigger", "aria-expanded"), "false");
  await openExample(page, "counter");
  // Dev mode and Minify rebuild with the new option.
  const optsBefore = await page.evaluate(() => JSON.parse(localStorage.getItem("krate-playground:v1")).opts);
  await afterEdit(page, () => page.locator("label.krate-checkbox", { hasText: "Dev mode" }).click());
  assert.equal(await page.locator("#pg-dev").isChecked(), true);
  await page.waitForFunction(() => JSON.parse(localStorage.getItem("krate-playground:v1")).opts.dev === true, null, { timeout: 5000 });
  await afterEdit(page, () => page.locator("label.krate-checkbox", { hasText: "Dev mode" }).click());
  await afterEdit(page, () => page.locator("label.krate-checkbox", { hasText: "Minify" }).click());
  assert.equal(await page.locator("#pg-minify").isChecked(), !optsBefore.minify);
  await afterEdit(page, () => page.locator("label.krate-checkbox", { hasText: "Minify" }).click());
  assert.equal(await page.locator("#pg-minify").isChecked(), optsBefore.minify);
  // The output file pickers list the built files and switch the code shown.
  await tab(page, "js");
  const jsFiles = await page.locator("#pg-sel-js").evaluate((el) => el.value);
  assert.ok(jsFiles.endsWith(".js"), "the JS picker shows a built file: " + jsFiles);
  await page.click("#pg-sel-js .krate-select-trigger");
  const jsOptions = await page.locator("#pg-sel-js [role=option]").allInnerTexts();
  assert.ok(jsOptions.length >= 1 && jsOptions.includes(jsFiles), "the JS picker lists the built files");
  await page.keyboard.press("Escape");
  await tab(page, "html");
  assert.equal(await pickerValue(page, "pg-sel-html"), "index.html");
  await tab(page, "preview");
  await openExample(page, "fetch");
  log("example picker, Minify / Dev mode checkboxes and file pickers work");

  // ---- Layout: split drag, no horizontal overflow ----
  step = "layout";
  await openExample(page, "counter");
  const before = await page.locator("#pg-left").boundingBox();
  const handle = await page.locator("#pg-split").boundingBox();
  await page.mouse.move(handle.x + 4, handle.y + handle.height / 2);
  await page.mouse.down();
  await page.mouse.move(handle.x + 204, handle.y + handle.height / 2, { steps: 5 });
  await page.mouse.up();
  const after = await page.locator("#pg-left").boundingBox();
  assert.ok(after.width > before.width + 100, "dragging the handle resizes the panes");
  await page.focus("#pg-split");
  await page.keyboard.press("ArrowLeft");
  assert.ok((await page.locator("#pg-left").boundingBox()).width < after.width, "the handle works from the keyboard");
  log("resizable split");

  // ---- Screenshots (desktop + mobile, light + dark) ----
  step = "screenshots";
  await page.evaluate(() => localStorage.removeItem("krate-playground:split"));
  for (const scheme of ["light", "dark"]) {
    const desk = await browser.newContext({ viewport: { width: 1360, height: 860 }, colorScheme: scheme });
    const d = await newPlayground(desk, base + "/playground/#example=todo");
    await waitReady(d.page);
    await d.page.waitForFunction(() => +document.getElementById("pg").dataset.frames >= 1, null, { timeout: 30000 });
    await d.page.waitForTimeout(300);
    await d.page.screenshot({ path: join(shots, `desktop-${scheme}.png`) });
    assert.deepEqual(d.errors, [], `desktop ${scheme}: no console errors`);
    await desk.close();

    const mob = await browser.newContext({ viewport: { width: 390, height: 844 }, colorScheme: scheme, deviceScaleFactor: 2, hasTouch: true, isMobile: true });
    const m = await newPlayground(mob, base + "/playground/#example=todo");
    await waitReady(m.page);
    await m.page.waitForFunction(() => +document.getElementById("pg").dataset.frames >= 1, null, { timeout: 30000 });
    await m.page.waitForTimeout(300);
    const overflow = await m.page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    assert.ok(overflow <= 0, `mobile ${scheme}: no horizontal page scroll (overflow ${overflow}px)`);
    await m.page.screenshot({ path: join(shots, `mobile-${scheme}.png`), fullPage: true });
    assert.deepEqual(m.errors, [], `mobile ${scheme}: no console errors`);
    await mob.close();
  }
  log(`screenshots written to ${shots}`);

  // ---- Engine unavailable: friendly fallback ----
  step = "fallback";
  const fb = await browser.newContext({ viewport: { width: 1200, height: 800 } });
  await fb.route("**/playground/manifest.json", (r) => r.fulfill({ status: 404, body: "not found" }));
  const f2 = await newPlayground(fb);
  await f2.page.waitForSelector('#pg[data-stage="unavailable"]', { timeout: 15000 });
  const card = await f2.page.locator("#pg-boot").innerText();
  assert.match(card, /not available/i);
  assert.ok(await f2.page.locator('#pg-boot a[href="/docs/getting-started/"]').isVisible(), "links to the getting-started docs");
  await f2.page.screenshot({ path: join(shots, "fallback.png") });
  await fb.close();
  log("without the wasm build the page shows a fallback");

  assert.deepEqual(
    errors.filter((e) => !/Failed to load resource|boom-e2e/.test(e)),
    [],
    "no page errors on the main playground page",
  );
  console.log("playground e2e passed");
} catch (err) {
  console.error(`playground e2e failed during "${step}":`);
  console.error(err);
  process.exitCode = 1;
} finally {
  await browser.close();
  server.close();
}
