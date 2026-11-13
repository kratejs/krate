import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { chromium, firefox, webkit } from "playwright";

// Keyboard, ARIA and theme checks for the zero-JS component library, run
// against the built examples. axe-core is MPL-2.0 and is not vendored; this
// script asserts the same contracts (roles, names, focus order, Enter/Space/
// Escape, contrast, RTL) directly.
const root = process.argv[2];
const browserName = process.argv[3] || "chromium";
const browserType = { chromium, firefox, webkit }[browserName];
if (!root || !browserType) {
  console.error("usage: components_a11y.mjs <dist-root> [chromium|firefox|webkit]");
  process.exit(2);
}

const mime = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".json": "application/json", ".svg": "image/svg+xml" };
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

const failures = [];
const check = (cond, message) => {
  if (!cond) failures.push(message);
};
let browser;

try {
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const base = `http://127.0.0.1:${server.address().port}`;
  browser = await browserType.launch({ headless: true });
  const context = await browser.newContext();
  const page = await context.newPage();
  const pageErrors = [];
  page.on("pageerror", (e) => pageErrors.push(e.message));
  await page.goto(`${base}/zerojs-demo/`);

  const active = () =>
    page.evaluate(() => {
      const a = document.activeElement;
      if (!a || a === document.body) return "";
      return (a.tagName + "|" + (a.getAttribute("role") || "") + "|" + (a.textContent || "").trim().slice(0, 24));
    });
  // Focus moves after the component renders, which a slow runner can do a
  // frame or two after the key press; wait for it rather than read once.
  const focusIs = async (pred, message) => {
    const deadline = Date.now() + 2000;
    let a = await active();
    while (!pred(a) && Date.now() < deadline) {
      await page.waitForTimeout(25);
      a = await active();
    }
    check(pred(a), `${message} (focus: ${a})`);
  };
  const focusTrigger = (text) =>
    page.evaluate((t) => {
      const l = [...document.querySelectorAll("label[data-ka=t]")].find((x) => x.textContent.trim() === t && x.getClientRects().length);
      if (l) l.focus();
      return !!l;
    }, text);
  const visible = (sel) => page.evaluate((s) => !!document.querySelector(s) && document.querySelector(s).getClientRects().length > 0, sel);

  // Hidden controllers are not exposed or tabbable; every trigger is a labelled
  // role on a focusable element.
  const hidden = await page.evaluate(() =>
    [...document.querySelectorAll("[data-kz] input.krc-h")].filter((i) => i.getAttribute("aria-hidden") !== "true" || i.tabIndex !== -1).length);
  check(hidden === 0, `controller inputs under enhanced scopes must be aria-hidden with tabindex -1 (found ${hidden})`);
  const unfocusable = await page.evaluate(() =>
    [...document.querySelectorAll("label[data-ka]")].filter((l) => l.getClientRects().length && l.tabIndex < -1).length);
  check(unfocusable === 0, "visible enhanced labels must be focusable");

  // Dialog: reach the trigger with Tab, open with Enter.
  let reached = false;
  for (let i = 0; i < 80 && !reached; i++) {
    await page.keyboard.press("Tab");
    reached = (await active()).endsWith("Open dialog");
  }
  check(reached, "Tab must reach the Dialog trigger");
  const trigger = await page.evaluate(() => {
    const a = document.activeElement;
    return { role: a.getAttribute("role"), popup: a.getAttribute("aria-haspopup"), expanded: a.getAttribute("aria-expanded"), controls: a.getAttribute("aria-controls") };
  });
  check(trigger.role === "button" && trigger.popup === "dialog" && trigger.expanded === "false" && !!trigger.controls, `dialog trigger ARIA: ${JSON.stringify(trigger)}`);
  await page.keyboard.press("Enter");
  check(await visible(".krate-dialog-content"), "Enter must open the dialog");
  check(await page.evaluate(() => !!document.activeElement.closest("[role=dialog]")), "focus must move into the dialog");
  const dlg = await page.evaluate(() => {
    const d = document.querySelector(".krate-dialog-content");
    const name = d.getAttribute("aria-labelledby") && document.getElementById(d.getAttribute("aria-labelledby"));
    const desc = d.getAttribute("aria-describedby") && document.getElementById(d.getAttribute("aria-describedby"));
    return { modal: d.getAttribute("aria-modal"), name: name && name.textContent, desc: desc && desc.textContent, id: d.id, role: d.getAttribute("role") };
  });
  check(dlg.modal === "true" && dlg.role === "dialog", `dialog needs role + aria-modal: ${JSON.stringify(dlg)}`);
  check(dlg.name === "Edit profile" && dlg.desc === "Make changes and save.", `dialog name/description: ${JSON.stringify(dlg)}`);
  check(dlg.id === trigger.controls, "trigger aria-controls must reference the dialog");
  await page.keyboard.press("Tab");
  check(await page.evaluate(() => !!document.activeElement.closest("[role=dialog]")), "Tab must stay inside the open dialog (focus trap)");
  await page.keyboard.press("Shift+Tab");
  check(await page.evaluate(() => !!document.activeElement.closest("[role=dialog]")), "Shift+Tab must stay inside the open dialog");
  await page.keyboard.press("Escape");
  check(!(await visible(".krate-dialog-content")), "Escape must close the dialog");
  await focusIs((a) => a.endsWith("Open dialog"), `focus must return to the trigger after Escape`);
  await page.keyboard.press("Space");
  check(await visible(".krate-dialog-content"), "Space must open the dialog");
  await focusIs((a) => a.includes("Close"), `focus must land on the Close button`);
  await page.keyboard.press("Enter");
  check(!(await visible(".krate-dialog-content")), "Enter on Close must close the dialog");
  await focusIs((a) => a.endsWith("Open dialog"), "focus must return to the trigger after Close");
  // Pointer path still works without the keyboard.
  await page.locator("label.krate-dialog-trigger:visible").click();
  check(await visible(".krate-dialog-content"), "click must open the dialog");
  await page.keyboard.press("Escape");
  check(!(await visible(".krate-dialog-content")), "Escape must close a pointer-opened dialog");

  // Dropdown menu.
  await focusTrigger("Open menu");
  const menuTrigger = await page.evaluate(() => document.activeElement.getAttribute("aria-haspopup"));
  check(menuTrigger === "menu", "menu trigger must advertise aria-haspopup=menu");
  await page.keyboard.press("Enter");
  await focusIs((a) => a.startsWith("LABEL|menuitem|Profile"), `Enter must open the menu and focus the first item`);
  check(await page.evaluate(() => document.activeElement.closest("[role=menu]") !== null), "menu items live in role=menu");
  await page.keyboard.press("ArrowDown");
  await focusIs((a) => a.includes("Sign out"), "ArrowDown must move to the next item");
  await page.keyboard.press("ArrowUp");
  await focusIs((a) => a.includes("Profile"), "ArrowUp must move to the previous item");
  await page.keyboard.press("Escape");
  await focusIs((a) => a.endsWith("Open menu"), "Escape must close the menu and restore focus");
  await page.keyboard.press("ArrowDown");
  await focusIs((a) => a.includes("Profile"), "ArrowDown on the trigger must open the menu");
  await page.keyboard.press("Enter");
  await focusIs((a) => a.endsWith("Open menu"), "activating an item closes the menu and restores focus");

  // Menu bar: Left/Right between menus.
  await focusTrigger("File");
  await page.keyboard.press("Enter");
  await page.keyboard.press("Escape");
  await page.keyboard.press("ArrowRight");
  await focusIs((a) => a.endsWith("Edit"), `ArrowRight must move to the next menubar trigger`);

  // Popover and AlertDialog-style disclosure keep keyboard parity.
  await focusTrigger("Open popover");
  await page.keyboard.press("Enter");
  check(await page.evaluate(() => !!document.activeElement.closest("[role=dialog]")), "popover focus moves into the popover");
  await page.keyboard.press("Escape");
  await focusIs((a) => a.endsWith("Open popover"), "Escape closes the popover and restores focus");

  // Navigation menu: a disclosure of links, not a role=menu.
  await focusTrigger("Getting Started");
  const nav = await page.evaluate(() => ({ role: document.activeElement.getAttribute("role"), popup: document.activeElement.getAttribute("aria-haspopup") }));
  check(nav.role === "button" && nav.popup === null, `navigation trigger is a plain disclosure button: ${JSON.stringify(nav)}`);
  await page.keyboard.press("Enter");
  check(await visible(".krate-navigation-menu-link"), "Enter opens the navigation menu");
  await focusIs((a) => a.includes("Getting Started"), "navigation trigger keeps focus after opening");
  await page.keyboard.press("Escape");
  check(!(await visible(".krate-navigation-menu-link")), "Escape closes the navigation menu");
  await focusIs((a) => a.endsWith("Getting Started"), "focus stays on the navigation trigger after Escape");

  // Accordion: Enter toggles, aria-expanded follows, focus is kept.
  await focusTrigger("Is it zero-JS?");
  await page.keyboard.press("Enter");
  const acc = await page.evaluate(() => ({ expanded: document.activeElement.getAttribute("aria-expanded"), text: document.activeElement.textContent.trim() }));
  check(acc.expanded === "true" && acc.text.startsWith("Is it zero-JS?"), `accordion Enter keeps focus on the trigger: ${JSON.stringify(acc)}`);
  await page.keyboard.press("Space");
  await focusIs((a) => a.includes("Is it zero-JS?"), "accordion Space closes and keeps focus");

  // Tabs: tablist owns tabs, roving tabindex, arrows select.
  const tabs = await page.evaluate(() => [...document.querySelectorAll("[role=tablist] > [role=tab]")].length);
  check(tabs === 2, `tablist must directly own its tabs (found ${tabs})`);
  await focusTrigger("A");
  await page.keyboard.press("ArrowRight");
  const sel = await page.evaluate(() => [...document.querySelectorAll("[role=tab]")].map((t) => t.getAttribute("aria-selected") + ":" + t.tabIndex).join(","));
  check(sel === "false:-1,true:0", `ArrowRight selects the next tab (roving tabindex): ${sel}`);
  await focusIs((a) => a.endsWith("B"), "focus follows the selected tab");

  // Switch: a native checkbox with role=switch and an accessible name.
  const sw = page.getByRole("switch", { name: "Airplane mode" });
  check((await sw.count()) === 1, "Switch must expose role=switch named by its label");
  await sw.focus();
  await page.keyboard.press("Space");
  check(await sw.isChecked(), "Space must toggle the switch");

  check(pageErrors.length === 0, `page errors: ${pageErrors.join("; ")}`);

  // Entered by a soft navigation from a page without the enhancer: the router
  // must still run the page's boot script, or the modal has no scroll lock,
  // inert background or focus restore.
  await page.goto(`${base}/`);
  check(await page.evaluate(() => !window.__krateAria), "/ must not already carry the ARIA enhancer");
  await page.evaluate(() => {
    const a = document.createElement("a");
    a.href = "/zerojs-demo/";
    a.textContent = "to demo";
    a.setAttribute("data-krate-link", "");
    a.id = "to-demo";
    document.body.appendChild(a);
  });
  await page.evaluate(() => (window.__softMarker = 1));
  await page.click("#to-demo");
  await page.waitForFunction(() => location.pathname === "/zerojs-demo/");
  check(await page.evaluate(() => window.__softMarker === 1), "the link must be followed by the client router, not a document load");
  await page.waitForFunction(() => !!window.__krateAria);
  // A closed dialog can look visible while the page's CSS is applying, which
  // must not lock the page: everything would stay inert, and the trigger dead.
  const softIdle = await page.evaluate(() => ({
    scroll: document.documentElement.style.overflow,
    inert: document.querySelectorAll("[data-kinert]").length,
  }));
  check(softIdle.scroll !== "hidden" && softIdle.inert === 0, `a closed modal must not lock a soft-navigated page: ${JSON.stringify(softIdle)}`);
  check(await focusTrigger("Open dialog"), "the soft-navigated page must show the Dialog trigger");
  await page.keyboard.press("Enter");
  await page.waitForTimeout(250);
  const softModal = await page.evaluate(() => ({
    scroll: document.documentElement.style.overflow,
    inert: document.querySelectorAll("[data-kinert]").length,
  }));
  check(softModal.scroll === "hidden" && softModal.inert > 0, `a soft-navigated modal must lock scroll and inert the page: ${JSON.stringify(softModal)}`);
  await page.keyboard.press("Escape");
  // The enhancer restores focus after the close has committed, so wait for it.
  await page
    .waitForFunction(() => (document.activeElement?.textContent ?? "").endsWith("Open dialog"), null, { timeout: 2000 })
    .catch(() => {});
  await focusIs((a) => a.endsWith("Open dialog"), `Escape must restore focus to the trigger after a soft navigation`);

  // Controlled and imperative open state.
  const errs2 = [];
  page.on("pageerror", (e) => errs2.push(e.message));
  await page.goto(`${base}/overlay-control/`);
  const titleVisible = (text) =>
    page.evaluate((t) => [...document.querySelectorAll(".krate-dialog-title,.krate-sheet-title")].some((e) => e.textContent === t && e.getClientRects().length > 0), text);
  check(await titleVisible("Open on load"), "defaultOpen must render the dialog open on load");
  await page.keyboard.press("Escape");
  check(!(await titleVisible("Open on load")), "Escape closes a defaultOpen dialog");
  check(!(await titleVisible("Controlled dialog")), "controlled dialog starts closed");
  await page.click("#ctl-open");
  await page.waitForFunction(() => document.querySelector("#ctl-state").textContent === "open");
  check(await titleVisible("Controlled dialog"), "open={true} must open the controlled dialog");
  await page.keyboard.press("Escape");
  await page.waitForFunction(() => document.querySelector("#ctl-state").textContent === "closed");
  check(!(await titleVisible("Controlled dialog")), "closing must update the controlled state and hide the dialog");
  const changes = await page.textContent("#ctl-changes");
  check(Number(changes) >= 2, `onOpenChange must fire for open and close (got ${changes})`);
  await page.click("#imp-open");
  check(await titleVisible("Imperative sheet"), "openOverlay('#id') must open the sheet");
  await page.keyboard.press("Escape");
  check(!(await titleVisible("Imperative sheet")), "Escape closes the imperatively opened sheet");
  await page.evaluate(() => document.querySelector("#imp-open").click());
  await page.evaluate(() => document.querySelector("#imp-close").click());
  check(!(await titleVisible("Imperative sheet")), "closeOverlay('#id') must close the sheet");
  await page.click("#toast-btn");
  check((await page.locator(".krate-toaster .krate-toast-title", { hasText: "Saved" }).count()) === 1, "toast() must render a toast");
  check((await page.locator(".krate-toaster").getAttribute("aria-live")) === "polite", "toast viewport is a polite live region");
  await page.click(".krate-toaster .krate-toast-close");
  check((await page.locator(".krate-toaster .krate-toast").count()) === 0, "toast close button dismisses the toast");
  check(errs2.length === 0, `overlay-control page errors: ${errs2.join("; ")}`);

  // Contrast of the primary button and badge, light and dark (WCAG AA 4.5:1).
  const ratio = (pageObj) =>
    pageObj.evaluate(() => {
      const rgb = (css) => {
        const c = document.createElement("canvas").getContext("2d");
        c.fillStyle = "#000";
        c.fillStyle = css;
        c.fillRect(0, 0, 1, 1);
        const d = c.getImageData(0, 0, 1, 1).data;
        return [d[0], d[1], d[2]];
      };
      const lum = ([r, g, b]) => {
        const f = (v) => ((v /= 255) <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
        return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
      };
      const out = {};
      // Probes outside <main> so the example page's own button styles do not
      // mask the library tokens.
      for (const [sel, cls] of [[".krate-button-primary", "krate-button krate-button-primary"], [".krate-badge-default", "krate-badge krate-badge-default"]]) {
        const el = document.createElement("span");
        el.className = cls;
        el.textContent = "x";
        document.body.appendChild(el);
        const cs = getComputedStyle(el);
        const a = lum(rgb(cs.color)), b = lum(rgb(cs.backgroundColor));
        out[sel] = (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
      }
      return out;
    });
  for (const scheme of ["light", "dark"]) {
    const ctx = await browser.newContext({ colorScheme: scheme });
    const p = await ctx.newPage();
    await p.goto(`${base}/ui-demo/`);
    const r = await ratio(p);
    check(Object.keys(r).length >= 1, `ui-demo must render a primary button or badge (${scheme})`);
    for (const [sel, v] of Object.entries(r)) check(v >= 4.5, `${sel} contrast ${v.toFixed(2)} < 4.5 (${scheme})`);
    await ctx.close();
  }

  // RTL: logical properties mirror the layout.
  const edge = (selA, selB) =>
    page.evaluate(([a, b]) => {
      const x = document.querySelector(a), y = document.querySelector(b);
      if (!x || !y) return null;
      const r = x.getBoundingClientRect(), q = y.getBoundingClientRect();
      return Math.abs(r.left - q.left) < Math.abs(r.right - q.right) ? "left" : "right";
    }, [selA, selB]);
  const pairs = [
    ["/components-demo/", ".krate-step-number", ".krate-step"],
    ["/components-demo/", ".krate-link-card-icon", ".krate-link-card"],
    ["/interactive-demo/", ".krate-password-toggle-button", ".krate-password-toggle-field"],
  ];
  for (const [url, a, b] of pairs) {
    await page.goto(`${base}${url}`);
    const before = await edge(a, b);
    await page.evaluate(() => document.documentElement.setAttribute("dir", "rtl"));
    const after = await edge(a, b);
    check(before !== null, `${url} must render ${a}`);
    check(before !== null && before !== after, `${a} must mirror under dir=rtl (${before} -> ${after})`);
  }
  await page.goto(`${base}/components-demo/`);
  await page.evaluate(() => document.documentElement.setAttribute("dir", "rtl"));
  const stepsBorder = await page.evaluate(() => {
    const e = document.querySelector(".krate-steps");
    const cs = e && getComputedStyle(e);
    return cs ? [cs.borderLeftWidth, cs.borderRightWidth] : null;
  });
  check(stepsBorder && stepsBorder[0] === "0px" && stepsBorder[1] !== "0px", `steps rail must move to the right in RTL: ${stepsBorder}`);
} catch (error) {
  failures.push(String(error && error.stack ? error.stack : error));
} finally {
  if (browser) await browser.close();
  server.close();
}

if (failures.length) {
  console.error(failures.map((f) => "FAIL: " + f).join("\n"));
  process.exit(1);
}
console.log("components a11y ok");
