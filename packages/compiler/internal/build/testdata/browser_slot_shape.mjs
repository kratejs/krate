// Checks that the server children of client islands sit directly in the
// component's element (see TestBrowserSlotDOMShape): child-combinator CSS
// matches after hydration, after the component renders nodes around the
// slot, and after a hide and show, and a nested island keeps its state.
import { chromium, firefox, webkit } from "playwright";
import { waitHydrated } from "./island_helpers.mjs";

const browserType = { chromium, firefox, webkit }[process.argv[2] || "chromium"];
const base = process.argv[3];
const browser = await browserType.launch({ headless: true });
const failures = [];
try {
  const page = await (await browser.newContext()).newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push("pageerror: " + e.message));
  page.on("console", (m) => {
    if (m.type() === "error") errors.push("console: " + m.text());
  });
  await page.goto(base + "/");
  await waitHydrated(page, "#count");
  await page.waitForFunction(() => (window.__krateIslands || []).filter((i) => i.status === "hydrated").length === 4, null, { timeout: 15000 });
  const state = () => page.evaluate(() => {
    const btn = document.querySelector("#btn");
    const lis = [...document.querySelectorAll("#list > li")];
    return JSON.stringify({
      wrappers: document.querySelectorAll("krate-children").length,
      pad: btn && getComputedStyle(btn).paddingLeft,
      btn: btn && [...btn.children].map((c) => c.localName).join(","),
      text: btn && btn.textContent,
      lis: lis.length,
      gaps: lis.map((li) => getComputedStyle(li).marginBottom).join(","),
      color: lis[0] && getComputedStyle(lis[0]).color,
      count: document.querySelector("#count")?.textContent,
      counts: document.querySelectorAll("#count").length,
    });
  });
  const sorted = (o) => JSON.stringify(Object.fromEntries(Object.entries(o).sort()));
  const expect = async (label, want) => {
    const w = sorted({ wrappers: 0, pad: "16px", color: "rgb(1, 2, 3)", lis: 3, gaps: "8px,8px,0px", counts: 1, ...want });
    let got = "";
    for (let i = 0; i < 50; i++) {
      got = sorted(JSON.parse(await state()));
      if (got === w) return;
      await page.waitForTimeout(100);
    }
    failures.push(`${label}: got ${got}, want ${w}`);
  };
  await expect("after hydration", { btn: "svg", text: " Sign in", count: "count 0" });
  await page.click("#btn");
  await expect("with nodes around the slot", { btn: "b,svg,i", text: "! Sign in*", count: "count 0" });
  await page.click("#btn");
  await expect("after removing them", { btn: "svg", text: " Sign in", count: "count 0" });
  await page.click("#count");
  await expect("after a click in the nested island", { btn: "svg", text: " Sign in", count: "count 1" });
  await page.click("#toggle");
  await page.waitForSelector("#hidden", { timeout: 5000 }).catch(() => failures.push("the list did not hide its children"));
  if (await page.evaluate(() => document.querySelectorAll("li").length) !== 0) failures.push("hidden server children are still in the document");
  await page.click("#toggle");
  await expect("after hide and show", { btn: "svg", text: " Sign in", count: "count 1" });
  await page.click("#count");
  await expect("after hide, show and a click", { btn: "svg", text: " Sign in", count: "count 2" });
  const kids = (sel) => page.evaluate((sel) => [...document.querySelector(sel).children].map((c) => c.id || c.localName).join(","), sel);
  const waitKids = async (label, sel, want) => {
    let got = "";
    for (let i = 0; i < 50; i++) {
      got = await kids(sel);
      if (got === want) return;
      await page.waitForTimeout(100);
    }
    failures.push(`${label}: ${sel} holds ${got}, want ${want}`);
  };
  await waitKids("adjacent slots", "#pair", "in,r");
  await page.focus("#in");
  await page.keyboard.type("hi");
  await page.evaluate(() => document.querySelector("#bump").click());
  await waitKids("adjacent slots after a re-render", "#pair", "in,r,tail");
  await page.evaluate(() => document.querySelector("#bump").click());
  await waitKids("adjacent slots after another re-render", "#pair", "head,in,r,tail");
  const focus = await page.evaluate(() => [document.activeElement && document.activeElement.id, document.querySelector("#in").value].join("|"));
  if (focus !== "in|hi") failures.push("the input in a slot lost focus or its value on a re-render: " + focus);
  await waitKids("a slot after keyed siblings", "#order", "x,y,kid");
  await page.click("#flip");
  await waitKids("a moved slot", "#order", "kid,x,y");
  await page.click("#flip");
  await waitKids("a slot moved back", "#order", "x,y,kid");
  if (errors.length) failures.push(errors.join("\n"));
} finally {
  await browser.close();
}
if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log("ok");
