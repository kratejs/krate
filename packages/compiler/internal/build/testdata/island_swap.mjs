// Router swaps over mounted islands (see TestBrowserIslandSwapKeepsIslands).
// Prints JSON last.
import { chromium, firefox, webkit } from "playwright";
import { waitHydrated } from "./island_helpers.mjs";

const [base, browserName = "chromium"] = process.argv.slice(2);
const browser = await { chromium, firefox, webkit }[browserName].launch();
const errors = [];
const result = { errors };
try {
  const page = await browser.newPage();
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => {
    if (m.type() === "error") errors.push(m.text());
  });
  await page.addInitScript(() => {
    window.__ends = 0;
    addEventListener("krate:navigate-end", () => window.__ends++);
  });
  const ends = () => page.evaluate(() => window.__ends);
  // A step that cannot run (the element it needs is gone) is recorded, and
  // the checks of that flow then fail on the state it left.
  const click = (sel) => page.click(sel, { timeout: 5000 }).catch((e) => errors.push("click " + sel + ": " + e.message.split("\n")[0]));
  const waitSel = (sel) => page.waitForSelector(sel, { timeout: 5000 }).catch((e) => errors.push("wait " + sel + ": " + e.message.split("\n")[0]));
  const refreshed = async (n) => {
    const ok = await page
      .waitForFunction((n) => window.__ends > n, n, { timeout: 5000 })
      .then(() => true)
      .catch(() => false);
    await page.waitForTimeout(250);
    return ok;
  };
  const ready = async (path, sel) => {
    await page.goto(base + path);
    await waitHydrated(page, sel);
    await page.waitForFunction(() => !!globalThis[Symbol.for("krate.router")], null, { timeout: 5000 });
  };
  const state = () =>
    page.evaluate(() => ({
      same: document.getElementById("share") === window.__btn,
      dialogs: document.querySelectorAll('[role="dialog"]').length,
      buttons: document.querySelectorAll("#share").length,
      note: (document.getElementById("note") || {}).textContent || "",
      url: (document.getElementById("share-url") || {}).textContent || "",
      stamp: (document.getElementById("stamp") || {}).textContent || "",
    }));

  async function shareFlow(path) {
    await ready(path, "#share");
    await page.evaluate(() => (window.__btn = document.getElementById("share")));
    const stamp = (await state()).stamp;
    const out = {};

    // An action that revalidates another page (like revalidatePath of a list).
    let n = await ends();
    await click("#share");
    await waitSel('[role="dialog"]');
    let ok = await refreshed(n);
    let s = await state();
    out.create = { ...s, refreshed: ok && s.stamp !== stamp };

    n = await ends();
    let before = s.stamp;
    await click("#revoke");
    await page.waitForFunction(() => (document.getElementById("note") || {}).textContent === "Link revoked", null, { timeout: 5000 }).catch(() => {});
    ok = await refreshed(n);
    await page.waitForFunction(() => !document.querySelector('[role="dialog"]'), null, { timeout: 2000 }).catch(() => {});
    s = await state();
    out.revoke = { ...s, refreshed: ok && s.stamp !== before };

    // An action that revalidates the current page.
    n = await ends();
    await click("#share");
    await waitSel('[role="dialog"]');
    await refreshed(n);
    n = await ends();
    before = (await state()).stamp;
    await click("#touch");
    await page.waitForFunction(() => (document.getElementById("note") || {}).textContent === "Touched", null, { timeout: 5000 }).catch(() => {});
    ok = await refreshed(n);
    await page.waitForFunction(() => !document.querySelector('[role="dialog"]'), null, { timeout: 2000 }).catch(() => {});
    s = await state();
    out.touch = { ...s, refreshed: ok && s.stamp !== before };

    // Leaving the page with the dialog open unmounts the island.
    n = await ends();
    await click("#share");
    await waitSel('[role="dialog"]');
    await refreshed(n);
    await page.evaluate(() => globalThis[Symbol.for("krate.router")].push("/other/"));
    await waitSel("#other-page");
    await page.waitForTimeout(300);
    out.leave = await page.evaluate(() => ({
      dialogs: document.querySelectorAll('[role="dialog"]').length,
      guards: document.querySelectorAll("[data-radix-focus-guard]").length,
    }));
    return out;
  }

  async function searchFlow(path) {
    await ready(path + "?q=cv", "#search");
    await page.evaluate(() => {
      const input = document.getElementById("search");
      window.__input = input;
      window.__blurs = 0;
      input.addEventListener("blur", () => window.__blurs++);
    });
    await page.focus("#search");
    await page.keyboard.press("End");
    await page.keyboard.type("x");
    let n = await ends();
    await page.keyboard.press("Enter");
    await page.waitForFunction(() => location.search === "?q=cvx", null, { timeout: 5000 }).catch(() => {});
    await refreshed(n);
    const out = await page.evaluate(() => {
      const input = document.getElementById("search");
      return {
        same: input === window.__input,
        focused: document.activeElement === window.__input,
        seen: document.getElementById("seen").textContent,
        server: document.getElementById("server-q").textContent,
        value: input.value,
        mounts: window.__mounts || 0,
        blurs: window.__blurs,
      };
    });
    n = await ends();
    await page.keyboard.press("Escape");
    await page.waitForFunction(() => location.search === "", null, { timeout: 5000 }).catch(() => {});
    await refreshed(n);
    const cleared = await page.evaluate(() => ({
      cleared: document.getElementById("search") === window.__input && document.activeElement === window.__input && window.__input.value === "",
      clearSeen: document.getElementById("seen").textContent,
    }));
    return { ...out, ...cleared };
  }

  result.preact = await shareFlow("/order/");
  result.real = await shareFlow("/realorder/");
  result.preactSearch = await searchFlow("/search/");
  result.realSearch = await searchFlow("/realsearch/");
  // The search island sits in the server children of an island around the
  // whole page (a Providers component in a root layout).
  await page.evaluate(() => (window.__mounts = 0));
  result.nestedSearch = await searchFlow("/wrapped/");
  Object.assign(
    result,
    await page.evaluate(() => ({
      providers: window.__providers || 0,
      slot: !!document.querySelector(".shell") && (window.__krateIslands || []).some((i) => i.export === "Providers" && i.contains(document.querySelector(".shell"))),
    })),
  );
} finally {
  await browser.close();
}
console.log(JSON.stringify(result));
