import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { chromium, firefox, webkit } from "playwright";

// Keyboard behaviour of the interactive components, driven in a real browser
// against the built examples (page /component-keyboard/).
const root = process.argv[2];
const browserName = process.argv[3] || "chromium";
const browserType = { chromium, firefox, webkit }[browserName];
if (!root || !browserType) {
  console.error("usage: components_keyboard.mjs <dist-root> [chromium|firefox|webkit]");
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
  const page = await (await browser.newContext({ viewport: { width: 900, height: 700 } })).newPage();
  const pageErrors = [];
  page.on("pageerror", (e) => pageErrors.push(e.message));
  await page.goto(`${base}/component-keyboard/?q=1&p=3`);
  await page.waitForTimeout(400);

  const act = () => page.evaluate(() => {
    const a = document.activeElement;
    return a && a !== document.body ? `${a.tagName}|${a.getAttribute("role") || ""}|${a.id || (a.textContent || "").trim().slice(0, 16)}` : "";
  });
  const formData = () => page.evaluate(() => JSON.stringify([...new FormData(document.getElementById("f")).entries()]));

  // OTPField: typing, Backspace removes digits, paste fills and completes, form value.
  const boxes = page.locator(".krate-otp-field-input");
  const digits = () => page.evaluate(() => [...document.querySelectorAll(".krate-otp-field-input")].map((i) => i.value).join("|"));
  await boxes.first().focus();
  await page.keyboard.type("12");
  check((await digits()) === "1|2||", `OTP typing: ${await digits()}`);
  await page.keyboard.press("Backspace");
  check((await digits()) === "1|||", `OTP Backspace on an empty box must clear the previous digit: ${await digits()}`);
  await page.evaluate(() => {
    const d = new DataTransfer();
    d.setData("text", "9876");
    document.querySelector(".krate-otp-field-input").dispatchEvent(new ClipboardEvent("paste", { clipboardData: d, bubbles: true, cancelable: true }));
  });
  check((await digits()) === "9|8|7|6", `OTP paste: ${await digits()}`);
  check((await page.evaluate(() => window.otpDone)) === "9876", "OTP onComplete must fire after paste");
  check((await formData()).includes('["otp","9876"]'), `OTP must submit under its name: ${await formData()}`);

  // Checkbox / Switch: native inputs, Space toggles, name/value/defaultChecked/disabled.
  const fd0 = await formData();
  check(fd0.includes('["pre","on"]') && !fd0.includes('"off"'), `defaultChecked and disabled: ${fd0}`);
  await page.focus("#agree");
  await page.keyboard.press("Space");
  check((await formData()).includes('["agree","yes"]'), "Space on Checkbox must toggle it and submit its value");
  await page.focus("#notify");
  await page.keyboard.press("Space");
  check(await page.evaluate(() => document.getElementById("notify").checked && document.getElementById("notify").getAttribute("role") === "switch"), "Space on Switch must turn it on");
  check(await page.evaluate(() => document.querySelector("label.krate-label").htmlFor === "vol" && !!document.getElementById("vol")), "Label htmlFor must pair with the Slider id");
  check(await page.evaluate(() => document.querySelector("select.krate-native-select").selectedIndex === 0 && document.querySelector("select.krate-native-select").value === ""), "NativeSelect must show its placeholder");

  // Searchable Select: keyboard operable combobox.
  const selectTrigger = page.locator(".krate-select-trigger");
  const optionTexts = () => page.evaluate(() => [...document.querySelectorAll(".krate-select-content [role=option]")].map((o) => o.textContent.trim() + (o.getAttribute("data-active") === "true" ? "*" : "")));
  check((await selectTrigger.locator(".krate-select-value").textContent()) === "Orange", "Select must show the label of defaultValue");
  await selectTrigger.focus();
  await page.keyboard.press("ArrowDown");
  await page.waitForTimeout(150);
  check((await act()).includes("INPUT"), `ArrowDown on the Select button must open it and focus the search field (got ${await act()})`);
  check((await optionTexts()).join(",") === "Apple,Orange*,Lemon,Melon", `Select active option starts on the selected value: ${await optionTexts()}`);
  await page.keyboard.press("ArrowDown");
  check((await optionTexts()).join(",") === "Apple,Orange,Lemon,Melon*", `ArrowDown skips the disabled option: ${await optionTexts()}`);
  await page.keyboard.press("Enter");
  await page.waitForTimeout(150);
  check((await selectTrigger.locator(".krate-select-value").textContent()) === "Melon", "Enter must choose the active option");
  check((await formData()).includes('["fruit","melon"]'), "Select must submit the chosen value");
  check((await act()).includes("BUTTON"), "focus returns to the Select button after choosing");
  await page.keyboard.press("Space");
  await page.waitForTimeout(150);
  await page.keyboard.press("Escape");
  await page.waitForTimeout(100);
  check((await page.locator(".krate-select-content").count()) === 0 && (await act()).includes("BUTTON"), "Escape must close the Select and return to its button");

  // Command: combobox with an active descendant.
  await page.locator(".krate-command-input").focus();
  await page.keyboard.press("ArrowDown");
  const cmd = await page.evaluate(() => {
    const i = document.querySelector(".krate-command-input");
    const ad = i.getAttribute("aria-activedescendant");
    return { ad, found: !!ad && !!document.getElementById(ad), exp: i.getAttribute("aria-expanded") };
  });
  check(cmd.found, `Command input must point at the active option with aria-activedescendant: ${JSON.stringify(cmd)}`);

  // ContextMenu: Shift+F10 opens, arrows skip disabled items, Enter chooses, Escape returns.
  const cmState = () => page.evaluate(() => document.querySelector(".krate-context-menu").dataset.state);
  await page.locator(".krate-context-menu-trigger").focus();
  await page.keyboard.press("Shift+F10");
  await page.waitForTimeout(150);
  check((await cmState()) === "open" && (await act()).startsWith("DIV|menuitem|Copy"), `Shift+F10 must open the menu on its first item (got ${await cmState()} ${await act()})`);
  await page.keyboard.press("ArrowDown");
  check((await act()).includes("Paste"), `ArrowDown must skip the disabled item (got ${await act()})`);
  await page.keyboard.press("Escape");
  await page.waitForTimeout(100);
  check((await cmState()) === "closed" && (await act()).includes("Context area"), "Escape must close the menu and return to the trigger");
  await page.keyboard.press("ContextMenu");
  await page.waitForTimeout(150);
  await page.keyboard.press("Enter");
  await page.waitForTimeout(100);
  check((await page.evaluate(() => window.picked)) === "copy;", "Enter must run the item's onSelect");

  // Tooltip / HoverCard: described-by link, Escape dismissal, real button is one tab stop.
  const links = await page.evaluate(() => [...document.querySelectorAll("[aria-describedby]")].filter((e) => e.closest(".krate-tooltip, .krate-hover-card")).map((e) => !!document.getElementById(e.getAttribute("aria-describedby"))));
  check(links.length === 3 && links.every(Boolean), `tooltip and hover card triggers must reference their content: ${JSON.stringify(links)}`);
  check(await page.evaluate(() => document.getElementById("tip-button").tabIndex === 0 && !document.getElementById("tip-button").parentElement.hasAttribute("tabindex")), "asChild trigger must not add a nested tab stop");
  await page.locator(".krate-tooltip-trigger").first().focus();
  await page.waitForTimeout(250);
  const tipVisible = () => page.evaluate(() => { const c = getComputedStyle(document.querySelector(".krate-tooltip-content")); return c.visibility === "visible" && c.opacity === "1"; });
  check(await tipVisible(), "tooltip must show on keyboard focus");
  await page.keyboard.press("Escape");
  await page.waitForTimeout(250);
  check(!(await tipVisible()), "Escape must hide the tooltip");

  // Pagination: param is used for the links and the other query is kept.
  const hrefs = await page.evaluate(() => [...document.querySelectorAll(".krate-pagination-page")].map((a) => a.getAttribute("href") + (a.getAttribute("aria-current") === "page" ? "*" : "")));
  check(hrefs.includes("?q=1&p=3*") && hrefs.every((h) => h.startsWith("?q=1&p=")), `Pagination links: ${JSON.stringify(hrefs)}`);

  // Data-driven Tabs: only the default panel is visible, and switching works.
  const tabState = () => page.evaluate(() => [...document.querySelectorAll(".krate-tabs-content")].map((e) => e.getClientRects().length > 0));
  check(JSON.stringify(await tabState()) === "[false,true,false]", `Tabs built from a mapped array must honour defaultValue: ${JSON.stringify(await tabState())}`);
  await page.locator(".krate-tabs-trigger").nth(2).click();
  check(JSON.stringify(await tabState()) === "[false,false,true]", "clicking a mapped tab must show its panel");

  // Modal dialog: backdrop, inert page, scroll lock, outside click.
  await page.locator("label.krate-dialog-trigger:visible").focus();
  await page.keyboard.press("Enter");
  await page.waitForTimeout(250);
  const modal = await page.evaluate(() => ({
    inert: document.querySelectorAll("[data-kinert]").length,
    scroll: document.documentElement.style.overflow,
    shadow: getComputedStyle(document.querySelector(".krate-dialog-content")).boxShadow,
  }));
  check(modal.inert > 0 && modal.scroll === "hidden", `an open modal must make the page inert and lock scrolling: ${JSON.stringify(modal)}`);
  check(modal.shadow.includes("0px 0px 0px"), "an open modal must draw a backdrop");
  await page.mouse.click(5, 5);
  await page.waitForTimeout(250);
  check(await page.evaluate(() => document.querySelector(".krate-dialog-content").getClientRects().length === 0 && document.querySelectorAll("[data-kinert]").length === 0), "clicking outside must close the modal and release the page");

  check(pageErrors.length === 0, `page errors: ${pageErrors.join("; ")}`);
} catch (e) {
  failures.push(`script error: ${e && e.stack ? e.stack : e}`);
} finally {
  if (browser) await browser.close();
  server.close();
}

if (failures.length) {
  console.error(failures.map((f) => "FAIL: " + f).join("\n"));
  process.exit(1);
}
console.log("components keyboard ok");
