import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { chromium, firefox, webkit } from "playwright";

const root = process.argv[2];
const browserName = process.argv[3] || "chromium";
// Optional origin of the server-action endpoint (Go action handler in front of
// the Node sidecar). /__krate/action/* requests are proxied to it.
const actionOrigin = process.argv[4] || "";
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
  if (actionOrigin && req.url.startsWith("/__krate/action/")) {
    const target = new URL(req.url, actionOrigin);
    const upstream = http.request(target, { method: req.method, headers: req.headers }, (ures) => {
      res.writeHead(ures.statusCode || 502, ures.headers);
      ures.pipe(res);
    });
    upstream.on("error", (err) => {
      res.statusCode = 502;
      res.end(String(err));
    });
    req.pipe(upstream);
    return;
  }
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

  // React 19 actions: useActionState + useFormStatus + useOptimistic on the
  // server-action transport, without page reloads.
  let actions = { skipped: !actionOrigin };
  if (actionOrigin) {
    const text = (id) => page.locator(`[data-testid=${id}]`).textContent();
    const waitText = async (id, value) => {
      try {
        await page.waitForFunction(
          ([sel, v]) => document.querySelector(sel)?.textContent === v,
          [`[data-testid=${id}]`, value],
          { timeout: 8000 },
        );
      } catch {
        throw new Error(`[data-testid=${id}] is ${JSON.stringify(await text(id))}, want ${JSON.stringify(value)}; errors: ${errors.join(" | ")}`);
      }
    };
    await go("/react-actions/", "[data-testid=todo-form]");
    await page.evaluate(() => {
      window.__krateNoReload = true;
    });

    await page.locator("[data-testid=todo-title]").fill("write docs");
    await page.locator("[data-testid=todo-submit]").click();
    await waitText("todo-submit", "Working...");
    const todoPending = {
      label: await text("todo-submit"),
      disabled: await page.locator("[data-testid=todo-submit]").isDisabled(),
      otherForm: await text("message-submit"),
      saving: await text("todo-pending"),
    };
    await waitText("todo-count", "1");
    await waitText("todo-submit", "Add todo");
    const todoAfter = {
      last: await text("todo-last"),
      input: await page.locator("[data-testid=todo-title]").inputValue(),
    };
    await page.locator("[data-testid=todo-title]").fill("ship it");
    await page.locator("[data-testid=todo-submit]").click();
    await waitText("todo-count", "2");
    const todoSecond = await text("todo-last");

    await page.locator("[data-testid=message-text]").fill("hi");
    await page.locator("[data-testid=message-submit]").click();
    await waitText("message-last", "hi (sending)");
    const optimisticCount = await text("message-count");
    await waitText("message-last", "hi");
    const messageFinal = await text("message-count");

    await page.locator("[data-testid=feedback-text]").fill("changed");
    const feedbackResponse = page.waitForResponse((r) => r.url().includes("/__krate/action/") && r.request().method() === "POST");
    await page.locator("[data-testid=feedback-form] button").click();
    const feedbackStatus = (await feedbackResponse).status();
    await page.waitForFunction(() => !document.querySelector("[data-testid=feedback-form]")?.hasAttribute("data-krate-action-pending"));
    const feedbackInput = await page.locator("[data-testid=feedback-text]").inputValue();

    await page.getByRole("button", { name: "Run action directly" }).click();
    await waitText("direct-result", "direct call:11:fetch");
    const noReload = await page.evaluate(() => window.__krateNoReload === true);

    // Progressive enhancement: the same forms post natively without JS.
    const noJS = await browser.newContext({ javaScriptEnabled: false });
    const plain = await noJS.newPage();
    await plain.goto(baseURL + "/react-actions/");
    await plain.locator("[data-testid=todo-title]").fill("no js");
    const [nojsTodo] = await Promise.all([
      plain.waitForResponse((r) => r.url().includes("/__krate/action/")),
      plain.locator("[data-testid=todo-submit]").click(),
    ]);
    await plain.waitForLoadState("load");
    const nojsTodoURL = plain.url();
    await plain.goto(baseURL + "/react-actions/");
    const [nojsFeedback] = await Promise.all([
      plain.waitForResponse((r) => r.url().includes("/__krate/action/")),
      plain.locator("[data-testid=feedback-form] button").click(),
    ]);
    await plain.waitForLoadState("load");
    const nojsFeedbackURL = plain.url();
    await noJS.close();

    actions = {
      todoPending,
      todoAfter,
      todoSecond,
      optimisticCount,
      messageFinal,
      feedbackStatus,
      feedbackInput,
      noReload,
      nojs: { todo: nojsTodo.status(), todoURL: nojsTodoURL, feedback: nojsFeedback.status(), feedbackURL: nojsFeedbackURL },
    };
    if (todoPending.label !== "Working..." || !todoPending.disabled) fail(`useFormStatus pending: ${JSON.stringify(todoPending)}`);
    if (todoPending.otherForm !== "Send") fail(`useFormStatus leaked to another form: ${todoPending.otherForm}`);
    if (todoPending.saving !== " (saving)") fail(`useActionState isPending: ${todoPending.saving}`);
    if (todoAfter.last !== "write docs") fail(`useActionState state: ${todoAfter.last}`);
    if (todoAfter.input !== "") fail(`form not reset after success: ${todoAfter.input}`);
    if (todoSecond !== "ship it") fail(`useActionState second call: ${todoSecond}`);
    if (optimisticCount !== "2") fail(`optimistic count while pending: ${optimisticCount}`);
    if (messageFinal !== "2") fail(`message count after settle: ${messageFinal}`);
    if (feedbackStatus !== 200) fail(`enhanced server action form status: ${feedbackStatus}`);
    if (feedbackInput !== "hello from a form") fail(`server action form not reset: ${feedbackInput}`);
    if (!noReload) fail("a form action reloaded the page");
    if (nojsTodo.status() !== 303 || !nojsTodoURL.endsWith("/react-actions/")) fail(`no-JS useActionState post: ${JSON.stringify(actions.nojs)}`);
    if (nojsFeedback.status() !== 303 || !nojsFeedbackURL.endsWith("/react-actions/")) fail(`no-JS server action post: ${JSON.stringify(actions.nojs)}`);
  }

  if (errors.length) fail("console errors: " + errors.join(" | "));

  console.log(JSON.stringify({ browser: browserName, before, after, tooltip, tooltipDebug, formResult, actions, errors, failures }, null, 2));
  if (failures.length) process.exitCode = 1;
} catch (err) {
  console.error(err?.stack || String(err));
  process.exitCode = 1;
} finally {
  if (browser) await browser.close();
  server.close();
}
