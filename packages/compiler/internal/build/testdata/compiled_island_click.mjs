// Drives compiled React package components rendered by an async page: a
// Radix checkbox and a Radix dialog (compound components that share context)
// hydrate as islands, and the lucide icon next to them stays plain HTML.
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
  await page.waitForSelector("krate-island[data-krate-island-hydrated] #cb", { timeout: 15000 });
  await page.waitForSelector("krate-island[data-krate-island-hydrated] #open", { timeout: 15000 });
  const iconInIsland = await page.evaluate(() => !!document.querySelector("krate-island svg.lucide"));
  const iconCount = await page.evaluate(() => document.querySelectorAll("svg.lucide").length);
  const before = await page.getAttribute("#cb", "aria-checked");
  await page.click("#cb");
  await page.waitForFunction(() => document.querySelector("#cb").getAttribute("aria-checked") === "true");
  const after = await page.getAttribute("#cb", "aria-checked");
  await page.click("#open");
  await page.waitForSelector("[role=dialog]", { timeout: 5000 });
  const title = await page.textContent("[role=dialog] h2");
  await page.click("#close");
  await page.waitForFunction(() => !document.querySelector("[role=dialog]"));
  console.log(JSON.stringify({ before, after, title, iconInIsland, iconCount, errors }));
} finally {
  await browser.close();
}
