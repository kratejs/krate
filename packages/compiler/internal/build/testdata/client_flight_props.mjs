// Loads a page whose client component received Date/Map/Set/BigInt props and
// reports what the component saw after hydration (written by its effect).
import { chromium, firefox, webkit } from "playwright";
import { waitHydrated } from "./island_helpers.mjs";

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
  await waitHydrated(page);
  await page.waitForSelector("#client[data-ready]", { timeout: 15000 });
  const client = await page.textContent("#client");
  const ssrLike = await page.textContent("#ssr");
  console.log(JSON.stringify({ client, ssrLike, errors }));
} finally {
  await browser.close();
}
