// Browser driver for the React browser differential (react_browser_diff_test.go).
//
// Usage: node run.mjs <plan.json> <out.json>
//
// The plan lists fixtures, each with a React URL, a Krate URL and a list of
// interaction steps. Relative URLs are served from plan.root by a static
// server started here; absolute URLs (server-action fixtures) point at a real
// `krate serve` instance. Every fixture side runs in a fresh browser context.
// After page load and after every step the driver records a snapshot:
// normalized DOM of #root (plus body-level portal content), form control
// state, the focused element, new window.__log entries and new uncaught
// errors. Comparison happens in Go.
import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { chromium, firefox, webkit } from "playwright";

const [planPath, outPath] = process.argv.slice(2);
if (!planPath || !outPath) {
  console.error("usage: run.mjs <plan.json> <out.json>");
  process.exit(2);
}
const plan = JSON.parse(fs.readFileSync(planPath, "utf8"));
const browserType = { chromium, firefox, webkit }[plan.browser || "chromium"];
if (!browserType) {
  console.error(`unsupported browser ${plan.browser}`);
  process.exit(2);
}

const mime = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json",
  ".svg": "image/svg+xml",
};

const server = http.createServer((req, res) => {
  let p = decodeURIComponent(req.url.split("?")[0]);
  if (p.endsWith("/")) p += "index.html";
  else if (!path.extname(p)) p += "/index.html";
  const file = path.join(plan.root, path.normalize(p));
  if (!file.startsWith(path.resolve(plan.root))) {
    res.statusCode = 403;
    res.end("forbidden");
    return;
  }
  fs.readFile(file, (err, body) => {
    if (err) {
      res.statusCode = 404;
      res.end("not found");
      return;
    }
    res.setHeader("content-type", mime[path.extname(file)] || "application/octet-stream");
    res.end(body);
  });
});

const actionTimeout = 2500;

async function settle(page, ms) {
  await page.evaluate(
    () =>
      new Promise((resolve) => {
        setTimeout(() => requestAnimationFrame(() => requestAnimationFrame(() => setTimeout(resolve, 0))), 0);
      }),
  );
  if (ms > 0) await page.waitForTimeout(ms);
}

function describeStep(step) {
  const parts = [step.op];
  for (const k of ["sel", "text", "key", "value", "values", "event", "ms", "js"]) {
    if (step[k] !== undefined) parts.push(`${k}=${JSON.stringify(step[k])}`);
  }
  return parts.join(" ");
}

async function runStep(page, step) {
  const loc = step.sel ? page.locator(step.sel).first() : null;
  const opts = { timeout: step.timeout || actionTimeout };
  switch (step.op) {
    case "click":
      return loc.click(step.modifiers ? { ...opts, modifiers: step.modifiers } : opts);
    case "dblclick":
      return loc.dblclick(opts);
    case "hover":
      return loc.hover(opts);
    case "type":
      return loc.pressSequentially(step.text, opts);
    case "fill":
      return loc.fill(step.text, opts);
    case "clear":
      return loc.fill("", opts);
    case "press":
      if (loc) return loc.press(step.key, opts);
      return page.keyboard.press(step.key);
    case "select":
      return loc.selectOption(step.values ?? step.value, opts);
    case "check":
      return loc.check(opts);
    case "uncheck":
      return loc.uncheck(opts);
    case "focus":
      return loc.focus(opts);
    case "blur":
      return loc.blur(opts);
    case "submit":
      return loc.evaluate((form) => form.requestSubmit(), undefined, opts);
    case "reset":
      return loc.evaluate((form) => form.reset(), undefined, opts);
    case "waitFor":
      if (step.text !== undefined) {
        return page.getByText(step.text, { exact: false }).first().waitFor({ timeout: step.timeout || 5000 });
      }
      return page.waitForSelector(step.sel, { timeout: step.timeout || 5000, state: step.state || "attached" });
    case "wait":
      return page.waitForTimeout(step.ms || 0);
    case "dispatch":
      return loc.evaluate(
        (el, { event, ctor, init }) => {
          const C = (ctor && window[ctor]) || Event;
          el.dispatchEvent(new C(event, { bubbles: true, cancelable: true, composed: true, ...(init || {}) }));
        },
        { event: step.event, ctor: step.ctor, init: step.init },
        opts,
      );
    case "hash":
      return page.evaluate((v) => {
        location.hash = v;
      }, step.value);
    case "eval":
      return page.evaluate(step.js);
    default:
      throw new Error(`unknown step op ${step.op}`);
  }
}

