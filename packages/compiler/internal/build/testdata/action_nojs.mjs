// Posts a useActionState form with JavaScript disabled and checks the server
// renders the returned validation state into the page.
import { chromium } from "playwright";

const [base] = process.argv.slice(2);
const browser = await chromium.launch();
const result = {};
try {
  const ctx = await browser.newContext({ javaScriptEnabled: false });
  const page = await ctx.newPage();
  await page.goto(base + "/");
  result.initialBanner = await page.locator(".krate-flash").count();
  await page.click("#go");
  await page.waitForLoadState("load");
  result.afterPost = {
    path: new URL(page.url()).pathname,
    banner: await page.locator(".krate-flash").allTextContents(),
    inForm: await page.locator("form .krate-flash").count(),
  };
  const cookies = await ctx.cookies();
  result.flashCookieLeft = cookies.some((c) => c.name === "krate_flash");
  await page.reload();
  result.afterReload = await page.locator(".krate-flash").count();
  await page.fill("#user", "ada");
  await page.click("#go");
  await page.waitForLoadState("load");
  result.afterValid = { path: new URL(page.url()).pathname, page: await page.textContent("#page") };
  // With JavaScript the hydrated form starts from the same state and drops the
  // server-rendered notice.
  const ctx2 = await browser.newContext();
  const page2 = await ctx2.newPage();
  const html = await (await fetch(base + "/")).text();
  const id = /\/__krate\/action\/([^?"'\s>]+)/.exec(html)[1];
  const value = Buffer.from(JSON.stringify({ a: id, r: { error: "user is required" } })).toString("base64url");
  await ctx2.addCookies([{ name: "krate_flash", value, url: base }]);
  await page2.goto(base + "/");
  await page2.waitForSelector("#error", { timeout: 8000 });
  result.hydrated = {
    error: await page2.textContent("#error"),
    notices: await page2.locator(".krate-flash").count(),
  };
  console.log(JSON.stringify(result));
} catch (e) {
  console.log(JSON.stringify({ failed: String(e.message).split("\n")[0], result }));
  process.exitCode = 1;
} finally {
  await browser.close();
}
