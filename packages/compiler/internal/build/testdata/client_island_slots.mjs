// Drives a client component that receives server-rendered JSX in props other
// than children: waits for hydration, toggles state and clicks a client
// component nested in a slot, and reports what is on the page as JSON.
import { chromium, firefox, webkit } from "playwright";

const [url, browserName = "chromium"] = process.argv.slice(2);
const browser = await { chromium, firefox, webkit }[browserName].launch();
const errors = [];
try {
  const page = await browser.newPage();
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => {
    if (m.type() === "error") errors.push(m.text());
  });
  await page.goto(url);
  await page.waitForSelector("krate-island[data-krate-island-hydrated]", { timeout: 15000 });
  await page.click("#toggle");
  await page.waitForFunction(() => document.querySelector("#toggle").textContent === "open");
  await page.waitForSelector("#like[data-ready]", { timeout: 15000 }).catch(() => {});
  await page.click("#like");
  await page.waitForFunction(() => document.querySelector("#like").textContent === "likes 1");
  const who = await page.textContent("#who");
  const tools = await page.textContent("#tools");
  const body = await page.textContent("#body");
  console.log(JSON.stringify({ who, tools, body, errors }));
} finally {
  await browser.close();
}