// Runs in the page. Returns the normalized state of the fixture.
function capture(captureHead) {
  const isKrateAttr = (n) =>
    n === "data-k" || /^data-k[a-z]$/.test(n) || n.startsWith("data-kattr-") || n.startsWith("data-krate-");
  const skipTags = new Set(["SCRIPT", "TEMPLATE", "STYLE", "LINK", "NOSCRIPT"]);
  // Live form state is compared through the forms channel, so the reflected
  // attributes are not part of the DOM comparison.
  const formAttrs = { INPUT: ["value", "checked"], TEXTAREA: ["value"], OPTION: ["selected"] };
  const root = document.getElementById("root");
  const attrs = (el) => {
    const out = [];
    for (const a of el.attributes) {
      const n = a.name;
      if (isKrateAttr(n)) continue;
      if ((formAttrs[el.tagName] || []).includes(n)) continue;
      let v = a.value;
      // React writes this placeholder for function actions; it is an
      // implementation detail, not app markup.
      if ((n === "action" || n === "formaction") && v.startsWith("javascript:throw new Error('A React form was unexpectedly submitted")) continue;
      if (n === "style") {
        v = [...el.style]
          .sort()
          .map((p) => p + ": " + el.style.getPropertyValue(p) + (el.style.getPropertyPriority(p) ? " !important" : ""))
          .join("; ");
      } else if (n === "class") {
        v = v.split(/\s+/).filter(Boolean).sort().join(" ");
      }
      out.push(n + "=" + JSON.stringify(v));
    }
    return out.sort();
  };
  const walk = (nodes, depth, out) => {
    const ind = "  ".repeat(depth);
    let text = "";
    const flush = () => {
      if (text) out.push(ind + "#text " + JSON.stringify(text));
      text = "";
    };
    for (const n of nodes) {
      if (n.nodeType === 3) {
        text += n.data;
        continue;
      }
      if (n.nodeType !== 1 || skipTags.has(n.tagName)) continue;
      flush();
      const a = attrs(n);
      out.push(ind + "<" + n.localName + (a.length ? " " + a.join(" ") : "") + ">");
      if (n.tagName !== "TEXTAREA") walk(n.childNodes, depth + 1, out);
    }
    flush();
  };
  const dom = [];
  if (root) walk(root.childNodes, 0, dom);
  else dom.push("NO #root");
  const extraNodes = [...document.body.children].filter((el) => el !== root && !skipTags.has(el.tagName));
  const extra = [];
  walk(extraNodes, 0, extra);

  const containers = [root, ...extraNodes].filter(Boolean);
  const pathOf = (el) => {
    if (!el || el === document.body || el === document.documentElement) return "body";
    const parts = [];
    let cur = el;
    while (cur && cur !== document.body) {
      const parent = cur.parentElement;
      if (!parent) break;
      const sibs = [...parent.children].filter((c) => !skipTags.has(c.tagName));
      const tag = cur === root ? "#root" : cur.localName + "[" + sibs.indexOf(cur) + "]";
      parts.unshift(tag);
      if (cur === root) break;
      cur = parent;
    }
    return parts.join("/");
  };
  const forms = [];
  for (const c of containers) {
    for (const el of c.querySelectorAll("input,textarea,select")) {
      let state;
      if (el.tagName === "SELECT") {
        state = "selected=" + JSON.stringify([...el.options].filter((o) => o.selected).map((o) => o.value));
      } else if (el.type === "checkbox" || el.type === "radio") {
        state = "checked=" + el.checked + (el.indeterminate ? " indeterminate" : "");
      } else {
        state = "value=" + JSON.stringify(el.value);
      }
      forms.push(pathOf(el) + " " + state);
    }
  }
  const ae = document.activeElement;
  let focus = pathOf(ae);
  if (ae && ae !== document.body && ae.id) focus += " #" + ae.id;

  const head = [];
  if (captureHead) {
    head.push("title " + JSON.stringify(document.title));
    for (const m of document.head.querySelectorAll("meta[name],meta[property]")) {
      const key = m.getAttribute("name") || m.getAttribute("property");
      if (key === "viewport" || key === "generator") continue;
      head.push("meta " + key + "=" + JSON.stringify(m.getAttribute("content")));
    }
    for (const l of document.head.querySelectorAll("link[rel=canonical],link[rel=author],link[rel=alternate]")) {
      head.push("link " + l.getAttribute("rel") + "=" + JSON.stringify(l.getAttribute("href")));
    }
  }
  return { dom, extra, forms, focus, head, log: (window.__log || []).slice() };
}

