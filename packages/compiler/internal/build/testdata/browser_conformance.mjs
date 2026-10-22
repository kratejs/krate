import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { chromium, firefox, webkit } from "playwright";

const root = process.argv[2];
const browserName = process.argv[3] || "chromium";
// Base URL of a running `krate serve` for routes that need the server
// (server actions). Empty skips those routes.
const actionsBase = process.argv[4] || "";
const port = 4189;

if (!root) {
  console.error("usage: browser_conformance.mjs <dist-root> [chromium|firefox|webkit]");
  process.exit(2);
}

const browsers = { chromium, firefox, webkit };
const browserType = browsers[browserName];
if (!browserType) {
  console.error(`unsupported browser ${browserName}`);
  process.exit(2);
}

const mime = {
  ".html": "text/html",
  ".js": "text/javascript",
  ".css": "text/css",
  ".json": "application/json",
  ".svg": "image/svg+xml",
  ".woff2": "font/woff2",
};

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

const baseURL = `http://127.0.0.1:${port}`;
const failures = [];
let browser;

function fail(message) {
  failures.push(message);
}

try {
  await new Promise((resolve) => server.listen(port, "127.0.0.1", resolve));
  browser = await browserType.launch({ headless: true });
  const context = await browser.newContext();
  const page = await context.newPage();
  const errors = [];
  page.on("console", (msg) => {
    if (msg.type() !== "error") return;
    // Resource 404s are reported with their URL by the response listener below.
    if (msg.text().startsWith("Failed to load resource")) return;
    errors.push(msg.text());
  });
  page.on("pageerror", (err) => errors.push(err.message));
  page.on("response", (response) => {
    if (response.status() < 400) return;
    const url = response.url();
    if (url.endsWith("/analytics.js") || url.endsWith("/favicon.ico")) return;
    errors.push(`${response.status()} ${url}`);
  });

  const go = async (route, waitFor) => {
    await page.goto(baseURL + route, { waitUntil: "networkidle" });
    if (waitFor) await page.waitForSelector(waitFor, { timeout: 8000 });
  };

  // React source + compiled Radix islands.
  await go("/react-demo/", "[role=switch]");
  await page.waitForTimeout(1500);
  const before = await page.evaluate(() => ({
    state: document.querySelector("[role=switch]")?.getAttribute("data-state"),
    label: (document.body.innerText.match(/Live switch \w+/) || [])[0],
    timer: (document.body.innerText.match(/timer: \d+s/) || [])[0],
    classTick: Number((document.body.innerText.match(/Class component ticked (\d+) times/) || [])[1]),
  }));
  await page.locator("[role=switch]").click();
  await page.getByRole("button", { name: "tick", exact: true }).click();
  const after = await page.evaluate(() => ({
    state: document.querySelector("[role=switch]")?.getAttribute("data-state"),
    label: (document.body.innerText.match(/Live switch \w+/) || [])[0],
    classTick: Number((document.body.innerText.match(/Class component ticked (\d+) times/) || [])[1]),
  }));
  const tooltipTrigger = page.getByRole("button", { name: "Hover me" });
  await tooltipTrigger.scrollIntoViewIfNeeded();
  await tooltipTrigger.focus();
  let tooltip = null;
  for (let attempt = 0; attempt < 3 && !tooltip; attempt++) {
    await tooltipTrigger.dispatchEvent("pointerenter");
    await page.waitForTimeout(500);
    tooltip = await page.locator("[role=tooltip]").textContent().catch(() => null);
  }
  const tooltipDebug = {
    count: await tooltipTrigger.count(),
    state: await tooltipTrigger.getAttribute("data-state").catch(() => null),
    tooltips: await page.locator("[role=tooltip]").count(),
  };

  if (before.state !== "checked") fail(`switch initial state: ${before.state}`);
  if (before.label !== "Live switch on") fail(`switch initial label: ${before.label}`);
  if (!before.timer) fail("timer did not start");
  if (after.state !== "unchecked") fail(`switch state after click: ${after.state}`);
  if (after.label !== "Live switch off") fail(`switch label after click: ${after.label}`);
  if (!(after.classTick > before.classTick)) fail(`class component tick: ${before.classTick} -> ${after.classTick}`);
  if (tooltip !== "Tooltip content") fail(`tooltip content: ${tooltip}`);

  // React source dashboard: controlled input, derived list, dynamic insertion.
  await go("/react-dashboard/", 'input[placeholder="Filter reports..."]');
  const dashboardRows = () => page.locator("[data-testid=dashboard-row]").count();
  if ((await dashboardRows()) !== 3) fail(`dashboard initial rows: ${await dashboardRows()}`);
  await page.locator('input[placeholder="Filter reports..."]').fill("beta");
  await page.waitForTimeout(50);
  if ((await dashboardRows()) !== 1) fail(`dashboard filtered rows: ${await dashboardRows()}`);
  if (!(await page.locator("[data-testid=dashboard-row]").textContent())?.includes("Beta migration")) {
    fail("dashboard filter did not select Beta migration");
  }
  await page.locator('input[placeholder="Filter reports..."]').fill("");
  await page.getByRole("button", { name: "Add report" }).click();
  if ((await dashboardRows()) !== 4) fail(`dashboard rows after add: ${await dashboardRows()}`);
  if ((await page.locator("[data-testid=dashboard-total]").textContent()) !== "4") {
    fail(`dashboard total after add: ${await page.locator("[data-testid=dashboard-total]").textContent()}`);
  }

  // React source forms: text/select/checkbox/radio/textarea/submit.
  await go("/react-forms/", 'input[placeholder="Ada Lovelace"]');
  await page.locator('input[placeholder="Ada Lovelace"]').fill("Ada");
  await page.locator("[data-testid=form-role]").selectOption("designer");
  await page.locator("[data-testid=form-subscribe]").check();
  await page.locator("[data-testid=form-plan-enterprise]").check();
  await page.locator("[data-testid=form-notes]").fill("ship it");
  await page.getByRole("button", { name: "Submit" }).click();
  const formResult = await page.locator("[data-testid=form-result]").textContent();
  if (formResult !== "Ada|designer|subscribed|enterprise|ship it") {
    fail(`form result: ${formResult}`);
  }

  // React Suspense, lazy, use() and transitions. Promises in the page resolve
  // after 300-900ms, so the fallbacks are checked right after the DOM loads.
  const suspenseLoads = [];
  page.on("request", (req) => suspenseLoads.push(req.url()));
  await page.goto(baseURL + "/react-suspense/", { waitUntil: "domcontentloaded" });
  const text = (id) =>
    page.evaluate((id) => document.querySelector(`#root [data-testid="${id}"]`)?.textContent ?? null, id);
  const waitText = (id, want) =>
    page
      .waitForFunction(
        ([id, want]) => document.querySelector(`#root [data-testid="${id}"]`)?.textContent === want,
        [id, want],
        { timeout: 8000 },
      )
      .catch(() => fail(`suspense: ${id} never became ${JSON.stringify(want)}`));
  const suspense = {
    initialFallback: await text("use-fallback"),
    initialContent: await text("use-content"),
    siblingsFallback: await text("siblings-fallback"),
    outerShell: await text("outer-shell"),
    outerFallback: await text("outer-fallback"),
    innerFallback: await text("inner-fallback"),
  };
  if (suspense.initialFallback !== "Loading greeting..." || suspense.initialContent !== null) {
    fail(`suspense: expected the fallback before use() resolves: ${JSON.stringify(suspense)}`);
  }
  if (suspense.outerShell !== "outer shell" || suspense.outerFallback !== null || suspense.innerFallback !== "Loading inner...") {
    fail(`suspense: nested boundaries must suspend independently: ${JSON.stringify(suspense)}`);
  }
  await waitText("sibling-fast", "fast");
  if ((await text("sibling-slow")) !== "slow") fail("suspense: siblings must reveal together");
  await waitText("use-content", "Hello from use()");
  await waitText("inner-content", "nested content");
  await waitText("source-content", "first");
  if (!suspenseLoads.some((u) => /\/react-suspense\/lazy-LazyPanel\.[a-z0-9]+\.js$/.test(u))) {
    fail("suspense: lazy component chunk was not loaded");
  }
  await waitText("lazy-panel", "Lazy panel: 0");
  await page.locator("[data-testid=lazy-panel]").click();
  await waitText("lazy-panel", "Lazy panel: 1");
  await page.locator("[data-testid=source-urgent]").click();
  await page.waitForTimeout(30);
  if ((await text("source-fallback")) !== "Loading source...") fail("suspense: urgent update must show the fallback again");
  await waitText("source-content", "urgent");
  await page.locator("[data-testid=source-transition]").click();
  await page.waitForTimeout(30);
  const during = { content: await text("source-content"), fallback: await text("source-fallback"), pending: await text("transition-pending") };
  if (during.content !== "urgent" || during.fallback !== null || during.pending !== "pending") {
    fail(`suspense: a transition must keep the revealed content: ${JSON.stringify(during)}`);
  }
  await waitText("source-content", "transition");
  await waitText("transition-pending", "idle");
  await page.locator("[data-testid=action-start]").click();
  if ((await text("action-pending")) !== "pending") fail("suspense: async action must keep isPending true");
  await waitText("action-result", "saved");
  await waitText("action-pending", "idle");
  await page.locator("[data-testid=deferred-input]").fill("abc");
  await waitText("deferred-value", "abc");
  await page.locator("[data-testid=store-bump]").click();
  await page.locator("[data-testid=store-bump]").click();
  await waitText("store-value", "2");
  // Server actions: direct call through the client proxy, a form action and a
  // button formAction, all through POST /__krate/action/<id>.
  let actions = null;
  if (actionsBase) {
    await page.goto(actionsBase + "/react-actions/", { waitUntil: "networkidle" });
    await page.waitForSelector("[data-testid=feedback-form]", { timeout: 8000 });
    await page.getByRole("button", { name: "Run action directly" }).click();
    await page
      .waitForFunction(() => document.querySelector("[data-testid=direct-result]")?.textContent?.includes(":"), null, { timeout: 8000 })
      .catch(() => {});
    const direct = await page.locator("[data-testid=direct-result]").textContent();
    if (direct !== "direct call:11") fail(`server action direct result: ${direct}`);

    // A successful form action reloads the page, which discards the response
    // body, so capture it while proxying the request.
    const submitAction = async (formTestId) => {
      let captured = null;
      await page.route("**/__krate/action/**", async (route) => {
        const response = await route.fetch();
        captured = { status: response.status(), body: await response.text() };
        await route.fulfill({ response });
      });
      await page.locator(`[data-testid=${formTestId}] button[type=submit]`).click();
      for (let i = 0; i < 80 && !captured; i++) await page.waitForTimeout(100);
      await page.unroute("**/__krate/action/**");
      await page.waitForLoadState("networkidle");
      let body = null;
      try {
        body = JSON.parse(captured?.body || "");
      } catch {}
      return { status: captured?.status, ok: body?.ok === true, text: body?.result?.text };
    };
    const form = await submitAction("feedback-form");
    if (form.status !== 200 || !form.ok || form.text !== "hello from a form") {
      fail(`server action form: ${JSON.stringify(form)}`);
    }
    const formAction = await submitAction("formaction-form");
    if (formAction.status !== 200 || !formAction.ok || formAction.text !== "submitted by formAction") {
      fail(`server action formAction: ${JSON.stringify(formAction)}`);
    }
    actions = { direct, form, formAction };
  }

  // React effects: phase order, children before parents, Object.is deps,
  // batching, and cleanups when a conditional branch hides.
  await go("/react-effects/", "[data-testid=effects-inc]");
  await page.waitForTimeout(200);
  const effectLog = () => page.evaluate(() => (window.__effectLog || []).splice(0));
  const expectLog = async (step, want) => {
    const got = await effectLog();
    if (JSON.stringify(got) !== JSON.stringify(want)) {
      fail(`effects ${step}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
    }
  };
  await expectLog("mount", [
    "child insertion",
    "child layout",
    "parent layout",
    "child passive",
    "parent passive 0",
    "nan effect",
  ]);
  await page.locator("[data-testid=effects-inc]").click();
  await page.waitForTimeout(100);
  await expectLog("batched update", ["parent passive cleanup", "parent passive 2"]);
  const doubled = await page.locator("[data-testid=effects-doubled]").textContent();
  if (doubled !== "4") fail(`effects useMemo: ${doubled}`);
  await page.locator("[data-testid=effects-nan]").click();
  await page.waitForTimeout(100);
  await expectLog("Object.is bail-out", []);
  await page.locator("[data-testid=effects-toggle]").click();
  await page.waitForTimeout(100);
  await expectLog("hide branch", ["child layout cleanup", "child passive cleanup"]);
  await page.locator("[data-testid=effects-toggle]").click();
  await page.waitForTimeout(100);
  await expectLog("show branch", ["child insertion", "child layout", "child passive"]);

  // React events, forms and refs: delegation order, native interleaving,
  // controlled restore, enter/leave, focus bubbling, refs and handles.
  await go("/react-events/", "[data-testid=order-button]");
  await page.waitForTimeout(100);
  const eventLog = () => page.locator("[data-testid=event-log]").textContent();
  await page.locator("[data-testid=clear-log]").click();
  await page.locator("[data-testid=order-button]").click();
  const order = await eventLog();
  const wantOrder =
    "outer-capture;middle-capture;native-inner;button:click:self;middle-bubble;outer-bubble;native-document;";
  if (order !== wantOrder) fail(`event order: ${order}`);
  await page.locator("[data-testid=stop-toggle]").check();
  await page.locator("[data-testid=clear-log]").click();
  await page.locator("[data-testid=order-button]").click();
  const stopped = await eventLog();
  if (stopped !== "outer-capture;middle-capture;native-inner;button:click:self;") fail(`stopPropagation order: ${stopped}`);

  await page.locator("[data-testid=upper]").fill("ab");
  const upper = await page.locator("[data-testid=upper]").inputValue();
  if (upper !== "AB") fail(`controlled transform: ${upper}`);
  await page.locator("[data-testid=locked]").pressSequentially("xyz");
  const locked = await page.locator("[data-testid=locked]").inputValue();
  if (locked !== "locked") fail(`controlled restore: ${locked}`);
  await page.locator("[data-testid=agree]").click();
  if (await page.locator("[data-testid=agree]").isChecked()) fail("controlled checkbox was not restored");

  const hoverCount = async () => Number(await page.locator("[data-testid=hover-count]").textContent());
  await page.locator("[data-testid=hover-box]").scrollIntoViewIfNeeded();
  await page.mouse.move(0, 0);
  const hover0 = await hoverCount();
  const box = await page.locator("[data-testid=hover-box]").boundingBox();
  await page.mouse.move(box.x + 5, box.y + box.height / 2);
  const hover1 = await hoverCount();
  await page.mouse.move(0, 0);
  const hover2 = await hoverCount();
  if (hover1 !== hover0 + 1 || hover2 !== hover1 + 10) fail(`mouseenter/mouseleave: ${hover0} -> ${hover1} -> ${hover2}`);

  await page.locator("[data-testid=ref-input]").click();
  await page.locator("[data-testid=other-input]").click();
  const focusLog = await page.locator("[data-testid=focus-log]").textContent();
  if (focusLog !== "focus:ref-input;blur:ref-input;focus:other-input;") fail(`focus bubbling: ${focusLog}`);
  await page.locator("[data-testid=focus-ref]").click();
  const focused = await page.evaluate(() => document.activeElement?.getAttribute("data-testid"));
  if (focused !== "ref-input") fail(`ref as prop focus: ${focused}`);
  const layoutSaw = await page.locator("[data-testid=layout-saw]").textContent();
  if (layoutSaw !== "INPUT") fail(`layout effect saw ref: ${layoutSaw}`);

  await page.locator("[data-testid=bump]").click();
  const handleCount = await page.locator("[data-testid=handle-count]").textContent();
  if (handleCount !== "1") fail(`useImperativeHandle: ${handleCount}`);

  const refLog = () => page.locator("[data-testid=ref-log]").textContent();
  const refInitial = await refLog();
  await page.locator("[data-testid=toggle-ref]").click();
  const refHidden = await refLog();
  await page.locator("[data-testid=toggle-ref]").click();
  const refShown = await refLog();
  if (refInitial !== "attach:EM;" || refHidden !== "attach:EM;cleanup;" || refShown !== "attach:EM;cleanup;attach:EM;") {
    fail(`callback ref cleanup: ${refInitial} / ${refHidden} / ${refShown}`);
  }

  await page.locator("[data-testid=uncontrolled]").fill("changed");
  await page.locator("[data-testid=reset]").click();
  const resetValue = await page.locator("[data-testid=uncontrolled]").inputValue();
  if (resetValue !== "start") fail(`form reset: ${resetValue}`);

  if (errors.length) fail("console errors: " + errors.join(" | "));

  console.log(JSON.stringify({ browser: browserName, before, after, tooltip, tooltipDebug, formResult, suspense, actions, errors, failures }, null, 2));
  if (failures.length) process.exitCode = 1;
} catch (err) {
  console.error(err?.stack || String(err));
  process.exitCode = 1;
} finally {
  if (browser) await browser.close();
  server.close();
}
