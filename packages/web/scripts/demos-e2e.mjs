// The page callbacks read innerText on purpose: the checks are about rendered,
// visible text, which textContent does not reflect.
/* oxlint-disable unicorn/prefer-dom-node-text-content */
// End-to-end test for the homepage and the /demos/ gallery (Chromium, headless).
//
//   pnpm build && node scripts/demos-e2e.mjs
//
// Serves packages/web/dist with a tiny static server on a free 127.0.0.1 port,
// opens the homepage and every demo, interacts with it, and fails on any
// console error. A second context with JavaScript disabled checks the zero-JS
// demos. Screenshots go to $DEMOS_E2E_SHOTS (default .playground-build/demo-shots).
// DEMOS_ONLY=slug[,slug] limits the run while developing.

import assert from "node:assert/strict";
import { existsSync, mkdirSync, readFileSync, readdirSync, statSync } from "node:fs";
import http from "node:http";
import { dirname, extname, join, normalize, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { loadDemos } from "./playground-examples.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const dist = resolve(here, "../dist");
const shots = resolve(process.env.DEMOS_E2E_SHOTS || join(here, "../.playground-build/demo-shots"));
const only = (process.env.DEMOS_ONLY || "").split(",").filter(Boolean);

if (!existsSync(join(dist, "demos/index.html"))) {
  console.error("demos-e2e: dist/demos/index.html is missing; run `pnpm build` first");
  process.exit(1);
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
  ".webmanifest": "application/manifest+json",
  ".xml": "application/xml",
};

const server = http.createServer((req, res) => {
  const pathname = decodeURIComponent(new URL(req.url, "http://x").pathname);
  let file = join(dist, normalize(pathname));
  if (!file.startsWith(dist)) return void res.writeHead(403).end();
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

const browser = await chromium.launch();
let step = "start";
const log = (msg) => console.log(`  ok  ${msg}`);
const wanted = (slug) => only.length === 0 || only.includes(slug);

// Opens a page with console and request-failure collection.
async function open(context, path) {
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push("pageerror: " + e.message));
  page.on("console", (m) => {
    if (m.type() === "error") errors.push("console.error: " + m.text());
  });
  page.on("requestfailed", (r) => errors.push("requestfailed: " + r.url()));
  page.on("response", (r) => {
    if (r.status() >= 400) errors.push(`http ${r.status()}: ${r.url()}`);
  });
  await page.goto(base + path, { waitUntil: "load" });
  return { page, errors };
}

// Route-specific page script (the file next to the page's index.html), if any.
function pageScripts(path) {
  const dir = join(dist, path);
  return existsSync(dir) ? readdirSync(dir).filter((f) => /^index\.[a-z0-9]+\.js$/.test(f)) : [];
}

// Errors a test provokes on purpose (the data demo requests a missing file).
const expected = [/missing\.json/, /Failed to load resource: the server responded with a status of 404/];
function noErrors(label, errors) {
  const bad = errors.filter((e) => !expected.some((re) => re.test(e)));
  assert.deepEqual(bad, [], `${label}: console and network errors`);
}

const SHOT = (name) => join(shots, name + ".png");
const { demos } = await loadDemos();
const slugs = demos.map((d) => d.slug);

// ---- Per-demo interactions ----
const tests = {
  async reactivity(page) {
    const writes = (n) => page.locator(`[data-writes="${n}"]`);
    await page.getByRole("button", { name: "a + 1" }).click();
    await page.getByRole("button", { name: "c + 1" }).click();
    await page.getByRole("button", { name: "c + 1" }).click();
    assert.equal((await page.locator('[data-node="0"]').innerText()).trim(), "3");
    assert.equal((await page.locator('[data-node="4"]').innerText()).trim(), "2");
    await page.waitForFunction(() => document.querySelector('[data-writes="4"]').textContent.trim() === "2");
    assert.equal((await writes(1).innerText()).trim(), "0", "b was never written");
    assert.equal((await writes(3).innerText()).trim(), "0", "the size memo did not change, so its node was not written");
    assert.match(await page.locator(".rx-total").innerText(), /4 text writes in 3 clicks/);
  },
  async "css-signals"(page) {
    await page.locator(".cz-seg label", { hasText: "Yearly" }).click();
    assert.equal(await page.locator(".cz-seg label", { hasText: "Yearly" }).evaluate((el) => getComputedStyle(el).fontWeight), "600");
    const amount = await page.locator(".cz-amount").evaluate((el) => getComputedStyle(el, "::after").content);
    assert.match(amount, /\$9/, "the live text follows the choice: " + amount);
    await page.locator(".cz-chips label", { hasText: /^CSS$/ }).click();
    assert.ok(await page.locator(".demo-stage li:visible", { hasText: "CSS Modules and Tailwind" }).isVisible());
    assert.ok(!(await page.locator(".demo-stage li:visible", { hasText: "Go components and API routes" }).isVisible()));
    await page.locator(".cz-q:visible", { hasText: "Where does the state live" }).click();
    assert.ok(await page.locator(".demo-stage").getByText("In the DOM: a hidden radio").isVisible());
    await page.locator(".cz-q:visible", { hasText: "Where does the state live" }).click();
    assert.ok(!(await page.locator(".demo-stage").getByText("In the DOM: a hidden radio").isVisible()), "re-clicking closes the accordion");
    await page.locator(".cz-chips label", { hasText: /^3$/ }).click();
    await page.waitForTimeout(450);
    const w = await page.locator(".cz-fill").evaluate((el) => el.getBoundingClientRect().width / el.parentElement.getBoundingClientRect().width);
    assert.ok(w > 0.95, "the range fill reaches 100%: " + w);
  },
  async compiled(page) {
    await page.getByRole("button", { name: "Click me" }).click();
    await page.getByRole("button", { name: "Click me" }).click();
    assert.equal((await page.locator(".cm-label strong").innerText()).trim(), "2");
    await page.locator(".co-tabs label", { hasText: "Page JS out" }).click();
    assert.ok(await page.getByText("kbindText").first().isVisible(), "the JS panel shows the hydration script");
    await page.locator(".co-tabs label", { hasText: "HTML out" }).click();
    assert.ok(await page.getByText("<!--k:").first().isVisible());
  },
  async tasks(page) {
    await page.evaluate(() => localStorage.clear());
    await page.reload();
    await page.getByRole("button", { name: "Start Ship it" }).click();
    await page.waitForFunction(() => document.querySelector('[aria-label="Doing"] .tk-n').textContent.trim() === "2");
    await page.getByPlaceholder("Add a task").fill("Write tests");
    await page.getByRole("button", { name: "Add", exact: true }).click();
    await page.locator('[aria-label="To do"]').getByText("Write tests").waitFor();
    await page.reload();
    await page.locator('[aria-label="To do"]').getByText("Write tests").waitFor();
    assert.equal((await page.locator('[aria-label="Doing"] .tk-n').innerText()).trim(), "2", "the board survives a reload");
    await page.getByRole("button", { name: "Delete Write tests" }).click();
    await page.waitForFunction(() => !document.body.innerText.includes("Write tests"));
  },
  async form(page) {
    const submit = page.getByRole("button", { name: "Create account" });
    assert.ok(await submit.isDisabled());
    await page.locator("#fm-email").fill("nope");
    assert.match(await page.locator("#fm-email-msg").innerText(), /does not look like an email/);
    await page.locator("#fm-email").fill("a@b.co");
    assert.match(await page.locator("#fm-email-msg").innerText(), /Looks good/);
    await page.locator("#fm-pw").fill("Abcdef1!x");
    assert.match(await page.locator("#fm-pw-msg").innerText(), /Strong/);
    await page.locator(".fm input[type=range]").fill("20");
    assert.match(await page.locator(".fm-msg", { hasText: "seats" }).innerText(), /20 seats: \$240/);
    await page.getByLabel("I agree to the terms").check();
    await page.locator(".krate-otp-field-input").first().click();
    await page.keyboard.type("424242");
    await page.waitForFunction(() => !document.querySelector(".fm .dk-btn-primary").disabled);
    await submit.click();
    assert.match(await page.locator(".fm-done").innerText(), /Account created for a@b\.co/);
  },
  async data(page) {
    await page.locator(".dt-state strong", { hasText: "ready" }).waitFor();
    await page.getByRole("button", { name: "dialog" }).click();
    await page.locator(".dt-card h2", { hasText: "Dialog" }).waitFor();
    await page.getByLabel("Break the request").check();
    await page.locator(".dt-error").waitFor();
    assert.match(await page.locator(".dt-error").innerText(), /HTTP 404/);
    await page.getByRole("button", { name: "Try again" }).click();
    await page.locator(".dt-card h2", { hasText: "Dialog" }).waitFor();
  },
  async overlays(page, shot) {
    await page.keyboard.press("Alt+p");
    const input = page.getByPlaceholder("Type a command...");
    await input.waitFor();
    await input.fill("toast");
    await page.waitForTimeout(600);
    await shot("overlays-palette");
    await page.keyboard.press("Enter");
    await page.locator(".ov-last strong", { hasText: "success toast" }).waitFor();
    await page.locator(".krate-toast").first().waitFor();
    await page.getByRole("button", { name: "Sheet" }).first().click();
    await page.locator(".krate-sheet-description").waitFor();
    await page.keyboard.press("Escape");
    await page.locator(".krate-sheet-description").waitFor({ state: "hidden" });
  },
  async transitions(page) {
    await page.getByRole("link", { name: "Ocean" }).click();
    await page.waitForURL("**/demos/transitions/ocean/");
    await page.getByRole("heading", { name: "Ocean", level: 2 }).waitFor();
    await page.getByRole("link", { name: "Back to the grid" }).click();
    await page.waitForURL("**/demos/transitions/");
  },
  async canvas(page) {
    await page.waitForFunction(() => /fps/.test(document.querySelector(".cv-fps").textContent) && parseInt(document.querySelector(".cv-fps").textContent, 10) > 0, null, { timeout: 8000 });
    const painted = () => page.evaluate(() => {
      const c = document.querySelector("canvas");
      const d = c.getContext("2d").getImageData(0, 0, c.width, c.height).data;
      let n = 0;
      for (let i = 3; i < d.length; i += 4) if (d[i]) n++;
      return n;
    });
    assert.ok((await painted()) > 100, "the canvas has dots");
    await page.locator(".cv-run").click();
    assert.equal((await page.locator(".cv-fps").innerText()).trim(), "paused");
  },
  async chart(page) {
    const before = await page.locator(".ch-stats").innerText();
    await page.waitForFunction((b) => document.querySelector(".ch-stats").innerText !== b, before, { timeout: 8000 });
    await page.locator(".ch-live").click();
    assert.equal(await page.locator(".ch-live").getAttribute("aria-pressed"), "false");
    await page.locator(".ch-field input").fill("20");
    assert.equal((await page.locator(".ch-field output").innerText()).trim(), "20");
    assert.equal((await page.locator(".ch-line").getAttribute("points")).split(" ").length, 20);
  },
  async context(page) {
    await page.evaluate(() => localStorage.clear());
    await page.reload();
    await page.getByRole("button", { name: "Add Mouse to the cart" }).click();
    await page.getByRole("button", { name: "Add Mouse to the cart" }).click();
    await page.getByRole("button", { name: "Add Monitor to the cart" }).click();
    assert.match(await page.locator(".cx-badge").innerText(), /3 items/);
    assert.match(await page.locator(".cx-total").innerText(), /\$347/);
    await page.getByRole("button", { name: "Show prices in EUR" }).click();
    await page.waitForFunction(() => document.querySelector(".cx-total").innerText.includes("EUR 347"));
    await page.reload();
    assert.match(await page.locator(".cx-total").innerText(), /\$347/, "the cart survives a reload (the symbol is not persisted)");
    await page.getByRole("button", { name: "Remove mouse" }).click();
    assert.match(await page.locator(".cx-total").innerText(), /\$249/);
  },
};

const hasHorizontalScroll = (page) => page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1);
const desktop = { viewport: { width: 1280, height: 900 } };
const phone = { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 2 };

try {
  // ---- Homepage ----
  step = "homepage";
  const ctx = await browser.newContext({ ...desktop, colorScheme: "light" });
  {
    const { page, errors } = await open(ctx, "/");
    assert.match(await page.title(), /Krate/);
    assert.equal((await page.locator(".ht-panel:visible").count()), 1, "one hero example at a time");
    await page.getByRole("button", { name: "Increase" }).click();
    await page.getByRole("button", { name: "Increase" }).click();
    assert.equal((await page.locator(".demo-value").innerText()).trim(), "2");
    await page.locator(".ht-tabs label", { hasText: "CSS signals" }).click();
    await page.locator(".ts-seg label", { hasText: "Dark" }).click();
    assert.ok(await page.locator(".ts-dark").isVisible() && !(await page.locator(".ts-light").isVisible()));
    await page.locator(".ht-tabs label", { hasText: "Resources" }).click();
    await page.locator(".fd-pick button", { hasText: "dialog" }).click();
    await page.getByText("Modal with focus trap").waitFor();
    await page.locator(".pl-steps label", { hasText: "Static HTML" }).click();
    assert.ok(await page.getByText("<!--k:").first().isVisible(), "the pipeline shows the emitted HTML");
    await page.locator(".bc-tabs label", { hasText: "API requests" }).click();
    assert.ok(await page.locator(".bc-table:visible").getByText("Next.js - route handler").isVisible());
    assert.ok((await page.locator(".bc-table:visible tbody tr").count()) >= 5);
    await page.locator(".bc-tabs label", { hasText: "Client JS" }).click();
    assert.ok(await page.locator(".bc-table:visible").getByRole("row", { name: /Krate/ }).first().isVisible());
    assert.ok((await page.locator(".home-demos a").count()) >= 6, "demo teasers");
    assert.ok(await page.getByRole("link", { name: "Demos", exact: true }).first().isVisible(), "header nav has Demos");
    noErrors("homepage", errors);
    log("homepage: tabs, counter, resource, pipeline, benchmark chart and teasers work");
    await page.close();
  }

  // ---- Gallery ----
  step = "gallery";
  {
    const { page, errors } = await open(ctx, "/demos/");
    assert.equal(await page.locator(".demo-card").count(), slugs.length);
    for (const s of slugs) assert.ok(await page.locator(`a.demo-card[href="/demos/${s}/"]`).count(), "card for " + s);
    noErrors("gallery", errors);
    log(`gallery lists ${slugs.length} demos`);
    await page.close();
  }

  // ---- Every demo ----
  for (const slug of slugs.filter(wanted)) {
    step = "demo " + slug;
    const { page, errors } = await open(ctx, `/demos/${slug}/`);
    assert.ok(await page.locator("h1").count(), slug + ": has an h1");
    assert.ok(await page.locator(".demo-source pre").count(), slug + ": shows its source");
    const playgroundLink = await page.locator(".demo-notes a.btn").getAttribute("href");
    assert.equal(playgroundLink, `/playground/#example=demo-${slug}`);
    assert.ok(tests[slug], "a test exists for " + slug);
    const shot = (name) => page.screenshot({ path: SHOT(name), fullPage: false });
    await tests[slug](page, shot);
    noErrors(slug, errors);
    log(`demo ${slug}`);
    await page.close();
  }

  // ---- Page scripts: zero-JS demos ship no demo code ----
  step = "zero-js";
  for (const d of demos.filter((x) => x.js === "zero").filter((x) => wanted(x.slug))) {
    const files = pageScripts(`demos/${d.slug}`);
    for (const f of files) {
      const text = readFileSync(join(dist, "demos", d.slug, f), "utf8");
      assert.ok(!/createCSS|createSignal|kbindText|kbindList/.test(text), `${d.slug}: page script contains no demo code`);
    }
    log(`${d.slug}: the page script holds only site chrome (${files.length} file)`);
  }

  // ---- JavaScript disabled ----
  step = "no javascript";
  {
    const off = await browser.newContext({ ...desktop, javaScriptEnabled: false });
    const { page } = await open(off, "/demos/css-signals/");
    await page.locator(".cz-seg label", { hasText: "Yearly" }).click();
    assert.match(await page.locator(".cz-amount").evaluate((el) => getComputedStyle(el, "::after").content), /\$9/);
    await page.locator(".cz-chips label", { hasText: /^Go$/ }).click();
    assert.ok(await page.locator(".demo-stage li:visible", { hasText: "Go components and API routes" }).isVisible());
    await page.locator(".cz-q:visible", { hasText: "Can it be closed again" }).click();
    assert.ok(await page.locator(".demo-stage").getByText("Clicking an open question closes it.").isVisible());
    await page.locator(".cz-chips label", { hasText: /^2$/ }).click();
    assert.ok((await page.locator(".cz-fill").evaluate((el) => el.getBoundingClientRect().width)) > 20);
    await page.close();
    const home = await open(off, "/");
    await home.page.locator(".ht-tabs label", { hasText: "CSS signals" }).click();
    await home.page.locator(".ts-seg label", { hasText: "Dark" }).click();
    assert.ok(await home.page.locator(".ts-dark").isVisible());
    await home.page.locator(".pl-steps label", { hasText: "Tiny script" }).click();
    assert.ok(await home.page.getByText("kbindText").first().isVisible());
    await home.page.locator(".bc-tabs label", { hasText: "API requests" }).click();
    assert.ok(await home.page.locator(".bc-table:visible").count());
    await home.page.close();
    const tx = await open(off, "/demos/transitions/");
    await tx.page.getByRole("link", { name: "Forest" }).click();
    await tx.page.getByRole("heading", { name: "Forest", level: 2 }).waitFor();
    await tx.page.close();
    await off.close();
    log("with JavaScript disabled: CSS-signal demo, homepage tabs, pipeline, chart and transitions links work");
  }

  // ---- No horizontal overflow on a phone ----
  step = "mobile overflow";
  {
    const m = await browser.newContext({ ...phone, colorScheme: "light" });
    for (const path of ["/", "/demos/", ...slugs.filter(wanted).map((s) => `/demos/${s}/`)]) {
      const { page } = await open(m, path);
      await page.waitForTimeout(150);
      assert.ok(!(await hasHorizontalScroll(page)), `${path}: no horizontal scroll at 390px`);
      await page.close();
    }
    await m.close();
    log("no horizontal overflow at 390px on the homepage, the gallery and every demo");
  }

  // ---- Screenshots ----
  step = "screenshots";
  const wantShots = [
    ["home", "/"],
    ["demos-index", "/demos/"],
    ["demo-reactivity", "/demos/reactivity/"],
    ["demo-css-signals", "/demos/css-signals/"],
    ["demo-tasks", "/demos/tasks/"],
    ["demo-canvas", "/demos/canvas/"],
    ["demo-compiled", "/demos/compiled/"],
  ];
  for (const [scheme, view, name] of [["light", desktop, "desktop-light"], ["dark", desktop, "desktop-dark"], ["light", phone, "mobile-light"], ["dark", phone, "mobile-dark"]]) {
    const c = await browser.newContext({ ...view, colorScheme: scheme });
    for (const [label, path] of wantShots) {
      const { page } = await open(c, path);
      await page.waitForTimeout(400);
      await page.screenshot({ path: SHOT(`${label}-${name}`), fullPage: true });
      await page.close();
    }
    await c.close();
  }
  log("screenshots written to " + shots);
  await ctx.close();
  console.log("demos e2e passed");
} catch (err) {
  console.error(`demos-e2e failed during "${step}":`, err);
  process.exitCode = 1;
} finally {
  await browser.close();
  server.close();
}
