// Submits island forms whose server action arrives as a prop: the FormData
// reaches the action, bound arguments survive, and the SPA router leaves the
// form to the island (no GET navigation).
import { chromium, firefox, webkit } from "playwright";

const [base, browserName = "chromium"] = process.argv.slice(2);
const browser = await { chromium, firefox, webkit }[browserName].launch();
const errors = [];
const result = {};
let pageRef;
try {
  const page = (pageRef = await browser.newPage());
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto(base + "/");
  await page.waitForSelector("#plain");
  await page.waitForTimeout(800);
  // The bound value never reaches the browser in the clear; a forged token or
  // one sealed for another action is refused.
  const html = await page.content();
  result.leaked = html.includes("s3cret-tag");
  const urls = [...html.matchAll(/(?:\\u0026|&amp;|&|")?(\/__krate\/action\/[0-9a-f]+\?__krate_bound=k1\.[A-Za-z0-9_%-]+)/g)].map((m) => m[1].replace(/&amp;/g, "&"));
  const post = (u) => page.evaluate(async (u) => {
    const r = await fetch(u, { method: "POST", headers: { "content-type": "application/json", "x-krate-action": "fetch" }, body: JSON.stringify({ args: [null, "x"] }) });
    return r.status;
  }, u);
  result.tokenURLs = urls.length;
  if (urls.length) {
    const u = urls[0];
    const [path, q] = u.split("?");
    result.tampered = await post(path + "?" + q.slice(0, -6) + "AAAAAA");
    const ids = [...new Set([...html.matchAll(/\/__krate\/action\/([0-9a-f]+)/g)].map((m) => m[1]))];
    const otherId = ids.find((id) => !path.endsWith(id));
    result.otherAction = await post("/__krate/action/" + otherId + "?" + q);
  }
  for (const id of ["plain", "bound"]) {
    await page.fill(`form:has(#${id}) input`, "ada-" + id);
    await page.click("#" + id);
    await page.waitForSelector("#" + id + "-status", { timeout: 8000 });
    result[id] = await page.textContent("#" + id + "-status");
  }
  result.url = new URL(page.url()).pathname + new URL(page.url()).search;
  await page.click("#direct");
  await page.waitForSelector("#done", { timeout: 8000 });
  result.direct = new URL(page.url()).pathname + new URL(page.url()).search;
  result.errors = errors;
  console.log(JSON.stringify(result));
} catch (e) {
  console.log(JSON.stringify({ failed: String(e.message).split("\n")[0], result, errors, url: pageRef && pageRef.url() }));
  process.exitCode = 1;
} finally {
  await browser.close();
}