async function runSide(browser, fx, url) {
  const context = await browser.newContext({ viewport: { width: 1000, height: 800 } });
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (err) => errors.push(String((err && err.message) || err)));
  page.on("dialog", (d) => d.accept().catch(() => {}));
  await page.addInitScript(() => {
    window.__log = window.__log || [];
  });
  const snaps = [];
  let seenLog = 0;
  let seenErr = 0;
  const record = async (label, stepError) => {
    let s;
    try {
      s = await page.evaluate(capture, !!fx.captureHead);
    } catch (e) {
      s = { dom: ["CAPTURE FAILED: " + String(e.message || e).split("\n")[0]], extra: [], forms: [], focus: "", head: [], log: [] };
    }
    const all = s.log;
    s.log = all.slice(seenLog);
    seenLog = all.length;
    s.errors = errors.slice(seenErr);
    seenErr = errors.length;
    s.step = label;
    s.stepError = stepError || "";
    snaps.push(s);
  };
  let fatal = "";
  try {
    const target = /^https?:/.test(url) ? url : `http://127.0.0.1:${server.address().port}${url}`;
    const resp = await page.goto(target, { waitUntil: "load", timeout: 20000 });
    if (!resp || resp.status() >= 400) throw new Error(`GET ${target}: ${resp ? resp.status() : "no response"}`);
    await settle(page, fx.settleMs ?? 30);
    await record("load");
    for (const step of fx.steps) {
      let stepError = "";
      try {
        await runStep(page, step);
      } catch (e) {
        stepError = String((e && e.message) || e)
          .split("\n")[0]
          .replace(/\s+/g, " ");
      }
      try {
        await settle(page, step.settleMs ?? fx.settleMs ?? 30);
      } catch (e) {
        stepError = stepError || "settle: " + String(e.message || e).split("\n")[0];
      }
      await record(describeStep(step), stepError);
    }
  } catch (e) {
    fatal = String((e && e.message) || e).split("\n")[0];
  } finally {
    await context.close().catch(() => {});
  }
  return { snaps, fatal };
}

let browser;
try {
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  browser = await browserType.launch({ headless: true });
  const results = {};
  const queue = [...plan.fixtures];
  const worker = async () => {
    while (queue.length) {
      const fx = queue.shift();
      const [react, krate] = await Promise.all([runSide(browser, fx, fx.react), runSide(browser, fx, fx.krate)]);
      results[fx.name] = { react, krate };
    }
  };
  await Promise.all(Array.from({ length: Math.max(1, plan.concurrency || 4) }, worker));
  fs.writeFileSync(outPath, JSON.stringify(results));
} catch (err) {
  console.error(err?.stack || String(err));
  process.exitCode = 1;
} finally {
  if (browser) await browser.close();
  server.close();
}
