// Kept wrapper islands whose server children change shape across client
// navigations, back/forward and refreshes (see
// TestBrowserKeptProviderSlotChangesShape). Prints JSON last.
import { chromium, firefox, webkit } from "playwright";
import { waitHydrated } from "./island_helpers.mjs";

const [base, browserName = "chromium"] = process.argv.slice(2);
const browser = await { chromium, firefox, webkit }[browserName].launch();
const errors = [];
const steps = {};
const order = [];
try {
  const context = await browser.newContext();
  const page = await context.newPage();
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => {
    if (m.type() === "error") errors.push(m.text());
  });
  await page.addInitScript(() => {
    window.__ends = 0;
    addEventListener("krate:navigate-end", () => window.__ends++);
  });
  const ends = () => page.evaluate(() => window.__ends);
  const settled = async (n) => {
    await page.waitForFunction((n) => window.__ends > n, n, { timeout: 5000 }).catch(() => errors.push("no navigate-end"));
    // The loader mounts the new islands after the page scripts ran.
    await page.waitForFunction(() => (window.__krateIslands || []).every((r) => r.status !== "loading"), null, { timeout: 5000 }).catch(() => {});
    await page.waitForTimeout(300);
  };
  // The island invariants after a swap, and what <main> and the provider's
  // range hold.
  const look = () =>
    page.evaluate(() => {
      const problems = [];
      const isOpen = (n) => n.nodeType === 8 && n.data.startsWith("krate-island:");
      const isClose = (n) => n.nodeType === 8 && n.data === "/krate-island";
      const closeOf = (s) => {
        let d = 0;
        for (let n = s.nextSibling; n; n = n.nextSibling) {
          if (isOpen(n)) d++;
          else if (isClose(n)) {
            if (!d) return n;
            d--;
          }
        }
        return null;
      };
      const text = (s, e) => {
        let t = "";
        for (let n = s.nextSibling; n && n !== e; n = n.nextSibling) t += n.nodeType === 8 ? "" : n.textContent;
        return t;
      };
      const all = window.__krateIslands || [];
      for (const r of all) {
        if (r.status !== "hydrated") continue;
        if (r.gone) {
          // An island that was unmounted has no markup left in the document.
          if (r.el ? r.el.isConnected : r.start.isConnected) problems.push("unmounted " + r.export + " still in the document");
          continue;
        }
        if (r.el) {
          if (!r.el.isConnected) problems.push("mounted " + r.export + " detached");
          continue;
        }
        if (!r.start.isConnected || !r.end.isConnected) problems.push("mounted " + r.export + " detached");
        else if (closeOf(r.start) !== r.end) problems.push("unbalanced range of " + r.export);
        // Every slot set node of a mounted island is inside its range (once
        // synced, as the island's next render does).
        if (r.handle && r.handle.sync) r.handle.sync();
        for (const s of (r.handle && r.handle.slots) || []) {
          for (const n of s.set) {
            if (!n.isConnected) problems.push("slot node of " + r.export + " detached");
            else if (!(r.start.compareDocumentPosition(n) & 4) || !(r.end.compareDocumentPosition(n) & 2)) problems.push("slot node of " + r.export + " outside its range");
          }
        }
      }
      // No server-children marker is left in the live document.
      const w = document.createTreeWalker(document.body, 128);
      for (let c = w.nextNode(); c; c = w.nextNode()) if (/^\/?krate-slot:/.test(c.data)) problems.push("slot marker " + c.data + " in the document");
      const p = all.find((r) => r.export === "BotWorkspaceProvider" && r.status === "hydrated" && !r.gone);
      let after = "";
      if (p) for (let n = p.end.nextSibling; n; n = n.nextSibling) after += n.nodeType === 8 ? "<!--" + n.data.slice(0, 40) + "-->" : n.textContent || "";
      const main = document.getElementById("main");
      return {
        main: main ? main.textContent : "",
        range: p ? text(p.start, p.end) : "",
        after: after.trim(),
        hasBWP: !!p,
        status: { x: window.__x || "", y: window.__y || "", z: window.__z || "", w: window.__w || "", v: window.__v || "", bwp: window.__bwp || "" },
        portals: document.querySelectorAll("#x-portal").length,
        bump: (document.getElementById("bump") || {}).textContent || "",
        sameBump: document.getElementById("bump") === window.__bump,
        problems,
      };
    });
  const step = async (name, act, wait) => {
    const n = await ends();
    await act();
    await settled(n);
    if (wait) await page.waitForSelector(wait, { timeout: 5000 }).catch(() => errors.push("no " + wait + " at " + name));
    steps[name] = await look();
    order.push(name);
  };

  await page.goto(base + "/ws/bot/a/");
  await waitHydrated(page, "#x", { depth: 2 });
  await page.waitForFunction(() => !!globalThis[Symbol.for("krate.router")], null, { timeout: 5000 });
  await page.waitForTimeout(300);
  await page.click("#bump");
  await page.click("#bump");
  await page.evaluate(() => (window.__bump = document.getElementById("bump")));
  steps.a = await look();
  order.push("a");

  await step("b", () => page.click("#to-b"), "#y");
  await step("back", () => page.goBack(), "#x");
  await step("forward", () => page.goForward(), "#y");
  await step(
    "refresh",
    async () => {
      await context.addCookies([{ name: "v", value: "1", url: base }]);
      await page.evaluate(() => globalThis[Symbol.for("krate.router")].refresh());
    },
    "#z",
  );
  await step(
    "action",
    async () => {
      await context.addCookies([{ name: "v", value: "2", url: base }]);
      await page.click("#save");
    },
    "#z",
  );
  await step("list", () => page.click("#nav-m"), "#w");
  await step("detail", () => page.click("#to-detail"), "#v");
  await step("list2", () => page.click("#to-list"), "#w");
  await step("detail2", () => page.goBack(), "#v");
  await step("c", () => page.click("#nav-c"), "#c-page");
} catch (e) {
  errors.push("script: " + e.message);
} finally {
  await browser.close();
}
console.log(JSON.stringify({ steps, order, errors }));
