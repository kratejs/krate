// Drives a client component that uses the React 19 hooks on Preact compat
// (useOptimistic, useActionState, useFormStatus, use) with a real server
// action. Reports the observed texts as JSON.
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
  const text = (sel) => page.textContent(sel);
  const res = { before: await text("#opt"), namedBefore: await text("#named") };
  await page.click("#go");
  await page.waitForFunction(() => document.querySelector("#opt").textContent === "opt 6", null, { timeout: 5000 });
  res.during = await text("#opt");
  await page.waitForFunction(() => document.querySelector("#opt").textContent === "opt 5", null, { timeout: 10000 });
  res.settled = await text("#opt");
  await page.click("#submit");
  await page.waitForFunction(() => document.querySelector("#submit").textContent === "saving", null, { timeout: 5000 });
  res.status = await text("#submit");
  await page.waitForFunction(() => document.querySelector("#submit").textContent === "save", null, { timeout: 10000 });
  res.state = await text("#state");
  res.errors = errors;
  console.log(JSON.stringify(res));
} finally {
  await browser.close();
}
