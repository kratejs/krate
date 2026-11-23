// Collects every id on a page of useId() islands at each step (server HTML,
// hydrated, a portal opened, after router navigations) and reports repeats.
import { chromium, firefox, webkit } from "playwright";
import { waitHydrated } from "./island_helpers.mjs";

const [base, browserName = "chromium"] = process.argv.slice(2);
const browser = await { chromium, firefox, webkit }[browserName].launch();
const errors = [];
try {
  const page = await browser.newPage();
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => {
    if (m.type() === "error" || m.type() === "warning") errors.push(m.text());
  });
  const list = () =>
    page.evaluate(() =>
      [...document.querySelectorAll("[id]")]
        .map((e) => e.id)
        .filter((id) => /^(trigger-|content-)?P/.test(id)),
    );
  const labelIds = () =>
    page.evaluate(() => [...document.querySelectorAll("label[for]")].map((l) => l.htmlFor));
  const all = async () => [...(await list()), ...(await labelIds())].sort();
  const dups = {};
  const check = async (name, v) => {
    const seen = new Set();
    const every = await page.evaluate(() => [...document.querySelectorAll("[id]")].map((e) => e.id));
    for (const id of every) if (seen.has(id)) (dups[name] ||= []).push(id);
    else seen.add(id);
    return v;
  };

  const html = await (await fetch(base + "/s/a/")).text();
  const server = [...html.matchAll(/\s(?:id|for)="((?:trigger-|content-)?P[^"]*)"/g)].map((m) => m[1]);
  await page.goto(base + "/s/a/");
  await waitHydrated(page, ".menu button");
  await page.waitForFunction(() => (window.__krateIslands || []).filter((i) => i.status === "hydrated").length >= 4);
  const hydrated = await check("hydrated", await all());
  await page.click(".menu[data-name=one] button");
  await page.waitForSelector("[role=menu]");
  const opened = await check("opened", await all());
  await page.click(".menu[data-name=one] button");
  await page.waitForSelector("[role=menu]", { state: "detached" });
  await page.click("#to-b");
  await page.waitForSelector("#page-b");
  await page.waitForFunction(() => (window.__krateIslands || []).filter((i) => i.status === "hydrated" && i.connected()).length >= 4);
  const navigated = await check("navigated", await all());
  await page.click(".menu[data-name=three] button");
  await page.waitForSelector("[role=menu]");
  await check("opened-b", await all());
  await page.click(".menu[data-name=three] button");
  await page.click("#to-a");
  await page.waitForSelector("#page-a");
  await page.waitForFunction(() => (window.__krateIslands || []).filter((i) => i.status === "hydrated" && i.connected()).length >= 4);
  const back = await check("back", await all());
  console.log(JSON.stringify({ server: server.sort(), hydrated, opened, navigated, back, dups, errors }));
} finally {
  await browser.close();
}
